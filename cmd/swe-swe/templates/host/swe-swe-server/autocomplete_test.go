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

	t.Run("pi project autocomplete reads pi prompts", func(t *testing.T) {
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "project")
		mkdirAll(t, filepath.Join(projectDir, ".pi", "prompts", "swe-swe"))
		writeFile(t, filepath.Join(projectDir, ".pi", "prompts", "swe-swe", "zzzz-pi-project-only-123.md"),
			"---\ndescription: Pi-only project command\n---\n")

		sessions["test-uuid"] = &Session{
			UUID:      "test-uuid",
			Assistant: "pi",
			WorkDir:   projectDir,
			AssistantConfig: AssistantConfig{
				SlashCmdFormat: SlashCmdMD,
			},
		}
		defer delete(sessions, "test-uuid")

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey, strings.NewReader(`{"type":"slash-command","query":"zzzzpi"}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp autocompleteResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !hasAutocompleteValue(resp.Results, "swe-swe:zzzz-pi-project-only-123") {
			t.Fatalf("expected pi project command in results, got %+v", resp.Results)
		}
	})

	t.Run("pi project autocomplete projects legacy claude commands", func(t *testing.T) {
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "project")
		mkdirAll(t, filepath.Join(projectDir, ".claude", "commands", "swe-swe"))
		writeFile(t, filepath.Join(projectDir, ".claude", "commands", "swe-swe", "zzzz-claude-project-only-123.md"),
			"---\ndescription: Claude-only project command\n---\n")

		sessions["test-uuid"] = &Session{
			UUID:      "test-uuid",
			Assistant: "pi",
			WorkDir:   projectDir,
			AssistantConfig: AssistantConfig{
				SlashCmdFormat: SlashCmdMD,
			},
		}
		defer delete(sessions, "test-uuid")

		req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey, strings.NewReader(`{"type":"slash-command","query":"zzzzclaude"}`))
		w := httptest.NewRecorder()
		handleAutocompleteAPI(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp autocompleteResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !hasAutocompleteValue(resp.Results, "swe-swe:zzzz-claude-project-only-123") {
			t.Fatalf("expected pi autocomplete to find projected legacy claude command, got %+v", resp.Results)
		}
		assertSymlink(t, filepath.Join(projectDir, ".swe-swe", "commands", "md"))
		assertSymlink(t, filepath.Join(projectDir, ".pi", "prompts"))
	})

	t.Run("returns empty results for agent with no slash commands or skills", func(t *testing.T) {
		// Point HOME at an empty temp dir so skill discovery (which is
		// agent-agnostic and always scans ~/.{claude,codex,...}/skills)
		// finds nothing for this test.
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", t.TempDir())
		defer os.Setenv("HOME", origHome)

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

func assertSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("expected symlink %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink, got mode %s", path, info.Mode())
	}
}

func hasAutocompleteValue(items []autocompleteItem, value string) bool {
	for _, item := range items {
		if item.V == value {
			return true
		}
	}
	return false
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
		{"pi", "/workspace", "/workspace/.pi/prompts"},
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
		{"pi", SlashCmdMD, "/home/app/.pi/agent/prompts", "md"},
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
			{V: "swe-swe:debug-preview-page", H: "Inspect App Preview page content -- use instead of browser tools for preview"},
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
		// Query "reboo", both candidates are tier 2 (value fuzzy -- neither
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

// TestDiscoverSkills verifies the skills discovery walks one level deep,
// uses the directory name as the canonical handle (so prefixed --with-skills
// installs stay distinct), prefixes hints with `[skill]`, truncates
// multi-sentence descriptions, and silently returns nil for missing dirs.
// These cover the behaviors that handleAutocompleteAPI relies on.
func TestDiscoverSkills(t *testing.T) {
	t.Run("returns nil for missing dir", func(t *testing.T) {
		if got := discoverSkills("/nonexistent/skills"); got != nil {
			t.Fatalf("expected nil for missing dir, got %v", got)
		}
	})

	t.Run("returns nil for empty dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		if got := discoverSkills(tmpDir); got != nil {
			t.Fatalf("expected nil for empty dir, got %v", got)
		}
	})

	t.Run("flat layout with frontmatter name and description", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "cli-development"))
		writeFile(t, filepath.Join(tmpDir, "cli-development", "SKILL.md"),
			"---\nname: cli-development\ndescription: Build CLIs in Go.\n---\n# CLI\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 {
			t.Fatalf("expected 1 skill, got %d: %+v", len(got), got)
		}
		if got[0].V != "cli-development" {
			t.Errorf("expected V=cli-development, got %q", got[0].V)
		}
		if got[0].H != "[skill] Build CLIs in Go." {
			t.Errorf("expected [skill] prefix + first sentence, got %q", got[0].H)
		}
	})

	t.Run("directory name is the handle, not frontmatter name", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "mattpocock-init"))
		writeFile(t, filepath.Join(tmpDir, "mattpocock-init", "SKILL.md"),
			"---\nname: tdspec:init\ndescription: Initialize tdspec.\n---\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 || got[0].V != "mattpocock-init" {
			t.Fatalf("expected V=mattpocock-init (dir name, not frontmatter), got %+v", got)
		}
	})

	t.Run("uses dirname even when frontmatter lacks name", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "fallback-skill"))
		writeFile(t, filepath.Join(tmpDir, "fallback-skill", "SKILL.md"),
			"---\ndescription: No name field.\n---\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 || got[0].V != "fallback-skill" {
			t.Fatalf("expected dirname handle, got %+v", got)
		}
	})

	t.Run("truncates multi-sentence description to first sentence", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "verbose"))
		writeFile(t, filepath.Join(tmpDir, "verbose", "SKILL.md"),
			"---\nname: verbose\ndescription: First sentence here. Second sentence. Third sentence.\n---\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(got))
		}
		if got[0].H != "[skill] First sentence here." {
			t.Errorf("expected first sentence only, got %q", got[0].H)
		}
	})

	t.Run("skill prefix with no description", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "bare"))
		writeFile(t, filepath.Join(tmpDir, "bare", "SKILL.md"),
			"---\nname: bare\n---\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 || got[0].H != "[skill]" {
			t.Fatalf("expected bare [skill] hint, got %+v", got)
		}
	})

	t.Run("skips entries without SKILL.md", func(t *testing.T) {
		tmpDir := t.TempDir()
		mkdirAll(t, filepath.Join(tmpDir, "empty-dir"))
		mkdirAll(t, filepath.Join(tmpDir, "valid"))
		writeFile(t, filepath.Join(tmpDir, "valid", "SKILL.md"),
			"---\nname: valid\ndescription: Counts.\n---\n")
		writeFile(t, filepath.Join(tmpDir, "loose-file.md"),
			"# Not a skill\n")

		got := discoverSkills(tmpDir)
		if len(got) != 1 || got[0].V != "valid" {
			t.Fatalf("expected only valid skill, got %+v", got)
		}
	})

	// Regression: --with-skills installs each skill as a symlink into the
	// canonical store (entrypoint `ln -sfn <real skill dir> ~/.swe-swe/skills/
	// <alias>-<dirname>`). os.ReadDir reports a symlink-to-dir with
	// IsDir()==false, so an IsDir() guard silently dropped every installed
	// skill. discoverSkills must resolve the link and find the skill.
	t.Run("discovers symlinked skill dirs (--with-skills install shape)", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Real skill lives outside the scanned dir (mirrors skills-src/).
		realSrc := t.TempDir()
		mkdirAll(t, filepath.Join(realSrc, "engineering", "tdd"))
		writeFile(t, filepath.Join(realSrc, "engineering", "tdd", "SKILL.md"),
			"---\nname: tdd\ndescription: Drive code with tests.\n---\n")
		// Flat symlink into the canonical store, as the entrypoint creates it.
		if err := os.Symlink(filepath.Join(realSrc, "engineering", "tdd"),
			filepath.Join(tmpDir, "mattpocock-tdd")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		got := discoverSkills(tmpDir)
		if len(got) != 1 {
			t.Fatalf("expected 1 skill via symlink, got %d: %+v", len(got), got)
		}
		// Handle is the prefixed store name, not the frontmatter `name: tdd`.
		if got[0].V != "mattpocock-tdd" || got[0].H != "[skill] Drive code with tests." {
			t.Errorf("unexpected skill from symlink: %+v", got[0])
		}
	})

	// Collision avoidance: two repos that both ship a skill declaring the same
	// frontmatter `name:` install under distinct prefixed store dirs. Keying
	// on the dir name surfaces both -- neither silently shadows the other,
	// which is the whole point of flattening-with-prefix.
	t.Run("distinct store names with same frontmatter name both surface", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcA := t.TempDir()
		srcB := t.TempDir()
		mkdirAll(t, filepath.Join(srcA, "grill-me"))
		writeFile(t, filepath.Join(srcA, "grill-me", "SKILL.md"),
			"---\nname: grill-me\ndescription: From repo A.\n---\n")
		mkdirAll(t, filepath.Join(srcB, "grill-me"))
		writeFile(t, filepath.Join(srcB, "grill-me", "SKILL.md"),
			"---\nname: grill-me\ndescription: From repo B.\n---\n")
		if err := os.Symlink(filepath.Join(srcA, "grill-me"),
			filepath.Join(tmpDir, "alpha-grill-me")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if err := os.Symlink(filepath.Join(srcB, "grill-me"),
			filepath.Join(tmpDir, "beta-grill-me")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		got := discoverSkills(tmpDir)
		if len(got) != 2 {
			t.Fatalf("expected both skills (no collision drop), got %d: %+v", len(got), got)
		}
		names := map[string]bool{}
		for _, item := range got {
			names[item.V] = true
		}
		if !names["alpha-grill-me"] || !names["beta-grill-me"] {
			t.Errorf("expected alpha-grill-me and beta-grill-me, got %+v", got)
		}
	})

	t.Run("skips dangling symlinks", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Symlink(filepath.Join(tmpDir, "gone"),
			filepath.Join(tmpDir, "dangling")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if got := discoverSkills(tmpDir); got != nil {
			t.Fatalf("expected nil for dangling symlink, got %+v", got)
		}
	})
}

// TestSkillCandidateDirs verifies the candidate dir lists cover every
// known agent convention plus the swe-swe canonical store. The exact set
// is load-bearing -- handleAutocompleteAPI scans these regardless of
// session.Assistant so a skill installed under any convention appears in
// every agent's autocomplete.
func TestSkillCandidateDirs(t *testing.T) {
	t.Run("system dirs cover all known conventions", func(t *testing.T) {
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", "/h")
		defer os.Setenv("HOME", origHome)

		want := []string{
			"/h/.swe-swe/skills",
			"/h/.claude/skills",
			"/h/.codex/skills",
			"/h/.gemini/skills",
			"/h/.opencode/skills",
			"/h/.pi/skills",
		}
		got := skillCandidateDirsSystem()
		if len(got) != len(want) {
			t.Fatalf("expected %d system dirs, got %d: %v", len(want), len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("system dir [%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("project dirs nil when workDir empty", func(t *testing.T) {
		if got := skillCandidateDirsProject(""); got != nil {
			t.Errorf("expected nil for empty workDir, got %v", got)
		}
	})

	t.Run("project dirs cover all known conventions", func(t *testing.T) {
		want := []string{
			"/work/.swe-swe/skills",
			"/work/.claude/skills",
			"/work/.codex/skills",
			"/work/.gemini/skills",
			"/work/.opencode/skills",
			"/work/.pi/skills",
		}
		got := skillCandidateDirsProject("/work")
		if len(got) != len(want) {
			t.Fatalf("expected %d project dirs, got %d: %v", len(want), len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("project dir [%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
}

// TestExtractSkillFrontmatter verifies frontmatter parsing edge cases for
// SKILL.md files: no frontmatter, partial fields, quoted values, missing
// closing fence.
func TestExtractSkillFrontmatter(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantName    string
		wantDescrip string
	}{
		{"no frontmatter", "# Just markdown\n", "", ""},
		{"missing close fence", "---\nname: foo\n", "", ""},
		{"both fields", "---\nname: foo\ndescription: Bar baz.\n---\n", "foo", "Bar baz."},
		{"only name", "---\nname: foo\n---\n", "foo", ""},
		{"only description", "---\ndescription: Bar.\n---\n", "", "Bar."},
		{"double-quoted values", "---\nname: \"foo\"\ndescription: \"Bar.\"\n---\n", "foo", "Bar."},
		{"single-quoted values", "---\nname: 'foo'\ndescription: 'Bar.'\n---\n", "foo", "Bar."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotDesc := extractSkillFrontmatter(tc.input)
			if gotName != tc.wantName {
				t.Errorf("name: got %q, want %q", gotName, tc.wantName)
			}
			if gotDesc != tc.wantDescrip {
				t.Errorf("description: got %q, want %q", gotDesc, tc.wantDescrip)
			}
		})
	}
}

// TestFirstSentence verifies sentence truncation handles `.`, `!`, `?`,
// no terminator, and surrounding whitespace.
func TestFirstSentence(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"One. Two.", "One."},
		{"Hello! Goodbye.", "Hello!"},
		{"Question? Answer.", "Question?"},
		{"No terminator at all", "No terminator at all"},
		{"  Leading and trailing.  ", "Leading and trailing."},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := firstSentence(tc.in); got != tc.want {
				t.Errorf("firstSentence(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestHandleAutocompleteAPI_SkillsSurfaced is an end-to-end check: a skill
// dropped under the session's workDir/.claude/skills/ surfaces in
// autocomplete with the [skill] hint prefix, even when the session's
// assistant is not Claude. Confirms the agent-agnostic discovery wiring.
func TestHandleAutocompleteAPI_SkillsSurfaced(t *testing.T) {
	origKey := mcpAuthKey
	mcpAuthKey = "test-api-key"
	defer func() { mcpAuthKey = origKey }()

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	workDir := t.TempDir()
	// Drop a project-level skill under .codex/skills/ to also prove the
	// non-Claude convention is scanned.
	mkdirAll(t, filepath.Join(workDir, ".codex", "skills", "deploy"))
	writeFile(t, filepath.Join(workDir, ".codex", "skills", "deploy", "SKILL.md"),
		"---\nname: deploy\ndescription: Push to prod.\n---\n")

	// Session is a gemini agent -- skills must still surface.
	sessions["test-uuid"] = &Session{
		UUID:      "test-uuid",
		Assistant: "gemini",
		WorkDir:   workDir,
		AssistantConfig: AssistantConfig{
			SlashCmdFormat: SlashCmdTOML,
		},
	}
	defer delete(sessions, "test-uuid")

	req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey,
		strings.NewReader(`{"type":"slash-command","query":""}`))
	w := httptest.NewRecorder()
	handleAutocompleteAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp autocompleteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *autocompleteItem
	for i, item := range resp.Results {
		if item.V == "deploy" {
			found = &resp.Results[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("skill 'deploy' missing from results: %+v", resp.Results)
	}
	if found.H != "[skill] Push to prod." {
		t.Errorf("hint: got %q, want %q", found.H, "[skill] Push to prod.")
	}
}

// TestHandleAutocompleteAPI_SkillsDedupProjectWins verifies that when the
// same skill name appears in both project-level and system-level dirs,
// only one entry appears in autocomplete and project-level wins (its
// description survives).
func TestHandleAutocompleteAPI_SkillsDedupProjectWins(t *testing.T) {
	origKey := mcpAuthKey
	mcpAuthKey = "test-api-key"
	defer func() { mcpAuthKey = origKey }()

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	workDir := t.TempDir()
	mkdirAll(t, filepath.Join(workDir, ".claude", "skills", "shared"))
	writeFile(t, filepath.Join(workDir, ".claude", "skills", "shared", "SKILL.md"),
		"---\nname: shared\ndescription: Project version.\n---\n")

	mkdirAll(t, filepath.Join(tmpHome, ".claude", "skills", "shared"))
	writeFile(t, filepath.Join(tmpHome, ".claude", "skills", "shared", "SKILL.md"),
		"---\nname: shared\ndescription: System version.\n---\n")

	sessions["test-uuid"] = &Session{
		UUID:      "test-uuid",
		Assistant: "claude",
		WorkDir:   workDir,
		AssistantConfig: AssistantConfig{
			SlashCmdFormat: SlashCmdMD,
		},
	}
	defer delete(sessions, "test-uuid")

	req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey,
		strings.NewReader(`{"type":"slash-command","query":""}`))
	w := httptest.NewRecorder()
	handleAutocompleteAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp autocompleteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count := 0
	var sharedHint string
	for _, item := range resp.Results {
		if item.V == "shared" {
			count++
			sharedHint = item.H
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'shared' skill (project-wins dedup), got %d", count)
	}
	if sharedHint != "[skill] Project version." {
		t.Errorf("expected project description to win, got hint %q", sharedHint)
	}
}

// TestHandleAutocompleteAPI_SkillsDedupAcrossAgentDirs verifies that two
// different agent dirs at the same level (e.g. ~/.claude/skills/foo and
// ~/.codex/skills/foo) collapse into a single autocomplete entry. The
// first scanned wins (deterministic order: .swe-swe, .claude, .codex, ...).
func TestHandleAutocompleteAPI_SkillsDedupAcrossAgentDirs(t *testing.T) {
	origKey := mcpAuthKey
	mcpAuthKey = "test-api-key"
	defer func() { mcpAuthKey = origKey }()

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// .claude is scanned before .codex per skillCandidateDirsSystem order
	// (after .swe-swe, which is empty here).
	mkdirAll(t, filepath.Join(tmpHome, ".claude", "skills", "review"))
	writeFile(t, filepath.Join(tmpHome, ".claude", "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: From claude dir.\n---\n")

	mkdirAll(t, filepath.Join(tmpHome, ".codex", "skills", "review"))
	writeFile(t, filepath.Join(tmpHome, ".codex", "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: From codex dir.\n---\n")

	sessions["test-uuid"] = &Session{
		UUID:      "test-uuid",
		Assistant: "claude",
		WorkDir:   t.TempDir(),
		AssistantConfig: AssistantConfig{
			SlashCmdFormat: SlashCmdMD,
		},
	}
	defer delete(sessions, "test-uuid")

	req := httptest.NewRequest(http.MethodPost, "/api/autocomplete/test-uuid?key="+mcpAuthKey,
		strings.NewReader(`{"type":"slash-command","query":""}`))
	w := httptest.NewRecorder()
	handleAutocompleteAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp autocompleteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count := 0
	var hint string
	for _, item := range resp.Results {
		if item.V == "review" {
			count++
			hint = item.H
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'review' skill (cross-agent dedup), got %d", count)
	}
	if hint != "[skill] From claude dir." {
		t.Errorf("expected .claude dir to win (scanned first), got hint %q", hint)
	}
}
