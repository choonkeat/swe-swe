package main

// Tunnel-mode supervisor: spawns the swe-swe-tunnel client as a child
// process and consumes its JSONL event stream on stdout. The assigned
// public hostname appears in the live state as soon as the child emits
// register_ok. Companion task plan:
// /workspace/tasks/2026-04-29-tunnel-subprocess-pivot.md.
//
// This file is the supervisor in isolation. Wiring of the live hostname
// into the WS status broadcast is a separate commit; for now the
// hostname lives in an atomic accessor and the rest of the codebase
// keeps using serverPublicHostname.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// supervisorOpenPort extracts the port portion of a listen address
// (":9898", "127.0.0.1:9898", "[::]:9898") for the OPEN AT log line.
// Falls back to "9898" if the address is empty or unparsable so the
// log line is never blank.
func supervisorOpenPort(listenAddr string) string {
	if listenAddr == "" {
		return "9898"
	}
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return "9898"
	}
	return port
}

// tunnelSupervisorOpts captures the configuration for one supervisor
// instance. ServerURL is the tunneld base URL (e.g.
// "https://tunnel.example.com"). Unique is the bare label the tunnel
// client requests (server appends "-tunnel" to it). BinPath is the
// path to the swe-swe-tunnel binary.
type tunnelSupervisorOpts struct {
	ServerURL string
	Unique    string
	BinPath   string

	// LocalAddr is the swe-swe-server listen address (e.g. ":9898",
	// "127.0.0.1:9898"). The supervisor uses just the port portion to
	// build the OPEN AT URL; tunneld demuxes {port}.{hostname} ->
	// 127.0.0.1:{port} so the URL must match the actual bind.
	LocalAddr string

	// MinBackoff and MaxBackoff bound the restart delay after a child
	// exit. Defaults: 1s and 60s.
	MinBackoff time.Duration
	MaxBackoff time.Duration

	// startChild is an injection point for tests so they can substitute
	// a fake child process. Production code leaves it nil and the
	// supervisor uses exec.CommandContext on BinPath.
	startChild func(ctx context.Context) (childProcess, error)

	// onEvent is an optional observer hook, called for every parsed
	// event. Used by tests to assert on the event stream without
	// scraping logs. Production code leaves it nil.
	onEvent func(supervisorEvent)

	// fatalReason, when non-nil, is set by applyEvent to the deny
	// reason whenever a kind=fatal event is observed. The outer loop
	// checks it after each child exit and stops the supervisor instead
	// of restarting (matches swe-swe-tunnel's EventFatal contract:
	// permanent failures like key_mismatch / bad_sig / version
	// mismatch must not loop). runTunnelSupervisor allocates a
	// non-nil pointer if the caller leaves it nil.
	fatalReason *atomic.Pointer[string]
}

// childProcess is the surface area the supervisor needs from a running
// tunnel-client child. exec.Cmd satisfies this via execChild below;
// tests provide their own implementation that emits scripted events.
type childProcess interface {
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Pid() int
	// Wait blocks until the child exits and returns the exit status
	// (or context error if the supervisor cancelled).
	Wait() error
	// Kill sends SIGKILL. Used as a last resort if the child does not
	// honor context cancellation in a reasonable time.
	Kill() error
}

