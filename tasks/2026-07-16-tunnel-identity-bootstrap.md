# Tunnel identity bootstrap: generate-print-exit, and require unique

Date: 2026-07-16
Repos touched: `/repos/swe-swe-tunnel` (client) and `/workspace` (swe-swe-server + init scaffolder)

## Problem

When swe-swe runs in tunnel mode without a pre-provisioned identity key, the
tunnel client (`swe-swe-tunnel`) today **silently auto-generates** an Ed25519
key at `~/.swe-swe-tunnel/identity.key` and then **immediately connects and
registers** with a brand-new pubkey the tunnel server has never allowlisted.
That registration is rejected but still consumes a per-pubkey / per-IP
registration attempt (the server rate-limits these, ~5/hr per IP). The human
never sees the pubkey they need to get allowlisted, so they burn attempts
blindly.

Separately, there is a related bug: swe-swe-server's supervisor spawns the
tunnel child whenever `--tunnel-server-url` is set, **even if the unique name
is empty**. The child then exits 2 (`--unique` required), and the supervisor
restart-loops forever with backoff.

## Desired behavior

1. **First boot, no key:** the client generates the keypair, prints the
   generated **public key** (in the allowlist/wire format) and the **full path**
   of the keypair, emits a terminal `fatal` event so the supervisor stops
   (no restart loop), and exits 1. Zero registration attempts burned.
2. **Human step:** operator copies the printed pubkey, gets it allowlisted on
   the tunnel server.
3. **Next boot:** key now exists on disk, client connects and registers
   normally.
4. **Missing unique:** if tunnel mode is enabled but no unique name is
   provided, fail fast with a clear error before spawning / connecting
   (server side + init scaffold guard), instead of exit-2 restart-looping.

The generate-print-exit must **not** fire for the inline
`SWE_TUNNEL_IDENTITY_KEY` env case (base64 PKCS8 supplied directly), which
never touches disk.

## Current-state references

### Client (`/repos/swe-swe-tunnel/workspace`)
- `cmd/swe-swe-tunnel/main.go:26-121` — main flow. Identity resolsolution
  resolution precedence (`--identity-key` flag -> `SWE_TUNNEL_KEY` env -> default path) at
  `main.go:56-64`. `--server`/`--unique` required, else exit 2 at
  `main.go:71-75`. `LoadIdentity` at `main.go:83`. `Run` (connect/register) at
  `main.go:106`. Nothing between 83 and 106 touches the network -> clean seam.
- `internal/tunnelclient/identity.go:45` `LoadIdentity(filePath, logger)`:
  - `IdentityKeyEnv = "SWE_TUNNEL_IDENTITY_KEY"` inline branch returns early at
    ~`identity.go:49-56` WITHOUT reading/writing the file. (exit-1 logic must
    not fire here.)
  - Falls through to `LoadOrCreateIdentity(path, logger)` at `identity.go:117`.
- `internal/tunnelclient/identity.go:117-158` `LoadOrCreateIdentity`: on
  `os.ErrNotExist` it `MkdirAll(0700)`, `ed25519.GenerateKey`, writes PKCS8 PEM
  at 0600, logs `"generated new identity key" path=...` (no pubkey), and
  **returns the key so main() keeps going**. This is the path to intercept.
- `internal/tunnelclient/client.go:434-446` — where the pubkey is put on the
  wire: `base64.RawStdEncoding.EncodeToString(pub)`. Our printed pubkey MUST
  match this encoding to be paste-compatible with the allowlist.
- `internal/tunnelclient/events.go:31` `EventFatal = "fatal"`;
  `events.go:85-88` `FatalData{ Message string; ExitCode int }`.
  main.go already emits `EventFatal` on identity/TLS errors
  (`main.go:~86-91`, `~97-103`).
- `README.md:95` documents the human openssl recipe for deriving the pubkey
  (`openssl pkey -in identity.key -pubout -outform DER | tail -c 32 | base64
  -w0 | tr -d '='`) -- same raw-std format.

### Server + scaffolder (`/workspace`)
- `cmd/swe-swe/templates/host/swe-swe-server/main.go:1961-1971` — tunnel flags
  (`--tunnel-server-url`, `--tunnel-unique`, etc.).
- `main.go:2069-2092` — resolve URL/unique from flags+env; at `main.go:2085`
  the supervisor is spawned **whenever `resolvedTunnelServerURL != ""`**, with
  no check that `resolvedTunnelUnique != ""`. <-- the require-unique gap.
- `cmd/swe-swe/templates/host/swe-swe-server/tunnel_supervisor.go`:
  - `fatalReason *atomic.Pointer[string]` (`:83-90`): outer loop stops instead
    of restarting when a `fatal` event is seen. Perfect for our generate case.
  - `applyEvent` `case "fatal"` (`:434-453`): unmarshals `{ "reason": ... }`
    from the event data; empty -> `"unspecified"`; stores into `fatalReason`.
    **NOTE MISMATCH:** the client's `FatalData` has `Message`/`ExitCode`, NOT
    `Reason`. So today a client `fatal` event surfaces as reason
    `"unspecified"` (the loop still stops correctly, but the reason string is
    useless). To surface a clean `identity_generated` reason we should add a
    `Reason` field to the client's `FatalData` (see step 2).
