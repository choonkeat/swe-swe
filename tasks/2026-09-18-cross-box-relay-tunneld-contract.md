# Contract: box-to-box relay in swe-swe-tunnel (for the swe-swe-tunnel agent)

Requested by swe-swe on 2026-09-18. Full context and the swe-swe side of the
work: /workspace/tasks/2026-09-18-cross-box-session-comms.md (swe-swe repo).
This file is the interface swe-swe will code against. Internals are yours.

## What swe-swe needs

Box A's swe-swe server must be able to send ONE HTTP request to box B's
swe-swe server through tunneld, using the connection each box already holds,
with tunneld vouching for who sent it. Tunneld stays STATELESS: forward, or
answer "unreachable". No queues, no storage, no history.

## Client side (`swe-swe-tunnel` binary, runs inside each box)

1. New flag `--relay-listen 127.0.0.1:0` (env SWE_TUNNEL_RELAY_LISTEN; empty =
   off). A plain HTTP listener on loopback.
2. Every request received there is forwarded to tunneld over the EXISTING
   yamux session as a client-initiated stream, unchanged except that the
   client requires header `X-Swe-Relay-To: <target unique>` and rejects
   requests without it (400) before opening a stream.
3. When `--report-format jsonl` is on, emit once after bind:
   `{"event":"relay_listen","addr":"127.0.0.1:NNNNN"}`. swe-swe-server's
   supervisor reads this to learn where to send.
4. Relay listener death or tunnel reconnect: rebind and re-emit the event.
   Same reconnect discipline as the main tunnel.

## Server side (`swe-swe-tunneld`)

1. Accept client-initiated streams on each registered session (today tunneld
   only opens streams INTO clients). Each stream carries one HTTP request.
2. New flag `--relay off|all|owner` (default `off`).
   - `off`: reject every relay stream (403).
   - `all`: any registered box may relay to any other registered box.
   - `owner`: only between boxes whose pubkeys share an owner. Owner
     grouping in the allowlist is your design; ship `all` first if `owner`
     needs allowlist format changes.
3. Per relay request:
   - Caller identity = the registered unique of the yamux session the stream
     arrived on. Never read it from a header.
   - STRIP any client-supplied `X-Swe-Relay-From`; SET
     `X-Swe-Relay-From: <caller unique>`.
   - Target = `X-Swe-Relay-To`. Not registered / not connected -> respond
     `503` with body exactly `unreachable: <unique> not connected`. Policy
     denial -> `403`.
   - Rewrite `Host` to `<relayPort>.<target unique>.<apex>` and hand the
     request to the existing `route()` path, with the port policy bypassed
     for relayPort ONLY on this code path. swe-swe-server binds the relay
     port on the target box's loopback. `relayPort` comes from a new
     tunneld flag `--relay-port N` (default agreed with swe-swe; see plan
     "Open decisions" 2).
4. Public ingress hardening (this is what makes the trust model hold):
   - ALWAYS strip `X-Swe-Relay-From` from requests arriving on the public
     listener, before routing.
   - The public port policy continues to reject relayPort (it is not in the
     allowed set), so no internet request can reach the relay listener.
5. Logging: one line per relay with caller, target, status, duration. No
   bodies.

## Tests swe-swe will rely on

1. e2e: two clients (uniques `a`, `b`) + one tunneld; a request on `a`'s
   relay listener with `X-Swe-Relay-To: b` reaches a stub server on `b`'s
   loopback relayPort with `X-Swe-Relay-From: a` and a rewritten Host.
2. `b` disconnected -> `503 unreachable: b not connected` within 2s.
3. Public request to `https://<relayPort>.b.<apex>/` -> rejected by port
   policy. Public request carrying `X-Swe-Relay-From` -> header absent when it
   reaches the box.
4. `--relay off` -> 403.

## Not requested

- No message storage, retries, or fan-out in tunneld.
- No DNS changes: relay needs only the tunneld connection, not per-unique
  DNS records.
- No change to the existing public ingress behaviour beyond the header strip.
