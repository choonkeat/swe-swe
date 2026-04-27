package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestApplyEvent_RegisterOK_SetsLiveHostname covers the happy path:
// a register_ok event with a non-empty hostname updates the atomic
// accessor.
func TestApplyEvent_RegisterOK_SetsLiveHostname(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("")
	applyEvent(supervisorEvent{
		V:    1,
		Kind: "register_ok",
		Data: []byte(`{"hostname":"alpha-tunnel.example.com","unique":"alpha"}`),
	}, tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("after register_ok: getLiveTunnelHostname() = %q, want %q",
			got, "alpha-tunnel.example.com")
	}
}

// TestApplyEvent_Relabel_UpdatesHostname covers a server-driven label
// rotation: relabel events overwrite the cached hostname.
func TestApplyEvent_Relabel_UpdatesHostname(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("alpha-tunnel.example.com")
	applyEvent(supervisorEvent{
		V:    1,
		Kind: "relabel",
		Data: []byte(`{"hostname":"beta-tunnel.example.com","old_hostname":"alpha-tunnel.example.com"}`),
	}, tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "beta-tunnel.example.com" {
		t.Errorf("after relabel: getLiveTunnelHostname() = %q, want %q",
			got, "beta-tunnel.example.com")
	}
}

// TestApplyEvent_EmptyHostnameIgnored verifies that a malformed
// register_ok payload (empty hostname field) does not clobber a
// previously-cached good value.
func TestApplyEvent_EmptyHostnameIgnored(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("alpha-tunnel.example.com")
	applyEvent(supervisorEvent{
		V:    1,
		Kind: "register_ok",
		Data: []byte(`{"hostname":"","unique":"alpha"}`),
	}, tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("empty-hostname register_ok must not clobber; got %q", got)
	}
}

// TestApplyEvent_DisconnectedKeepsCache asserts that a disconnected
// event does not clear the hostname. The cache is cleared only when
// the child process exits (the outer supervisor loop's job).
func TestApplyEvent_DisconnectedKeepsCache(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("alpha-tunnel.example.com")
	applyEvent(supervisorEvent{
		V:    1,
		Kind: "disconnected",
		Data: []byte(`{"reason":"control stream EOF"}`),
	}, tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("disconnected must not clear hostname; got %q", got)
	}
}

// TestApplyEvent_UnknownKindForwardsCompat ensures the supervisor
// keeps consuming events even when the producer adds a new kind not
// known here. This is the forward-compat clause from the protocol.
func TestApplyEvent_UnknownKindForwardsCompat(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("alpha-tunnel.example.com")
	applyEvent(supervisorEvent{
		V:    1,
		Kind: "future_unicorn_kind",
		Data: []byte(`{"foo":"bar"}`),
	}, tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("unknown kind must not change hostname; got %q", got)
	}
}

// TestDrainEvents_ParsesScriptedLifecycle feeds a full happy-path
// JSONL stream through the supervisor's stdout reader and asserts
// each event is decoded in order, plus the live hostname ends up at
// the latest register_ok value.
func TestDrainEvents_ParsesScriptedLifecycle(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("")

	stream := strings.Join([]string{
		`{"v":1,"ts":"2026-04-29T10:00:00Z","kind":"starting","data":{"unique":"alpha","server_url":"https://tunnel.example.com"}}`,
		`{"v":1,"ts":"2026-04-29T10:00:00.120Z","kind":"connecting","data":{"server_url":"https://tunnel.example.com","attempt":1}}`,
		`{"v":1,"ts":"2026-04-29T10:00:00.480Z","kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
		`{"v":1,"ts":"2026-04-29T10:05:12.300Z","kind":"disconnected","data":{"reason":"control stream EOF"}}`,
		`{"v":1,"ts":"2026-04-29T10:05:13.300Z","kind":"reconnecting","data":{"after_ms":1000,"attempt":2}}`,
		`{"v":1,"ts":"2026-04-29T10:05:13.450Z","kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
		`{"v":1,"ts":"2026-04-29T10:30:00.000Z","kind":"deregister_ok","data":{"unique":"alpha"}}`,
		"",
	}, "\n")

	var (
		mu   sync.Mutex
		seen []string
	)
	drainEvents(io.NopCloser(strings.NewReader(stream)), tunnelSupervisorOpts{
		onEvent: func(ev supervisorEvent) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, ev.Kind)
		},
	})

	wantKinds := []string{
		"starting", "connecting", "register_ok",
		"disconnected", "reconnecting", "register_ok",
		"deregister_ok",
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != len(wantKinds) {
		t.Fatalf("event count: got %d (%v) want %d (%v)",
			len(seen), seen, len(wantKinds), wantKinds)
	}
	for i, want := range wantKinds {
		if seen[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, seen[i], want)
		}
	}
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("after lifecycle: getLiveTunnelHostname() = %q, want %q",
			got, "alpha-tunnel.example.com")
	}
}

