package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Dockerless runs the host-native binaries directly; today those binaries
// are Linux-only (abstract-socket broker + GNU `script` recording flags), so
// `swe-swe init --dockerless` must refuse on a non-Linux CLI rather than
// dump binaries that cannot run. Mac-native support is Phase 6.
func TestDockerlessGOOSGuard(t *testing.T) {
	// Linux + macOS are supported (macOS is experimental, Phase 6); other
	// platforms are still refused.
	for _, goos := range []string{"linux", "darwin"} {
		if err := dockerlessGOOSGuard(goos); err != nil {
			t.Errorf("dockerlessGOOSGuard(%q) = %v, want nil", goos, err)
		}
	}
	for _, goos := range []string{"windows", "freebsd"} {
		if err := dockerlessGOOSGuard(goos); err == nil {
			t.Errorf("dockerlessGOOSGuard(%q) = nil, want error (unsupported platform)", goos)
		}
	}
}

// extractDockerlessBinaries dumps the embedded static-Linux binaries onto
// disk; init --dockerless calls it to populate .swe-swe/bin. Each file must
// land executable (0755) and byte-identical to the embed.
func TestExtractDockerlessBinaries(t *testing.T) {
	// The Makefile builds the host arch into the embed; on this Linux CI
	// host that is runtime.GOARCH.
	dest := t.TempDir()
	if err := extractDockerlessBinaries(dest, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("extractDockerlessBinaries: %v", err)
	}
	for _, name := range dockerlessBinaries {
		p := filepath.Join(dest, name)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: not extracted: %v", name, err)
			continue
		}
		if info.Mode().Perm()&0111 == 0 {
			t.Errorf("%s: mode %v is not executable", name, info.Mode().Perm())
		}
		want, err := dockerlessPayload.ReadFile(filepath.Join(dockerlessPayloadBinDir(runtime.GOOS, runtime.GOARCH), name))
		if err != nil {
			t.Fatalf("read embed %s: %v", name, err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read extracted %s: %v", name, err)
		}
		if len(got) != len(want) {
			t.Errorf("%s: extracted %d bytes, embed has %d", name, len(got), len(want))
		}
	}
}

// The mode marker is how `swe-swe up` decides to run the host-native server
// instead of docker compose. A fresh metadata dir is not dockerless; one
// written by writeDockerlessMarker is.
func TestDockerlessMarker(t *testing.T) {
	sweDir := t.TempDir()
	if isDockerlessProject(sweDir) {
		t.Errorf("fresh metadata dir reported as dockerless")
	}
	if err := writeDockerlessMarker(sweDir); err != nil {
		t.Fatalf("writeDockerlessMarker: %v", err)
	}
	if !isDockerlessProject(sweDir) {
		t.Errorf("after writeDockerlessMarker, isDockerlessProject = false")
	}
	// A metadata dir that does not exist is not dockerless (no panic).
	if isDockerlessProject(filepath.Join(sweDir, "does-not-exist")) {
		t.Errorf("missing dir reported as dockerless")
	}
}

// Regression: a dockerless -> docker re-init must clear the mode marker,
// otherwise every subsequent `swe-swe up` still routes to the (broken)
// dockerless path and the user is locked out of compose mode until they
// delete the marker by hand. executeInit calls clearDockerlessMarker.
func TestClearDockerlessMarker(t *testing.T) {
	sweDir := t.TempDir()
	if err := writeDockerlessMarker(sweDir); err != nil {
		t.Fatalf("writeDockerlessMarker: %v", err)
	}
	if err := clearDockerlessMarker(sweDir); err != nil {
		t.Fatalf("clearDockerlessMarker: %v", err)
	}
	if isDockerlessProject(sweDir) {
		t.Errorf("after clearDockerlessMarker, isDockerlessProject = true")
	}
	// Idempotent: clearing an already-clear (or never-dockerless) dir is not
	// an error.
	if err := clearDockerlessMarker(sweDir); err != nil {
		t.Errorf("clearDockerlessMarker on clean dir: %v", err)
	}
	if err := clearDockerlessMarker(filepath.Join(sweDir, "does-not-exist")); err != nil {
		t.Errorf("clearDockerlessMarker on missing dir: %v", err)
	}
}

