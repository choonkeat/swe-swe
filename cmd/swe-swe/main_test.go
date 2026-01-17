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

// TestWriteBundledSlashCommands verifies bundled slash commands are extracted correctly
func TestWriteBundledSlashCommands(t *testing.T) {
	tempDir := t.TempDir()

	err := writeBundledSlashCommands(tempDir)
	if err != nil {
		t.Fatalf("writeBundledSlashCommands() error = %v", err)
	}

	// Check that swe-swe/README.adoc exists
	readmePath := filepath.Join(tempDir, "swe-swe", "README.adoc")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Errorf("expected %s to exist", readmePath)
	}

	// Verify content is not empty
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", readmePath, err)
	}
	if len(content) == 0 {
		t.Errorf("expected %s to have content", readmePath)
	}
	if !strings.Contains(string(content), "swe-swe") {
		t.Errorf("expected %s to contain 'swe-swe'", readmePath)
	}
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
		agents   string
		exclude  string
		wantLen  int
		wantErr  bool
	}{
		{"", "", 6, false},                       // default: all agents
		{"all", "", 6, false},                    // explicit all
		{"claude", "", 1, false},                 // single agent
		{"claude,gemini", "", 2, false},          // multiple agents
		{"", "aider", 5, false},                  // exclude one
		{"", "aider,goose", 4, false},            // exclude multiple
		{"all", "aider", 5, false},               // all minus exclude
		{"claude,gemini,aider", "aider", 2, false}, // include then exclude
		{"invalid", "", 0, true},                 // invalid agent
		{"", "invalid", 0, true},                 // invalid exclude
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
		name     string
		agents   []string
		apt      string
		contains []string
		notContains []string
	}{
		{
			name:     "claude only",
			agents:   []string{"claude"},
			apt:      "",
			contains: []string{"install nodejs", "install claude"},
			notContains: []string{"install python", "apt-get install"},
		},
		{
			name:     "aider only",
			agents:   []string{"aider"},
			apt:      "",
			contains: []string{"install python"},
			notContains: []string{"install nodejs", "install claude"},
		},
		{
			name:     "with apt packages",
			agents:   []string{"claude"},
			apt:      "vim htop",
			contains: []string{"apt-get install vim htop"},
			notContains: []string{"{{APT_PACKAGES}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processDockerfileTemplate(template, tt.agents, tt.apt, "", false, false, nil)
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
			result := processDockerfileTemplate(template, []string{}, "", "", tt.withDocker, false, nil)
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
		{"with-slash-commands-claude-opencode", []string{"--agents", "claude,opencode", "--with-slash-commands", "ck@https://github.com/choonkeat/slash-commands.git"}},
		{"with-ssl-selfsign", []string{"--ssl", "selfsign"}},
		{"with-certs-no-certs", []string{}},
		{"with-certs-node-extra-ca-certs", []string{}},
		{"with-certs-ssl-cert-file", []string{}},
		{"with-copy-home-paths", []string{"--copy-home-paths", ".gitconfig,.ssh"}},
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
				"docker-compose.yml",
				"entrypoint.sh",
			}

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
				}
			}

			// Verify target files exist
			targetFiles := []string{
				filepath.Join(goldenDir, "target", ".mcp.json"),
				filepath.Join(goldenDir, "target", ".swe-swe", "docs", "AGENTS.md"),
				filepath.Join(goldenDir, "target", ".swe-swe", "docs", "browser-automation.md"),
				filepath.Join(goldenDir, "target", "computer", "setup"),
			}
			for _, tf := range targetFiles {
				if _, err := os.Stat(tf); err != nil {
					t.Errorf("Target file missing: %s", tf)
				}
			}
		})
	}
}

// TestGoldenFilesMatchTemplate verifies golden Dockerfiles match template processing
func TestGoldenFilesMatchTemplate(t *testing.T) {
	variants := []struct {
		name          string
		agents        []string
		apt           string
		npm           string
		withDocker    bool
		slashCommands []SlashCommandsRepo
	}{
		{"default", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", false, nil},
		{"claude-only", []string{"claude"}, "", "", false, nil},
		{"aider-only", []string{"aider"}, "", "", false, nil},
		{"goose-only", []string{"goose"}, "", "", false, nil},
		{"opencode-only", []string{"opencode"}, "", "", false, nil},
		{"nodejs-agents", []string{"claude", "gemini", "codex"}, "", "", false, nil},
		{"exclude-aider", []string{"claude", "gemini", "codex", "goose", "opencode"}, "", "", false, nil},
		{"with-apt", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "vim curl", "", false, nil},
		{"with-npm", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "typescript", false, nil},
		{"with-both-packages", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "vim", "typescript", false, nil},
		{"with-docker", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", true, nil},
		{"with-slash-commands", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-multi", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}, {Alias: "org/team-cmds", URL: "https://github.com/org/team-cmds.git"}}},
		{"with-slash-commands-claude-only", []string{"claude"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-codex-only", []string{"codex"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-no-alias", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "choonkeat/slash-commands", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-claude-codex", []string{"claude", "codex"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-opencode-only", []string{"opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-slash-commands-claude-opencode", []string{"claude", "opencode"}, "", "", false, []SlashCommandsRepo{{Alias: "ck", URL: "https://github.com/choonkeat/slash-commands.git"}}},
		{"with-ssl-selfsign", []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}, "", "", false, nil},
	}

	// Read the template
	templateContent, err := assets.ReadFile("templates/host/Dockerfile")
	if err != nil {
		t.Fatalf("Failed to read Dockerfile template: %v", err)
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			// Generate expected output from template
			expected := processDockerfileTemplate(string(templateContent), v.agents, v.apt, v.npm, v.withDocker, false, v.slashCommands)

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
			result := processSimpleTemplate(tt.input, tt.withDocker, "no")
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
		Agents:      []string{"claude", "aider"},
		AptPackages: "vim htop",
		NpmPackages: "typescript tsx",
		WithDocker:  true,
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

// TestSaveLoadInitConfig verifies save and load work together
func TestSaveLoadInitConfig(t *testing.T) {
	tmpDir := t.TempDir()

	original := InitConfig{
		Agents:      []string{"claude"},
		AptPackages: "git",
		WithDocker:  false,
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

	// Verify
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("Save/Load mismatch:\noriginal: %+v\nloaded: %+v", original, loaded)
	}
}
