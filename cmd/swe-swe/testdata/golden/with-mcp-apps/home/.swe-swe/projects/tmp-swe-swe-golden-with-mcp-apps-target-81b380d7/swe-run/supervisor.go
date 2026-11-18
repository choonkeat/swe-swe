package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// supervisor launches every Procfile service as a child process group and,
// following foreman semantics, tears the whole set down when any service exits
// or when the context is cancelled (SIGINT/SIGTERM at the CLI).
type supervisor struct {
	services  []Service
	ports     map[string]int
	sweEnv    map[string]string
	dotEnv    map[string]string
	inherited []string
	out       io.Writer
	grace     time.Duration
	noColor   bool
}

// exitInfo reports a finished (or never-started) service.
type exitInfo struct {
	name string
	pid  int
	code int
}

// runningProc is a started service whose process group we can signal.
type runningProc struct {
	name string
	pgid int // == leader pid, since we Setpgid at start
}

// exitCodeOf extracts a process exit code from cmd.Wait's error. A signal-killed
// process yields -1 (used for torn-down siblings, whose code we ignore).
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}

// logf writes a runner meta-line ("swe-run | ...") under mu so it never
// interleaves with service output.
func (s *supervisor) logf(mu *sync.Mutex, format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(s.out, "swe-run | "+format+"\n", args...)
}

// run starts all services and blocks until the stack is torn down. It returns
// the exit code of the service that triggered shutdown, or 0 when shutdown was
// triggered by context cancellation (a signal).
func (s *supervisor) run(ctx context.Context) int {
	mu := &sync.Mutex{}
	width := nameWidth(s.services)

	// Every service emits exactly one exitInfo (start failures included), so the
	// buffered channel never blocks a waiter.
	exits := make(chan exitInfo, len(s.services))
	var procs []runningProc

	for i, svc := range s.services {
		prefix := servicePrefix(svc.Name, width, colorFor(i, s.noColor))
		env := buildServiceEnv(s.inherited, s.sweEnv, s.dotEnv, s.ports, svc.Name)

		cmd := exec.Command("sh", "-c", svc.Command)
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			s.logf(mu, "%s: failed to open stdout: %v", svc.Name, err)
			exits <- exitInfo{name: svc.Name, code: 1}
			continue
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			s.logf(mu, "%s: failed to open stderr: %v", svc.Name, err)
			exits <- exitInfo{name: svc.Name, code: 1}
			continue
		}
		if err := cmd.Start(); err != nil {
			s.logf(mu, "%s: failed to start: %v", svc.Name, err)
			exits <- exitInfo{name: svc.Name, code: 1}
			continue
		}

		pid := cmd.Process.Pid
		s.logf(mu, "%s started (pid %d, port %d)", svc.Name, pid, s.ports[svc.Name])
		procs = append(procs, runningProc{name: svc.Name, pgid: pid})

		// One goroutine per process: drain both pipes to EOF, THEN Wait (the
		// StdoutPipe contract forbids Wait before reads complete), then report.
		go func(svc Service, cmd *exec.Cmd, prefix string, stdout, stderr io.ReadCloser) {
			var swg sync.WaitGroup
			swg.Add(2)
			go func() { defer swg.Done(); streamLines(s.out, mu, prefix, stdout) }()
			go func() { defer swg.Done(); streamLines(s.out, mu, prefix, stderr) }()
			swg.Wait()
			code := exitCodeOf(cmd.Wait())
			exits <- exitInfo{name: svc.Name, pid: cmd.Process.Pid, code: code}
		}(svc, cmd, prefix, stdout, stderr)
	}

	remaining := len(s.services)
	triggerCode := 0

	// Wait for the first trigger: a service exiting (one-exits-all) or a signal.
	select {
	case first := <-exits:
		remaining--
		triggerCode = first.code
		s.logf(mu, "%s exited (pid %d, code %d); shutting down remaining services", first.name, first.pid, first.code)
	case <-ctx.Done():
		s.logf(mu, "received shutdown signal; stopping services")
	}

	s.teardown(mu, procs, exits, &remaining)
	return triggerCode
}

// teardown sends SIGTERM to every service group, waits up to grace for them to
// exit, then SIGKILLs any survivors and drains their exit reports.
func (s *supervisor) teardown(mu *sync.Mutex, procs []runningProc, exits chan exitInfo, remaining *int) {
	for _, p := range procs {
		// Negative pid targets the whole process group. ESRCH (already gone) is
		// harmless.
		_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
	}

	timer := time.NewTimer(s.grace)
	defer timer.Stop()
	for *remaining > 0 {
		select {
		case ex := <-exits:
			*remaining--
			if ex.code != 0 && ex.code != -1 {
				s.logf(mu, "%s exited (code %d)", ex.name, ex.code)
			}
		case <-timer.C:
			s.logf(mu, "grace period elapsed; sending SIGKILL to survivors")
			for _, p := range procs {
				_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
			}
			for *remaining > 0 {
				<-exits
				*remaining--
			}
		}
	}
}
