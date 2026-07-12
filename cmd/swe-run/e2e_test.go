package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests exercise the full runMain entry point end-to-end -- a real
// Procfile on disk, .env / .swe-swe/env loading, port assignment, discovery env
// vars reaching REAL child processes, and the load-bearing guarantee that
// nothing leaks after teardown (including grandchildren, which the supervisor
// unit tests do not check: they only assert run() returns).

// startRunMain writes a Procfile into dir and runs runMain against it in a
// goroutine, returning a cancel func and a channel that yields the exit code.
func startRunMain(t *testing.T, dir, procfile string, base int) (context.CancelFunc, <-chan int, *bytes.Buffer) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "Procfile"), []byte(procfile), 0o644); err != nil {
		t.Fatalf("write Procfile: %v", err)
	}
	getenv := func(k string) string {
		if k == "PORT" {
			return strconv.Itoa(base)
		}
		return ""
	}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		// inherited env carries PATH so sh/sleep/printf resolve; workdir roots
		// the Procfile and env-file lookups at dir.
		done <- runMain(ctx, nil, &out, &out, getenv,
			[]string{"PATH=" + defaultPathForTest()}, dir)
	}()
	return cancel, done, &out
}

// waitForFiles polls until every path exists or the deadline elapses.
func waitForFiles(t *testing.T, out *bytes.Buffer, paths []string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		all := true
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				all = false
				break
			}
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for files %v; runner log:\n%s", paths, out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// processRunning reports whether pid is still a live, non-zombie process. A
// reaped pid (ESRCH) or a zombie (state Z, i.e. dead-but-not-yet-waited) is NOT
// a leak; only a running process is. Linux-only, which matches swe-run's target.
func processRunning(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false // fully reaped
		}
		return true // e.g. EPERM: exists but not ours
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false // vanished between the kill and the read
	}
	// stat is "pid (comm) STATE ..."; comm may contain spaces/parens, so the
	// state is the char two positions after the LAST ')'.
	if i := strings.LastIndexByte(string(data), ')'); i >= 0 && i+2 < len(data) {
		return data[i+2] != 'Z'
	}
	return true
}

// assertAllGone polls until every pid has stopped running, or fails.
func assertAllGone(t *testing.T, pids map[string]int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		var leaked []string
		for label, pid := range pids {
			if processRunning(pid) {
				leaked = append(leaked, fmt.Sprintf("%s(pid %d)", label, pid))
			}
		}
		if len(leaked) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processes leaked after teardown: %s", strings.Join(leaked, ", "))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func readPidFile(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parse pid in %s (%q): %v", path, string(b), err)
	}
	return pid
}

// TestE2E_DiscoveryAndPrimaryPort drives runMain against a real two-service
// Procfile and asserts, from what the child processes actually saw in their
// environment: the primary bound the session base PORT, a sibling learned the
// primary's port via PORT_WEB, each service saw its own port as PORT, and a
// value from a .env file reached the services.
func TestE2E_DiscoveryAndPrimaryPort(t *testing.T) {
	dir := t.TempDir()
	base := 3000

	// .env value must reach every service (foreman parity), verified via web.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GREETING=hello\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Each service records the discovery vars it sees, then blocks on a
	// backgrounded sleep so it is still alive when we cancel (proving teardown
	// actually has to stop it).
	record := func(name string) string {
		f := filepath.Join(dir, name+".env")
		return fmt.Sprintf(`printf 'PORT=%%s\nPORT_WEB=%%s\nPORT_WORKER=%%s\nGREETING=%%s\n' "$PORT" "$PORT_WEB" "$PORT_WORKER" "$GREETING" > %s; sleep 300 & wait`, f)
	}
	procfile := fmt.Sprintf("web: %s\nworker: %s\n", record("web"), record("worker"))

	cancel, done, out := startRunMain(t, dir, procfile, base)
	defer cancel()

	waitForFiles(t, out, []string{filepath.Join(dir, "web.env"), filepath.Join(dir, "worker.env")}, 8*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("runMain did not return after cancel; log:\n%s", out.String())
	}

	// Expected ports straight from the assigner (primary defaults to "web").
	svcs := []Service{{Name: "web"}, {Name: "worker"}}
	ports, err := assignPorts(base, svcs, "")
	if err != nil {
		t.Fatalf("assignPorts: %v", err)
	}

	web := parseEnvSnapshot(t, filepath.Join(dir, "web.env"))
	worker := parseEnvSnapshot(t, filepath.Join(dir, "worker.env"))

	if web["PORT"] != strconv.Itoa(base) {
		t.Errorf("web PORT = %q, want base %d", web["PORT"], base)
	}
	if web["PORT"] != strconv.Itoa(ports["web"]) {
		t.Errorf("web PORT = %q, want assigned %d", web["PORT"], ports["web"])
	}
	if worker["PORT_WEB"] != strconv.Itoa(base) {
		t.Errorf("worker saw PORT_WEB = %q, want the primary base %d", worker["PORT_WEB"], base)
	}
	if worker["PORT"] != strconv.Itoa(ports["worker"]) {
		t.Errorf("worker PORT = %q, want its assigned %d", worker["PORT"], ports["worker"])
	}
	if worker["PORT_WORKER"] != worker["PORT"] {
		t.Errorf("worker PORT_WORKER=%q should equal its own PORT=%q", worker["PORT_WORKER"], worker["PORT"])
	}
	if web["GREETING"] != "hello" {
		t.Errorf("web GREETING = %q, want hello (from .env)", web["GREETING"])
	}
}