- `cmd/swe-swe/init.go` (scaffolder) — `--tunnel-client-cert` mirror at
  `init.go:645`; tunnel config persisted (`TunnelClientCert` etc.). Check
  whether a `--tunnel-unique` scaffold flag exists; add the init-time guard
  here.

## Plan

### Step 1 - client: signal "just generated" out of the identity loader
Make the generate case distinguishable from the load case without changing the
happy path.
- Option A (preferred): add `LoadOrCreateIdentityStatus` / change
  `LoadOrCreateIdentity` to also return a `generated bool`, and thread it up
  through `LoadIdentity` (new return `(priv, generated bool, err error)` or a
  small struct). Inline-env branch returns `generated=false`.
- Option B (less invasive): in `main()`, `os.Stat(*identityKey)` BEFORE calling
  `LoadIdentity`; if it does not exist AND the inline env var is unset, treat
  the subsequent successful load as "generated". (Weaker: TOCTOU, and duplicates
  the env-precedence logic. Prefer A.)

### Step 2 - client: generate-print-fatal-exit in main()
When `generated == true` (file path branch only):
- Derive `pub := priv.Public().(ed25519.PublicKey)`.
- Print to stderr (human-facing), clearly:
  - the pubkey as `base64.RawStdEncoding.EncodeToString(pub)`
  - the full keypair path (`*identityKey`)
  - a one-line instruction ("allowlist this pubkey, then start again").
- Emit `EventFatal` with `FatalData{ Reason: "identity_generated", Message:
  "generated new identity <path>; allowlist pubkey <b64> then restart",
  ExitCode: 1 }`.
  - Add `Reason string \`json:"reason"\`` to `FatalData` in `events.go` so the
    supervisor's `case "fatal"` surfaces it (otherwise "unspecified").
- `os.Exit(1)`.
- Do this BEFORE `Run` (`main.go:106`) so no network call happens.
- Guard: skip entirely when `SWE_TUNNEL_IDENTITY_KEY` inline env is set.

### Step 3 - server: require unique when tunnel enabled (fast-fail)
In `swe-swe-server/main.go` around `:2085`:
- If `resolvedTunnelServerURL != "" && resolvedTunnelUnique == ""`: log a clear
  fatal error and refuse to start the tunnel supervisor (either exit non-zero,
  or skip spawning and surface a fatal tunnel status). Decide: hard-exit the
  whole server vs. leave the rest of the server running with tunnel disabled +
  a visible error. **Recommendation:** don't spawn the child; set a fatal
  tunnel status (`reason: "unique_required"`) and log loudly, so the rest of
  swe-swe still runs. Confirm preference with user.

### Step 4 - init scaffolder guard -- NOT APPLICABLE
`cmd/swe-swe/init.go` has `--tunnel-server-url` but NO `--tunnel-unique`
flag: the unique is supplied at runtime via `SWE_TUNNEL_UNIQUE`, not baked in
at scaffold time. So init cannot guard a value it never sees; the runtime
fast-fail (Step 3) is the correct and only enforcement point. (The server
template edits still triggered `make build golden-update` because the golden
fixtures embed a copy of the swe-swe-server source.)

### Step 5 - tests
- Client: unit test `LoadIdentity`/`LoadOrCreateIdentity` returns
  `generated=true` on fresh path, `false` on existing file and on inline-env.
  Test that main's generate path prints pubkey (raw-std) + path and exits 1
  without connecting (may need a small seam / table test around the
  print+emit function).
- Server: supervisor/opts test that empty unique + non-empty URL does not spawn
  and reports fatal `unique_required`.
- `FatalData.Reason` round-trips through the supervisor `applyEvent` "fatal"
  case to `fatalReason`.
- Run `make test` (per CLAUDE.md, not `go test` directly) in each repo.

### Step 6 - docs
- Update `/repos/swe-swe-tunnel/README.md` and `/workspace/docs/tunnel-*.md`
  (tunnel-explained / tunnel-fly / tunnel-laptop / tunnel-paas) to describe the
  new first-boot generate-print-exit bootstrap, replacing / supplementing the
  manual openssl recipe. Note the "generated -> allowlist -> restart" loop and
  that inline `SWE_TUNNEL_IDENTITY_KEY` bypasses it.

## Open questions for the user
1. Step 3 behavior: hard-exit the whole swe-swe-server on missing unique, or
   keep the server running with tunnel disabled + a loud fatal status?
   (Recommendation: keep running, fatal tunnel status.)
2. Print destination: stderr only, or also emit the pubkey inside the JSONL
   `fatal` event's Message/Reason so a supervising UI can display it? (Leaning:
   both -- human-readable stderr + machine-readable in the event.)
3. Should `swe-swe init` hard-refuse tunnel-without-unique, or just warn?
4. Do we want a `--print-pubkey` convenience flag on the client too (derive +
   print pubkey for an existing key without connecting)? Nice-to-have, not
   required for this feature.

## Sequencing / commits
- Commit A (client): FatalData.Reason field + identity loader `generated`
  signal + main generate-print-exit + client tests.
- Commit B (server): require-unique fast-fail + supervisor test.
- Commit C (scaffolder): init guard + golden-update.
- Commit D (docs).
Client repo and workspace repo are separate git repos -> separate commits/PRs.