// TestDrainEvents_SkipsMalformedLine asserts that one malformed JSON
// line does not abort the stream; subsequent valid events still apply.
func TestDrainEvents_SkipsMalformedLine(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("")
	stream := strings.Join([]string{
		`not-valid-json-{`,
		`{"v":1,"kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
		"",
	}, "\n")
	drainEvents(io.NopCloser(strings.NewReader(stream)), tunnelSupervisorOpts{})
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("after malformed-then-valid: got %q want %q",
			got, "alpha-tunnel.example.com")
	}
}

// fakeChild is a scriptable childProcess for runTunnelChild tests. It
// emits a fixed sequence of events on stdout and exits with a
// configurable error.
type fakeChild struct {
	events  []string
	exitErr error
	pid     int

	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter

	closed chan struct{}
	once   sync.Once
}

func newFakeChild(events []string, exitErr error, pid int) *fakeChild {
	soR, soW := io.Pipe()
	seR, seW := io.Pipe()
	c := &fakeChild{
		events:  events,
		exitErr: exitErr,
		pid:     pid,
		stdoutR: soR, stdoutW: soW,
		stderrR: seR, stderrW: seW,
		closed: make(chan struct{}),
	}
	go func() {
		for _, line := range c.events {
			_, _ = c.stdoutW.Write([]byte(line + "\n"))
		}
		// Closing stdout simulates the child's stdout EOF. The
		// supervisor's drainEvents returns; runTunnelChild then calls
		// Wait which blocks until we mark closed.
		_ = c.stdoutW.Close()
		_ = c.stderrW.Close()
	}()
	return c
}

func (c *fakeChild) Stdout() io.ReadCloser { return c.stdoutR }
func (c *fakeChild) Stderr() io.ReadCloser { return c.stderrR }
func (c *fakeChild) Pid() int              { return c.pid }
func (c *fakeChild) Wait() error {
	c.once.Do(func() { close(c.closed) })
	<-c.closed
	return c.exitErr
}
func (c *fakeChild) Kill() error { c.once.Do(func() { close(c.closed) }); return nil }

// TestRunTunnelChild_EmitsHostname runs one fake child invocation and
// asserts the live hostname is set after the scripted register_ok.
func TestRunTunnelChild_EmitsHostname(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	setLiveTunnelHostname("")
	events := []string{
		`{"v":1,"kind":"starting","data":{"unique":"alpha","server_url":"https://tunnel.example.com"}}`,
		`{"v":1,"kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
	}
	opts := tunnelSupervisorOpts{
		ServerURL: "https://tunnel.example.com",
		Unique:    "alpha",
		BinPath:   "/fake",
		startChild: func(ctx context.Context) (childProcess, error) {
			return newFakeChild(events, nil, 4242), nil
		},
	}
	if err := runTunnelChild(context.Background(), opts); err != nil {
		t.Fatalf("runTunnelChild: %v", err)
	}
	if got := getLiveTunnelHostname(); got != "alpha-tunnel.example.com" {
		t.Errorf("after fake child: getLiveTunnelHostname() = %q, want %q",
			got, "alpha-tunnel.example.com")
	}
}

// TestRunTunnelSupervisor_RestartsAfterChildExit drives the outer
// supervisor loop with a series of fake children and asserts the
// supervisor keeps restarting until the context is cancelled.
func TestRunTunnelSupervisor_RestartsAfterChildExit(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var (
		mu     sync.Mutex
		starts int
	)

	done := make(chan struct{})
	go func() {
		runTunnelSupervisor(ctx, tunnelSupervisorOpts{
			ServerURL:  "https://tunnel.example.com",
			Unique:     "alpha",
			BinPath:    "/fake",
			MinBackoff: 5 * time.Millisecond,
			MaxBackoff: 10 * time.Millisecond,
			startChild: func(ctx context.Context) (childProcess, error) {
				mu.Lock()
				starts++
				mu.Unlock()
				return newFakeChild([]string{
					`{"v":1,"kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
				}, errors.New("simulated exit"), 4243), nil
			},
		})
		close(done)
	}()

	// Wait until the supervisor has restarted at least 3 times.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		s := starts
		mu.Unlock()
		if s >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervisor did not restart at least 3 times in 2s; got %d", s)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not exit within 2s of context cancel")
	}
}

// TestRunTunnelSupervisor_ClearsHostnameAfterChildExit asserts that
// when the child exits, the live hostname is cleared so the frontend
// can fall back to legacy mode rather than displaying a stale value.
func TestRunTunnelSupervisor_ClearsHostnameAfterChildExit(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	ctx, cancel := context.WithCancel(context.Background())

	gotEvents := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		runTunnelSupervisor(ctx, tunnelSupervisorOpts{
			ServerURL:  "https://tunnel.example.com",
			Unique:     "alpha",
			BinPath:    "/fake",
			MinBackoff: 5 * time.Millisecond,
			MaxBackoff: 10 * time.Millisecond,
			onEvent: func(ev supervisorEvent) {
				if ev.Kind == "register_ok" {
					select {
					case gotEvents <- struct{}{}:
					default:
					}
				}
			},
			startChild: func(ctx context.Context) (childProcess, error) {
				return newFakeChild([]string{
					`{"v":1,"kind":"register_ok","data":{"hostname":"alpha-tunnel.example.com","unique":"alpha"}}`,
				}, errors.New("simulated exit"), 4244), nil
			},
		})
		close(done)
	}()

	// Wait for the first register_ok before cancelling.
	select {
	case <-gotEvents:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("supervisor never observed register_ok")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not exit within 2s of context cancel")
	}

	if got := getLiveTunnelHostname(); got != "" {
		t.Errorf("after supervisor exit: hostname should be cleared, got %q", got)
	}
}
