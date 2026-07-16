package main

import (
	"os/exec"
	"syscall"
	"testing"
)

// finishedCmd starts a trivial process, lets it exit normally, and reaps it so
// ProcessState is populated (ProcessState is nil until Wait returns).
func finishedCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	return cmd
}

// signalKilledCmd starts a process and kills it with a signal, mirroring how an
// agent dies under an OOM kill (SIGKILL, exit 137) or a crash.
func signalKilledCmd(t *testing.T, sig syscall.Signal) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("signal: %v", err)
	}
	cmd.Wait() // non-nil error for a signalled process; ProcessState is what we want
	return cmd
}

// The regression this fixes: (*os.ProcessState).Exited() is WIFEXITED -- true
// only for a NORMAL exit and false for a signal-killed process. The old reaper
// predicate required Exited(), so an OOM-killed (SIGKILL) or crashed agent was
// never reaped: its session lingered in the sessions map forever, invisible to
// list_sessions (which skips ProcessState != nil) while still holding all four
// per-port proxy listeners and squatting their ports.
func TestSignalKilledProcessIsNotExited(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
		cmd := signalKilledCmd(t, sig)
		if cmd.ProcessState == nil {
			t.Fatalf("%v: ProcessState must be set once Wait returned", sig)
		}
		if cmd.ProcessState.Exited() {
			t.Errorf("%v: expected Exited()==false for a signalled process (WIFEXITED)", sig)
		}
	}
	// Contrast: a normal exit does report Exited().
	if c := finishedCmd(t); !c.ProcessState.Exited() {
		t.Error("normal exit should report Exited()==true")
	}
}

// A session whose agent was signal-killed must be reapable -- this is the leak.
func TestSessionReapableAfterSignalKill(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
		s := &Session{UUID: "sig", Cmd: signalKilledCmd(t, sig)}
		if !s.reapable() {
			t.Errorf("%v: signal-killed session must be reapable (else it leaks its proxy ports forever)", sig)
		}
	}
}

// A normally-exited session stays reapable (unchanged behavior).
func TestSessionReapableAfterNormalExit(t *testing.T) {
	s := &Session{UUID: "normal", Cmd: finishedCmd(t)}
	if !s.reapable() {
		t.Error("normally-exited session must be reapable")
	}
}

// A live session (process still running -> ProcessState nil) is never reapable.
func TestSessionNotReapableWhileRunning(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	s := &Session{UUID: "live", Cmd: cmd}
	if s.reapable() {
		t.Error("a session whose process is still running must never be reapable")
	}
	// A session with no Cmd at all is not reapable either.
	if (&Session{UUID: "nocmd"}).reapable() {
		t.Error("a session with a nil Cmd must not be reapable")
	}
}

// The restart guard: startPTYReader calls cmd.Wait() OUTSIDE s.mu and only then
// calls RestartProcess, so there is a window where the old Cmd is finished
// (ProcessState != nil) but the session is about to be restarted. Reaping there
// would kill an in-flight yolo restart, so a restarting session is never
// reapable -- and clearing the flag must restore reapability (otherwise a failed
// restart would leak the session forever, reintroducing the original bug).
func TestSessionNotReapableWhileRestarting(t *testing.T) {
	s := &Session{UUID: "restarting", Cmd: signalKilledCmd(t, syscall.SIGKILL)}
	if !s.reapable() {
		t.Fatal("precondition: a finished, non-restarting session should be reapable")
	}

	s.setRestarting(true)
	if s.reapable() {
		t.Error("a session mid-restart must NOT be reapable (would kill an in-flight restart)")
	}

	s.setRestarting(false)
	if !s.reapable() {
		t.Error("clearing the restart guard must restore reapability")
	}
}
