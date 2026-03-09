package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleAutocompleteAPI(t *testing.T) {
	t.Run("GET returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/autocomplete/test-uuid", nil)
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})

	t.Run("missing session UUID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/", strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("unknown session returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/nonexistent-uuid", strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		sessions["test-uuid"] = &Session{
			UUID:      "test-uuid",
			Assistant: "claude",
			AssistantConfig: AssistantConfig{
				SlashCmdFormat: SlashCmdMD,
			},
		}
		defer delete(sessions, "test-uuid")

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid", strings.NewReader(`{invalid`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("returns empty results for agent with no slash commands", func(t *testing.T) {
		sessions["test-uuid"] = &Session{
			UUID:      "test-uuid",
			Assistant: "shell",
			AssistantConfig: AssistantConfig{
				SlashCmdFormat: SlashCmdNone,
			},
		}
		defer delete(sessions, "test-uuid")

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid", strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp []autocompleteItem
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp) != 0 {
			t.Errorf("expected empty results, got %d", len(resp))
		}
	})
}

func TestSlashCommandDiscovery(t *testing.T) {
	// Create a temporary directory structure mimicking slash command dirs
	tmpDir := t.TempDir()

	// Create namespace/command structure for Claude (.md format)
	claudeDir := filepath.Join(tmpDir, ".claude", "commands")
	mkdirAll(t, filepath.Join(claudeDir, "swe-swe"))
	mkdirAll(t, filepath.Join(claudeDir, "custom"))

	writeFile(t, filepath.Join(claudeDir, "swe-swe", "setup.md"),
		"---\ndescription: Configure git, SSH, testing\n---\n\n# Setup\n")
	writeFile(t, filepath.Join(claudeDir, "swe-swe", "extract-skills.md"),
		"---\ndescription: Extract skills from task runners\n---\n\n# Extract\n")
	writeFile(t, filepath.Join(claudeDir, "custom", "deploy.md"),
		"---\ndescription: Deploy to production\n---\n\n# Deploy\n")
	// File without frontmatter
	writeFile(t, filepath.Join(claudeDir, "custom", "simple.md"),
		"# Just a simple command\n")
	// Non-.md file should be ignored
	writeFile(t, filepath.Join(claudeDir, "custom", "README.adoc"),
		"= README\n")

	t.Run("discovers all md slash commands", func(t *testing.T) {
		items := discoverSlashCommands(claudeDir, "md")
		if len(items) != 4 {
			t.Fatalf("expected 4 commands, got %d: %+v", len(items), items)
		}

		// Check that results contain expected commands
		found := map[string]string{}
		for _, item := range items {
			found[item.V] = item.H
		}

		if found["swe-swe:setup"] != "Configure git, SSH, testing" {
			t.Errorf("unexpected hint for swe-swe:setup: %q", found["swe-swe:setup"])
		}
		if found["custom:deploy"] != "Deploy to production" {
			t.Errorf("unexpected hint for custom:deploy: %q", found["custom:deploy"])
		}
		if _, ok := found["custom:simple"]; !ok {
			t.Error("expected custom:simple to be present (even without description)")
		}
	})

	t.Run("fuzzy filters by query", func(t *testing.T) {
		items := discoverSlashCommands(claudeDir, "md")
		filtered := filterAutocomplete(items, "setup")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 result for 'setup', got %d: %+v", len(filtered), filtered)
		}
		if filtered[0].V != "swe-swe:setup" {
			t.Errorf("expected swe-swe:setup, got %s", filtered[0].V)
		}
	})

	t.Run("empty query returns all", func(t *testing.T) {
		items := discoverSlashCommands(claudeDir, "md")
		filtered := filterAutocomplete(items, "")
		if len(filtered) != 4 {
			t.Errorf("expected 4 results for empty query, got %d", len(filtered))
		}
	})

	t.Run("query matches across namespace:command", func(t *testing.T) {
		items := discoverSlashCommands(claudeDir, "md")
		filtered := filterAutocomplete(items, "swe")
		// Should match swe-swe:setup, swe-swe:extract-skills
		if len(filtered) != 2 {
			t.Fatalf("expected 2 results for 'swe', got %d: %+v", len(filtered), filtered)
		}
	})

	// TOML format (Gemini)
	t.Run("discovers toml slash commands", func(t *testing.T) {
		geminiDir := filepath.Join(tmpDir, ".gemini", "commands")
		mkdirAll(t, filepath.Join(geminiDir, "swe-swe"))
		writeFile(t, filepath.Join(geminiDir, "swe-swe", "setup.toml"),
			"description = \"Configure git and SSH\"\n\n[prompt]\ncontent = \"...\"\n")
		writeFile(t, filepath.Join(geminiDir, "swe-swe", "debug.toml"),
			"# No description field\n\n[prompt]\ncontent = \"...\"\n")

		items := discoverSlashCommands(geminiDir, "toml")
		if len(items) != 2 {
			t.Fatalf("expected 2 commands, got %d: %+v", len(items), items)
		}
		found := map[string]string{}
		for _, item := range items {
			found[item.V] = item.H
		}
		if found["swe-swe:setup"] != "Configure git and SSH" {
			t.Errorf("unexpected hint for swe-swe:setup: %q", found["swe-swe:setup"])
		}
	})

	t.Run("nonexistent directory returns empty", func(t *testing.T) {
		items := discoverSlashCommands("/nonexistent/path", "md")
		if len(items) != 0 {
			t.Errorf("expected 0 results, got %d", len(items))
		}
	})
}

