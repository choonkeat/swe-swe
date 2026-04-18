package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertAgentDocInclude_CreatesFileForClaude(t *testing.T) {
	dir := t.TempDir()

	if err := upsertAgentDocInclude(dir, "claude"); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if !strings.Contains(string(b), agentContextIncludeLine) {
		t.Errorf("include line missing; got:\n%s", b)
	}
	if filepath.Base(filepath.Clean(dir)) == "AGENTS.md" {
		t.Error("unexpected: dir looks like AGENTS.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("AGENTS.md should not be created for claude")
	}
}

func TestUpsertAgentDocInclude_UsesAgentsMdForNonClaude(t *testing.T) {
	for _, assistant := range []string{"codex", "aider", "goose", "opencode", "gemini"} {
		t.Run(assistant, func(t *testing.T) {
			dir := t.TempDir()
			if err := upsertAgentDocInclude(dir, assistant); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
			if err != nil {
				t.Fatalf("AGENTS.md not created: %v", err)
			}
			if !strings.Contains(string(b), agentContextIncludeLine) {
				t.Errorf("include line missing; got:\n%s", b)
			}
			if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
				t.Error("CLAUDE.md should not be created for non-claude")
			}
		})
	}
}

func TestUpsertAgentDocInclude_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := "# Project\n\nSome existing content.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := upsertAgentDocInclude(dir, "claude"); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, existing) {
		t.Errorf("existing content not preserved; got:\n%s", got)
	}
	if !strings.Contains(got, agentContextIncludeLine) {
		t.Errorf("include line missing; got:\n%s", got)
	}
}

func TestUpsertAgentDocInclude_Idempotent(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		if err := upsertAgentDocInclude(dir, "claude"); err != nil {
			t.Fatalf("upsert #%d failed: %v", i+1, err)
		}
	}

	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if count := strings.Count(got, agentContextIncludeLine); count != 1 {
		t.Errorf("expected exactly 1 occurrence of include line, got %d:\n%s", count, got)
	}
}

func TestUpsertAgentDocInclude_SkipsShellAndUnknown(t *testing.T) {
	for _, assistant := range []string{"shell", "custom", "", "random"} {
		t.Run(assistant, func(t *testing.T) {
			dir := t.TempDir()
			if err := upsertAgentDocInclude(dir, assistant); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("expected empty dir for assistant %q, got: %v", assistant, names)
			}
		})
	}
}

func TestUpsertAgentDocInclude_EmptyWorkDir(t *testing.T) {
	if err := upsertAgentDocInclude("", "claude"); err != nil {
		t.Errorf("expected nil for empty workDir, got %v", err)
	}
}

func TestUpsertAgentDocInclude_FileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	existing := "# Project\n\nLast line without newline"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := upsertAgentDocInclude(dir, "claude"); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "Last line without newline\n"+agentContextIncludeLine) {
		t.Errorf("expected newline inserted before include line; got:\n%s", got)
	}
}