// `swe-swe up` on a dockerless project execs the dumped server with the
// project as working dir, the dumped bin/ on PATH, and a loopback bind.
func TestDockerlessServerInvocation(t *testing.T) {
	sweDir := "/home/u/.swe-swe/projects/proj"
	absPath := "/work/proj"
	bin, args, env := dockerlessServerInvocation(sweDir, absPath, "1977", []string{"PATH=/usr/bin", "HOME=/home/u"}, tunnelConfig{})

	if want := filepath.Join(sweDir, "bin", "swe-swe-server"); bin != want {
		t.Errorf("bin = %q, want %q", bin, want)
	}
	if !argsContainPair(args, "-working-directory", absPath) {
		t.Errorf("args %v missing -working-directory %s", args, absPath)
	}
	// Host-native paths wired through to the server.
	if !argsContainPair(args, "-workspace", absPath) {
		t.Errorf("args %v missing -workspace %s", args, absPath)
	}
	if !argsContainPair(args, "-swe-home", sweDir) {
		t.Errorf("args %v missing -swe-home %s", args, sweDir)
	}
	if !argsContainPair(args, "-worktrees", filepath.Join(sweDir, "worktrees")) {
		t.Errorf("args %v missing -worktrees", args)
	}
	if !argsContainPair(args, "-repos", filepath.Join(sweDir, "repos")) {
		t.Errorf("args %v missing -repos", args)
	}
	// Binds loopback on the chosen port by default (no surprise LAN exposure).
	if !argsContainValue(args, "127.0.0.1:1977") {
		t.Errorf("args %v missing loopback bind 127.0.0.1:1977", args)
	}
	// bin/ is prepended to PATH so the git/helper shims resolve.
	binDir := filepath.Join(sweDir, "bin")
	foundPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") && strings.HasPrefix(strings.TrimPrefix(e, "PATH="), binDir) {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("env %v has no PATH starting with %s", env, binDir)
	}
}