func TestSlashCommandDirForAgent(t *testing.T) {
	tests := []struct {
		assistant string
		format    SlashCommandFormat
		wantDir   string
		wantExt   string
	}{
		{"claude", SlashCmdMD, "/home/app/.claude/commands", "md"},
		{"codex", SlashCmdMD, "/home/app/.codex/prompts", "md"},
		{"opencode", SlashCmdMD, "/home/app/.config/opencode/command", "md"},
		{"gemini", SlashCmdTOML, "/home/app/.gemini/commands", "toml"},
		{"shell", SlashCmdNone, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.assistant, func(t *testing.T) {
			dir, ext := slashCommandDirForAgent(tt.assistant, tt.format)
			if dir != tt.wantDir {
				t.Errorf("dir: got %q, want %q", dir, tt.wantDir)
			}
			if ext != tt.wantExt {
				t.Errorf("ext: got %q, want %q", ext, tt.wantExt)
			}
		})
	}
}

func TestExtractDescription(t *testing.T) {
	t.Run("md with frontmatter", func(t *testing.T) {
		desc := extractDescription("---\ndescription: Configure git\n---\n\n# Setup\n", "md")
		if desc != "Configure git" {
			t.Errorf("got %q, want %q", desc, "Configure git")
		}
	})

	t.Run("md with quoted description", func(t *testing.T) {
		desc := extractDescription("---\ndescription: \"Deploy to prod\"\n---\n", "md")
		if desc != "Deploy to prod" {
			t.Errorf("got %q, want %q", desc, "Deploy to prod")
		}
	})

	t.Run("md without frontmatter", func(t *testing.T) {
		desc := extractDescription("# Just a command\nDo stuff\n", "md")
		if desc != "" {
			t.Errorf("got %q, want empty", desc)
		}
	})

	t.Run("toml with description", func(t *testing.T) {
		desc := extractDescription("description = \"Configure git and SSH\"\n\n[prompt]\n", "toml")
		if desc != "Configure git and SSH" {
			t.Errorf("got %q, want %q", desc, "Configure git and SSH")
		}
	})

	t.Run("toml without description", func(t *testing.T) {
		desc := extractDescription("[prompt]\ncontent = \"...\"\n", "toml")
		if desc != "" {
			t.Errorf("got %q, want empty", desc)
		}
	})
}

// helpers

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
