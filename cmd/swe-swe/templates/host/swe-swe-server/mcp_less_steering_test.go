package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Enabled: the steering block lands in CLAUDE.local.md for claude, and only
// there -- CLAUDE.md (committed project memory) is never touched.
func TestSyncMcpLessSteering_WritesLocalMemoryForClaude(t *testing.T) {
	dir := t.TempDir()
	if err := syncMcpLessSteering(dir, "claude", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if err != nil {
		t.Fatalf("CLAUDE.local.md not written: %v", err)
	}
	got := string(b)
	for _, want := range []string{mcpLessSteeringBegin, mcpLessSteeringEnd, "mcp swe-swe-agent-chat check_messages", "mcp -h"} {
		if !strings.Contains(got, want) {
			t.Errorf("steering missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md must not be created by the steering sync")
	}
}

// Re-running replaces the block rather than appending a second copy, and
// leaves the user's own content around it intact.
func TestSyncMcpLessSteering_IdempotentAndPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(path, []byte("my private notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := syncMcpLessSteering(dir, "claude", true); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if n := strings.Count(got, mcpLessSteeringBegin); n != 1 {
		t.Errorf("want exactly 1 steering block, got %d:\n%s", n, got)
	}
	if !strings.HasPrefix(got, "my private notes\n") {
		t.Errorf("user content not preserved:\n%s", got)
	}
}

// Disabled: the block is removed; the file goes away when it held nothing
// else, and survives (block-free) when the user had their own notes in it.
func TestSyncMcpLessSteering_DisabledRemovesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.local.md")
	if err := syncMcpLessSteering(dir, "claude", true); err != nil {
		t.Fatal(err)
	}
	if err := syncMcpLessSteering(dir, "claude", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("steering-only CLAUDE.local.md should be deleted when MCP-less is off")
	}

	if err := os.WriteFile(path, []byte("keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := syncMcpLessSteering(dir, "claude", true); err != nil {
		t.Fatal(err)
	}
	if err := syncMcpLessSteering(dir, "claude", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file with user content must survive: %v", err)
	}
	if got := string(b); got != "keep me\n" {
		t.Errorf("want only user content back, got:\n%s", got)
	}
}

// Disabled with nothing present is a no-op: no file is created.
func TestSyncMcpLessSteering_DisabledNoFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := syncMcpLessSteering(dir, "claude", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Error("no file should be created when disabled")
	}
}

// Only claude has a machine-local memory file; other assistants and an empty
// workDir are skipped without touching the filesystem.
func TestSyncMcpLessSteering_SkipsNonClaudeAndEmptyWorkDir(t *testing.T) {
	dir := t.TempDir()
	for _, a := range []string{"pi", "codex", "shell", ""} {
		if err := syncMcpLessSteering(dir, a, true); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("non-claude assistants must write nothing, got %d entries", len(entries))
	}
	if err := syncMcpLessSteering("", "claude", true); err != nil {
		t.Fatalf("empty workDir: %v", err)
	}
}

// The socket root plus a full uuid plus the longest server socket name must
// fit sun_path, or every proxy dies on bind (seen live: the per-project
// metadata dir alone pushed a dockerless path to 121 bytes).
func TestMcpLessSocketRoot_FitsSunPath(t *testing.T) {
	t.Setenv("TMPDIR", "")
	longest := ""
	for _, spec := range mcpLessProxySpecs("chat") {
		if len(spec.socketName()) > len(longest) {
			longest = spec.socketName()
		}
	}
	path := filepath.Join(mcpLessSocketRoot(), "2c286dfa-93c6-4eb0-8808-249daee0aea8", longest)
	if len(path) >= unixSocketPathMax {
		t.Errorf("socket path %d bytes >= %d: %s", len(path), unixSocketPathMax, path)
	}
	if !strings.HasPrefix(mcpLessSocketRoot(), os.TempDir()) {
		t.Errorf("socket root %s not under the temp dir", mcpLessSocketRoot())
	}
}
