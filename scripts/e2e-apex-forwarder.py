#!/usr/bin/env python3
"""Forward 127.0.0.1:<port> to <upstream-host>:<port>, for wildcard-mode e2e.

Why this exists
---------------
`make test-e2e-wildcard` reaches the stack at the apex (lvh.me) so the frontend
builds real "{port}.lvh.me" links. Every name under lvh.me is a genuine public
DNS record for 127.0.0.1 -- which is the point, and also the problem: 127.0.0.1
is this *test runner's* loopback, while the stack runs in a sibling container
published on the Docker host.

Chromium is told the truth with --host-resolver-rules (see e2e/resolver-args.js).
Playwright's Node side cannot be: `page.request` goes through the operating
system's resolver, and /etc/hosts is root-owned here. So the one port Node
talks to -- the main listener at the apex -- is forwarded from this container's
loopback to where the stack actually is. Per-port pane traffic never comes
through here; that is all chromium, already pinned.

Usage: e2e-apex-forwarder.py <listen-port> <upstream-host> [upstream-port]
"""
import socket
import socketserver
import sys
import threading


class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        try:
            upstream = socket.create_connection((UPSTREAM_HOST, UPSTREAM_PORT), timeout=10)
        except OSError:
            return
        with upstream:
            # Half-close each direction as it ends: the panes stream (SSE, the
            # terminal websocket), so neither side may be torn down because the
            # other went quiet.
            def pump(src, dst):
                try:
                    while True:
                        data = src.recv(65536)
                        if not data:
                            break
                        dst.sendall(data)
                except OSError:
                    pass
                finally:
                    try:
                        dst.shutdown(socket.SHUT_WR)
                    except OSError:
                        pass

            t = threading.Thread(target=pump, args=(self.request, upstream), daemon=True)
            t.start()
            pump(upstream, self.request)
            t.join()


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    LISTEN_PORT = int(sys.argv[1])
    UPSTREAM_HOST = sys.argv[2]
    UPSTREAM_PORT = int(sys.argv[3]) if len(sys.argv) > 3 else LISTEN_PORT
    with Server(("127.0.0.1", LISTEN_PORT), Handler) as srv:
        print(f"apex forwarder: 127.0.0.1:{LISTEN_PORT} -> {UPSTREAM_HOST}:{UPSTREAM_PORT}", flush=True)
        srv.serve_forever()
