package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGitCredHelperEnv verifies the pure env-builder appends exactly the three
// GIT_CONFIG vars that wire git to the swe-swe credential helper, leaving the
// base env untouched.
func TestGitCredHelperEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/app"}
	got := gitCredHelperEnv(base)

	// Base entries preserved.
	for _, want := range base {
		if !envContains(got, want) {
			t.Errorf("gitCredHelperEnv dropped base var %q", want)
		}
	}
	// Helper wiring appended.
	for _, want := range []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=swe-swe",
	} {
		if !envContains(got, want) {
			t.Errorf("gitCredHelperEnv missing %q", want)
		}
	}
}

// TestRunGitWithTransientCredBare verifies that when token == "" the child git
// runs with the inherited env and NO credential.helper wiring -- identical to
// the old bare `git clone` behavior.
func TestRunGitWithTransientCredBare(t *testing.T) {
	// This process may itself run inside a credential-wired session shell
	// (the dev container does). Neutralize the ambient wiring so the test
	// asserts only that the BARE path adds none of its own.
	for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if v, ok := os.LookupEnv(k); ok {
			os.Unsetenv(k)
			kk, vv := k, v
			t.Cleanup(func() { os.Setenv(kk, vv) })
		}
	}

	dir := t.TempDir()
	envFile := filepath.Join(dir, "child.env")
	fakeGit(t, dir, envFile, 0, "")
	withPath(t, dir)

	out, err := runGitWithTransientCred("", "", "", "config", "--list")
	if err != nil {
		t.Fatalf("bare run returned error: %v, output=%s", err, out)
	}
	childEnv := readFile(t, envFile)
	if strings.Contains(childEnv, "GIT_CONFIG_VALUE_0=swe-swe") {
		t.Errorf("bare run leaked credential.helper wiring into child env:\n%s", childEnv)
	}
}

// TestRunGitWithTransientCredWiresAndClears verifies the token path:
//   - a prep-* transient sid is minted and registered for the clone pid, so
//     the broker ancestry walk (findSessionForPID) resolves it WHILE git runs;
//   - the credential is stored for the requested host during the run;
//   - both the credential and the pid registration are torn down afterwards,
//     even when git exits non-zero (defer coverage).
func TestRunGitWithTransientCredWiresAndClears(t *testing.T) {
	// Deterministic transient id so the test can inspect the cred store.
	const fixedID = "prep-testfixedid"
	orig := newTransientID
	newTransientID = func() string { return fixedID }
	defer func() { newTransientID = orig }()
	defer clearSessionCredentials(fixedID)

	dir := t.TempDir()
	startedFile := filepath.Join(dir, "started") // fake git writes its pid here
	gateFile := filepath.Join(dir, "gate")       // fake git waits until this exists
	// Fake git: record pid, block on the gate, then exit non-zero.
	fakeGitGated(t, dir, startedFile, gateFile)
	withPath(t, dir)

	host := "github.example.com"
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := runGitWithTransientCred(host, "", "ghp_secrettoken", "clone", "x", "y")
		done <- result{out, err}
	}()

	// Wait for the child to report it has started.
	gitPid := waitForPid(t, startedFile)

	// WHILE the clone runs: the credential must be resolvable via the exact
	// path the broker uses -- (transient sid, host) lookup, and the ancestry
	// walk from the clone pid must map back to the transient sid.
	c, ok := getCredential(fixedID, host)
	if !ok {
		t.Errorf("credential not stored for transient sid during run")
	} else {
		if c.Token != "ghp_secrettoken" {
			t.Errorf("stored token = %q, want ghp_secrettoken", c.Token)
		}
		if c.Username != "x-access-token" {
			t.Errorf("stored username = %q, want x-access-token (default)", c.Username)
		}
	}
	if sid := findSessionForPID(gitPid); sid != fixedID {
		t.Errorf("findSessionForPID(%d) = %q during run, want %q", gitPid, sid, fixedID)
	}

	// Release the gate; git exits non-zero.
	if err := os.WriteFile(gateFile, []byte("go"), 0644); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.err == nil {
		t.Errorf("expected non-zero git exit to surface as error, got nil")
	}

	// AFTER the run: everything torn down despite the non-zero exit.
	if _, ok := getCredential(fixedID, host); ok {
		t.Errorf("credential not cleared after run")
	}
	pidToSidMu.RLock()
	_, stillRegistered := pidToSid[gitPid]
	pidToSidMu.RUnlock()
	if stillRegistered {
		t.Errorf("clone pid %d still registered after run", gitPid)
	}
}

// --- helpers ---

func envContains(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// withPath prepends dir to PATH for the duration of the test so exec.Command
// resolves the fake `git` there.
func withPath(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
}

// fakeGit writes a `git` script into dir that dumps its env to envFile and
// exits with exitCode after printing stdoutMsg.
func fakeGit(t *testing.T, dir, envFile string, exitCode int, stdoutMsg string) {
	t.Helper()
	script := "#!/bin/sh\nenv > '" + envFile + "'\n"
	if stdoutMsg != "" {
		script += "echo '" + stdoutMsg + "'\n"
	}
	script += "exit " + itoa(exitCode) + "\n"
	writeExec(t, filepath.Join(dir, "git"), script)
}

// fakeGitGated writes a `git` script that records its own pid to startedFile,
// busy-waits until gateFile exists, then exits non-zero (simulating an auth
// failure after the credential helper has had a chance to run).
func fakeGitGated(t *testing.T, dir, startedFile, gateFile string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo $$ > '" + startedFile + "'\n" +
		"while [ ! -f '" + gateFile + "' ]; do sleep 0.02; done\n" +
		"exit 1\n"
	writeExec(t, filepath.Join(dir, "git"), script)
}

func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func waitForPid(t *testing.T, startedFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(startedFile)
		if err == nil {
			if pid := atoi(strings.TrimSpace(string(b))); pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake git never reported its pid via %s", startedFile)
	return 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