// supervisorEvent is the on-the-wire shape of the tunnel client's
// JSONL stdout events. Mirrors the schema in the swe-swe-tunnel repo
// at internal/tunnelclient/events.go.
type supervisorEvent struct {
	V    int             `json:"v"`
	TS   string          `json:"ts"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// supervisorRegisterOK matches the data payload of a register_ok or
// relabel event. Only Hostname is consumed today; Unique is captured
// for log clarity.
type supervisorRegisterOK struct {
	Hostname    string `json:"hostname"`
	Unique      string `json:"unique"`
	OldHostname string `json:"old_hostname"`
}

// liveTunnelHostname is the latest hostname observed from the
// supervisor's child process. Empty string means "no tunnel currently
// registered" (either no supervisor running, or the child has not
// reached register_ok yet, or the tunnel is in the disconnected gap
// between register_ok and the next reconnect's register_ok).
//
// Read lock-free via getLiveTunnelHostname. Set only by the supervisor
// goroutine via setLiveTunnelHostname.
var liveTunnelHostname atomic.Pointer[string]

// tunnelStatusInfo is the shape exposed to the frontend over the WS
// status payload. State follows the JSONL event lifecycle plus a
// terminal "fatal" sink for permanent denies. RetryAfterMs is set on
// reconnecting events so the UI can display "rate-limited; retrying
// in 5m" instead of an indefinite spinner. Reason is a human-readable
// string from the underlying event (deny reason, error reason, etc).
type tunnelStatusInfo struct {
	State        string `json:"state"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// liveTunnelStatus mirrors the tunnel client's lifecycle for the
// frontend. Updated by applyEvent on every relevant kind. Read by
// buildStatusPayload to ride along with the next status broadcast.
var liveTunnelStatus atomic.Pointer[tunnelStatusInfo]

// getLiveTunnelStatus returns a copy of the current tunnel status. If
// no status has been recorded yet (no supervisor, or supervisor not
// yet started), returns the zero value with State="".
func getLiveTunnelStatus() tunnelStatusInfo {
	if p := liveTunnelStatus.Load(); p != nil {
		return *p
	}
	return tunnelStatusInfo{}
}

// setLiveTunnelStatus stores ts as the current tunnel status. Returns
// true when the value differs from the previous value (so the caller
// can gate a broadcast).
func setLiveTunnelStatus(ts tunnelStatusInfo) bool {
	prev := getLiveTunnelStatus()
	tsCopy := ts
	liveTunnelStatus.Store(&tsCopy)
	return prev != ts
}

// getLiveTunnelHostname returns the current live hostname or "" if
// none has been observed. Safe for hot-path readers.
func getLiveTunnelHostname() string {
	if p := liveTunnelHostname.Load(); p != nil {
		return *p
	}
	return ""
}

// setLiveTunnelHostname stores h as the current live hostname.
// Returns true if the value changed (caller can use this to gate a
// broadcast).
func setLiveTunnelHostname(h string) bool {
	prev := getLiveTunnelHostname()
	hCopy := h
	liveTunnelHostname.Store(&hCopy)
	return prev != h
}

// broadcastPublicHostnameChange triggers a status broadcast on every
// active session so connected browsers pick up a hostname change
// immediately. Called by the supervisor on register_ok / relabel
// when the live value actually changes.
//
// It walks the sessions registry under sessionsMu.RLock and fires
// each Session.BroadcastStatus on its own goroutine to avoid one
// slow client gating the rest. recoverGoroutine guards each.
var broadcastPublicHostnameChange = func() {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	for _, s := range sessions {
		s := s
		go func() {
			defer recoverGoroutine("broadcastPublicHostnameChange")
			s.BroadcastStatus()
		}()
	}
}

// runTunnelSupervisor is the long-running supervisor goroutine. It
// loops on (start child, drain events, wait for exit, backoff). Exits
// only when ctx is cancelled; never on its own.
func runTunnelSupervisor(ctx context.Context, opts tunnelSupervisorOpts) {
	defer recoverGoroutine("tunnelSupervisor")

	if opts.MinBackoff == 0 {
		opts.MinBackoff = 1 * time.Second
	}
	if opts.MaxBackoff == 0 {
		opts.MaxBackoff = 60 * time.Second
	}
	if opts.fatalReason == nil {
		opts.fatalReason = &atomic.Pointer[string]{}
	}

	backoff := opts.MinBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := runTunnelChild(ctx, opts)
		// Hostname is no longer live now that the child has exited.
		// Clear it so a reader cannot mistake a stale value for the
		// current tunnel state. If it was previously set, push the
		// change to connected clients so the frontend can fall back
		// to legacy mode (or show a "tunnel down" state).
		if setLiveTunnelHostname("") {
			broadcastPublicHostnameChange()
		}

		if ctx.Err() != nil {
			return
		}

		// kind=fatal means the tunnel client surfaced a permanent
		// deny (key_mismatch, bad_sig, version mismatch, invalid
		// unique). Restarting just hammers the server with the same
		// rejected request. Stop the supervisor; an operator must
		// fix the underlying config and restart the container.
		if rp := opts.fatalReason.Load(); rp != nil {
			log.Printf("[tunnel-supervisor] permanent failure (reason=%s); not restarting -- fix config and restart container",
				*rp)
			// applyEvent already pushed a fatal status to the live
			// atomic; nothing more to do -- the frontend will see
			// {state:"fatal",reason:...} on its next status frame.
			return
		}

		// Per CLAUDE.md: never silently discard child exit status.
		// runTunnelChild already logs PID + exit; here we log the
		// supervisor-level decision to back off and retry.
		log.Printf("[tunnel-supervisor] child exited (err=%v); backing off %s before restart",
			err, backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}

		backoff *= 2
		if backoff > opts.MaxBackoff {
			backoff = opts.MaxBackoff
		}
	}
}

