<!-- agent-chat export
title: Preview Reach Ports First Doc
date: 2026-07-16
index: 01
slug: preview-reach-ports-first-doc
agent: claude
version: 0.8.10 (14f2f8d)
-->

# Preview Reach Ports First Doc

_2026-07-16 · 01 · claude · agent-chat 0.8.10 (14f2f8d)_

**USER**

> There was a worktree or here where we discussed and created a file docs/preview-reach-options.html

<small>took 49.9s</small><br>
**AGENT**

> Found it — `docs/preview-reach-options.html` (14.8 KB) exists in the main `/workspace` checkout right now, committed but **not yet pushed**.
>
> - Content: the "App Preview: reach options" comparison page — addressing forms (localhost path proxy, port-based, bare-port demux, named vhost demux) + reach mechanisms (lvh.me, sslip.io, admin wildcard, pinned, tunnel, Agent View remap), plus a quick-decision guide.
> - Two commits on local `main`, both ahead of `origin/main`:
>   - `62848ab70` docs(preview-vhost): reach-options comparison table
>   - `b91078b44` docs(preview-vhost): reflect wildcard+password auth-cookie fix (option A shipped)
> - No separate worktree exists anymore (`git worktree list` shows only `/workspace [main]`), so whatever branch it started in has already landed on main.
>
> What would you like to do with it — push it, keep editing the page, or something else?

[Quick replies]
- Open it in Preview so I can look
- Push main
- I want to edit it
- Just needed to find it, thanks

**USER**

> Discuss it

<small>took 17.6s</small><br>
**AGENT**

> Read the whole page. My take:
>
> **What works well**
> - The "two axes" split (addressing forms = *how you name a target*, rows 1–4; reach mechanisms = *how the browser's DNS finds swe-swe*, rows 5–10) is the key insight. Most people conflate these, and the framing "you pick one form × one reach" is exactly right. The note box up top earns its place.
> - The auth-cookie column is the sharpest part — it's the thing that actually bites people (host-only cookie → 401 on a wildcard sibling), and option A being marked shipped with the `Domain=<reach>` explanation is the real payoff.
>
> **Where I'd push back / questions for you**
> 1. **The two axes leak into each other.** "Bare-port demux" and "named vhost demux" (forms) each *require* a wildcard reach, and "Tunnel mode" and "Pinned mode" (reaches) actually dictate the addressing form too. So the clean "pick 1×1" story has real coupling the tables don't show. Worth a validity matrix (which forms work with which reaches)?
> 2. **Pinned mode is arguably a form, not a reach.** It's "no wildcard, one bare origin, re-pin to switch" — that's an addressing constraint more than a DNS mechanism. Its placement in the reach table is a bit of a stretch.
> 3. **The `<chip no>` for sslip.io + password** is the one genuine footgun, and the fix (set `SWE_PREVIEW_REACH_DOMAIN` to the sslip domain) is buried in the note. Could be promoted.
> 4. **Audience?** Is this for end users deciding how to expose a preview, or for us documenting the design space? The quick-decision guide says "users," but the ADR cross-refs and env-var density say "us." It's trying to be both.
>
> Which thread do you want to pull on — the coupling/validity-matrix gap, the pinned-mode taxonomy, the audience question, or something you already had in mind?

[Quick replies]
- The coupling / validity matrix
- Pinned-mode taxonomy
- The audience question
- I have my own angle

**USER**

> My constraints are: swe-swe could be hosted on a server without control over dynamic dns, eg a fixed hostname like my-swe-swe.example.com or localhost. So different ports is how we reach different services of different sessions. We could fake different subdomains in the user’s Preview tab url, eg myapp.lvh.me:3456 but iframe doesn’t actually go there; just my-swe-swe.example.com:3456 but request Host header is somehow myapp.lvh.me:3456 or something— possible? How hacky?

<small>took 53.6s</small><br>
**AGENT**

> Short answer: **fully possible, and not hacky at all — because the Host rewrite happens server-side, not in the browser. And it's already built (that's "pinned mode" in the doc).**
>
> Split your idea into two halves — one is impossible, the other is trivial:
>
> **1. "Fake `myapp.lvh.me:3456` in the iframe URL"** — ❌ dead end. The iframe navigates to whatever's in `src`. If that's `myapp.lvh.me`, the *user's* browser resolves it → `127.0.0.1` = the user's own laptop, not your server. No wildcard DNS = the browser can't be pointed at a fake subdomain. So the subdomain in the URL the browser sees is off the table.
>
> **2. "Browser goes to `my-swe-swe.example.com:3456`, but the upstream request Host is `myapp.lvh.me`"** — ✅ 100% standard. The browser connects to your real fixed host+port. swe-swe (the reverse proxy) receives it and rewrites `Host:` to *anything* before forwarding to `127.0.0.1:<app port>`. This is bread-and-butter reverse-proxy behavior (`proxy_set_header Host …`). Zero hackiness. Your app sees `Host: myapp.lvh.me:3456` even though no such name ever resolved anywhere.
>
> **So the real design collapses to:** the **port is your demux key** (browser can vary it against a fixed hostname), and the **Host header is a free variable you fabricate server-side**. The subdomain is cosmetic — you don't need it in the URL, only in the upstream Host, and that you fully control.
>
> **This is exactly the doc's "Pinned mode" row:** single bare origin `<host>:<proxyPort>`, `POST …/vhost-pin {name,port}`, then label-less requests get routed to that port *with the Host rewrite*. It exists precisely for your "fixed hostname, no wildcard DNS" case.
>
> **Three bonuses for your constraint:**
> - **Auth cookie just works.** Browser never leaves `my-swe-swe.example.com` (only the port changes; cookies are port-agnostic on the same host), so the login cookie is sent. None of the cross-host / sslip.io 401 pain applies to you.
> - **Apps that key off Host but emit relative URLs** (most reverse-proxy-aware stacks, traefik/nginx vhosts) work perfectly — they get the Host they expect.
> - **localhost dev** is the same story with `localhost:3456`.
>
> **The one real limitation:** if a *single session* runs several services that must be told apart by Host name alone (e.g. an in-app traefik routing `myapp.lvh.me` vs `api.lvh.me` on the *same* port), then per fixed-hostname deployment you have two choices — give each service its **own port** (clean, works today, browser varies the port), or **one port + re-pin** to switch (pinned mode, one at a time, shared cookie jar). You can't carry two vhost names on one port through the browser without wildcard DNS.
>
> Want me to sketch what a "fixed-host, port-per-service, Host-rewrite" mode would look like end to end (or map it onto what pinned mode already does), and note it in the doc?

