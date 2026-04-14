package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadEnvFileExpandsAgainstSessionEnv verifies the regression where a
// PATH=/x:$PATH line in .swe-swe/env used to expand $PATH against the
// server's env (which doesn't have the swe-swe PATH prefixes), silently
// dropping /home/app/.swe-swe/bin from the session PATH. Subsequent npx /
// binary lookups then resolved to the wrong binary or failed outright,
// causing MCP servers to fail to start with no obvious signal.
func TestLoadEnvFileExpandsAgainstSessionEnv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env")
	if err := os.WriteFile(envPath, []byte("PATH=/usr/local/go/bin:$PATH\n"), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	sessionEnv := []string{
		"PATH=/swe-swe/bin:/usr/bin",
		"OTHER=1",
	}

	got := loadEnvFile(envPath, envLookup(sessionEnv))
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(got), got)
	}
	want := "PATH=/usr/local/go/bin:/swe-swe/bin:/usr/bin"
	if got[0] != want {
		t.Errorf("loadEnvFile:\n  got  %q\n  want %q", got[0], want)
	}
}

// TestLoadEnvFileFallsBackToOsGetenv verifies that $VAR references that
// aren't present in the session env fall through to the server's env.
func TestLoadEnvFileFallsBackToOsGetenv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env")
	if err := os.WriteFile(envPath, []byte("GREETING=hello $WHO\n"), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	t.Setenv("WHO", "world")

	got := loadEnvFile(envPath, envLookup([]string{"PATH=/ignored"}))
	if len(got) != 1 || got[0] != "GREETING=hello world" {
		t.Errorf("expected GREETING=hello world, got %v", got)
	}
}

// TestLoadEnvFileLocalReferencesWinOverSessionEnv verifies that an earlier
// line in the env file shadows the session env for later references (the
// existing semantic, preserved by the patch).
func TestLoadEnvFileLocalReferencesWinOverSessionEnv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env")
	content := "WHO=local\nGREETING=hello $WHO\n"
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	sessionEnv := []string{"WHO=session"}
	got := loadEnvFile(envPath, envLookup(sessionEnv))
	if len(got) != 2 || got[1] != "GREETING=hello local" {
		t.Errorf("expected GREETING=hello local, got %v", got)
	}
}

// TestEnvLookupReturnsLastValue verifies that when the session env has
// duplicate keys (which can happen as later appends override earlier ones),
// envLookup returns the last value, matching exec's "last-one-wins" rule.
func TestEnvLookupReturnsLastValue(t *testing.T) {
	env := []string{"FOO=first", "BAR=1", "FOO=last"}
	if v := envLookup(env)("FOO"); v != "last" {
		t.Errorf("envLookup FOO: got %q, want %q", v, "last")
	}
}

// TestLoadEnvFileNilLookupFallsBackToOsGetenv verifies the nil-lookup guard
// so existing callers (and future ones that don't care about session env)
// still work.
func TestLoadEnvFileNilLookupFallsBackToOsGetenv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env")
	if err := os.WriteFile(envPath, []byte("X=$HOME\n"), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("HOME", "/root")
	got := loadEnvFile(envPath, nil)
	if len(got) != 1 || got[0] != "X=/root" {
		t.Errorf("expected X=/root, got %v", got)
	}
}
