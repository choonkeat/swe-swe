package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// newTestSupervisor builds a supervisor writing to buf with a short grace.
func newTestSupervisor(buf *bytes.Buffer, svcs []Service, ports map[string]int, grace time.Duration) *supervisor {
	return &supervisor{
		services:  svcs,
		ports:     ports,
		inherited: []string{"PATH=" + defaultPathForTest()},
		out:       buf,
		grace:     grace,
		noColor:   true,
	}
}

func defaultPathForTest() string {
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

func TestSupervisor_OneExitsAll(t *testing.T) {
	var buf bytes.Buffer
	svcs := []Service{
		{Name: "quick", Command: "exit 3"},
		{Name: "sleeper", Command: "sleep 30"},
	}
	ports := map[string]int{"quick": 3000, "sleeper": 8000}
	sup := newTestSupervisor(&buf, svcs, ports, 2*time.Second)

	start := time.Now()
	code := sup.run(context.Background())
	elapsed := time.Since(start)

	if code != 3 {
		t.Errorf("exit code=%d want 3 (the exiting service's code)", code)
	}
	if elapsed > 10*time.Second {
		t.Errorf("run took %v, expected fast teardown of the sleeper", elapsed)
	}
	if !strings.Contains(buf.String(), "quick exited") {
		t.Errorf("expected 'quick exited' log line, got:\n%s", buf.String())
	}
}

func TestSupervisor_ContextCancelCleanShutdown(t *testing.T) {
	var buf bytes.Buffer
	svcs := []Service{
		{Name: "a", Command: "sleep 30"},
		{Name: "b", Command: "sleep 30"},
	}
	ports := map[string]int{"a": 3000, "b": 8000}
	sup := newTestSupervisor(&buf, svcs, ports, 2*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- sup.run(ctx) }()

	time.Sleep(300 * time.Millisecond) // let both start
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code=%d want 0 on signal-triggered shutdown", code)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("run did not return after context cancel; services leaked")
	}
}

func TestSupervisor_SigkillEscalation(t *testing.T) {
	var buf bytes.Buffer
	// Ignores SIGTERM and keeps its group alive, forcing SIGKILL escalation.
	svcs := []Service{
		{Name: "stubborn", Command: "trap '' TERM; while true; do sleep 0.2; done"},
	}
	ports := map[string]int{"stubborn": 3000}
	sup := newTestSupervisor(&buf, svcs, ports, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- sup.run(ctx) }()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// returned -> SIGKILL escalation worked (grace was 500ms).
	case <-time.After(6 * time.Second):
		t.Fatal("run did not return; SIGKILL escalation failed (process ignored SIGTERM)")
	}
}

func TestSupervisor_StartFailure(t *testing.T) {
	var buf bytes.Buffer
	// A command that fails to exec cleanly still resolves (sh returns 127).
	svcs := []Service{
		{Name: "bad", Command: "this-binary-does-not-exist-xyz"},
		{Name: "sleeper", Command: "sleep 30"},
	}
	ports := map[string]int{"bad": 3000, "sleeper": 8000}
	sup := newTestSupervisor(&buf, svcs, ports, 2*time.Second)

	code := sup.run(context.Background())
	if code == 0 {
		t.Errorf("expected non-zero exit when a service fails, got 0; log:\n%s", buf.String())
	}
}
