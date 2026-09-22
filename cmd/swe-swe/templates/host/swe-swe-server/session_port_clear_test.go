package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestHelperListenProcess is not a real test: re-exec'd by the tests below as
// a child that listens on SWE_TEST_LISTEN_ADDR until killed.
func TestHelperListenProcess(t *testing.T) {
	addr := os.Getenv("SWE_TEST_LISTEN_ADDR")
	if addr == "" {
		t.Skip("helper process only")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		os.Exit(3)
	}
	defer ln.Close()
	fmt.Println("listening")
	time.Sleep(time.Minute)
	os.Exit(0)
}

// startListenerChild starts a child of this test process listening on addr
// and waits until it is listening.
func startListenerChild(t *testing.T, addr string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperListenProcess$")
	cmd.Env = append(os.Environ(), "SWE_TEST_LISTEN_ADDR="+addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 9)
	if _, err := out.Read(buf); err != nil || string(buf) != "listening" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("helper did not start listening on %s: %q %v", addr, buf, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		err := cmd.Wait()
		t.Logf("helper pid %d exited: %v", cmd.Process.Pid, err)
	})
	return cmd
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// Node binds "::" by default, which only appears in /proc/net/tcp6. The old
// IPv4-only scan never saw such listeners, so leftover dev servers survived
// session cleanup.
func TestListenersOnPortsSeesIPv6(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp6"); err != nil {
		t.Skip("no /proc/net/tcp6")
	}
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	held := listenersOnPorts([]int{port})[os.Getpid()]
	if len(held) != 1 || held[0] != port {
		t.Fatalf("listenersOnPorts(%d) for own pid = %v, want [%d]", port, held, port)
	}
}

func withIsolatedSessions(t *testing.T) {
	t.Helper()
	sessionsMu.Lock()
	saved := sessions
	sessions = make(map[string]*Session)
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		sessions = saved
		sessionsMu.Unlock()
	})
}

// A process spawned under the server whose session has ended is a leftover:
// clearing the slot kills it and frees the port.
func TestClearSessionPortsKillsLeftover(t *testing.T) {
	withIsolatedSessions(t)
	port := freePort(t)
	child := startListenerChild(t, "127.0.0.1:"+strconv.Itoa(port))
	// Registered to a session that is no longer in the sessions map.
	registerSessionPid(child.Process.Pid, "ended-session")
	defer unregisterSessionPid(child.Process.Pid)

	sessionsMu.Lock()
	ok := clearSessionPorts([]int{port})
	sessionsMu.Unlock()
	if !ok {
		t.Fatalf("clearSessionPorts did not free port %d held by leftover pid %d", port, child.Process.Pid)
	}
	if err := syscall.Kill(child.Process.Pid, 0); err == nil {
		// Still a zombie until Wait; check the port instead.
		if len(listenersOnPorts([]int{port})) != 0 {
			t.Fatalf("port %d still held after clearSessionPorts", port)
		}
	}
}

// A process owned by a LIVE session (e.g. its app hardcodes a port) must not
// be killed; the slot is skipped instead.
func TestClearSessionPortsSparesLiveSession(t *testing.T) {
	withIsolatedSessions(t)
	port := freePort(t)
	child := startListenerChild(t, "127.0.0.1:"+strconv.Itoa(port))
	registerSessionPid(child.Process.Pid, "live-session")
	defer unregisterSessionPid(child.Process.Pid)
	sessionsMu.Lock()
	sessions["live-session"] = &Session{UUID: "live-session"}
	ok := clearSessionPorts([]int{port})
	sessionsMu.Unlock()

	if ok {
		t.Fatalf("clearSessionPorts reported port %d free while a live session holds it", port)
	}
	if err := syscall.Kill(child.Process.Pid, 0); err != nil {
		t.Fatalf("live session's pid %d was killed: %v", child.Process.Pid, err)
	}
}

// A listener inside the server itself (e.g. an orphaned proxy listener) cannot
// be killed; findAvailablePortQuintuple must move on to the next slot.
func TestFindAvailablePortQuintupleSkipsHeldSlot(t *testing.T) {
	withIsolatedSessions(t)
	oldStart, oldEnd := previewPortStart, previewPortEnd
	defer func() { previewPortStart, previewPortEnd = oldStart, oldEnd }()

	first := freePort(t)
	// agent-chat port of the first slot
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(agentChatPortFromPreview(first)))
	if err != nil {
		t.Skipf("cannot bind %d: %v", agentChatPortFromPreview(first), err)
	}
	defer ln.Close()
	previewPortStart, previewPortEnd = first, first+1

	sessionsMu.Lock()
	got, _, _, _, _, err := findAvailablePortQuintuple()
	sessionsMu.Unlock()
	if err != nil {
		t.Fatalf("findAvailablePortQuintuple: %v", err)
	}
	if got != first+1 {
		t.Fatalf("preview port = %d, want %d (slot %d is held)", got, first+1, first)
	}
}
