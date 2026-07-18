# TODO: per-session network namespaces for multi-tenant browser backends

Follow-up stub from tasks/2026-07-18-agent-view-reverse-tunnel.md (Future
section). Not scheduled.

## Problem

The Agent View reverse tunnel binds session ports on the browser
backend's SHARED loopback:

- Cross-tenant port collisions: first bind wins per port across sessions
  (refusals are loud, but the loser's Agent View pages on that port do
  not load). Two tenants both running `npm run dev` on 3000 cannot both
  have it.
- The peercred guard (/proc peer-pid + ancestry against the session's
  chromium tree) is enforcement-by-identification; a shared loopback
  still means every session's listeners are dialable by every process on
  the box, and the guard is the only thing between them.

Single-tenant deploys (the documented `docker run -p 9333:9333` shape)
are unaffected -- this matters only when one backend serves multiple
mutually-untrusting swe-swe boxes.

## Sketch

Give each session its own network namespace so each chromium gets a
PRIVATE loopback:

- Backend creates a netns per session; Xvfb/chromium/x11vnc/websockify
  run inside it; the tunnel bind manager binds 127.0.0.1:<port> INSIDE
  that netns. Same port number in two sessions -> two distinct sockets.
  Cross-tenant collisions become structurally impossible and the
  peercred guard turns into defense-in-depth instead of the only wall.
- CDP/VNC egress: the allocation API's public CDP/VNC ports must be
  bridged from the root netns into the session netns (veth pair or
  socket-activation-style proxy).
- Needs CAP_NET_ADMIN in the backend container (documented tradeoff;
  keep the flat-loopback mode for unprivileged deploys).
- The sync protocol is UNCHANGED -- only where the binds happen moves
  (tunnelBindManager gains a netns handle per session). Reserved-port
  refusals for the backend's own service/CDP/VNC ports stay, since those
  live in the root netns.

## Acceptance sketch

- Two concurrent sessions both sync port 3000; both get `bound`, and each
  chromium sees ITS swe-swe box's 3000.
- A process in session A's container cannot connect to session B's
  loopback listeners even with the guard disabled.
- Unprivileged backend (no CAP_NET_ADMIN) falls back to today's shared
  loopback with a startup log note.
