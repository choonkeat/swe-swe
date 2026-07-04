// broker.go -- per-session credential broker (Option B PoC).
//
// Listens on the abstract-namespace unix socket @swe-swe-broker. On each
// accepted connection it identifies the calling process via SO_PEERCRED,
// walks /proc/<pid>/status PPid chain until it hits a known session-shell
// pid (registered via registerSessionPid), and replies with a JSON echo
// that carries the session UUID. PoC scope: no credential lookup, no
// credential protocol -- just measures whether kernel peer-credentials
// plus an ancestry walk identify the caller correctly through the actual
// session spawn pipeline (server -> script -> bash -> grandchildren).
//
// Fail-open: if the listener cannot bind, log and continue. Sessions still
// start; they just cannot reach the broker. See research/2026-04-25-per-
// session-git-credentials.md (addendum) for the full design.
package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const brokerSocketName = "@swe-swe-broker"

var (
	pidToSid   = map[int]string{}
	pidToSidMu sync.RWMutex
)

func registerSessionPid(pid int, sid string) {
	if pid <= 0 || sid == "" {
		return
	}
	pidToSidMu.Lock()
	pidToSid[pid] = sid
	pidToSidMu.Unlock()
}

func unregisterSessionPid(pid int) {
	if pid <= 0 {
		return
	}
	pidToSidMu.Lock()
	delete(pidToSid, pid)
	pidToSidMu.Unlock()
}

// findSessionForPID walks the PPid chain from pid upward until it hits an
// entry in pidToSid, or pid 1, or a fixed step limit. Returns "" if no
// ancestor was registered as a session shell.
func findSessionForPID(pid int) string {
	for steps := 0; pid > 1 && steps < 32; steps++ {
		pidToSidMu.RLock()
		sid, ok := pidToSid[pid]
		pidToSidMu.RUnlock()
		if ok {
			return sid
		}
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
		if err != nil {
			return ""
		}
		next := parsePPid(data)
		if next == 0 || next == pid {
			return ""
		}
		pid = next
	}
	return ""
}

// startBrokerListener opens @swe-swe-broker and serves accept-loop in a
// goroutine. Fail-open: a Listen error logs and returns; sessions still
// work without a reachable broker.
func startBrokerListener() {
	// The broker identifies callers via SO_PEERCRED + a /proc ancestry walk,
	// both Linux-specific (see peercred_*.go). Off Linux it cannot resolve a
	// caller to a session, and brokerSocketName ("@...") is not an abstract
	// socket there but a literal filename -- so disable it cleanly rather than
	// litter the workspace with a stray socket file. Clients fail open exactly
	// as they do on Linux when the broker is unreachable (git falls back to its
	// normal credential flow). Full macOS support needs per-session sockets
	// (Phase 6, tracked in the dockerless plan).
	if runtime.GOOS != "linux" {
		log.Printf("[BROKER] disabled on %s -- per-session credential broker is Linux-only for now (Phase 6)", runtime.GOOS)
		return
	}
	addr := &net.UnixAddr{Name: brokerSocketName, Net: "unix"}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		log.Printf("[BROKER] listen on %s failed: %v (continuing without broker)", brokerSocketName, err)
		return
	}
	log.Printf("[BROKER] listening on %s", brokerSocketName)
	go func() {
		defer recoverGoroutine("broker accept loop")
		for {
			c, err := l.AcceptUnix()
			if err != nil {
				log.Printf("[BROKER] accept failed: %v", err)
				return
			}
			go handleBrokerConn(c)
		}
	}()
}

// handleBrokerConn is the PoC handler. SO_PEERCRED -> ancestry walk -> JSON
// echo with sid. Real credential logic lands in v1.
func handleBrokerConn(c *net.UnixConn) {
	defer c.Close()
	defer recoverGoroutine("broker conn handler")

	pid, err := peerPID(c)
	if err != nil {
		log.Printf("[BROKER] SO_PEERCRED failed: %v", err)
		brokerWriteJSON(c, map[string]any{"error": "peer credentials unavailable"})
		return
	}

	sid := findSessionForPID(pid)
	if sid == "" {
		log.Printf("[BROKER] no session ancestor for pid=%d", pid)
		brokerWriteJSON(c, map[string]any{"error": "unknown session", "peerPid": pid})
		return
	}

	var req map[string]any
	if err := json.NewDecoder(c).Decode(&req); err != nil {
		log.Printf("[BROKER] decode request failed for sid=%s pid=%d: %v", sid, pid, err)
		return
	}

	op, _ := req["op"].(string)
	switch op {
	case "get":
		host, _ := req["host"].(string)
		if host == "" {
			brokerWriteJSON(c, map[string]any{"error": "missing host"})
			return
		}
		cred, ok := getCredential(sid, host)
		if !ok {
			log.Printf("[BROKER] sid=%s no credential for host=%q (peerPid=%d)", sid, host, pid)
			brokerWriteJSON(c, map[string]any{"error": "no credential for host", "host": host})
			return
		}
		username := cred.Username
		if username == "" {
			username = "x-access-token"
		}
		log.Printf("[BROKER] sid=%s served credential for host=%q (peerPid=%d, user=%s)", sid, host, pid, username)
		brokerWriteJSON(c, map[string]any{
			"username": username,
			"password": cred.Token,
		})
	case "sign-ssh":
		key, ok := getSigningKey(sid)
		if !ok {
			log.Printf("[BROKER] sid=%s no signing key (peerPid=%d)", sid, pid)
			brokerWriteJSON(c, map[string]any{"error": "no signing key for session"})
			return
		}
		namespace, _ := req["namespace"].(string)
		if namespace == "" {
			namespace = "git"
		}
		dataB64, _ := req["data"].(string)
		data, err := base64.StdEncoding.DecodeString(dataB64)
		if err != nil {
			log.Printf("[BROKER] sid=%s sign-ssh bad data: %v (peerPid=%d)", sid, err, pid)
			brokerWriteJSON(c, map[string]any{"error": "data not base64"})
			return
		}
		armor, err := signSSH(data, key.Signer, namespace)
		if err != nil {
			log.Printf("[BROKER] sid=%s sign-ssh failed: %v (peerPid=%d)", sid, err, pid)
			brokerWriteJSON(c, map[string]any{"error": "sign failed"})
			return
		}
		log.Printf("[BROKER] sid=%s signed %d bytes via %s (peerPid=%d, fp=%s)", sid, len(data), namespace, pid, key.Fingerprint)
		brokerWriteJSON(c, map[string]any{"signature": armor})
	default:
		// PoC echo behavior, kept for the swe-swe-broker-probe diagnostic tool.
		brokerWriteJSON(c, map[string]any{
			"sid":     sid,
			"ts":      time.Now().Unix(),
			"echoed":  req,
			"peerPid": pid,
		})
	}
}

// peerPID is platform-specific: SO_PEERCRED on Linux (peercred_linux.go),
// unsupported elsewhere (peercred_other.go). It returns the kernel-reported
// pid of the connection's peer; cannot be forged by the peer.

func brokerWriteJSON(c *net.UnixConn, v any) {
	if err := json.NewEncoder(c).Encode(v); err != nil {
		log.Printf("[BROKER] encode response failed: %v", err)
	}
}