// runTunnelChild spawns one child process, drains its stdout JSONL
// stream, and returns when the child exits. The supervisor's outer
// loop owns restart logic; this function is single-shot.
func runTunnelChild(ctx context.Context, opts tunnelSupervisorOpts) error {
	start := opts.startChild
	if start == nil {
		start = func(ctx context.Context) (childProcess, error) {
			return startExecChild(ctx, opts)
		}
	}

	child, err := start(ctx)
	if err != nil {
		return fmt.Errorf("start child: %w", err)
	}
	pid := child.Pid()
	log.Printf("[tunnel-supervisor] child started: pid=%d server=%s unique=%s",
		pid, opts.ServerURL, opts.Unique)

	// Drain stderr in a goroutine so the child does not block on a full
	// stderr buffer. We tee its lines into our log with a clear prefix.
	go func() {
		defer recoverGoroutine("tunnelChildStderr")
		drainStderr(child.Stderr())
	}()

	// Drain stdout JSONL stream synchronously. When stdout closes
	// (child exiting) the loop ends and we fall through to Wait.
	drainEvents(child.Stdout(), opts)

	// Per CLAUDE.md no-silent-Wait rule: log PID + exit status. The
	// outer supervisor loop logs the backoff decision separately.
	exitErr := child.Wait()
	switch {
	case exitErr == nil:
		log.Printf("[tunnel-supervisor] child exited: pid=%d status=ok", pid)
	default:
		var ee *exec.ExitError
		if errors.As(exitErr, &ee) {
			log.Printf("[tunnel-supervisor] child exited: pid=%d status=%d err=%v",
				pid, ee.ExitCode(), exitErr)
		} else {
			log.Printf("[tunnel-supervisor] child exited: pid=%d err=%v", pid, exitErr)
		}
	}
	return exitErr
}

// drainEvents reads JSONL from r and routes recognized event kinds to
// the supervisor state. Unknown kinds are logged but otherwise
// ignored, per the forward-compat rule on the protocol side.
func drainEvents(r io.ReadCloser, opts tunnelSupervisorOpts) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	// Allow large lines; default Scanner buffer is 64KB which is
	// plenty for our event shapes, but be explicit so a future event
	// addition cannot silently truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev supervisorEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("[tunnel-supervisor] malformed event: %v line=%q", err, line)
			continue
		}
		applyEvent(ev, opts)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("[tunnel-supervisor] stdout scanner err: %v", err)
	}
}