// init --dockerless writes the swe-swe-open shim (executable) plus the
// xdg-open/open/... symlinks into bin/, and SWE_SERVER_PORT is wired into the
// server env so the shim resolves the preview endpoint.
func TestWriteDockerlessOpenShim(t *testing.T) {
	binDir := t.TempDir()
	if err := writeDockerlessOpenShim(binDir); err != nil {
		t.Fatalf("writeDockerlessOpenShim: %v", err)
	}
	shim := filepath.Join(binDir, "swe-swe-open")
	fi, err := os.Stat(shim)
	if err != nil {
		t.Fatalf("swe-swe-open missing: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("swe-swe-open not executable: %v", fi.Mode())
	}
	for _, name := range dockerlessOpenShimNames {
		target, err := os.Readlink(filepath.Join(binDir, name))
		if err != nil {
			t.Errorf("%s not a symlink: %v", name, err)
			continue
		}
		if target != "swe-swe-open" {
			t.Errorf("%s -> %q, want swe-swe-open", name, target)
		}
	}
	// Idempotent: a second call must not error on existing symlinks.
	if err := writeDockerlessOpenShim(binDir); err != nil {
		t.Fatalf("re-run writeDockerlessOpenShim: %v", err)
	}
}

// init --dockerless writes a project-scoped .mcp.json with the five swe-swe
// MCP servers, preserving the `sh -c` form so session env vars expand at
// launch.
func TestWriteDockerlessMCPConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeDockerlessMCPConfig(dir); err != nil {
		t.Fatalf("writeDockerlessMCPConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var doc struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, name := range []string{"swe-swe-agent-chat", "swe-swe-playwright", "swe-swe-preview", "swe-swe-whiteboard", "swe-swe"} {
		if _, ok := doc.MCPServers[name]; !ok {
			t.Errorf("missing MCP server %q", name)
		}
	}
	// agent-chat keeps the sh -c form with the autocomplete env-var URL.
	ac := doc.MCPServers["swe-swe-agent-chat"]
	if ac.Command != "sh" || len(ac.Args) != 2 || !strings.Contains(ac.Args[1], "$SWE_SERVER_PORT") {
		t.Errorf("agent-chat spec not preserved: %+v", ac)
	}
	// whiteboard is a plain swe-npx command (no shell, no env vars needed).
	if wb := doc.MCPServers["swe-swe-whiteboard"]; wb.Command != "swe-npx" {
		t.Errorf("whiteboard command = %q, want swe-npx", wb.Command)
	}
}

// With no tunnel config, no tunnel flags are passed. With a tunnel serverURL,
// -tunnel-server-url + -tunnel-bin (pointing at the dumped client) are added.
func TestDockerlessServerInvocationTunnel(t *testing.T) {
	sweDir := "/home/u/.swe-swe/projects/proj"
	// Disabled: no tunnel args.
	_, args, _ := dockerlessServerInvocation(sweDir, "/p", "1977", nil, tunnelConfig{})
	if argsContainValue(args, "-tunnel-server-url") {
		t.Errorf("unexpected tunnel args when disabled: %v", args)
	}
	// Enabled.
	_, args, _ = dockerlessServerInvocation(sweDir, "/p", "1977", nil,
		tunnelConfig{serverURL: "https://tunnel.example.com", clientCert: "/c.pem"})
	if !argsContainPair(args, "-tunnel-server-url", "https://tunnel.example.com") {
		t.Errorf("args %v missing -tunnel-server-url", args)
	}
	if !argsContainPair(args, "-tunnel-bin", filepath.Join(sweDir, "bin", "swe-swe-tunnel")) {
		t.Errorf("args %v missing -tunnel-bin pointing at dumped client", args)
	}
	if !argsContainPair(args, "-tunnel-client-cert", "/c.pem") {
		t.Errorf("args %v missing -tunnel-client-cert", args)
	}
	// Regression: --tunnel-local-ports is compose-only port publishing;
	// swe-swe-server has no such flag and exits 2 (usage dump) if passed.
	if argsContainValue(args, "-tunnel-local-ports") {
		t.Errorf("args %v must not contain -tunnel-local-ports (server has no such flag)", args)
	}
}

func TestDockerlessServerInvocationSetsServerPort(t *testing.T) {
	_, _, env := dockerlessServerInvocation("/s", "/p", "1977", []string{"PATH=/usr/bin"}, tunnelConfig{})
	found := false
	for _, e := range env {
		if e == "SWE_SERVER_PORT=1977" {
			found = true
		}
	}
	if !found {
		t.Errorf("env %v missing SWE_SERVER_PORT=1977", env)
	}
}

func argsContainValue(args []string, v string) bool {
	for _, a := range args {
		if a == v {
			return true
		}
	}
	return false
}

func argsContainPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

// writeDockerlessHooks drops both guard scripts into <sweDir>/hooks and wires
// them into the project's .claude/settings.local.json, preserving foreign
// keys/hooks and staying idempotent across re-init.
func TestWriteDockerlessHooks(t *testing.T) {
	projectDir := t.TempDir()
	sweDir := t.TempDir()

	// Pre-existing settings with a foreign key, a foreign Stop hook, and a
	// stale AskUserQuestion entry that must be replaced, not duplicated.
	pre := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "old-guard"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-linter"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "my-notifier"}]}
    ]
  }
}`
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(projectDir, ".claude", "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	// Twice: second run must not duplicate entries.
	for i := 0; i < 2; i++ {
		if err := writeDockerlessHooks(projectDir, sweDir); err != nil {
			t.Fatalf("writeDockerlessHooks run %d: %v", i+1, err)
		}
	}

	for _, name := range []string{"swe-swe-stop-guard.sh", "swe-swe-ask-guard.sh"} {
		fi, err := os.Stat(filepath.Join(sweDir, "hooks", name))
		if err != nil {
			t.Fatalf("hook script %s: %v", name, err)
		}
		if fi.Mode()&0o111 == 0 {
			t.Errorf("hook script %s not executable", name)
		}
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("settings.local.json invalid: %v", err)
	}
	if root["model"] != "opus" {
		t.Errorf("foreign key dropped: model = %v", root["model"])
	}
	hooks := root["hooks"].(map[string]any)
	pre2 := hooks["PreToolUse"].([]any)
	if len(pre2) != 2 {
		t.Fatalf("PreToolUse len = %d, want 2 (foreign + ours): %v", len(pre2), pre2)
	}
	joined := fmt.Sprint(pre2)
	if !strings.Contains(joined, "my-linter") || !strings.Contains(joined, "swe-swe-ask-guard.sh") || strings.Contains(joined, "old-guard") {
		t.Errorf("PreToolUse merge wrong: %s", joined)
	}
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop len = %d, want 2 (foreign + ours): %v", len(stop), stop)
	}
	joined = fmt.Sprint(stop)
	if !strings.Contains(joined, "my-notifier") || !strings.Contains(joined, "swe-swe-stop-guard.sh") {
		t.Errorf("Stop merge wrong: %s", joined)
	}
}