// TestE2E_NoGrandchildLeakOnTeardown is the headline guarantee: a service whose
// command spawns a background grandchild (like a wrapper that execs a daemon)
// must have BOTH the shell and the grandchild killed by the process-group
// teardown -- nothing left running on the host after runMain returns.
func TestE2E_NoGrandchildLeakOnTeardown(t *testing.T) {
	dir := t.TempDir()
	base := 3000

	// Each service: spawn a long grandchild, record its pid and the shell's own
	// pid, then wait (stay alive until torn down).
	svc := func(name string) string {
		child := filepath.Join(dir, name+".child")
		self := filepath.Join(dir, name+".self")
		return fmt.Sprintf(`sleep 300 & echo $! > %s; echo $$ > %s; wait`, child, self)
	}
	procfile := fmt.Sprintf("web: %s\nworker: %s\n", svc("web"), svc("worker"))

	cancel, done, out := startRunMain(t, dir, procfile, base)
	defer cancel()

	pidFiles := []string{
		filepath.Join(dir, "web.child"), filepath.Join(dir, "web.self"),
		filepath.Join(dir, "worker.child"), filepath.Join(dir, "worker.self"),
	}
	waitForFiles(t, out, pidFiles, 8*time.Second)

	pids := map[string]int{
		"web.child":    readPidFile(t, filepath.Join(dir, "web.child")),
		"web.self":     readPidFile(t, filepath.Join(dir, "web.self")),
		"worker.child": readPidFile(t, filepath.Join(dir, "worker.child")),
		"worker.self":  readPidFile(t, filepath.Join(dir, "worker.self")),
	}
	// Sanity: they really are running before teardown.
	for label, pid := range pids {
		if !processRunning(pid) {
			t.Fatalf("%s (pid %d) not running before teardown; log:\n%s", label, pid, out.String())
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("runMain did not return after cancel; log:\n%s", out.String())
	}

	// After runMain returns, every process (shells AND grandchildren) must be
	// gone -- this is the no-leak contract.
	assertAllGone(t, pids, 6*time.Second)
}

// TestE2E_EnvFileCannotOverridePorts proves the discovery contract's precedence
// end-to-end: a .env / .swe-swe/env that tries to set PORT or PORT_<NAME> must
// NOT override the runner's assignments -- the child process still sees the
// runner-assigned ports. Non-port vars from the env files still pass through
// (proving the files were actually loaded, so this is precedence, not skipping).
func TestE2E_EnvFileCannotOverridePorts(t *testing.T) {
	dir := t.TempDir()
	base := 3000

	// Both env files try to hijack the port vars with bogus values; .env also
	// carries a normal var that MUST pass through.
	if err := os.MkdirAll(filepath.Join(dir, ".swe-swe"), 0o755); err != nil {
		t.Fatalf("mkdir .swe-swe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".swe-swe", "env"),
		[]byte("PORT=11111\nPORT_WEB=22222\nPORT_WORKER=33333\n"), 0o644); err != nil {
		t.Fatalf("write .swe-swe/env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("PORT=59999\nPORT_WEB=58888\nPORT_WORKER=57777\nGREETING=passthrough\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	record := func(name string) string {
		f := filepath.Join(dir, name+".env")
		return fmt.Sprintf(`printf 'PORT=%%s\nPORT_WEB=%%s\nPORT_WORKER=%%s\nGREETING=%%s\n' "$PORT" "$PORT_WEB" "$PORT_WORKER" "$GREETING" > %s; sleep 300 & wait`, f)
	}
	procfile := fmt.Sprintf("web: %s\nworker: %s\n", record("web"), record("worker"))

	cancel, done, out := startRunMain(t, dir, procfile, base)
	defer cancel()

	waitForFiles(t, out, []string{filepath.Join(dir, "web.env"), filepath.Join(dir, "worker.env")}, 8*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("runMain did not return after cancel; log:\n%s", out.String())
	}

	ports, err := assignPorts(base, []Service{{Name: "web"}, {Name: "worker"}}, "")
	if err != nil {
		t.Fatalf("assignPorts: %v", err)
	}
	web := parseEnvSnapshot(t, filepath.Join(dir, "web.env"))
	worker := parseEnvSnapshot(t, filepath.Join(dir, "worker.env"))

	// The runner-assigned ports win over BOTH env files.
	if web["PORT"] != strconv.Itoa(ports["web"]) {
		t.Errorf("web PORT = %q, want runner-assigned %d (env files must not override)", web["PORT"], ports["web"])
	}
	if worker["PORT"] != strconv.Itoa(ports["worker"]) {
		t.Errorf("worker PORT = %q, want runner-assigned %d (env files must not override)", worker["PORT"], ports["worker"])
	}
	if worker["PORT_WEB"] != strconv.Itoa(ports["web"]) {
		t.Errorf("worker PORT_WEB = %q, want %d (env files must not override)", worker["PORT_WEB"], ports["web"])
	}
	if worker["PORT_WORKER"] != strconv.Itoa(ports["worker"]) {
		t.Errorf("worker PORT_WORKER = %q, want %d (env files must not override)", worker["PORT_WORKER"], ports["worker"])
	}
	// Guard against a false pass where the env files were simply not loaded.
	if web["GREETING"] != "passthrough" {
		t.Errorf("web GREETING = %q, want passthrough -- the .env must still load its non-port vars", web["GREETING"])
	}
	// And make sure none of the bogus hijack values leaked through anywhere.
	for _, bogus := range []string{"11111", "22222", "33333", "59999", "58888", "57777"} {
		for k, v := range web {
			if v == bogus {
				t.Errorf("web %s=%s is a bogus env-file value that should have been overridden", k, v)
			}
		}
		for k, v := range worker {
			if v == bogus {
				t.Errorf("worker %s=%s is a bogus env-file value that should have been overridden", k, v)
			}
		}
	}
}

// parseEnvSnapshot reads a KEY=value file (one per line) into a map.
func parseEnvSnapshot(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = v
		}
	}
	return m
}