// applyEvent updates supervisor state based on one parsed event. Only
// register_ok and relabel are interesting to the rest of the system;
// everything else is logged at info level so an operator following the
// supervisor can see the lifecycle.
func applyEvent(ev supervisorEvent, opts tunnelSupervisorOpts) {
	if opts.onEvent != nil {
		opts.onEvent(ev)
	}
	// Track whether the tunnelStatus changed so we can broadcast once
	// per event (instead of broadcasting from every case below).
	statusChanged := false
	switch ev.Kind {
	case "register_ok", "relabel":
		var data supervisorRegisterOK
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			log.Printf("[tunnel-supervisor] %s data unmarshal: %v", ev.Kind, err)
			return
		}
		if data.Hostname == "" {
			log.Printf("[tunnel-supervisor] %s with empty hostname; ignoring", ev.Kind)
			return
		}
		hostnameChanged := setLiveTunnelHostname(data.Hostname)
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{State: "connected"})
		if hostnameChanged {
			log.Printf("[tunnel-supervisor] hostname=%s (kind=%s)", data.Hostname, ev.Kind)
			// Operator-friendly URL on a separate line so it's easy
			// to spot in PaaS log streams. The {port} component is
			// the swe-swe-server port; tunneld demuxes
			// {port}.{hostname} -> 127.0.0.1:{port}.
			log.Printf("[tunnel-supervisor] OPEN AT https://%s.%s/", supervisorOpenPort(opts.LocalAddr), data.Hostname)
		}
		if hostnameChanged || statusChanged {
			broadcastPublicHostnameChange()
		}
		return
	case "starting", "connecting":
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{State: "connecting"})
		log.Printf("[tunnel-supervisor] event kind=%s data=%s", ev.Kind, string(ev.Data))
	case "reconnecting":
		// Carry after_ms forward so the UI can show a countdown
		// instead of an indefinite spinner. swe-swe-tunnel imposes a
		// 5-minute floor on rate-limit denies; this surfaces that.
		var data struct {
			AfterMs int64  `json:"after_ms"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &data)
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{
			State:        "reconnecting",
			RetryAfterMs: data.AfterMs,
			Reason:       data.Reason,
		})
		log.Printf("[tunnel-supervisor] reconnecting after_ms=%d reason=%q", data.AfterMs, data.Reason)
	case "disconnected":
		// Keep the cached hostname during a disconnect window. The
		// child will emit a fresh register_ok on reconnect; if the
		// disconnect is permanent the child will eventually exit and
		// the outer loop's setLiveTunnelHostname("") will clear it.
		var data struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &data)
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{
			State:  "disconnected",
			Reason: data.Reason,
		})
		log.Printf("[tunnel-supervisor] disconnected reason=%q", data.Reason)
	case "error":
		var data struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &data)
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{
			State:  "error",
			Reason: data.Reason,
		})
		log.Printf("[tunnel-supervisor] error reason=%q data=%s", data.Reason, string(ev.Data))
	case "fatal":
		// Permanent deny from tunneld -- record the reason so the
		// outer supervisor loop can stop instead of restarting.
		var data struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &data)
		reason := data.Reason
		if reason == "" {
			reason = "unspecified"
		}
		statusChanged = setLiveTunnelStatus(tunnelStatusInfo{
			State:  "fatal",
			Reason: reason,
		})
		log.Printf("[tunnel-supervisor] fatal: reason=%s data=%s", reason, string(ev.Data))
		if opts.fatalReason != nil {
			r := reason
			opts.fatalReason.Store(&r)
		}
	case "deregister_ok":
		log.Printf("[tunnel-supervisor] event kind=%s data=%s", ev.Kind, string(ev.Data))
	default:
		log.Printf("[tunnel-supervisor] unknown kind=%q data=%s", ev.Kind, string(ev.Data))
	}
	if statusChanged {
		broadcastPublicHostnameChange()
	}
}

// drainStderr tees the child's stderr into our logger with a clear
// prefix. Runs until the pipe closes.
func drainStderr(r io.ReadCloser) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		log.Printf("[tunnel-client] %s", scanner.Text())
	}
}

// execChild is the production childProcess implementation backed by
// exec.Cmd. Used when tunnelSupervisorOpts.startChild is nil.
type execChild struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (c *execChild) Stdout() io.ReadCloser { return c.stdout }
func (c *execChild) Stderr() io.ReadCloser { return c.stderr }
func (c *execChild) Pid() int              { return c.cmd.Process.Pid }
func (c *execChild) Wait() error           { return c.cmd.Wait() }
func (c *execChild) Kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Signal(syscall.SIGKILL)
}

// startExecChild builds and starts the swe-swe-tunnel binary with the
// supervisor's options. Called by runTunnelChild when no fake is
// installed.
func startExecChild(ctx context.Context, opts tunnelSupervisorOpts) (childProcess, error) {
	if opts.BinPath == "" {
		return nil, errors.New("tunnel-bin path is empty")
	}
	args := []string{
		"--server", opts.ServerURL,
		"--report-format", "jsonl",
	}
	if opts.Unique != "" {
		args = append(args, "--unique", opts.Unique)
	}
	cmd := exec.CommandContext(ctx, opts.BinPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", opts.BinPath, err)
	}
	return &execChild{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}