[Quick replies]
- Sketch the fixed-host port-per-service mode
- How does pinned mode pick the Host name today?
- What breaks with absolute URLs in the app?
- Add this framing to the doc

**USER**

> Our Procfile service name “app123” can actually be namespaced “app123.{session}.lvh.me underlying though merely shown as “app@23” in Preview tab 😭 still coherent?

<small>took 37.0s</small><br>
**AGENT**

> Coherent — *as long as the fabricated Host stays strictly server-internal.* The trick is that you actually have **three independent layers**, and they don't have to agree with each other:
>
> | Layer | Value | Who consumes it | Must be unique per |
> |---|---|---|---|
> | **UI label** | `app123` (Procfile name) | human, Preview tab | nothing — cosmetic |
> | **Browser URL** | `my-swe-swe.example.com:<port>` | the browser's DNS + your proxy's demux | `(session, service)` → so allocate the **port** per (session, service) |
> | **Upstream Host** | `app123.{session}.lvh.me` | the app / its in-container router only | free string, you fabricate it |
>
> Because those are decoupled, "shown as `app123`, routed by port, addressed upstream as `app123.{session}.lvh.me`" is a totally consistent story. The Host is just a private contract between your proxy and the app's router. The browser never sees it, so it can be as rich as you like (service + session both encoded) for disambiguation.
>
> **The one rule that keeps it coherent (and the moment it turns hacky):** the fabricated Host must **never escape back into anything the browser acts on.** Three specific leaks to watch:
> - **`Location:` redirect headers** — if the app 302s to `http://app123.{session}.lvh.me/foo`, the browser tries to resolve that → user's own loopback → dead. You'd have to rewrite `Location` back to `host:port`.
> - **`Set-Cookie; Domain=app123.{session}.lvh.me`** — browser is on `my-swe-swe.example.com`, so it silently drops that cookie. App sessions/CSRF break with no error.
> - **Absolute asset/link URLs** baked into HTML using that Host — same resolve failure.
>
> If the app emits **relative** URLs and host-agnostic cookies (most well-behaved apps behind a proxy do), you never touch any of that and it's clean. The instant an app hard-codes its own Host into responses, you're into response-body/header rewriting — *that's* the hacky tier, and it's the same tier `lvh.me`/wildcard modes were invented to avoid.
>
> So: encode `{session}` in the upstream Host if it helps your in-container router; keep the port as the real demux key; show the friendly `app123`. Coherent. Just budget for "does this app leak its Host?" as the compatibility question per app.
>
> Want me to write this up as a short "fixed-host / port-per-service, Host-fabrication" section in `preview-reach-options.html` — including the three-layer table and the leak checklist?

