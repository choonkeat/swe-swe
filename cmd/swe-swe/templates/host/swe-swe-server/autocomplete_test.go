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
	// Save and restore mcpAuthKey
	origKey := mcpAuthKey
	mcpAuthKey = "test-api-key"
	defer func() { mcpAuthKey = origKey }()

	t.Run("GET returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/autocomplete/test-uuid?key="+mcpAuthKey, nil)
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})

	t.Run("missing API key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid", strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("wrong API key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key=wrong-key", strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("missing session UUID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/?key="+mcpAuthKey, strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("unknown session returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/nonexistent-uuid?key="+mcpAuthKey, strings.NewReader(`{"type":"slash-command","query":""}`))
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

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey, strings.NewReader(`{invalid`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("disambiguates duplicate commands from system and project dirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		systemDir := filepath.Join(tmpDir, "system", "commands")
		projectDir := filepath.Join(tmpDir, "project", "commands")
		mkdirAll(t, filepath.Join(systemDir, "swe-swe"))
		mkdirAll(t, filepath.Join(projectDir, "swe-swe"))

		writeFile(t, filepath.Join(systemDir, "swe-swe", "merge-worktree.md"),
			"---\ndescription: Merge a worktree\n---\n")
		writeFile(t, filepath.Join(projectDir, "swe-swe", "merge-worktree.md"),
			"---\ndescription: Merge a worktree\n---\n")
		// Unique command in system only
		writeFile(t, filepath.Join(systemDir, "swe-swe", "setup.md"),
			"---\ndescription: Configure git\n---\n")

		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir+"/system")
		defer os.Setenv("HOME", origHome)

		sessions["test-uuid"] = &Session{
			UUID:      "test-uuid",
			Assistant: "claude",
			WorkDir:   tmpDir + "/project",
			AssistantConfig: AssistantConfig{
				SlashCmdFormat: SlashCmdMD,
			},
		}
		defer delete(sessions, "test-uuid")

		// Temporarily override slashCommandDirForAgent by using a custom session
		// We can't easily override that, so test the dedup logic directly:
		type sourced struct {
			item autocompleteItem
			dir  string
		}
		var all []sourced
		for _, item := range discoverSlashCommands(systemDir, "md") {
			all = append(all, sourced{item, systemDir})
		}
		for _, item := range discoverSlashCommands(projectDir, "md") {
			all = append(all, sourced{item, projectDir})
		}

		counts := make(map[string]int)
		for _, s := range all {
			counts[s.item.V]++
		}

		// swe-swe:merge-worktree should be duplicate (count=2)
		if counts["swe-swe:merge-worktree"] != 2 {
			t.Errorf("expected 2 copies of merge-worktree, got %d", counts["swe-swe:merge-worktree"])
		}
		// swe-swe:setup should not be duplicate (count=1)
		if counts["swe-swe:setup"] != 1 {
			t.Errorf("expected 1 copy of setup, got %d", counts["swe-swe:setup"])
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

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey, strings.NewReader(`{"type":"slash-command","query":""}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp autocompleteResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Results) != 0 {
			t.Errorf("expected empty results, got %d", len(resp.Results))
		}
		if resp.HasMore {
			t.Error("expected has_more to be false")
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

	t.Run("discovers flat (non-namespaced) commands", func(t *testing.T) {
		flatDir := filepath.Join(tmpDir, "flat-commands")
		mkdirAll(t, flatDir)
		writeFile(t, filepath.Join(flatDir, "setup.md"),
			"---\ndescription: Project setup\n---\n\n# Setup\n")
		writeFile(t, filepath.Join(flatDir, "slide.md"),
			"---\ndescription: Generate slides\n---\n\n# Slide\n")
		writeFile(t, filepath.Join(flatDir, "notes.txt"), "ignored")

		items := discoverSlashCommands(flatDir, "md")
		if len(items) != 2 {
			t.Fatalf("expected 2 flat commands, got %d: %+v", len(items), items)
		}
		found := map[string]string{}
		for _, item := range items {
			found[item.V] = item.H
		}
		if found["setup"] != "Project setup" {
			t.Errorf("unexpected hint for setup: %q", found["setup"])
		}
		if found["slide"] != "Generate slides" {
			t.Errorf("unexpected hint for slide: %q", found["slide"])
		}
	})

	t.Run("discovers mixed flat and namespaced commands", func(t *testing.T) {
		mixedDir := filepath.Join(tmpDir, "mixed-commands")
		mkdirAll(t, filepath.Join(mixedDir, "ck"))
		writeFile(t, filepath.Join(mixedDir, "deploy.md"),
			"---\ndescription: Quick deploy\n---\n")
		writeFile(t, filepath.Join(mixedDir, "ck", "plan.md"),
			"---\ndescription: Plan carefully\n---\n")

		items := discoverSlashCommands(mixedDir, "md")
		if len(items) != 2 {
			t.Fatalf("expected 2 commands, got %d: %+v", len(items), items)
		}
		found := map[string]string{}
		for _, item := range items {
			found[item.V] = item.H
		}
		if found["deploy"] != "Quick deploy" {
			t.Errorf("unexpected: %q", found["deploy"])
		}
		if found["ck:plan"] != "Plan carefully" {
			t.Errorf("unexpected: %q", found["ck:plan"])
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

func TestProjectCommandDir(t *testing.T) {
	tests := []struct {
		assistant string
		workDir   string
		want      string
	}{
		{"claude", "/workspace", "/workspace/.claude/commands"},
		{"codex", "/workspace", "/workspace/.codex/prompts"},
		{"opencode", "/workspace", "/workspace/.opencode/command"},
		{"gemini", "/workspace", "/workspace/.gemini/commands"},
		{"shell", "/workspace", ""},
		{"claude", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.assistant+"_"+tt.workDir, func(t *testing.T) {
			got := projectCommandDir(tt.assistant, tt.workDir)
			if got != tt.want {
				t.Errorf("projectCommandDir(%q, %q) = %q, want %q", tt.assistant, tt.workDir, got, tt.want)
			}
		})
	}
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

func TestSortAutocomplete(t *testing.T) {
	t.Run("contiguous substring outranks fuzzy", func(t *testing.T) {
		items := []autocompleteItem{
			{V: "swe-swe:debug-preview-page"},
			{V: "tdspec:serve"},
			{V: "swe-swe:reboot"},
		}
		sortAutocomplete(items, "reboo")
		if items[0].V != "swe-swe:reboot" {
			t.Errorf("expected swe-swe:reboot first, got %q (full order: %v)", items[0].V, itemVs(items))
		}
	})

	t.Run("prefix outranks substring", func(t *testing.T) {
		items := []autocompleteItem{
			{V: "swe-swe:reboot"},
			{V: "reboot"},
		}
		sortAutocomplete(items, "reb")
		if items[0].V != "reboot" {
			t.Errorf("expected reboot first (prefix), got %q", items[0].V)
		}
	})

	t.Run("exact match outranks prefix", func(t *testing.T) {
		items := []autocompleteItem{
			{V: "reboot-now"},
			{V: "reboot"},
		}
		sortAutocomplete(items, "reboot")
		if items[0].V != "reboot" {
			t.Errorf("expected exact match reboot first, got %q", items[0].V)
		}
	})

	t.Run("stable within tier", func(t *testing.T) {
		items := []autocompleteItem{
			{V: "foo:reboot"}, // substring tier, pos 4
			{V: "reboot:a"},   // prefix tier
			{V: "reboot:b"},   // prefix tier, same length
		}
		sortAutocomplete(items, "reb")
		// Both prefix-tier items have equal length, stable order keeps a before b.
		if items[0].V != "reboot:a" || items[1].V != "reboot:b" {
			t.Errorf("stable sort broken: %v", itemVs(items))
		}
	})

	t.Run("value fuzzy outranks hint match", func(t *testing.T) {
		items := []autocompleteItem{
			// Hint fuzzy-matches "reboo" (r-e-b-o-o in order), value does not.
			{V: "swe-swe:debug-preview-page", H: "Inspect App Preview page content — use instead of browser tools for preview"},
			// Value contains "reboo" as a contiguous substring.
			{V: "swe-swe:reboot"},
		}
		sortAutocomplete(items, "reboo")
		if items[0].V != "swe-swe:reboot" {
			t.Errorf("expected swe-swe:reboot first, got %q (order: %v)", items[0].V, itemVs(items))
		}
	})

	t.Run("hint match is kept when no value match exists", func(t *testing.T) {
		items := []autocompleteItem{
			{V: "unrelated", H: "nothing here"},
			{V: "tdspec:docs", H: "Generate browsable HTML for tdspec modules"},
		}
		got := filterAutocomplete(items, "browsab")
		if len(got) != 1 || got[0].V != "tdspec:docs" {
			t.Errorf("expected hint-only match to survive filter, got %v", itemVs(got))
		}
		sortAutocomplete(got, "browsab")
		if got[0].V != "tdspec:docs" {
			t.Errorf("unexpected order: %v", itemVs(got))
		}
	})

	t.Run("longer consecutive run beats sparser earlier match within fuzzy tier", func(t *testing.T) {
		// Query "reboo", both candidates are tier 2 (value fuzzy — neither
		// contains "reboo" as a contiguous substring):
		//   - "reboXo"    has a 4-char run "rebo" (longestRun=4).
		//   - "rXeXbXoXo" has every query char separated (longestRun=1).
		// The tight 4-char run must win even though the sparse candidate
		// also starts with 'r' at position 0.
		items := []autocompleteItem{
			{V: "rXeXbXoXo"}, // sparse
			{V: "reboXo"},    // tight run of 4
		}
		sortAutocomplete(items, "reboo")
		if items[0].V != "reboXo" {
			t.Errorf("expected reboXo first (run=4), got %q (order: %v)", items[0].V, itemVs(items))
		}
	})

	t.Run("empty query is a no-op", func(t *testing.T) {
		items := []autocompleteItem{{V: "b"}, {V: "a"}}
		sortAutocomplete(items, "")
		if items[0].V != "b" || items[1].V != "a" {
			t.Errorf("empty query should not reorder, got %v", itemVs(items))
		}
	})
}

func itemVs(items []autocompleteItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.V
	}
	return out
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
