//go:build !linux

package main

import (
	"fmt"
	"net"
)

// peerPID is unsupported off Linux: SO_PEERCRED + the /proc ancestry walk the
// broker relies on are Linux-specific. macOS exposes only getpeereid (uid/gid,
// no pid) and has no /proc, so the per-session credential broker needs a
// different identity scheme (per-session socket paths) before it works here.
// Until then the broker fails open exactly as it does on Linux when the socket
// is unavailable -- sessions still start; git credential/signing helpers that
// dial the broker simply do not resolve. Tracked in the dockerless plan
// (Phase 6, Mac-native).
func peerPID(c *net.UnixConn) (int, error) {
	return 0, fmt.Errorf("peer-credential broker unsupported on this platform (Phase 6)")
}
