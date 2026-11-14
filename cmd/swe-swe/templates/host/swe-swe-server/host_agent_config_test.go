package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withHostAgentConfig points the server at a temp swe home holding the given
// agent-config files, as `swe-swe up` would have written them.
func withHostAgentConfig(t *testing.T, host bool, files ...string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "agent-config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldHost, oldHome := hostRuntime, sweHomeDir
	hostRuntime, sweHomeDir = host, home
	t.Cleanup(func() { hostRuntime, sweHomeDir = oldHost, oldHome })
	return dir
}

// The bug: on a host runtime a new project / clone had no swe-swe MCP
// servers or guards, because they were only written into the init
// workspace's files. Now every Claude launch, in any folder, gets them.
func TestHostAgentArgsClaude(t *testing.T) {
	dir := withHostAgentConfig(t, true, hostClaudeMCPFile, hostClaudeSettingsFile)

	got := hostAgentArgs("claude", []string{"--dangerously-skip-permissions", "--continue"})
	want := []string{
		"--mcp-config", filepath.Join(dir, hostClaudeMCPFile),
		"--settings", filepath.Join(dir, hostClaudeSettingsFile),
		"--dangerously-skip-permissions", "--continue",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claude args = %v\nwant %v", got, want)
	}
}

// MCP-less mode: no MCP config file, but the hook guards still load.
func TestHostAgentArgsClaudeMCPLess(t *testing.T) {
	dir := withHostAgentConfig(t, true, hostClaudeSettingsFile)

	got := hostAgentArgs("claude", nil)
	want := []string{"--settings", filepath.Join(dir, hostClaudeSettingsFile)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claude args = %v, want %v", got, want)
	}
}

// --mcp-config is variadic: alone, it must use the = form so it cannot
// swallow a later positional argument.
func TestHostAgentArgsClaudeMCPOnlyUsesEqualsForm(t *testing.T) {
	dir := withHostAgentConfig(t, true, hostClaudeMCPFile)

	got := hostAgentArgs("claude", []string{"hello"})
	want := []string{"--mcp-config=" + filepath.Join(dir, hostClaudeMCPFile), "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claude args = %v, want %v", got, want)
	}
}

func TestHostAgentArgsPi(t *testing.T) {
	dir := withHostAgentConfig(t, true, hostPiBridgeFile)

	got := hostAgentArgs("/usr/bin/pi", []string{"--continue"})
	want := []string{"-e", filepath.Join(dir, hostPiBridgeFile), "--continue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pi args = %v, want %v", got, want)
	}
}

// The container keeps using ~/.claude.json: no flags, even if a stray
// agent-config dir exists.
func TestHostAgentArgsContainerUnchanged(t *testing.T) {
	withHostAgentConfig(t, false, hostClaudeMCPFile, hostClaudeSettingsFile, hostPiBridgeFile)

	for _, bin := range []string{"claude", "pi", "codex", "gemini"} {
		in := []string{"--continue"}
		if got := hostAgentArgs(bin, in); !reflect.DeepEqual(got, in) {
			t.Errorf("container %s args = %v, want unchanged %v", bin, got, in)
		}
	}
}

func TestHostAgentArgsOtherAgentsUnchanged(t *testing.T) {
	withHostAgentConfig(t, true, hostClaudeMCPFile, hostClaudeSettingsFile, hostPiBridgeFile)

	for _, bin := range []string{"codex", "gemini", "opencode", "goose", "aider", "bash"} {
		in := []string{"--x"}
		if got := hostAgentArgs(bin, in); !reflect.DeepEqual(got, in) {
			t.Errorf("%s args = %v, want unchanged", bin, got)
		}
	}
}

func TestHostAgentEnv(t *testing.T) {
	dir := withHostAgentConfig(t, true, hostOpencodeFile, hostGeminiDefaultsFile)
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("GEMINI_CLI_SYSTEM_DEFAULTS_PATH", "")

	got := hostAgentEnv()
	want := []string{
		"OPENCODE_CONFIG=" + filepath.Join(dir, hostOpencodeFile),
		"GEMINI_CLI_SYSTEM_DEFAULTS_PATH=" + filepath.Join(dir, hostGeminiDefaultsFile),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env = %v\nwant %v", got, want)
	}
}

// A user's own OPENCODE_CONFIG names their file; replacing it would drop
// their config.
func TestHostAgentEnvKeepsUserValue(t *testing.T) {
	withHostAgentConfig(t, true, hostOpencodeFile)
	t.Setenv("OPENCODE_CONFIG", "/mine/opencode.json")

	for _, e := range hostAgentEnv() {
		if strings.HasPrefix(e, "OPENCODE_CONFIG=") {
			t.Errorf("overrode the user's OPENCODE_CONFIG: %s", e)
		}
	}
}

func TestHostAgentEnvContainerEmpty(t *testing.T) {
	withHostAgentConfig(t, false, hostOpencodeFile, hostGeminiDefaultsFile)
	if got := hostAgentEnv(); len(got) != 0 {
		t.Errorf("container env = %v, want none", got)
	}
}