[Quick replies]
- Yes, add it to the doc
- How would we rewrite Location/cookies if an app leaks?
- Is session-in-Host redundant if each session has its own loopback?
- Just discussing — don't edit yet

**USER**

> Or ignore the hostnames (on server side ) it’s always just unique ports

<small>took 34.8s</small><br>
**AGENT**

> Yes — and "always just unique ports, ignore Host" is arguably the *best* default, not a compromise. It's the doc's "port-based proxy" row scaled to N services, and it deletes the whole leak checklist I just gave you:
>
> **Why it's actually more coherent than faking Hosts:** if you *don't* rewrite the Host, the app sees `Host: my-swe-swe.example.com:3456` — the same origin the browser is on. So any absolute URL, `Location:` redirect, or `Set-Cookie` the app emits references a host:port the **browser can actually reach**. The proxy becomes fully transparent. All three leak traps (redirects, cookies, absolute links) evaporate because there's no fake name to leak. Port = identity, end to end.
>
> So the model is dead simple:
> - **Port** = the unique `(session, service)` key. Browser varies it against the fixed hostname.
> - **Host** = passed through untouched (`my-swe-swe.example.com:<port>`). No fabrication, no table.
> - **UI label** = `app123` from the Procfile. Purely cosmetic.
>
> **Two costs to name, so you go in eyes-open:**
>
> 1. **Apps that *require* a specific vhost Host to route internally** (an in-container traefik/nginx that only answers to `myapp.lvh.me` and 404s otherwise) won't work with pass-through Host. Those are the *minority* — for them you fabricate a Host as the **exception**, and eat the leak checklist only there. So Host-fabrication stays in the toolbox as a per-app fallback, not the default.
>
> 2. **You must be able to expose/allocate many ports** on `my-swe-swe.example.com`. Fine on a plain VPS/localhost. But if your ingress only gives you **one port** — 443-only PaaS like Fly/Railway — you *can't* open arbitrary ports, and port-per-service is impossible. That's the exact case where you're forced back to single-port **path proxy** (`/proxy/{uuid}/preview/`) or single-port Host/label demux.
>
> **Net:** port-per-service, Host-passthrough is the right default *whenever you control the port surface*. Keep (a) Host-fabrication for vhost-router apps and (b) single-port path/label demux for port-constrained platforms as the two documented fallbacks. That's a clean three-tier story the doc could state outright — right now it buries the port-only option among ten rows instead of leading with it.
>
> Want me to restructure the doc around that "ports first, fabricate Host only when forced, single-port demux only when you can't open ports" spine?

[Quick replies]
- Yes, restructure around the ports-first spine
- Does swe-swe already allocate a port per Procfile service?
- How do we detect a vhost-router app that needs the fallback?
- Just discussing — leave the doc

**USER**

> So we can still have conflicting service names across sessions it’s ok since ports are different right?

<small>took 14.4s</small><br>
**AGENT**

