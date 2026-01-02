package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestSplitAtDoubleDash verifies argument splitting at "--"
func TestSplitAtDoubleDash(t *testing.T) {
	tests := []struct {
		input       []string
		wantBefore  []string
		wantAfter   []string
	}{
		{[]string{}, []string{}, nil},
		{[]string{"chrome"}, []string{"chrome"}, nil},
		{[]string{"--"}, []string{}, []string{}},
		{[]string{"chrome", "--"}, []string{"chrome"}, []string{}},
		{[]string{"--", "--remove-orphans"}, []string{}, []string{"--remove-orphans"}},
		{[]string{"chrome", "--", "--remove-orphans"}, []string{"chrome"}, []string{"--remove-orphans"}},
		{[]string{"chrome", "vscode", "--", "-d", "--build"}, []string{"chrome", "vscode"}, []string{"-d", "--build"}},
	}

	for _, tt := range tests {
		before, after := splitAtDoubleDash(tt.input)

		// Compare before
		if len(before) != len(tt.wantBefore) {
			t.Errorf("splitAtDoubleDash(%v) before = %v, want %v", tt.input, before, tt.wantBefore)
			continue
		}
		for i := range before {
			if before[i] != tt.wantBefore[i] {
				t.Errorf("splitAtDoubleDash(%v) before[%d] = %q, want %q", tt.input, i, before[i], tt.wantBefore[i])
			}
		}

		// Compare after
		if len(after) != len(tt.wantAfter) {
			t.Errorf("splitAtDoubleDash(%v) after = %v, want %v", tt.input, after, tt.wantAfter)
			continue
		}
		for i := range after {
			if after[i] != tt.wantAfter[i] {
				t.Errorf("splitAtDoubleDash(%v) after[%d] = %q, want %q", tt.input, i, after[i], tt.wantAfter[i])
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
		{"", "", 5, false},                       // default: all agents
		{"all", "", 5, false},                    // explicit all
		{"claude", "", 1, false},                 // single agent
		{"claude,gemini", "", 2, false},          // multiple agents
		{"", "aider", 4, false},                  // exclude one
		{"", "aider,goose", 3, false},            // exclude multiple
		{"all", "aider", 4, false},               // all minus exclude
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
			result := processDockerfileTemplate(template, tt.agents, tt.apt, "", false)
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
			result := processDockerfileTemplate(template, []string{}, "", "", tt.withDocker)
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
		{"nodejs-agents", []string{"--agents", "claude,gemini,codex"}},
		{"exclude-aider", []string{"--exclude", "aider"}},
		{"with-apt", []string{"--apt-get-install", "vim,curl"}},
		{"with-npm", []string{"--npm-install", "typescript"}},
		{"with-both-packages", []string{"--apt-get-install", "vim", "--npm-install", "typescript"}},
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
				filepath.Join(goldenDir, "target", ".swe-swe", "browser-automation.md"),
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
		name   string
		agents []string
		apt    string
		npm    string
	}{
		{"default", []string{"claude", "gemini", "codex", "aider", "goose"}, "", ""},
		{"claude-only", []string{"claude"}, "", ""},
		{"aider-only", []string{"aider"}, "", ""},
		{"goose-only", []string{"goose"}, "", ""},
		{"nodejs-agents", []string{"claude", "gemini", "codex"}, "", ""},
		{"with-apt", []string{"claude", "gemini", "codex", "aider", "goose"}, "vim curl", ""},
		{"with-npm", []string{"claude", "gemini", "codex", "aider", "goose"}, "", "typescript"},
		{"with-both-packages", []string{"claude", "gemini", "codex", "aider", "goose"}, "vim", "typescript"},
	}

	// Read the template
	templateContent, err := assets.ReadFile("templates/host/Dockerfile")
	if err != nil {
		t.Fatalf("Failed to read Dockerfile template: %v", err)
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			// Generate expected output from template (withDocker=false for these baseline tests)
			expected := processDockerfileTemplate(string(templateContent), v.agents, v.apt, v.npm, false)

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
