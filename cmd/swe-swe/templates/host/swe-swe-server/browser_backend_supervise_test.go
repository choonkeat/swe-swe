package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// procDead reports whether pid is gone or a zombie. An orphaned child that was
// SIGKILLed stays visible as a zombie until its new parent reaps it, so
// kill(pid, 0) alone would read as "still alive".
func procDead(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(raw[strings.LastIndex(string(raw), ")")+1:]))
	return len(fields) > 0 && fields[0] == "Z"
}

// deadLoopbackAddr returns a loopback address nothing is listening on: bind a
// port, read its address, then release it.
func deadLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestWaitProcHealthyReportsDeath(t *testing.T) {
	died := make(chan error, 1)
	died <- fmt.Errorf("exit status 1")
	err := waitProcHealthy(died, "", 0, time.Second)
	if err == nil {
		t.Fatal("expected an error when the process already exited")
	}
}

func TestWaitProcHealthyWaitsForListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	died := make(chan error, 1)
	if err := waitProcHealthy(died, ln.Addr().String(), 0, 2*time.Second); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
}

// A process that is alive but never bound its port is exactly the blank Agent
// View pane: x11vnc/websockify up, nothing streaming.
func TestWaitProcHealthyRejectsAliveButNotListening(t *testing.T) {
	addr := deadLoopbackAddr(t)
	died := make(chan error, 1)
	err := waitProcHealthy(died, addr, 0, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error when nothing is listening on the ready address")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Fatalf("error should name the address, got %v", err)
	}
}

func TestWaitPortFreeReturnsWhenNothingListens(t *testing.T) {
	if err := waitPortFree(deadLoopbackAddr(t), time.Second); err != nil {
		t.Fatalf("expected the port to read as free, got %v", err)
	}
}

func TestWaitPortFreeFailsWhileHeld(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	err = waitPortFree(ln.Addr().String(), 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error while the port is still held")
	}
	if !strings.Contains(err.Error(), ln.Addr().String()) {
		t.Fatalf("error should name the address, got %v", err)
	}
}

// A leftover listener must never be mistaken for the process we just started:
// that false "ready" is what left an Agent View pane blank.
func TestStartSupervisedProcRefusesHeldPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	saved := portFreeTimeout
	portFreeTimeout = 200 * time.Millisecond
	defer func() { portFreeTimeout = saved }()

	b := &browserProcs{}
	defer b.stop()
	started := 0
	err = b.startSupervisedProc("noVNC proxy", "test-browser", func() *exec.Cmd {
		started++
		return exec.Command("sleep", "5")
	}, ln.Addr().String(), 1, 0, time.Second)
	if err == nil {
		t.Fatal("expected an error when the port is held by a leftover process")
	}
	if started != 0 {
		t.Fatalf("must not launch while the port is held, launched %d time(s)", started)
	}
}

// websockify forks a child that inherits the listening socket; killing only
// the parent leaves that child holding the VNC port.
func TestStopKillsForkedChildren(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	b := &browserProcs{}
	err := b.startSupervisedProc("fake helper", "test-browser", func() *exec.Cmd {
		return exec.Command("sh", "-c", "sleep 30 & echo $! > "+pidFile+"; sleep 30")
	}, "", 1, 200*time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatalf("expected the helper to come up, got %v", err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", raw, err)
	}
	b.stop()
	gone := false
	for i := 0; i < 40; i++ {
		if procDead(childPID) {
			gone = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gone {
		syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatalf("forked child PID %d survived stop()", childPID)
	}
}

func TestStartSupervisedProcRetriesUntilAlive(t *testing.T) {
	b := &browserProcs{}
	defer b.stop()
	calls := 0
	err := b.startSupervisedProc("fake helper", "test-browser", func() *exec.Cmd {
		calls++
		if calls < 3 {
			return exec.Command("sh", "-c", "exit 1")
		}
		return exec.Command("sleep", "5")
	}, "", 3, 100*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("expected the third attempt to succeed, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 start attempts, got %d", calls)
	}
}

func TestStartSupervisedProcGivesUpAndExplains(t *testing.T) {
	b := &browserProcs{}
	defer b.stop()
	calls := 0
	err := b.startSupervisedProc("noVNC proxy on port 65000", "test-browser", func() *exec.Cmd {
		calls++
		return exec.Command("sh", "-c", "exit 3")
	}, "", 2, 100*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("expected an error after every attempt died")
	}
	if !strings.Contains(err.Error(), "noVNC proxy on port 65000") {
		t.Fatalf("error should name the process, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 start attempts, got %d", calls)
	}
}

func TestStartSupervisedProcFailsWhenPortNeverBinds(t *testing.T) {
	addr := deadLoopbackAddr(t)
	b := &browserProcs{}
	defer b.stop()
	err := b.startSupervisedProc("x11vnc", "test-browser", func() *exec.Cmd {
		return exec.Command("sleep", "5")
	}, addr, 1, 0, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error when the process never bound its port")
	}
}

// A helper that dies must not be left registered as a live pid.
func TestStartSupervisedProcUntracksDeadAttempts(t *testing.T) {
	b := &browserProcs{}
	defer b.stop()
	if err := b.startSupervisedProc("fake helper", "test-browser", func() *exec.Cmd {
		return exec.Command("sh", "-c", "exit 1")
	}, "", 1, 100*time.Millisecond, time.Second); err == nil {
		t.Fatal("expected an error")
	}
	// untrackPid runs in the Wait goroutine just after it reports the death,
	// so give it a moment rather than racing it.
	for _, pid := range b.pids {
		untracked := false
		for i := 0; i < 20; i++ {
			if _, ok := trackedPids.Load(pid); !ok {
				untracked = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !untracked {
			t.Fatalf("pid %d still tracked after the process exited", pid)
		}
	}
}
