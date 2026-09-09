package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLookPath builds an exec.LookPath stand-in that resolves exactly the
// named programs.
func stubLookPath(present ...string) func(string) (string, error) {
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestMissingDisplayStack(t *testing.T) {
	tests := []struct {
		name    string
		present []string
		want    []string
	}{
		{
			name:    "everything present, chromium as chromium",
			present: []string{"Xvfb", "x11vnc", "websockify", "chromium"},
			want:    nil,
		},
		{
			// Ubuntu's historical name has to satisfy the chromium
			// requirement, or the command refuses to run on a box that is
			// perfectly capable.
			name:    "chromium-browser counts as chromium",
			present: []string{"Xvfb", "x11vnc", "websockify", "chromium-browser"},
			want:    nil,
		},
		{
			name:    "nothing installed",
			present: nil,
			want:    []string{"Xvfb", "x11vnc", "websockify", "chromium"},
		},
		{
			name:    "only chromium missing",
			present: []string{"Xvfb", "x11vnc", "websockify"},
			want:    []string{"chromium"},
		},
		{
			name:    "only the X pieces missing",
			present: []string{"chromium"},
			want:    []string{"Xvfb", "x11vnc", "websockify"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingDisplayStack(stubLookPath(tt.present...))
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("missingDisplayStack() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The error is the whole user-facing value of checking before exec, so it has
// to name what is missing AND how to get it.
func TestBrowserBackendMissingStackErrorNamesPackages(t *testing.T) {
	err := browserBackendMissingStackError([]string{"Xvfb", "chromium"})
	msg := err.Error()
	for _, want := range []string{"Xvfb", "chromium", "apt-get install", "novnc", "websockify"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}

func TestBrowserBackendAddrFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "unrelated flags only", args: []string{"-browser-backend-max", "4"}, want: false},
		{name: "-bind separate value", args: []string{"-bind", "0.0.0.0:9999"}, want: true},
		{name: "--bind= joined value", args: []string{"--bind=0.0.0.0:9999"}, want: true},
		{name: "-addr separate value", args: []string{"-addr", ":8080"}, want: true},
		{name: "-addr= joined value", args: []string{"-addr=:8080"}, want: true},
		{name: "listen flag after other flags", args: []string{"-browser-backend-max", "4", "-bind", ":9000"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserBackendAddrFlag(tt.args); got != tt.want {
				t.Errorf("browserBackendAddrFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// Regression, found by live smoke twice: the command was launched from a shell
// that had SWE_PORT and SWE_BIND exported for an unrelated swe-swe, and the
// backend silently bound :1977 instead of :9333. No environment variable is a
// statement about THIS service, so none of them may reach the child.
func TestBrowserBackendPortEnvClearsInheritedListenVars(t *testing.T) {
	env := []string{
		"SWE_BIND=:1977",
		"SWE_PORT=1977",
		"PORT=11817",
	}
	got := browserBackendPortEnv(env, "9333")

	if len(got) != 1 || got[0] != "SWE_PORT=9333" {
		t.Errorf("env = %v, want exactly [SWE_PORT=9333]", got)
	}
}

func TestBrowserBackendPortEnv(t *testing.T) {
	env := []string{
		"HOME=/home/app",
		"SWE_BIND=:1977",
		"SWE_PORT=1977",
		"PORT=11817",
		"SWE_BROWSER_BACKEND_TOKEN=secret",
		"PORTAL=keep-me",
	}
	got := browserBackendPortEnv(env, "9333")

	var swePorts int
	for _, e := range got {
		if strings.HasPrefix(e, "SWE_PORT=") {
			swePorts++
			if e != "SWE_PORT=9333" {
				t.Errorf("SWE_PORT = %q, want SWE_PORT=9333", e)
			}
		}
		for _, gone := range []string{"SWE_BIND=", "PORT="} {
			if strings.HasPrefix(e, gone) {
				t.Errorf("inherited %s survived as %q", gone, e)
			}
		}
	}
	if swePorts != 1 {
		t.Errorf("got %d SWE_PORT entries, want exactly 1 (duplicates resolve unpredictably)", swePorts)
	}

	// Only the listen keys go; everything else, including a variable that
	// merely starts with PORT, survives untouched.
	for _, want := range []string{"HOME=/home/app", "SWE_BROWSER_BACKEND_TOKEN=secret", "PORTAL=keep-me"} {
		found := false
		for _, e := range got {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env lost %q", want)
		}
	}
}

// The service must not masquerade as a project: `swe-swe list` enumerates
// ~/.swe-swe/projects and prunes what it cannot find, so a backend parked
// there would be listed and eventually swept.
func TestBrowserBackendHomeDirIsNotAProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := browserBackendHomeDir()
	if err != nil {
		t.Fatalf("browserBackendHomeDir() error: %v", err)
	}
	want := filepath.Join(home, ".swe-swe", "browser-backend")
	if dir != want {
		t.Errorf("browserBackendHomeDir() = %q, want %q", dir, want)
	}
	if strings.Contains(dir, filepath.Join(".swe-swe", "projects")) {
		t.Errorf("browser-backend home %q sits under the projects dir", dir)
	}
}

func TestExtractBrowserBackendServer(t *testing.T) {
	// The embedded payload is only present in a built tree; skip rather than
	// fail on a fresh checkout where .gitkeep is all there is.
	src := filepath.Join(dockerlessPayloadBinDir("linux", "amd64"), "swe-swe-server")
	if _, err := dockerlessPayload.ReadFile(src); err != nil {
		t.Skipf("no linux/amd64 payload embedded in this build: %v", err)
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	got, err := extractBrowserBackendServer(binDir, "linux", "amd64")
	if err != nil {
		t.Fatalf("extractBrowserBackendServer() error: %v", err)
	}
	want := filepath.Join(binDir, "swe-swe-server")
	if got != want {
		t.Errorf("returned path = %q, want %q", got, want)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat %s: %v", got, err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("extracted server is not executable: mode %v", info.Mode())
	}
	if info.Size() == 0 {
		t.Error("extracted server is empty")
	}

	// Re-running must be idempotent: the machine's start-up script calls this
	// on every boot, over a file the previous boot already wrote.
	if _, err := extractBrowserBackendServer(binDir, "linux", "amd64"); err != nil {
		t.Errorf("second extract failed: %v", err)
	}

	// Only the server: the credential broker, tunnel client and mcp proxies
	// have no role on a machine that runs no sessions.
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatalf("read %s: %v", binDir, err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("extracted %d files (%v), want only swe-swe-server", len(entries), names)
	}
}

func TestExtractBrowserBackendServerUnknownPlatform(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	_, err := extractBrowserBackendServer(binDir, "plan9", "mips")
	if err == nil {
		t.Fatal("expected an error for a platform with no payload")
	}
	if !strings.Contains(err.Error(), "plan9/mips") {
		t.Errorf("error should name the platform, got: %v", err)
	}
}
