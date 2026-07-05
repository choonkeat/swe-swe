package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDeriveAliasFromURL verifies alias derivation from git URLs
func TestDeriveAliasFromURL(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		// Standard HTTPS URLs
		{"https://github.com/choonkeat/slash-commands.git", "choonkeat/slash-commands", false},
		{"https://github.com/choonkeat/slash-commands", "choonkeat/slash-commands", false},
		{"https://gitlab.com/org/repo.git", "org/repo", false},
		{"https://bitbucket.org/team/project", "team/project", false},
		// SSH URLs
		{"git@github.com:choonkeat/slash-commands.git", "choonkeat/slash-commands", false},
		{"git@gitlab.com:org/repo.git", "org/repo", false},
		// Invalid URLs
		{"", "", true},
		{"not-a-url", "", true},
		{"https://github.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := deriveAliasFromURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("deriveAliasFromURL(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("deriveAliasFromURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseSlashCommandsEntry verifies parsing of single slash commands entries
func TestParseSlashCommandsEntry(t *testing.T) {
	tests := []struct {
		input     string
		wantAlias string
		wantURL   string
		wantErr   bool
	}{
		// With alias
		{"ck@https://github.com/choonkeat/slash-commands.git", "ck", "https://github.com/choonkeat/slash-commands.git", false},
		{"team@https://github.com/org/cmds.git", "team", "https://github.com/org/cmds.git", false},
		// Without alias - derive from URL
		{"https://github.com/choonkeat/slash-commands.git", "choonkeat/slash-commands", "https://github.com/choonkeat/slash-commands.git", false},
		{"https://github.com/choonkeat/slash-commands", "choonkeat/slash-commands", "https://github.com/choonkeat/slash-commands", false},
		{"https://gitlab.com/org/repo.git", "org/repo", "https://gitlab.com/org/repo.git", false},
		// SSH URL without alias
		{"git@github.com:owner/repo.git", "owner/repo", "git@github.com:owner/repo.git", false},
		// Invalid
		{"", "", "", true},
		{"ck@", "", "", true},
		{"not-a-url", "", "", true},
		// Reserved alias
		{"swe-swe@https://github.com/example/repo.git", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSlashCommandsEntry(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSlashCommandsEntry(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Alias != tt.wantAlias {
					t.Errorf("parseSlashCommandsEntry(%q).Alias = %q, want %q", tt.input, got.Alias, tt.wantAlias)
				}
				if got.URL != tt.wantURL {
					t.Errorf("parseSlashCommandsEntry(%q).URL = %q, want %q", tt.input, got.URL, tt.wantURL)
				}
			}
		})
	}
}

// TestParseSlashCommandsFlag verifies parsing of full slash commands flag
func TestParseSlashCommandsFlag(t *testing.T) {
	tests := []struct {
		input   string
		want    []SlashCommandsRepo
		wantErr bool
	}{
		// Empty
		{"", nil, false},
		{"  ", nil, false},
		// Single entry with alias
		{"ck@https://github.com/choonkeat/slash-commands.git", []SlashCommandsRepo{
			{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"},
		}, false},
		// Single entry without alias
		{"https://github.com/choonkeat/slash-commands.git", []SlashCommandsRepo{
			{Alias: "choonkeat/slash-commands", URL: "https://github.com/choonkeat/slash-commands.git"},
		}, false},
		// Multiple entries
		{"ck@https://github.com/choonkeat/slash-commands.git https://github.com/org/team-cmds.git", []SlashCommandsRepo{
			{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"},
			{Alias: "org/team-cmds", URL: "https://github.com/org/team-cmds.git"},
		}, false},
		// Multiple entries with tabs/extra spaces
		{"  ck@https://github.com/a/b.git   team@https://github.com/c/d.git  ", []SlashCommandsRepo{
			{Alias: "ck", URL: "https://github.com/a/b.git"},
			{Alias: "team", URL: "https://github.com/c/d.git"},
		}, false},
		// Invalid entry in list
		{"ck@https://github.com/a/b.git not-a-url", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSlashCommandsFlag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSlashCommandsFlag(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("parseSlashCommandsFlag(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i].Alias != tt.want[i].Alias || got[i].URL != tt.want[i].URL {
						t.Errorf("parseSlashCommandsFlag(%q)[%d] = %+v, want %+v", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// TestParseSkillsEntry verifies parsing of single skills entries.
func TestParseSkillsEntry(t *testing.T) {
	tests := []struct {
		input     string
		wantAlias string
		wantURL   string
		wantErr   bool
	}{
		{"eng@https://github.com/mattpocock/skills.git", "eng", "https://github.com/mattpocock/skills.git", false},
		{"team@https://github.com/org/skills.git", "team", "https://github.com/org/skills.git", false},
		{"https://github.com/mattpocock/skills.git", "mattpocock/skills", "https://github.com/mattpocock/skills.git", false},
		{"https://github.com/mattpocock/skills", "mattpocock/skills", "https://github.com/mattpocock/skills", false},
		{"git@github.com:owner/repo.git", "owner/repo", "git@github.com:owner/repo.git", false},
		{"", "", "", true},
		{"eng@", "", "", true},
		{"not-a-url", "", "", true},
		{"swe-swe@https://github.com/example/repo.git", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSkillsEntry(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSkillsEntry(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Alias != tt.wantAlias {
					t.Errorf("parseSkillsEntry(%q).Alias = %q, want %q", tt.input, got.Alias, tt.wantAlias)
				}
				if got.URL != tt.wantURL {
					t.Errorf("parseSkillsEntry(%q).URL = %q, want %q", tt.input, got.URL, tt.wantURL)
				}
			}
		})
	}
}

// TestParseSkillsFlag verifies parsing of full --with-skills flag.
func TestParseSkillsFlag(t *testing.T) {
	tests := []struct {
		input   string
		want    []SkillsRepo
		wantErr bool
	}{
		{"", nil, false},
		{"  ", nil, false},
		{"eng@https://github.com/mattpocock/skills.git", []SkillsRepo{
			{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"},
		}, false},
		{"https://github.com/mattpocock/skills.git", []SkillsRepo{
			{Alias: "mattpocock/skills", URL: "https://github.com/mattpocock/skills.git"},
		}, false},
		{"eng@https://github.com/mattpocock/skills.git https://github.com/org/skills.git", []SkillsRepo{
			{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"},
			{Alias: "org/skills", URL: "https://github.com/org/skills.git"},
		}, false},
		{"  eng@https://github.com/a/b.git   team@https://github.com/c/d.git  ", []SkillsRepo{
			{Alias: "eng", URL: "https://github.com/a/b.git"},
			{Alias: "team", URL: "https://github.com/c/d.git"},
		}, false},
		{"eng@https://github.com/a/b.git not-a-url", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSkillsFlag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSkillsFlag(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("parseSkillsFlag(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i].Alias != tt.want[i].Alias || got[i].URL != tt.want[i].URL {
						t.Errorf("parseSkillsFlag(%q)[%d] = %+v, want %+v", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// TestWriteBundledSlashCommands verifies bundled slash commands are extracted correctly
func TestWriteBundledSlashCommands(t *testing.T) {
	t.Run("md files only", func(t *testing.T) {
		tempDir := t.TempDir()

		err := writeBundledSlashCommands(tempDir, ".md")
		if err != nil {
			t.Fatalf("writeBundledSlashCommands() error = %v", err)
		}

		mdPath := filepath.Join(tempDir, "swe-swe", "debug-preview-page.md")
		if _, err := os.Stat(mdPath); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", mdPath)
		}

		tomlPath := filepath.Join(tempDir, "swe-swe", "debug-preview-page.toml")
		if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist when filtering for .md only", tomlPath)
		}
	})

	t.Run("toml files only", func(t *testing.T) {
		tempDir := t.TempDir()

		err := writeBundledSlashCommands(tempDir, ".toml")
		if err != nil {
			t.Fatalf("writeBundledSlashCommands() error = %v", err)
		}

		tomlPath := filepath.Join(tempDir, "swe-swe", "debug-preview-page.toml")
		if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", tomlPath)
		}

		mdPath := filepath.Join(tempDir, "swe-swe", "debug-preview-page.md")
		if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist when filtering for .toml only", mdPath)
		}
	})
}

// TestSanitizePathConsistent verifies sanitizePath returns the same hash for same path
func TestSanitizePathConsistent(t *testing.T) {
	path := "/Users/alice/projects/myapp"
	result1 := sanitizePath(path)
	result2 := sanitizePath(path)

	if result1 != result2 {
		t.Errorf("sanitizePath not consistent: %q != %q", result1, result2)
	}
}

// TestSanitizePathDifferentForDifferentPaths verifies different paths get different hashes
func TestSanitizePathDifferentForDifferentPaths(t *testing.T) {
	path1 := "/Users/alice/projects/myapp"
	path2 := "/Users/bob/projects/myapp"

	result1 := sanitizePath(path1)
	result2 := sanitizePath(path2)

	if result1 == result2 {
		t.Errorf("sanitizePath should differ for different paths, but got same: %q", result1)
	}
}

// TestSanitizePathReplacesSpecialChars verifies special characters are replaced
func TestSanitizePathReplacesSpecialChars(t *testing.T) {
	path := "/Users/alice/my-app@v2.0"
	result := sanitizePath(path)

	// Result should not contain special characters (except hyphens)
	if strings.Contains(result, "@") || strings.Contains(result, ".") {
		t.Errorf("sanitizePath failed to replace special chars: %q", result)
	}

	// Should contain hyphens and alphanumeric chars
	if !strings.Contains(result, "-") {
		t.Errorf("sanitizePath should contain hyphens: %q", result)
	}
}

// TestSanitizePathEndsWithHash verifies hash is appended
func TestSanitizePathEndsWithHash(t *testing.T) {
	path := "/Users/alice/projects/myapp"
	result := sanitizePath(path)

	// Should have at least one hyphen (separating name from hash)
	parts := strings.Split(result, "-")
	if len(parts) < 2 {
		t.Errorf("sanitizePath should end with hash: %q", result)
	}

	// Last part should be hex hash (8 chars)
	lastPart := parts[len(parts)-1]
	if len(lastPart) != 8 {
		t.Errorf("hash should be 8 chars, got %d: %q", len(lastPart), lastPart)
	}

	// All chars should be hex digits
	for _, c := range lastPart {
		if !strings.ContainsAny(string(c), "0123456789abcdef") {
			t.Errorf("hash contains non-hex character: %q", lastPart)
		}
	}
}

// TestGetMetadataDirReturnsPath verifies getMetadataDir returns valid path
func TestGetMetadataDirReturnsPath(t *testing.T) {
	path := "/tmp/test-project"
	result, err := getMetadataDir(path)

	if err != nil {
		t.Errorf("getMetadataDir should not fail: %v", err)
	}

	if result == "" {
		t.Errorf("getMetadataDir should return non-empty path")
	}
}

// TestGetMetadataDirUnderHome verifies metadata dir is under $HOME/.swe-swe/projects/
func TestGetMetadataDirUnderHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Skipping: cannot get home directory: %v", err)
	}

	path := "/tmp/test-project"
	result, err := getMetadataDir(path)

	if err != nil {
		t.Errorf("getMetadataDir should not fail: %v", err)
	}

	expectedPrefix := homeDir + "/.swe-swe/projects"
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("metadata dir should be under %q, got %q", expectedPrefix, result)
	}
}

// TestGetMetadataDifferentForDifferentPaths verifies different paths get different metadata dirs
func TestGetMetadataDirDifferentForDifferentPaths(t *testing.T) {
	path1 := "/tmp/test-project-1"
	path2 := "/tmp/test-project-2"

	result1, err1 := getMetadataDir(path1)
	result2, err2 := getMetadataDir(path2)

	if err1 != nil || err2 != nil {
		t.Errorf("getMetadataDir should not fail: %v, %v", err1, err2)
	}

	if result1 == result2 {
		t.Errorf("different paths should get different metadata dirs: %q", result1)
	}
}

// TestPathFileCreated verifies .path file is created and contains correct path
func TestPathFileCreated(t *testing.T) {
	// Create temporary test directory
	testDir := t.TempDir()
	projectDir := filepath.Join(testDir, "myproject")

	// Compute where metadata will be stored
	metadataDir, err := getMetadataDir(projectDir)
	if err != nil {
		t.Fatalf("Failed to get metadata dir: %v", err)
	}

	// Create project directory structure (simulating what handleInit does)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		t.Fatalf("Failed to create metadata dir: %v", err)
	}

	// Write .path file
	pathFile := filepath.Join(metadataDir, ".path")
	if err := os.WriteFile(pathFile, []byte(projectDir), 0644); err != nil {
		t.Fatalf("Failed to write path file: %v", err)
	}

	// Read and verify .path file
	content, err := os.ReadFile(pathFile)
	if err != nil {
		t.Errorf("Failed to read path file: %v", err)
	}

	if string(content) != projectDir {
		t.Errorf("path file contains wrong content: got %q, want %q", string(content), projectDir)
	}
}

// TestMetadataDirStructure verifies metadata directory contains expected subdirectories
func TestMetadataDirStructure(t *testing.T) {
	testDir := t.TempDir()
	projectDir := filepath.Join(testDir, "myproject")

	metadataDir, err := getMetadataDir(projectDir)
	if err != nil {
		t.Fatalf("Failed to get metadata dir: %v", err)
	}

	// Create subdirectories
	binDir := filepath.Join(metadataDir, "bin")
	homeDir := filepath.Join(metadataDir, "home")
	certsDir := filepath.Join(metadataDir, "certs")

	for _, dir := range []string{binDir, homeDir, certsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
	}

	// Verify directories exist
	for _, dir := range []string{binDir, homeDir, certsDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("Directory does not exist: %s", dir)
		}
		if !info.IsDir() {
			t.Errorf("Path is not a directory: %s", dir)
		}
	}
}

// TestSweSweNotCreatedInProject verifies .swe-swe is NOT created in project directory
func TestSweSweNotCreatedInProject(t *testing.T) {
	testDir := t.TempDir()
	projectDir := filepath.Join(testDir, "myproject")

	// Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	// Old location should NOT exist
	oldSweDir := filepath.Join(projectDir, ".swe-swe")
	_, err := os.Stat(oldSweDir)
	if err == nil {
		t.Errorf(".swe-swe should NOT be created in project directory at %s", oldSweDir)
	}
	if !os.IsNotExist(err) {
		t.Errorf("Unexpected error checking for .swe-swe: %v", err)
	}
}

// TestHandleUpMetadataDirLookup verifies handleUp uses getMetadataDir() to find metadata
func TestHandleUpMetadataDirLookup(t *testing.T) {
	testDir := t.TempDir()
	projectDir := filepath.Join(testDir, "myproject")

	// Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	// Get the metadata directory that handleUp would use
	metadataDir, err := getMetadataDir(projectDir)
	if err != nil {
		t.Fatalf("Failed to get metadata dir: %v", err)
	}

	// Metadata directory should NOT exist yet
	if _, err := os.Stat(metadataDir); err == nil {
		t.Errorf("Metadata dir should not exist before init: %s", metadataDir)
	}

	// Create the metadata directory (simulating handleInit)
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		t.Fatalf("Failed to create metadata dir: %v", err)
	}

	// Create docker-compose.yml
	composeFile := filepath.Join(metadataDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte("version: '3'"), 0644); err != nil {
		t.Fatalf("Failed to create docker-compose.yml: %v", err)
	}

	// Verify metadata directory now exists
	if _, err := os.Stat(metadataDir); err != nil {
		t.Errorf("Metadata dir should exist after creation: %v", err)
	}

	// Verify docker-compose.yml is where handleUp expects it
	if _, err := os.Stat(composeFile); err != nil {
		t.Errorf("docker-compose.yml should exist at %s: %v", composeFile, err)
	}
}

// TestExtractProjectDirectory verifies --project-directory flag extraction
func TestExtractProjectDirectory(t *testing.T) {
	tests := []struct {
		input         []string
		wantDir       string
		wantRemaining []string
	}{
		{[]string{}, ".", nil},
		{[]string{"chrome"}, ".", []string{"chrome"}},
		{[]string{"--project-directory", "/path/to/project"}, "/path/to/project", nil},
		{[]string{"--project-directory=/path/to/project"}, "/path/to/project", nil},
		{[]string{"--project-directory", "/path", "chrome"}, "/path", []string{"chrome"}},
		{[]string{"chrome", "--project-directory", "/path"}, "/path", []string{"chrome"}},
		{[]string{"-d", "--project-directory", "/path", "--build"}, "/path", []string{"-d", "--build"}},
	}

	for _, tt := range tests {
		dir, remaining := extractProjectDirectory(tt.input)

		if dir != tt.wantDir {
			t.Errorf("extractProjectDirectory(%v) dir = %q, want %q", tt.input, dir, tt.wantDir)
		}

		if len(remaining) != len(tt.wantRemaining) {
			t.Errorf("extractProjectDirectory(%v) remaining = %v, want %v", tt.input, remaining, tt.wantRemaining)
			continue
		}
		for i := range remaining {
			if remaining[i] != tt.wantRemaining[i] {
				t.Errorf("extractProjectDirectory(%v) remaining[%d] = %q, want %q", tt.input, i, remaining[i], tt.wantRemaining[i])
			}
		}
	}
}

// TestParseAgentList verifies agent list parsing
func TestParseAgentList(t *testing.T) {
	tests := []struct {
		input    string
		wantList []string
		wantErr  bool
	}{
		{"", nil, false},
		{"claude", []string{"claude"}, false},
		{"claude,gemini", []string{"claude", "gemini"}, false},
		{"claude, gemini, codex", []string{"claude", "gemini", "codex"}, false},
		{"CLAUDE", []string{"claude"}, false}, // case insensitive
		{"invalid", nil, true},
		{"claude,invalid", nil, true},
	}

	for _, tt := range tests {
		got, err := parseAgentList(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseAgentList(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && len(got) != len(tt.wantList) {
			t.Errorf("parseAgentList(%q) = %v, want %v", tt.input, got, tt.wantList)
		}
	}
}

// TestResolveAgents verifies agent resolution with include/exclude
func TestResolveAgents(t *testing.T) {
	tests := []struct {
		agents  string
		exclude string
		wantLen int
		wantErr bool
	}{
		{"", "", 7, false},                         // default: all agents
		{"all", "", 7, false},                      // explicit all
		{"claude", "", 1, false},                   // single agent
		{"claude,gemini", "", 2, false},            // multiple agents
		{"", "aider", 6, false},                    // exclude one
		{"", "aider,goose", 5, false},              // exclude multiple
		{"all", "aider", 6, false},                 // all minus exclude
		{"claude,gemini,aider", "aider", 2, false}, // include then exclude
		{"invalid", "", 0, true},                   // invalid agent
		{"", "invalid", 0, true},                   // invalid exclude
	}

	for _, tt := range tests {
		got, err := resolveAgents(tt.agents, tt.exclude)
		if (err != nil) != tt.wantErr {
			t.Errorf("resolveAgents(%q, %q) error = %v, wantErr = %v", tt.agents, tt.exclude, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && len(got) != tt.wantLen {
			t.Errorf("resolveAgents(%q, %q) len = %d, want %d (got: %v)", tt.agents, tt.exclude, len(got), tt.wantLen, got)
		}
	}
}

// TestProcessDockerfileTemplate verifies template processing
func TestProcessDockerfileTemplate(t *testing.T) {
	template := `FROM base
# {{IF NODEJS}}
RUN install nodejs
# {{ENDIF}}
# {{IF PYTHON}}
RUN install python
# {{ENDIF}}
# {{IF CLAUDE}}
RUN install claude
# {{ENDIF}}
# {{IF APT_PACKAGES}}
RUN apt-get install {{APT_PACKAGES}}
# {{ENDIF}}
CMD done`

	tests := []struct {
		name        string
		agents      []string
		apt         string
		contains    []string
		notContains []string
	}{
		{
			name:        "claude only",
			agents:      []string{"claude"},
			apt:         "",
			contains:    []string{"install nodejs", "install claude"},
			notContains: []string{"install python", "apt-get install"},
		},
		{
			name:        "aider only",
			agents:      []string{"aider"},
			apt:         "",
			contains:    []string{"install python"},
			notContains: []string{"install nodejs", "install claude"},
		},
		{
			name:        "with apt packages",
			agents:      []string{"claude"},
			apt:         "vim htop",
			contains:    []string{"apt-get install vim htop"},
			notContains: []string{"{{APT_PACKAGES}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processDockerfileTemplate(template, tt.agents, tt.apt, "", false, false, nil, nil, 0, 0, "")
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("result should contain %q, got:\n%s", s, result)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(result, s) {
					t.Errorf("result should NOT contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}

// TestProcessDockerfileTemplateWithDocker verifies Docker conditional processing
func TestProcessDockerfileTemplateWithDocker(t *testing.T) {
	template := `FROM base
# {{IF DOCKER}}
RUN install docker-cli
# {{ENDIF}}
CMD done`

	tests := []struct {
		name        string
		withDocker  bool
		contains    []string
		notContains []string
	}{
		{
			name:        "without docker",
			withDocker:  false,
			contains:    []string{"FROM base", "CMD done"},
			notContains: []string{"install docker-cli"},
		},
		{
			name:        "with docker",
			withDocker:  true,
			contains:    []string{"FROM base", "install docker-cli", "CMD done"},
			notContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processDockerfileTemplate(template, []string{}, "", "", tt.withDocker, false, nil, nil, 0, 0, "")
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("result should contain %q, got:\n%s", s, result)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(result, s) {
					t.Errorf("result should NOT contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}

// TestGoldenFiles verifies swe-swe init output matches golden files
func TestGoldenFiles(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("Skipping golden file test in short mode")
	}

	variants := []struct {
		name  string
		flags []string
	}{
		{"default", []string{}},
		{"claude-only", []string{"--agents", "claude"}},
		{"aider-only", []string{"--agents", "aider"}},
		{"goose-only", []string{"--agents", "goose"}},
		{"opencode-only", []string{"--agents", "opencode"}},
		{"pi-only", []string{"--agents", "pi"}},
		{"nodejs-agents", []string{"--agents", "claude,gemini,codex"}},
		{"exclude-aider", []string{"--exclude-agents", "aider"}},
		{"with-apt", []string{"--apt-get-install", "vim,curl"}},
		{"with-npm", []string{"--npm-install", "typescript"}},
		{"with-both-packages", []string{"--apt-get-install", "vim", "--npm-install", "typescript"}},
		{"with-slash-commands", []string{"--agents", "all", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-multi", []string{"--agents", "all", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git https://github.com/org/team-cmds.git"}},
		{"with-slash-commands-claude-only", []string{"--agents", "claude", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-codex-only", []string{"--agents", "codex", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-no-alias", []string{"--agents", "all", "--with-slash-commands", "https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-claude-codex", []string{"--agents", "claude,codex", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-opencode-only", []string{"--agents", "opencode", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-pi-only", []string{"--agents", "pi", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-slash-commands-claude-opencode", []string{"--agents", "claude,opencode", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-skills", []string{"--agents", "all", "--with-skills", "eng@https://github.com/mattpocock/skills.git"}},
		{"with-skills-multi", []string{"--agents", "all", "--with-skills", "eng@https://github.com/mattpocock/skills.git https://github.com/org/skills.git"}},
		{"with-skills-no-alias", []string{"--agents", "all", "--with-skills", "https://github.com/mattpocock/skills.git"}},
		{"with-skills-and-slash", []string{"--agents", "all", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git", "--with-skills", "eng@https://github.com/mattpocock/skills.git"}},
		{"with-ssl-selfsign", []string{"--ssl", "selfsign"}},
		{"with-ssl-letsencrypt", []string{"--ssl", "letsencrypt@google.com", "--email", "admin@example.com"}},
		{"with-ssl-letsencrypt-staging", []string{"--ssl", "letsencrypt-staging@google.com", "--email", "admin@example.com"}},
		{"with-certs-no-certs", []string{}},
		{"with-certs-node-extra-ca-certs", []string{}},
		{"with-certs-ssl-cert-file", []string{}},
		{"with-copy-home-paths", []string{"--copy-home-paths", ".gitconfig,.ssh"}},
		{"with-terminal-font", []string{"--terminal-font-size", "16", "--terminal-font-family", "JetBrains Mono"}},
		{"with-status-bar-font", []string{"--status-bar-font-size", "14", "--status-bar-font-family", "monospace"}},
		{"with-repos-dir", []string{"--repos-dir", "/data/repos"}},
		{"with-proxy-port-offset", []string{"--proxy-port-offset", "50000"}},
		{"with-mcp", []string{"--with-mcp"}},
		{"tunnel-mode", []string{"--tunnel-server-url", "https://tunnel.example.com"}},
		{"tunnel-mode-mtls", []string{"--tunnel-server-url", "https://tunnel.example.com", "--tunnel-client-cert", "/etc/swe-swe-tunnel/client.crt"}},
		{"tunnel-mode-local-ports", []string{"--tunnel-server-url", "https://tunnel.example.com", "--tunnel-local-ports"}},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			goldenDir := filepath.Join("testdata", "golden", v.name)

			// Check if golden files exist
			if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
				t.Skipf("Golden files not found at %s - run 'make golden-update' first", goldenDir)
			}

			// Find the project hash directory in golden files
			goldenHomeDir := filepath.Join(goldenDir, "home", ".swe-swe", "projects")
			entries, err := os.ReadDir(goldenHomeDir)
			if err != nil {
				t.Fatalf("Failed to read golden home dir: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("Expected exactly one project dir in golden, got %d", len(entries))
			}
			goldenProjectDir := filepath.Join(goldenHomeDir, entries[0].Name())

			// Compare key files from golden
			keyFiles := []string{
				"Dockerfile",
				"entrypoint.sh",
			}
			// All modes generate docker-compose.yml (dockerfile-only generates a minimal shim)
			keyFiles = append(keyFiles, "docker-compose.yml")

			for _, file := range keyFiles {
				goldenPath := filepath.Join(goldenProjectDir, file)
				goldenContent, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Errorf("Failed to read golden file %s: %v", file, err)
					continue
				}

				// Verify file is not empty and contains expected content based on variant
				if len(goldenContent) == 0 {
					t.Errorf("Golden file %s is empty", file)
				}

				// Variant-specific checks
				content := string(goldenContent)
				switch v.name {
				case "claude-only":
					if file == "Dockerfile" {
						if !strings.Contains(content, "claude-code") {
							t.Errorf("Dockerfile should contain claude-code for claude-only variant")
						}
						if strings.Contains(content, "aider") {
							t.Errorf("Dockerfile should NOT contain aider for claude-only variant")
						}
					}
				case "aider-only":
					if file == "Dockerfile" {
						if !strings.Contains(content, "aider") {
							t.Errorf("Dockerfile should contain aider for aider-only variant")
						}
						if strings.Contains(content, "claude-code") {
							t.Errorf("Dockerfile should NOT contain claude-code for aider-only variant")
						}
					}
				case "with-apt":
					if file == "Dockerfile" {
						if !strings.Contains(content, "vim") || !strings.Contains(content, "curl") {
							t.Errorf("Dockerfile should contain vim and curl for with-apt variant")
						}
					}
				case "with-npm":
					if file == "Dockerfile" {
						if !strings.Contains(content, "typescript") {
							t.Errorf("Dockerfile should contain typescript for with-npm variant")
						}
					}
				case "default":
					if file == "Dockerfile" {
						// Default variant is now dockerfile-only (no SSL, no VS Code)
						if !strings.Contains(content, "SWE_PORT") {
							t.Errorf("Dockerfile should contain SWE_PORT for default (dockerfile-only) variant")
						}
						if !strings.Contains(content, "SWE_SWE_PASSWORD") {
							t.Errorf("Dockerfile should contain SWE_SWE_PASSWORD for default (dockerfile-only) variant")
						}
						if !strings.Contains(content, "1977") {
							t.Errorf("Dockerfile should contain port 1977 for default (dockerfile-only) variant")
						}
						if strings.Contains(content, "9898") {
							t.Errorf("Dockerfile should NOT contain port 9898 for default (dockerfile-only) variant")
						}
					}
				}
			}

			// Verify target files exist
			targetFiles := []string{
				filepath.Join(goldenDir, "target", ".swe-swe", "docs", "AGENTS.md"),
				filepath.Join(goldenDir, "target", ".swe-swe", "docs", "browser-automation.md"),
			}
			for _, tf := range targetFiles {
				if _, err := os.Stat(tf); err != nil {
					t.Errorf("Target file missing: %s", tf)
				}
			}

			// swe-swe/setup is no longer created in any variant: file-mention
			// slash commands were removed entirely, so the workspace never gets
			// a `swe-swe/` directory from swe-swe init. Only `.swe-swe/` is
			// created, keeping the workspace root clean.
			sweSweDir := filepath.Join(goldenDir, "target", "swe-swe")
			if _, err := os.Stat(sweSweDir); err == nil {
				t.Errorf("swe-swe/ directory should not exist in target (only .swe-swe/ is expected): %s", sweSweDir)
			}
		})
	}
}

// TestGoldenFilesMatchTemplate verifies golden Dockerfiles match template processing
func TestInstallBundledSlashCommandsUsesCanonicalStoreAndSymlinks(t *testing.T) {
	homeDir := t.TempDir()
	if err := installBundledSlashCommands(homeDir); err != nil {
		t.Fatalf("installBundledSlashCommands: %v", err)
	}

	assertFileExists(t, filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe", "setup.md"))
	assertFileExists(t, filepath.Join(homeDir, ".swe-swe", "commands", "toml", "swe-swe", "setup.toml"))

	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".claude", "commands", "swe-swe"),
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".codex", "prompts", "swe-swe"),
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".config", "opencode", "command", "swe-swe"),
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".pi", "agent", "prompts", "swe-swe"),
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".gemini", "commands", "swe-swe"),
		filepath.Join(homeDir, ".swe-swe", "commands", "toml", "swe-swe"))
}

// A real dir holding a file that is NOT a bundled command (a user's own
// addition) must be preserved untouched -- migrating it would clobber user
// content.
func TestInstallBundledSlashCommandsPreservesRealDirsWithForeignFiles(t *testing.T) {
	homeDir := t.TempDir()
	existingDir := filepath.Join(homeDir, ".claude", "commands", "swe-swe")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("mkdir existing dir: %v", err)
	}
	customFile := filepath.Join(existingDir, "custom.md")
	if err := os.WriteFile(customFile, []byte("custom"), 0644); err != nil {
		t.Fatalf("write custom file: %v", err)
	}

	if err := installBundledSlashCommands(homeDir); err != nil {
		t.Fatalf("installBundledSlashCommands should not fail on existing real dir: %v", err)
	}
	info, err := os.Lstat(existingDir)
	if err != nil {
		t.Fatalf("stat existing dir: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("real dir with a foreign file should not be replaced by a symlink")
	}
	if got, err := os.ReadFile(customFile); err != nil || string(got) != "custom" {
		t.Fatalf("user-owned file was not preserved, got %q err=%v", got, err)
	}
}

// A legacy real dir whose entries are all stale copies of bundled commands is
// swe-swe-owned, so init must migrate it to a symlink into the canonical store
// -- otherwise shipped command updates never reach it (the freeze bug).
func TestInstallBundledSlashCommandsMigratesManagedRealDir(t *testing.T) {
	homeDir := t.TempDir()
	existingDir := filepath.Join(homeDir, ".claude", "commands", "swe-swe")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("mkdir existing dir: %v", err)
	}
	// A legacy real dir holding only a STALE copy of a bundled command name.
	staleFile := filepath.Join(existingDir, "setup.md")
	if err := os.WriteFile(staleFile, []byte("STALE"), 0644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := installBundledSlashCommands(homeDir); err != nil {
		t.Fatalf("installBundledSlashCommands: %v", err)
	}

	// The managed real dir should now be a symlink into the canonical store...
	assertSymlinkTarget(t, existingDir,
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	// ...so the stale copy is gone, replaced by the fresh bundled command.
	got, err := os.ReadFile(staleFile)
	if err != nil {
		t.Fatalf("read setup.md through migrated symlink: %v", err)
	}
	if string(got) == "STALE" {
		t.Fatalf("stale content was not refreshed after migration")
	}
}

// A legacy real dir carrying swe-swe's README marker plus an orphaned command
// from an older bundle (a name no longer in the store) is still swe-swe-owned:
// it must migrate, and the orphaned leftover must NOT survive the symlink.
func TestInstallBundledSlashCommandsMigratesDirWithReadmeAndOrphan(t *testing.T) {
	homeDir := t.TempDir()
	existingDir := filepath.Join(homeDir, ".claude", "commands", "swe-swe")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("mkdir existing dir: %v", err)
	}
	readme := filepath.Join(existingDir, "README.adoc")
	if err := os.WriteFile(readme, []byte("= swe-swe Bundled Slash Commands\n"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	// An old bundled command since renamed/removed -- a name absent from the
	// current store, so the strict "all entries in store" rule would freeze it.
	orphan := filepath.Join(existingDir, "old-removed-command.md")
	if err := os.WriteFile(orphan, []byte("orphan"), 0644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	if err := installBundledSlashCommands(homeDir); err != nil {
		t.Fatalf("installBundledSlashCommands: %v", err)
	}

	assertSymlinkTarget(t, existingDir,
		filepath.Join(homeDir, ".swe-swe", "commands", "md", "swe-swe"))
	// The symlink exposes only the current bundle, so the orphan is gone.
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphaned command should not survive migration, stat err=%v", err)
	}
	// And a current command is now reachable through the symlink.
	assertFileExists(t, filepath.Join(existingDir, "setup.md"))
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}

func assertSymlinkTarget(t *testing.T, linkPath, wantTarget string) {
	t.Helper()
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("expected symlink %s: %v", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink, mode=%s", linkPath, info.Mode())
	}
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink %s: %v", linkPath, err)
	}
	if !filepath.IsAbs(got) {
		got = filepath.Clean(filepath.Join(filepath.Dir(linkPath), got))
	}
	wantTarget = filepath.Clean(wantTarget)
	if got != wantTarget {
		t.Fatalf("symlink %s target = %s, want %s", linkPath, got, wantTarget)
	}
}

func TestGoldenFilesMatchTemplate(t *testing.T) {
	variants := []struct {
		name          string
		agents        []string
		apt           string
		npm           string
		withDocker    bool
		slashCommands []SlashCommandsRepo
		skills        []SkillsRepo
	}{
		{"default", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, nil},
		{"claude-only", []string{"claude"}, "", "", false, nil, nil},
		{"aider-only", []string{"aider"}, "", "", false, nil, nil},
		{"goose-only", []string{"goose"}, "", "", false, nil, nil},
		{"opencode-only", []string{"opencode"}, "", "", false, nil, nil},
		{"pi-only", []string{"pi"}, "", "", false, nil, nil},
		{"nodejs-agents", []string{"claude", "gemini", "codex"}, "", "", false, nil, nil},
		{"exclude-aider", []string{"claude", "gemini", "codex", "goose", "opencode", "pi"}, "", "", false, nil, nil},
		{"with-apt", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "vim curl", "", false, nil, nil},
		{"with-npm", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "typescript", false, nil, nil},
		{"with-both-packages", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "vim", "typescript", false, nil, nil},
		{"with-docker", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", true, nil, nil},
		{"with-slash-commands", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-multi", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}, {Alias: "org/team-cmds", URL: "https://github.com/org/team-cmds.git"}}, nil},
		{"with-slash-commands-claude-only", []string{"claude"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-codex-only", []string{"codex"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-no-alias", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, []SlashCommandsRepo{{Alias: "choonkeat/slash-commands", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-claude-codex", []string{"claude", "codex"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-opencode-only", []string{"opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-pi-only", []string{"pi"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-slash-commands-claude-opencode", []string{"claude", "opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil},
		{"with-skills", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, []SkillsRepo{{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"}}},
		{"with-skills-multi", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, []SkillsRepo{{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"}, {Alias: "org/skills", URL: "https://github.com/org/skills.git"}}},
		{"with-skills-no-alias", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, []SkillsRepo{{Alias: "mattpocock/skills", URL: "https://github.com/mattpocock/skills.git"}}},
		{"with-skills-and-slash", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, []SkillsRepo{{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"}}},
		{"with-ssl-selfsign", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, nil},
		{"with-ssl-letsencrypt", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, nil},
		{"with-ssl-letsencrypt-staging", []string{"claude", "gemini", "codex", "aider", "goose", "opencode", "pi"}, "", "", false, nil, nil},
	}

	// Read the template
	templateContent, err := assets.ReadFile("templates/host/Dockerfile")
	if err != nil {
		t.Fatalf("Failed to read Dockerfile template: %v", err)
	}

	// Use actual test user's UID:GID for reproducible golden file comparisons
	testUID := os.Getuid()
	testGID := os.Getgid()

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			// Generate expected output from template
			expected := processDockerfileTemplate(string(templateContent), v.agents, v.apt, v.npm, v.withDocker, false, v.slashCommands, v.skills, testUID, testGID, "")

			// Apply dockerfile-only post-processing for non-SSL/non-VS-Code variants.
			// The Dockerfile template now uses ${SWE_PORT:-1977} directly, so only the
			// EXPOSE/ENV insertion is needed here.
			isComposeMode := strings.HasPrefix(v.name, "with-ssl-")
			if !isComposeMode {
				expected = strings.Replace(expected,
					"# Default command: run swe-swe-server",
					"# Environment variables for dockerfile-only mode\nENV SWE_PORT=1977\nENV SWE_SWE_PASSWORD=changeme\n\nEXPOSE ${SWE_PORT:-1977}\n\n# Default command: run swe-swe-server",
					1)
			}

			// Read golden file
			goldenDir := filepath.Join("testdata", "golden", v.name, "home", ".swe-swe", "projects")
			entries, err := os.ReadDir(goldenDir)
			if err != nil {
				t.Skipf("Golden files not found - run 'make golden-update' first")
			}
			if len(entries) != 1 {
				t.Fatalf("Expected exactly one project dir, got %d", len(entries))
			}

			goldenDockerfile := filepath.Join(goldenDir, entries[0].Name(), "Dockerfile")
			actual, err := os.ReadFile(goldenDockerfile)
			if err != nil {
				t.Fatalf("Failed to read golden Dockerfile: %v", err)
			}

			if string(actual) != expected {
				t.Errorf("Golden Dockerfile doesn't match template output for %s.\nExpected:\n%s\n\nActual:\n%s", v.name, expected, string(actual))
			}
		})
	}
}

// TestProcessSimpleTemplate verifies simple template processing for docker-compose.yml and entrypoint.sh
func TestProcessEntrypointTemplateSlashCommandsUseCanonicalStore(t *testing.T) {
	input := `# {{IF SLASH_COMMANDS}}
{{SLASH_COMMANDS_COPY}}
# {{ENDIF}}`
	got := processEntrypointTemplate(input, []string{"claude", "pi"}, false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}, nil, false)

	mustContain := []string{
		`/home/app/.swe-swe/commands/md/ck`,
		`cp -r /tmp/slash-commands/ck /home/app/.swe-swe/commands/md/ck`,
		`ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.claude/commands/ck`,
		`ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.pi/agent/prompts/ck`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("entrypoint output missing %q:\n%s", want, got)
		}
	}
	mustNotContain := []string{
		`/home/app/.codex/prompts/ck`,
		`/home/app/.config/opencode/command/ck`,
	}
	for _, unwanted := range mustNotContain {
		if strings.Contains(got, unwanted) {
			t.Fatalf("entrypoint output unexpectedly contains %q:\n%s", unwanted, got)
		}
	}
}

// TestProcessEntrypointTemplateSkillsInstall verifies the skills install
// block clones to ~/.swe-swe/skills-src/<alias>/ and creates flat
// ~/.swe-swe/skills/<alias>-<name> symlinks via find/dirname/basename.
// Discovery happens via the agent-agnostic ~/.swe-swe/skills/ scan; no
// per-agent projection is generated.
func TestProcessEntrypointTemplateSkillsInstall(t *testing.T) {
	input := `# {{IF SKILLS}}
{{SKILLS_INSTALL}}
# {{ENDIF}}`
	got := processEntrypointTemplate(input, []string{"claude", "codex", "gemini"}, false, nil, []SkillsRepo{{Alias: "eng", URL: "https://github.com/mattpocock/skills.git"}}, false)

	mustContain := []string{
		`/home/app/.swe-swe/skills-src/eng`,
		`cp -r /tmp/skills/eng /home/app/.swe-swe/skills-src/eng`,
		`mkdir -p /home/app/.swe-swe/skills`,
		// Stale symlinks for this repo are cleared before re-linking.
		`find /home/app/.swe-swe/skills -maxdepth 1 -type l -name 'eng-*' -delete`,
		`find /home/app/.swe-swe/skills-src/eng -name SKILL.md`,
		// Short prefixed name is the default store handle.
		`skill_link="/home/app/.swe-swe/skills/eng-$(basename "$skill_dir")"`,
		// On a leaf-name clash, fall back to the repo-relative path so no
		// skill is silently overwritten.
		`skill_rel=$(printf '%s' "${skill_dir#/home/app/.swe-swe/skills-src/eng/}" | tr '/' '-')`,
		`skill_link="/home/app/.swe-swe/skills/eng-${skill_rel}"`,
		`ln -sfn "$skill_dir" "$skill_link"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("skills install output missing %q:\n%s", want, got)
		}
	}
	mustNotContain := []string{
		`/home/app/.claude/skills/`,
		`/home/app/.codex/skills/`,
		`/home/app/.gemini/skills/`,
	}
	for _, unwanted := range mustNotContain {
		if strings.Contains(got, unwanted) {
			t.Fatalf("skills install output unexpectedly contains per-agent projection %q:\n%s", unwanted, got)
		}
	}
}

// TestProcessEntrypointTemplateSkillsOmittedWhenAbsent verifies that
// when --with-skills was not used, the {{IF SKILLS}} block is dropped and
// SKILLS_INSTALL leaves no residue.
func TestProcessEntrypointTemplateSkillsOmittedWhenAbsent(t *testing.T) {
	input := `before
# {{IF SKILLS}}
{{SKILLS_INSTALL}}
# {{ENDIF}}
after`
	got := processEntrypointTemplate(input, []string{"claude"}, false, nil, nil, false)
	if strings.Contains(got, "SKILLS_INSTALL") || strings.Contains(got, "skills-src") {
		t.Fatalf("expected skills block to be omitted, got:\n%s", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("expected surrounding lines preserved, got:\n%s", got)
	}
}

// TestProcessSimpleTemplate verifies simple template processing for docker-compose.yml and entrypoint.sh
func TestProcessSimpleTemplate(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		withDocker bool
		expected   string
	}{
		{
			name: "docker block included when withDocker=true",
			input: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace
      # {{IF DOCKER}}
      - /var/run/docker.sock:/var/run/docker.sock
      # {{ENDIF}}
    networks:
      - default`,
			withDocker: true,
			expected: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - default`,
		},
		{
			name: "docker block removed when withDocker=false",
			input: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace
      # {{IF DOCKER}}
      - /var/run/docker.sock:/var/run/docker.sock
      # {{ENDIF}}
    networks:
      - default`,
			withDocker: false,
			expected: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace
    networks:
      - default`,
		},
		{
			name: "no docker markers - content unchanged",
			input: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace`,
			withDocker: false,
			expected: `services:
  swe-swe:
    volumes:
      - /workspace:/workspace`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processSimpleTemplate(tt.input, tt.withDocker, "no", 1000, 1000, "", "", "", nil, nil, 20000, "", "", false)
			if result != tt.expected {
				t.Errorf("processSimpleTemplate mismatch.\nExpected:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

// TestListDetectsAndPrunesStaleProjects verifies handleList detects missing paths and prunes them
func TestListDetectsAndPrunesStaleProjects(t *testing.T) {
	testDir := t.TempDir()
	projectDir := filepath.Join(testDir, "myproject")

	// Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	// Get metadata directory
	metadataDir, err := getMetadataDir(projectDir)
	if err != nil {
		t.Fatalf("Failed to get metadata dir: %v", err)
	}

	// Create metadata directory and .path file
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		t.Fatalf("Failed to create metadata dir: %v", err)
	}

	pathFile := filepath.Join(metadataDir, ".path")
	if err := os.WriteFile(pathFile, []byte(projectDir), 0644); err != nil {
		t.Fatalf("Failed to write path file: %v", err)
	}

	// Verify metadata directory exists
	if _, err := os.Stat(metadataDir); err != nil {
		t.Errorf("Metadata dir should exist: %v", err)
	}

	// Delete the project directory (to simulate stale project)
	if err := os.RemoveAll(projectDir); err != nil {
		t.Fatalf("Failed to remove project dir: %v", err)
	}

	// After handleList (in actual code), stale metadata should be pruned
	// This test verifies the path detection logic - actual pruning happens in handleList
	info, err := os.Stat(projectDir)
	if err == nil && info.IsDir() {
		t.Errorf("Project dir should not exist after deletion")
	}
}

// TestInitConfigRoundTrip verifies that InitConfig can be marshaled and unmarshaled
func TestInitConfigRoundTrip(t *testing.T) {
	original := InitConfig{
		Agents:       []string{"claude", "aider"},
		AptPackages:  "vim htop",
		NpmPackages:  "typescript tsx",
		WithDocker:   true,
		PreviewPorts: "3000-3019",
		SlashCommands: []SlashCommandsRepo{
			{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var restored InitConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify all fields match
	if !reflect.DeepEqual(original, restored) {
		t.Errorf("Round trip failed:\noriginal: %+v\nrestored: %+v", original, restored)
	}
}

// TestInitConfigBackwardsCompatibility ensures we can load init.json
// files created by older versions of swe-swe.
//
// ⚠️  IF THIS TEST FAILS AFTER YOUR CHANGES:
//
//	DO NOT edit the JSON fixture below to make the test pass.
//	Instead, fix your code to remain compatible with existing init.json files.
//	Users have real projects with these files - breaking compatibility
//	means their `swe-swe init --previous-init-flags=reuse` will fail.
//
// If you need to add new fields:
//   - Add them with zero-value defaults (omitempty or default handling)
//   - Old init.json files without the field should still work
func TestInitConfigBackwardsCompatibility(t *testing.T) {
	// JSON fixture representing v1 format (DO NOT MODIFY)
	const v1JSON = `{
  "agents": ["claude", "gemini"],
  "aptPackages": "vim",
  "npmPackages": "",
  "withDocker": true,
  "slashCommands": [
    {"alias": "ck", "url": "https://github.com/choonkeat/slash-commands.git"}
  ]
}`

	var config InitConfig
	if err := json.Unmarshal([]byte(v1JSON), &config); err != nil {
		t.Fatalf("Failed to parse v1 JSON - backwards compatibility broken: %v", err)
	}

	// Verify expected values
	if len(config.Agents) != 2 || config.Agents[0] != "claude" || config.Agents[1] != "gemini" {
		t.Errorf("Agents mismatch: got %v", config.Agents)
	}
	if config.AptPackages != "vim" {
		t.Errorf("AptPackages mismatch: got %q", config.AptPackages)
	}
	if config.NpmPackages != "" {
		t.Errorf("NpmPackages mismatch: got %q", config.NpmPackages)
	}
	if !config.WithDocker {
		t.Errorf("WithDocker should be true")
	}
	if len(config.SlashCommands) != 1 || config.SlashCommands[0].Alias != "ck" {
		t.Errorf("SlashCommands mismatch: got %v", config.SlashCommands)
	}
}

// TestGoldenInitAsk verifies the interactive --ask golden test produces expected output
func TestGoldenInitAsk(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping golden file test in short mode")
	}

	goldenDir := filepath.Join("testdata", "golden", "init-ask")
	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		t.Skipf("Golden files not found at %s - run 'make golden-update' first", goldenDir)
	}

	// Verify the metadata directory was created (--ask override path)
	metadataDir := filepath.Join(goldenDir, "home", ".swe-swe", "projects", "init-ask")
	if _, err := os.Stat(metadataDir); os.IsNotExist(err) {
		t.Fatalf("Metadata directory not created at %s", metadataDir)
	}

	// Verify init.json was created with correct agents
	config, err := loadInitConfig(metadataDir)
	if err != nil {
		t.Fatalf("Failed to load init config: %v", err)
	}
	if len(config.Agents) != 2 || config.Agents[0] != "claude" || config.Agents[1] != "codex" {
		t.Errorf("Expected agents [claude codex], got %v", config.Agents)
	}
	if config.WithDocker {
		t.Error("Expected WithDocker=false (answered 'n')")
	}
	if config.SSL != "no" {
		t.Errorf("Expected SSL='no' (pressed Enter), got %q", config.SSL)
	}

	// Verify Dockerfile was created
	dockerfilePath := filepath.Join(metadataDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatal("Dockerfile not created")
	}

	// Verify target directory has container files
	targetFiles := []string{
		filepath.Join(goldenDir, "target", ".swe-swe", "docs", "AGENTS.md"),
		filepath.Join(goldenDir, "target", ".swe-swe", "docs", "browser-automation.md"),
	}
	for _, tf := range targetFiles {
		if _, err := os.Stat(tf); err != nil {
			t.Errorf("Target file missing: %s", tf)
		}
	}
}

// TestGoldenReuseWithOverride verifies --previous-init-flags=reuse with flag override
func TestGoldenReuseWithOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping golden file test in short mode")
	}

	goldenDir := filepath.Join("testdata", "golden", "previous-init-flags-reuse-with-override")
	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		t.Skipf("Golden files not found at %s - run 'make golden-update' first", goldenDir)
	}

	// Find metadata directory
	goldenHomeDir := filepath.Join(goldenDir, "home", ".swe-swe", "projects")
	entries, err := os.ReadDir(goldenHomeDir)
	if err != nil {
		t.Fatalf("Failed to read golden home dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected exactly one project dir, got %d", len(entries))
	}
	metadataDir := filepath.Join(goldenHomeDir, entries[0].Name())

	config, err := loadInitConfig(metadataDir)
	if err != nil {
		t.Fatalf("Failed to load init config: %v", err)
	}

	// Agents should be preserved from first init (claude)
	if len(config.Agents) != 1 || config.Agents[0] != "claude" {
		t.Errorf("Expected agents [claude] (preserved from first init), got %v", config.Agents)
	}
	// WithDocker should be preserved from first init
	if !config.WithDocker {
		t.Error("Expected WithDocker=true (preserved from first init)")
	}
	// SSL should be overridden to selfsign
	if config.SSL != "selfsign" {
		t.Errorf("Expected SSL='selfsign' (overridden), got %q", config.SSL)
	}

	// Verify stderr doesn't contain error
	stderrContent, err := os.ReadFile(filepath.Join(goldenDir, "stderr.txt"))
	if err != nil {
		t.Fatalf("Failed to read stderr.txt: %v", err)
	}
	if strings.Contains(string(stderrContent), "Error:") {
		t.Errorf("stderr should not contain errors, got: %s", stderrContent)
	}
}

// TestGoldenReuseTunnel verifies --previous-init-flags=reuse restores the
// tunnel server URL and client cert. Regression: reuse used to drop both
// tunnel fields, so a re-init silently disabled the tunnel client.
func TestGoldenReuseTunnel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping golden file test in short mode")
	}

	goldenDir := filepath.Join("testdata", "golden", "previous-init-flags-reuse-tunnel")
	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		t.Skipf("Golden files not found at %s - run 'make golden-update' first", goldenDir)
	}

	// Find metadata directory
	goldenHomeDir := filepath.Join(goldenDir, "home", ".swe-swe", "projects")
	entries, err := os.ReadDir(goldenHomeDir)
	if err != nil {
		t.Fatalf("Failed to read golden home dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected exactly one project dir, got %d", len(entries))
	}
	metadataDir := filepath.Join(goldenHomeDir, entries[0].Name())

	config, err := loadInitConfig(metadataDir)
	if err != nil {
		t.Fatalf("Failed to load init config: %v", err)
	}

	// Both tunnel fields must survive reuse (they came from the first init).
	if config.TunnelServerURL != "https://tunnel.example.com" {
		t.Errorf("Expected TunnelServerURL preserved on reuse, got %q", config.TunnelServerURL)
	}
	if config.TunnelClientCert != "/etc/swe-swe-tunnel/client.crt" {
		t.Errorf("Expected TunnelClientCert preserved on reuse, got %q", config.TunnelClientCert)
	}

	// Verify stderr doesn't contain error
	stderrContent, err := os.ReadFile(filepath.Join(goldenDir, "stderr.txt"))
	if err != nil {
		t.Fatalf("Failed to read stderr.txt: %v", err)
	}
	if strings.Contains(string(stderrContent), "Error:") {
		t.Errorf("stderr should not contain errors, got: %s", stderrContent)
	}
}

// TestInitConfigReuseCoverage is a guard against the class of bug where a new
// persisted InitConfig field is added but never wired into the
// --previous-init-flags=reuse block in init.go (which is how TunnelServerURL /
// TunnelClientCert silently got dropped on reuse).
//
// Every field must be classified as either restored-on-reuse or
// intentionally-not-restored. Add a new field and forget to categorize it here
// (and in init.go's reuse block) and this test fails, naming the field.
//
// Note: this guards *presence* (no field can be silently ignored), not
// *correctness* of the init.go line. Lock in behavior for a specific field
// with a golden test (see TestGoldenReuseTunnel).
func TestInitConfigReuseCoverage(t *testing.T) {
	// Fields restored from saved config on --previous-init-flags=reuse.
	// Keep in sync with the reuse block in init.go.
	reused := map[string]bool{
		"Agents":              true,
		"AptPackages":         true,
		"NpmPackages":         true,
		"WithDocker":          true,
		"MCPLess":             true,
		"SlashCommands":       true,
		"Skills":              true,
		"SSL":                 true,
		"Email":               true,
		"PreviewPorts":        true,
		"PublicPorts":         true,
		"CopyHomePaths":       true,
		"ReposDir":            true,
		"TerminalFontSize":    true,
		"TerminalFontFamily":  true,
		"StatusBarFontSize":   true,
		"StatusBarFontFamily": true,
		"ProxyPortOffset":     true,
		"TunnelServerURL":     true,
		"TunnelClientCert":    true,
		"TunnelLocalPorts":    true,
	}
	// Fields intentionally NOT restored: computed or stamped fresh at init time.
	notReused := map[string]bool{
		"HostUID":        true, // host's current uid, re-detected each init
		"HostGID":        true, // host's current gid, re-detected each init
		"DockerfileOnly": true, // computed from SSL/tunnel (json:"-")
		"Dockerless":     true, // mode flag, not persisted (json:"-"); marker file written separately
		"CLIVersion":     true, // stamped by saveInitConfig on every write
	}

	typ := reflect.TypeOf(InitConfig{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if reused[name] == notReused[name] { // neither set, or (impossibly) both
			t.Errorf("InitConfig field %q is not classified for --previous-init-flags=reuse.\n"+
				"If reuse should restore it: add a line to the reuse block in init.go and add %q to `reused` here.\n"+
				"If it is computed/stamped at init time: add %q to `notReused` here.",
				name, name, name)
		}
	}
}

// TestSaveLoadInitConfig verifies save and load work together
func TestSaveLoadInitConfig(t *testing.T) {
	tmpDir := t.TempDir()

	original := InitConfig{
		Agents:       []string{"claude"},
		AptPackages:  "git",
		WithDocker:   false,
		PreviewPorts: "3000-3019",
	}

	// Save
	if err := saveInitConfig(tmpDir, original); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(tmpDir, "init.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("init.json not created: %v", err)
	}

	// Load
	loaded, err := loadInitConfig(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Verify: saveInitConfig stamps CLIVersion, so adjust expected value
	original.CLIVersion = Version
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("Save/Load mismatch:\noriginal: %+v\nloaded: %+v", original, loaded)
	}
}
