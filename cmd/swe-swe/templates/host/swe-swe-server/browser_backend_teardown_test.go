package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startGroupLeaderWithChild starts a shell in its own process group that forks a
// child and then sleeps. It returns the leader's PID and the child's PID. The
// child is the one that matters: it inherits whatever its parent held (a
// listening socket, for the real websockify) and survives a SIGKILL aimed at
// the leader alone.
func startGroupLeaderWithChild(t *testing.T) (int, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", fmt.Sprintf("sleep 300 & echo $! > %s; sleep 300", pidFile))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start leader: %v", err)
	}
	leader := cmd.Process.Pid
	t.Cleanup(func() {
		// Belt and braces: the test fails rather than leaks if the kill under
		// test did not work.
		syscall.Kill(-leader, syscall.SIGKILL)
		syscall.Kill(leader, syscall.SIGKILL)
		if _, err := cmd.Process.Wait(); err != nil {
			t.Logf("leader PID %d wait: %v", leader, err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			if child, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && child > 0 {
				return leader, child
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("child PID never appeared in %s", pidFile)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitProcDead(t *testing.T, pid int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if procDead(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Local (in-container) Agent View tears a session's browser down through
// stopSessionBrowser, not through browserProcs.stop(). It killed the recorded
// PIDs one at a time, so a forked child -- websockify's, which holds the
// listening VNC socket -- outlived the session and squatted the port that the
// next session allocated into that slot got. That is the same blank-pane
// failure killBrowserProc was written for on the backend side.
func TestStopSessionBrowserKillsProcessGroup(t *testing.T) {
	leader, child := startGroupLeaderWithChild(t)

	sess := &Session{UUID: "teardown-test", BrowserPIDs: []int{leader}}
	stopSessionBrowser(sess)

	if !waitProcDead(t, child, 3*time.Second) {
		t.Fatalf("child PID %d outlived stopSessionBrowser (only the group leader was killed)", child)
	}
	if !waitProcDead(t, leader, 3*time.Second) {
		t.Fatalf("leader PID %d survived stopSessionBrowser", leader)
	}
	if sess.BrowserPIDs != nil {
		t.Errorf("BrowserPIDs should be cleared, got %v", sess.BrowserPIDs)
	}
}

// The data dir goes away with the processes; a leftover profile dir is how
// disk filled up before browserProcs tracked it.
func TestStopSessionBrowserRemovesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sess := &Session{UUID: "teardown-test", BrowserDataDir: dir}
	stopSessionBrowser(sess)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("data dir %s still present: %v", dir, err)
	}
	if sess.BrowserDataDir != "" {
		t.Errorf("BrowserDataDir should be cleared, got %q", sess.BrowserDataDir)
	}
}