> Correct. ✅ The service **name is just a label** — it carries zero routing weight in the port-only model. Two sessions can each have a service called `web` (or `app123`) and they never collide, because the browser reaches them at **different ports**, and the port is the only thing the proxy keys on.
>
> The one invariant you must hold: **ports are unique across the whole swe-swe instance, not reused per session.** As long as the allocator hands out a globally distinct port per `(session, service)` — no two live services ever share a port — name collisions are a non-issue by construction. (swe-swe's existing `proxyPort = PreviewPort + 20000` per-session scheme already gives you that, assuming PreviewPorts themselves are allocated uniquely per session.)
>
> Two edge cases worth stating so nobody trips later:
> - **Same name twice *within one session*** — that *would* collide, but a Procfile shouldn't have two services with the same name anyway, so it's a validation concern, not a routing one.
> - **Port reuse after a session ends** — fine, as long as you don't recycle a port while its old session is still up. Standard "free on teardown" allocator behavior.
>
> So the coherent one-liner for the doc: *service names are display-only labels scoped to their session; the (globally-unique) port is the sole identity the proxy routes on.* Names may repeat freely across sessions.
>
> Still want me to fold the ports-first framing (plus this "names are labels, ports are identity" line) into the doc?

[Quick replies]
- Yes, fold it into the doc
- Confirm swe-swe allocates ports globally-unique today
- Just discussing — leave the doc for now

**USER**

> Yes fold into doc. Btw Procfile managed service names should never conflict in the same session; refuse to boot right?

<small>took 1m 1s</small><br>
**AGENT**

> Folded it in. Two things:
>
> **Your Procfile question — confirmed in code.** `cmd/swe-run/procfile.go:64` keeps a `seen` map and returns `"line N: duplicate service name %q"` on any repeat, so `swe-run` refuses to boot a session whose Procfile has two services with the same name. (Same parse pass also rejects invalid names, empty commands, missing `:`, and an empty Procfile.) So intra-session name uniqueness is guaranteed; cross-session repeats are fine because ports differ.
>
> **Doc changes** — added a new **"Recommended spine: ports first"** section right after the two-axes note, so the port-only model leads instead of being buried among the ten rows. It contains:
> - The core claim: fixed hostname + unique-port-per-service is the only identity the proxy needs — no wildcard DNS, no fake subdomains, no Host rewrite.
> - A "why port-only is the most robust default" callout (transparent proxy → no leaks; same-host so password cookies just work).
> - The **three-decoupled-layers** table (UI label / browser URL / upstream Host) with the "names are labels, ports are identity; duplicates rejected at parse time" consequence line.
> - The **three tiers**: default (port + Host passthrough) → fallback A (fabricate Host, only for vhost-router apps) → fallback B (single-port path demux, only on 443-only PaaS).
> - The **leak checklist** (Location / Set-Cookie Domain / absolute URLs), explicitly scoped to "only when you fabricate a Host."
>
> It's a docs-only edit (`docs/preview-reach-options.html`), not a template, so no golden-update needed. Want me to open it in Preview so you can eyeball the rendering, and/or commit it?

[Quick replies]
- Open it in Preview
- Commit it
- Both — preview then commit
- Let me read the diff first

**USER**

> Both — preview then commit

<small>took 376m 1s</small><br>
**AGENT**

> All done — the ports-first section is written, previewed (screenshot above), and committed as `5bbfaa918` (docs file only). Still local / unpushed.
>
> Just let me know: push `main`, or leave it local? No rush.

[Quick replies]
- Leave it local
- Push main
- We're done, thanks

<small>took 51ms</small><br>
**AGENT**

> Previewed and committed. ✅
>
> **Commit** `5bbfaa918` — `docs(preview-vhost): lead with ports-first spine (unique port = identity)`, 1 file changed, +64 lines, `docs/preview-reach-options.html` only (explicit path, so none of the pre-existing golden-file changes in the tree rode along).
>
> The screenshot above is the rendered page — the new **"Recommended spine: ports first"** section now sits right after the intro, ahead of the ten-row tables: the port-only claim, the "why it's the most robust default" callout, the three-decoupled-layers table, the three tiers, and the fabricate-Host leak checklist. Reads coherently top to bottom.
>
> Note: earlier my first commit attempt died with exit 144 — the `pkill -f "http.server 8899"` self-matched the shell running it and killed itself before committing. Harmless (the server did stop), and the redo went through fine. Flagging so it's not mysterious in the logs.
>
> It's committed but **not pushed** (you only asked to commit; also `main` here has other unpushed work). Want me to push, or leave it local?

[Quick replies]
- Leave it local
- Push main
- We're done, thanks

**USER**

> Push main

**USER**

> export chat logs and git commit

