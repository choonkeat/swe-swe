package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDeriveBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Basic transformations
		{"Fix Login Bug", "fix-login-bug"},
		{"  spaces  around  ", "spaces-around"},
		{"UPPERCASE", "uppercase"},
		{"with_underscores", "with_underscores"},

		// Special characters
		{"special!@#chars", "special-chars"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"test@#$%^&*()test", "test-test"},

		// Unicode handling (diacritics removed)
		{"émojis and üñíçödé", "emojis-and-unicode"},
		{"café résumé", "cafe-resume"},
		{"naïve coöperate", "naive-cooperate"},

		// Edge cases
		{"", ""},
		{"a", "a"},
		{"123-numbers-456", "123-numbers-456"},
		{"---leading-trailing---", "leading-trailing"},
		{"   ", ""},

		// Real-world examples
		{"20260107-143052", "20260107-143052"},
		{"Fix: user login #123", "fix-user-login-123"},
		{"bug_fix_issue", "bug_fix_issue"},

		// Hierarchical branch names (slash preserved)
		{"feat/add-new-feature", "feat/add-new-feature"},
		{"style/email-receipt-formatting-issues", "style/email-receipt-formatting-issues"},
		{"bugfix/login/oauth", "bugfix/login/oauth"},
		{"///multiple///slashes///", "multiple/slashes"},
		{"feature/-dash-after-slash", "feature/dash-after-slash"},
		{"-/leading-slash-hyphen", "leading-slash-hyphen"}, // edge case: leading hyphen then slash

		// Dots preserved (git allows dots with restrictions)
		{"release/v1.2.3", "release/v1.2.3"},
		{"feature/issue.123", "feature/issue.123"},
		{"hotfix/bug.fix.patch", "hotfix/bug.fix.patch"},
		{"v1.0.0-rc.1", "v1.0.0-rc.1"},

		// Git dot restrictions enforced
		{"..consecutive-dots", "consecutive-dots"},  // ".." not allowed
		{".hidden-start", "hidden-start"},           // no leading dot
		{"foo/.hidden/bar", "foo/hidden/bar"},       // no leading dot per component
		{"branch.lock", "branch"},                   // ".lock" suffix not allowed
		{"foo/bar.lock", "foo/bar"},                 // ".lock" suffix not allowed
		{"...multiple...dots...", "multiple.dots"},  // collapse consecutive dots
		{"./dot-slash", "dot-slash"},                // clean up "./"
		{"foo/./bar", "foo/bar"},                    // clean up component with just "."
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := deriveBranchName(tt.input)
			if result != tt.expected {
				t.Errorf("deriveBranchName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWorktreeDirName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple-branch", "simple-branch"},
		{"feat/add-feature", "feat--add-feature"},
		{"style/email-receipt-formatting-issues", "style--email-receipt-formatting-issues"},
		{"bugfix/login/oauth", "bugfix--login--oauth"},
		{"no-slash", "no-slash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := worktreeDirName(tt.input)
			if result != tt.expected {
				t.Errorf("worktreeDirName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBranchNameFromDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple-branch", "simple-branch"},
		{"feat--add-feature", "feat/add-feature"},
		{"style--email-receipt-formatting-issues", "style/email-receipt-formatting-issues"},
		{"bugfix--login--oauth", "bugfix/login/oauth"},
		{"no-double-dash", "no-double-dash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := branchNameFromDir(tt.input)
			if result != tt.expected {
				t.Errorf("branchNameFromDir(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// setupTestGitRepo creates a temporary git repository with specified files.
// tracked files are committed, untracked files are left uncommitted.
// Returns the repo path. Cleanup is handled by t.Cleanup.
func setupTestGitRepo(t *testing.T, files map[string]struct {
	content  string
	tracked  bool
	mode     os.FileMode
	symlink  string // if non-empty, create symlink to this target
}) string {
	t.Helper()

	dir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure git user (required for commits)
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config user.email failed: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config user.name failed: %v\n%s", err, out)
	}

	// Create files
	var trackedFiles []string
	for name, spec := range files {
		path := filepath.Join(dir, name)

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create parent dir for %s: %v", name, err)
		}

		if spec.symlink != "" {
			// Create symlink
			if err := os.Symlink(spec.symlink, path); err != nil {
				t.Fatalf("failed to create symlink %s: %v", name, err)
			}
		} else {
			// Create regular file
			mode := spec.mode
			if mode == 0 {
				mode = 0644
			}
			if err := os.WriteFile(path, []byte(spec.content), mode); err != nil {
				t.Fatalf("failed to create file %s: %v", name, err)
			}
		}

		if spec.tracked {
			trackedFiles = append(trackedFiles, name)
		}
	}

	// Stage and commit tracked files
	if len(trackedFiles) > 0 {
		args := append([]string{"add"}, trackedFiles...)
		cmd = exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add failed: %v\n%s", err, out)
		}

		cmd = exec.Command("git", "commit", "-m", "Initial commit")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %v\n%s", err, out)
		}
	}

	return dir
}

func TestIsTrackedInGit(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]struct {
			content string
			tracked bool
			mode    os.FileMode
			symlink string
		}
		checkFile string
		expected  bool
	}{
		{
			name: "tracked file",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"README.md": {content: "readme", tracked: true},
			},
			checkFile: "README.md",
			expected:  true,
		},
		{
			name: "untracked file",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".env": {content: "SECRET=123", tracked: false},
			},
			checkFile: ".env",
			expected:  false,
		},
		{
			name: "tracked dotfile",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".gitignore": {content: "*.log", tracked: true},
			},
			checkFile: ".gitignore",
			expected:  true,
		},
		{
			name: "untracked dotfile in nested dir",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".claude/settings.json": {content: "{}", tracked: false},
			},
			checkFile: ".claude/settings.json",
			expected:  false,
		},
		{
			name: "nonexistent file",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"README.md": {content: "readme", tracked: true},
			},
			checkFile: "missing.txt",
			expected:  false,
		},
		{
			name: "tracked file in subdir",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"src/main.go": {content: "package main", tracked: true},
			},
			checkFile: "src/main.go",
			expected:  true,
		},
		{
			name: "untracked file in subdir",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"src/.env.local": {content: "LOCAL=1", tracked: false},
			},
			checkFile: "src/.env.local",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir := setupTestGitRepo(t, tt.files)
			result := isTrackedInGit(repoDir, tt.checkFile)
			if result != tt.expected {
				t.Errorf("isTrackedInGit(%q) = %v, want %v", tt.checkFile, result, tt.expected)
			}
		})
	}
}

func TestCopyUntrackedFiles(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]struct {
			content string
			tracked bool
			mode    os.FileMode
			symlink string
		}
		expectedCopied    []string
		expectedNotCopied []string
	}{
		{
			name: "basic dotfiles",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".env":       {content: "SECRET=123", tracked: false},
				".gitignore": {content: "*.log", tracked: true},
			},
			expectedCopied:    []string{".env"},
			expectedNotCopied: []string{".gitignore"},
		},
		{
			name: "CLAUDE.md untracked",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"CLAUDE.md": {content: "instructions", tracked: false},
			},
			expectedCopied: []string{"CLAUDE.md"},
		},
		{
			name: "CLAUDE.md tracked",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"CLAUDE.md": {content: "instructions", tracked: true},
			},
			expectedNotCopied: []string{"CLAUDE.md"},
		},
		{
			name: "AGENTS.md untracked",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"AGENTS.md": {content: "agents", tracked: false},
			},
			expectedCopied: []string{"AGENTS.md"},
		},
		{
			name: "nested dotdir",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".claude/settings.json": {content: "{}", tracked: false},
			},
			expectedCopied: []string{".claude/settings.json"},
		},
		{
			name: "excluded .swe-swe",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".swe-swe/recordings/test.log": {content: "log", tracked: false},
			},
			expectedNotCopied: []string{".swe-swe"},
		},
		{
			name: "mixed scenario",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".env":                  {content: "SECRET=123", tracked: false},
				".claude/settings.json": {content: "{}", tracked: false},
				"CLAUDE.md":             {content: "instructions", tracked: false},
				".gitignore":            {content: "*.log", tracked: true},
				"README.md":             {content: "readme", tracked: true},
			},
			expectedCopied:    []string{".env", ".claude/settings.json", "CLAUDE.md"},
			expectedNotCopied: []string{".gitignore", "README.md"},
		},
		{
			name: "empty repo - no matching files",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				"README.md": {content: "readme", tracked: true},
			},
			expectedCopied: []string{},
		},
		{
			name: "only tracked dotfiles",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".gitignore": {content: "*.log", tracked: true},
				".eslintrc":  {content: "{}", tracked: true},
			},
			expectedNotCopied: []string{".gitignore", ".eslintrc"},
		},
		{
			name: "deeply nested untracked",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".claude/mcp/servers.json": {content: "{}", tracked: false},
			},
			expectedCopied: []string{".claude/mcp/servers.json"},
		},
		{
			name: "file permissions preserved",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".env": {content: "SECRET=123", tracked: false, mode: 0600},
			},
			expectedCopied: []string{".env"},
		},
		{
			name: "symlink copied",
			files: map[string]struct {
				content string
				tracked bool
				mode    os.FileMode
				symlink string
			}{
				".env.actual": {content: "SECRET=123", tracked: false},
				".env":        {symlink: ".env.actual", tracked: false},
			},
			expectedCopied: []string{".env", ".env.actual"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := setupTestGitRepo(t, tt.files)
			destDir := t.TempDir()

			err := copyUntrackedFiles(srcDir, destDir)
			if err != nil {
				t.Fatalf("copyUntrackedFiles failed: %v", err)
			}

			// Check expected copied files exist
			for _, file := range tt.expectedCopied {
				path := filepath.Join(destDir, file)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("expected file %s to be copied, but it doesn't exist", file)
				}
			}

			// Check expected not copied files don't exist
			for _, file := range tt.expectedNotCopied {
				path := filepath.Join(destDir, file)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("expected file %s to NOT be copied, but it exists", file)
				}
			}

			// Additional checks for specific tests
			if tt.name == "file permissions preserved" {
				path := filepath.Join(destDir, ".env")
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("failed to stat .env: %v", err)
				}
				if info.Mode().Perm() != 0600 {
					t.Errorf("expected .env mode 0600, got %o", info.Mode().Perm())
				}
			}

			if tt.name == "symlink copied" {
				path := filepath.Join(destDir, ".env")
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatalf("failed to lstat .env: %v", err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					t.Errorf("expected .env to be a symlink")
				}
				target, err := os.Readlink(path)
				if err != nil {
					t.Fatalf("failed to read symlink: %v", err)
				}
				if target != ".env.actual" {
					t.Errorf("expected symlink target .env.actual, got %s", target)
				}
			}
		})
	}
}

func TestCopyUntrackedFiles_SymlinkDirectoriesCopyFiles(t *testing.T) {
	// Test that directories are symlinked (for shared agent configs)
	// and files are copied (for potential per-worktree isolation)
	files := map[string]struct {
		content string
		tracked bool
		mode    os.FileMode
		symlink string
	}{
		".claude/settings.json": {content: `{"theme":"dark"}`, tracked: false},
		".claude/mcp/config.json": {content: `{}`, tracked: false},
		".env": {content: "SECRET=123", tracked: false},
		"README.md": {content: "readme", tracked: true}, // tracked file, should not be copied
	}

	srcDir := setupTestGitRepo(t, files)
	destDir := t.TempDir()

	err := copyUntrackedFiles(srcDir, destDir)
	if err != nil {
		t.Fatalf("copyUntrackedFiles failed: %v", err)
	}

	// .claude should be a symlink pointing to absolute path in srcDir
	claudePath := filepath.Join(destDir, ".claude")
	info, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("failed to lstat .claude: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected .claude to be a symlink, got mode %v", info.Mode())
	}
	target, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("failed to readlink .claude: %v", err)
	}
	expectedTarget := filepath.Join(srcDir, ".claude")
	if target != expectedTarget {
		t.Errorf("expected .claude symlink target %q, got %q", expectedTarget, target)
	}

	// .env should be a regular file (copied, not symlinked)
	envPath := filepath.Join(destDir, ".env")
	envInfo, err := os.Lstat(envPath)
	if err != nil {
		t.Fatalf("failed to lstat .env: %v", err)
	}
	if envInfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected .env to be a regular file, but it's a symlink")
	}
	if !envInfo.Mode().IsRegular() {
		t.Errorf("expected .env to be a regular file, got mode %v", envInfo.Mode())
	}
	// Verify content was copied
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	if string(content) != "SECRET=123" {
		t.Errorf("expected .env content 'SECRET=123', got %q", content)
	}

	// README.md should not exist (tracked file)
	readmePath := filepath.Join(destDir, "README.md")
	if _, err := os.Stat(readmePath); !os.IsNotExist(err) {
		t.Errorf("expected README.md to NOT be copied, but it exists")
	}
}

func TestCopySweSweDocsDir(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string // path relative to srcDir -> content
		expectedCopied  []string          // paths relative to destDir that should exist
		expectedMissing []string          // paths relative to destDir that should NOT exist
	}{
		{
			name: "copies all files from .swe-swe/docs",
			files: map[string]string{
				".swe-swe/docs/AGENTS.md":             "# AGENTS",
				".swe-swe/docs/browser-automation.md": "# Browser Automation",
				".swe-swe/docs/docker.md":             "# Docker",
			},
			expectedCopied: []string{
				".swe-swe/docs/AGENTS.md",
				".swe-swe/docs/browser-automation.md",
				".swe-swe/docs/docker.md",
			},
		},
		{
			name: "copies all file types from docs directory",
			files: map[string]string{
				".swe-swe/docs/AGENTS.md":    "# AGENTS",
				".swe-swe/docs/notes.txt":    "some notes",
				".swe-swe/docs/config.json":  "{}",
			},
			expectedCopied: []string{
				".swe-swe/docs/AGENTS.md",
				".swe-swe/docs/notes.txt",
				".swe-swe/docs/config.json",
			},
		},
		{
			name: "ignores files at .swe-swe root level",
			files: map[string]string{
				".swe-swe/docs/AGENTS.md":    "# AGENTS",
				".swe-swe/error.log":         "error log content",
				".swe-swe/restart-loop.sh":   "#!/bin/bash",
			},
			expectedCopied:  []string{".swe-swe/docs/AGENTS.md"},
			expectedMissing: []string{".swe-swe/error.log", ".swe-swe/restart-loop.sh"},
		},
		{
			name:            "handles missing .swe-swe/docs directory gracefully",
			files:           map[string]string{},
			expectedCopied:  []string{},
			expectedMissing: []string{".swe-swe/docs"},
		},
		{
			name: "handles .swe-swe without docs subdirectory",
			files: map[string]string{
				".swe-swe/uploads/file.txt": "uploaded file",
			},
			expectedCopied:  []string{},
			expectedMissing: []string{".swe-swe/docs", ".swe-swe/uploads"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			destDir := t.TempDir()

			// Create source files
			for path, content := range tt.files {
				fullPath := filepath.Join(srcDir, path)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					t.Fatalf("failed to create parent dir: %v", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create file %s: %v", path, err)
				}
			}

			err := copySweSweDocsDir(srcDir, destDir)
			if err != nil {
				t.Fatalf("copySweSweDocsDir failed: %v", err)
			}

			// Check expected copied files exist
			for _, path := range tt.expectedCopied {
				fullPath := filepath.Join(destDir, path)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					t.Errorf("expected file %s to be copied, but it doesn't exist", path)
				}
			}

			// Check expected missing files don't exist
			for _, path := range tt.expectedMissing {
				fullPath := filepath.Join(destDir, path)
				if _, err := os.Stat(fullPath); err == nil {
					t.Errorf("expected %s to NOT be copied, but it exists", path)
				}
			}
		})
	}
}

func TestCopyFileOrDir(t *testing.T) {
	t.Run("copy regular file", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "test.txt")
		dstFile := filepath.Join(dstDir, "test.txt")

		if err := os.WriteFile(srcFile, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileOrDir(srcFile, dstFile); err != nil {
			t.Fatalf("copyFileOrDir failed: %v", err)
		}

		content, err := os.ReadFile(dstFile)
		if err != nil {
			t.Fatalf("failed to read dest file: %v", err)
		}
		if string(content) != "hello" {
			t.Errorf("content mismatch: got %q, want %q", content, "hello")
		}
	})

	t.Run("copy directory recursively", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		// Create nested structure
		srcNested := filepath.Join(srcDir, "nested")
		if err := os.MkdirAll(srcNested, 0755); err != nil {
			t.Fatalf("failed to create nested dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcNested, "file.txt"), []byte("nested"), 0644); err != nil {
			t.Fatalf("failed to create nested file: %v", err)
		}

		dstNested := filepath.Join(dstDir, "nested")
		if err := copyFileOrDir(srcNested, dstNested); err != nil {
			t.Fatalf("copyFileOrDir failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(dstNested, "file.txt"))
		if err != nil {
			t.Fatalf("failed to read nested file: %v", err)
		}
		if string(content) != "nested" {
			t.Errorf("content mismatch: got %q, want %q", content, "nested")
		}
	})

	t.Run("copy symlink", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		// Create target file and symlink
		targetFile := filepath.Join(srcDir, "target.txt")
		if err := os.WriteFile(targetFile, []byte("target"), 0644); err != nil {
			t.Fatalf("failed to create target file: %v", err)
		}

		srcLink := filepath.Join(srcDir, "link.txt")
		if err := os.Symlink("target.txt", srcLink); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		dstLink := filepath.Join(dstDir, "link.txt")
		if err := copyFileOrDir(srcLink, dstLink); err != nil {
			t.Fatalf("copyFileOrDir failed: %v", err)
		}

		// Verify it's a symlink pointing to same target
		target, err := os.Readlink(dstLink)
		if err != nil {
			t.Fatalf("failed to read symlink: %v", err)
		}
		if target != "target.txt" {
			t.Errorf("symlink target mismatch: got %q, want %q", target, "target.txt")
		}
	})

	t.Run("preserve file mode", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "secret.txt")
		dstFile := filepath.Join(dstDir, "secret.txt")

		if err := os.WriteFile(srcFile, []byte("secret"), 0600); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}

		if err := copyFileOrDir(srcFile, dstFile); err != nil {
			t.Fatalf("copyFileOrDir failed: %v", err)
		}

		info, err := os.Stat(dstFile)
		if err != nil {
			t.Fatalf("failed to stat dest file: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("mode mismatch: got %o, want 0600", info.Mode().Perm())
		}
	})
}

func TestBuildExitMessage(t *testing.T) {
	tests := []struct {
		name           string
		workDir        string
		branchName     string
		exitCode       int
		expectWorktree bool
	}{
		{
			name:           "worktree session includes worktree info",
			workDir:        "/worktrees/fix-bug",
			branchName:     "fix-bug",
			exitCode:       0,
			expectWorktree: true,
		},
		{
			name:           "empty workdir does not include worktree info",
			workDir:        "",
			branchName:     "",
			exitCode:       0,
			expectWorktree: false,
		},
		{
			name:           "workspace root does not include worktree info",
			workDir:        "/workspace",
			branchName:     "",
			exitCode:       0,
			expectWorktree: false,
		},
		{
			name:           "non-worktree path does not include worktree info",
			workDir:        "/tmp/some-dir",
			branchName:     "",
			exitCode:       0,
			expectWorktree: false,
		},
		{
			name:           "worktree session with non-zero exit code",
			workDir:        "/worktrees/test-branch",
			branchName:     "test-branch",
			exitCode:       1,
			expectWorktree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{
				WorkDir:    tt.workDir,
				BranchName: tt.branchName,
			}

			msg := buildExitMessage(sess, tt.exitCode)

			// Check exit code
			if msg["exitCode"] != tt.exitCode {
				t.Errorf("exitCode = %v, want %v", msg["exitCode"], tt.exitCode)
			}

			// Check type
			if msg["type"] != "exit" {
				t.Errorf("type = %v, want 'exit'", msg["type"])
			}

			// Check worktree field
			worktree, hasWorktree := msg["worktree"]
			if tt.expectWorktree {
				if !hasWorktree {
					t.Errorf("expected worktree field to be present")
				} else {
					wt, ok := worktree.(map[string]string)
					if !ok {
						t.Errorf("worktree is not map[string]string: %T", worktree)
					} else {
						if wt["path"] != tt.workDir {
							t.Errorf("worktree.path = %v, want %v", wt["path"], tt.workDir)
						}
						if wt["branch"] != tt.branchName {
							t.Errorf("worktree.branch = %v, want %v", wt["branch"], tt.branchName)
						}
					}
				}
			} else {
				if hasWorktree {
					t.Errorf("expected worktree field to NOT be present, got %v", worktree)
				}
			}
		})
	}
}

func TestGetGitRoot(t *testing.T) {
	// Create a temp git repo
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Change to a subdirectory and verify we still get the root
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Save current dir and change to subdir
	oldDir, _ := os.Getwd()
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(oldDir)

	root, err := getGitRoot()
	if err != nil {
		t.Fatalf("getGitRoot failed: %v", err)
	}

	// Resolve symlinks for comparison (t.TempDir may return a symlinked path)
	expectedRoot, _ := filepath.EvalSymlinks(dir)
	actualRoot, _ := filepath.EvalSymlinks(root)

	if actualRoot != expectedRoot {
		t.Errorf("getGitRoot() = %q, want %q", actualRoot, expectedRoot)
	}
}

func TestListWorktrees(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	t.Run("non-existent directory returns empty list", func(t *testing.T) {
		worktreeDir = "/nonexistent/path/that/does/not/exist"

		worktrees, err := listWorktrees()
		if err != nil {
			t.Fatalf("listWorktrees() returned error: %v", err)
		}
		if len(worktrees) != 0 {
			t.Errorf("expected empty list, got %d items", len(worktrees))
		}
	})

	t.Run("empty directory returns empty list", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		worktrees, err := listWorktrees()
		if err != nil {
			t.Fatalf("listWorktrees() returned error: %v", err)
		}
		if len(worktrees) != 0 {
			t.Errorf("expected empty list, got %d items", len(worktrees))
		}
	})

	t.Run("directory with subdirs returns them as worktrees", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		// Create some subdirectories
		os.Mkdir(filepath.Join(tmpDir, "feat-hello"), 0755)
		os.Mkdir(filepath.Join(tmpDir, "fix-bug-123"), 0755)

		// Create a file (should be ignored)
		os.WriteFile(filepath.Join(tmpDir, "somefile.txt"), []byte("ignored"), 0644)

		worktrees, err := listWorktrees()
		if err != nil {
			t.Fatalf("listWorktrees() returned error: %v", err)
		}
		if len(worktrees) != 2 {
			t.Errorf("expected 2 worktrees, got %d", len(worktrees))
		}

		// Check worktree names (order may vary)
		names := make(map[string]bool)
		for _, wt := range worktrees {
			names[wt.Name] = true
			expectedPath := filepath.Join(tmpDir, wt.Name)
			if wt.Path != expectedPath {
				t.Errorf("expected path %s, got %s", expectedPath, wt.Path)
			}
		}
		if !names["feat-hello"] {
			t.Error("expected feat-hello in worktrees")
		}
		if !names["fix-bug-123"] {
			t.Error("expected fix-bug-123 in worktrees")
		}
	})
}

func TestHandleWorktreesAPI(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	t.Run("GET returns JSON with worktrees", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		// Create a worktree directory
		os.Mkdir(filepath.Join(tmpDir, "test-branch"), 0755)

		req := httptest.NewRequest(http.MethodGet, "/api/worktrees", nil)
		w := httptest.NewRecorder()

		handleWorktreesAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", contentType)
		}

		var result struct {
			Worktrees []WorktreeInfo `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(result.Worktrees) != 1 {
			t.Errorf("expected 1 worktree, got %d", len(result.Worktrees))
		}
		if result.Worktrees[0].Name != "test-branch" {
			t.Errorf("expected worktree name 'test-branch', got %s", result.Worktrees[0].Name)
		}
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/worktrees", nil)
		w := httptest.NewRecorder()

		handleWorktreesAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("empty worktrees returns empty array", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		req := httptest.NewRequest(http.MethodGet, "/api/worktrees", nil)
		w := httptest.NewRecorder()

		handleWorktreesAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result struct {
			Worktrees []WorktreeInfo `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.Worktrees == nil {
			t.Error("expected empty array, got nil")
		}
		if len(result.Worktrees) != 0 {
			t.Errorf("expected 0 worktrees, got %d", len(result.Worktrees))
		}
	})
}

func TestHandleWorktreesAPI_WithActiveSession(t *testing.T) {
	// Save original worktreeDir and sessions, restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	t.Run("returns activeSession for worktree with running session", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		// Create a worktree directory
		branchName := "feat/test-session"
		worktreeDirName := "feat--test-session" // as it would be stored on disk
		os.Mkdir(filepath.Join(tmpDir, worktreeDirName), 0755)

		// Create a mock session with this branch name
		sessionUUID := "test-session-uuid-123"
		sessionsMu.Lock()
		sessions[sessionUUID] = &Session{
			UUID:       sessionUUID,
			Name:       "My Test Session",
			BranchName: branchName,
			Assistant:  "claude",
			AssistantConfig: AssistantConfig{
				Name:   "Claude",
				Binary: "claude",
			},
			CreatedAt: time.Now().Add(-5 * time.Minute),
			wsClients: make(map[*websocket.Conn]bool),
		}
		// Add 2 mock clients
		sessions[sessionUUID].wsClients[nil] = true // Using nil as placeholder, won't be dereferenced
		sessionsMu.Unlock()

		defer func() {
			sessionsMu.Lock()
			delete(sessions, sessionUUID)
			sessionsMu.Unlock()
		}()

		req := httptest.NewRequest(http.MethodGet, "/api/worktrees", nil)
		w := httptest.NewRecorder()

		handleWorktreesAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result struct {
			Worktrees []struct {
				Name          string `json:"name"`
				Path          string `json:"path"`
				ActiveSession *struct {
					UUID        string `json:"uuid"`
					Name        string `json:"name"`
					Assistant   string `json:"assistant"`
					ClientCount int    `json:"clientCount"`
					DurationStr string `json:"durationStr"`
				} `json:"activeSession,omitempty"`
			} `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(result.Worktrees) != 1 {
			t.Fatalf("expected 1 worktree, got %d", len(result.Worktrees))
		}

		wt := result.Worktrees[0]
		if wt.Name != branchName {
			t.Errorf("expected worktree name %q, got %q", branchName, wt.Name)
		}

		if wt.ActiveSession == nil {
			t.Fatal("expected activeSession to be populated, got nil")
		}

		if wt.ActiveSession.UUID != sessionUUID {
			t.Errorf("expected UUID %q, got %q", sessionUUID, wt.ActiveSession.UUID)
		}
		if wt.ActiveSession.Name != "My Test Session" {
			t.Errorf("expected name 'My Test Session', got %q", wt.ActiveSession.Name)
		}
		if wt.ActiveSession.Assistant != "claude" {
			t.Errorf("expected assistant 'claude', got %q", wt.ActiveSession.Assistant)
		}
		if wt.ActiveSession.ClientCount != 1 {
			t.Errorf("expected clientCount 1, got %d", wt.ActiveSession.ClientCount)
		}
		// Duration should be around 5 minutes
		if wt.ActiveSession.DurationStr != "5m" {
			t.Errorf("expected durationStr '5m', got %q", wt.ActiveSession.DurationStr)
		}
	})

	t.Run("no activeSession for worktree without running session", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		// Create a worktree directory
		os.Mkdir(filepath.Join(tmpDir, "test-branch"), 0755)

		req := httptest.NewRequest(http.MethodGet, "/api/worktrees", nil)
		w := httptest.NewRecorder()

		handleWorktreesAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result struct {
			Worktrees []struct {
				Name          string      `json:"name"`
				Path          string      `json:"path"`
				ActiveSession interface{} `json:"activeSession,omitempty"`
			} `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(result.Worktrees) != 1 {
			t.Fatalf("expected 1 worktree, got %d", len(result.Worktrees))
		}

		if result.Worktrees[0].ActiveSession != nil {
			t.Error("expected activeSession to be nil for worktree without session")
		}
	})
}

func TestWorktreeExists(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	tmpDir := t.TempDir()
	worktreeDir = tmpDir

	t.Run("returns false when dir doesn't exist", func(t *testing.T) {
		if worktreeExists("nonexistent-branch") {
			t.Error("expected false for nonexistent directory")
		}
	})

	t.Run("returns true when dir exists", func(t *testing.T) {
		os.Mkdir(filepath.Join(tmpDir, "existing-branch"), 0755)
		if !worktreeExists("existing-branch") {
			t.Error("expected true for existing directory")
		}
	})
}

func TestLocalBranchExists(t *testing.T) {
	// Create a temp git repo
	repoDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	cmd.Run()

	// Create initial commit
	testFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = repoDir
	cmd.Run()

	// Save and change directory
	oldDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(oldDir)

	t.Run("returns false for non-existent branch", func(t *testing.T) {
		if localBranchExists("nonexistent-branch") {
			t.Error("expected false for nonexistent branch")
		}
	})

	t.Run("returns true for existing branch", func(t *testing.T) {
		// Create a branch
		cmd = exec.Command("git", "branch", "test-branch")
		cmd.Dir = repoDir
		cmd.Run()

		if !localBranchExists("test-branch") {
			t.Error("expected true for existing branch")
		}
	})
}

func TestCreateWorktree_ReentryExisting(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	// Create temp dirs
	tmpDir := t.TempDir()
	worktreeDir = tmpDir

	// Pre-create worktree directory (simulating existing worktree)
	existingPath := filepath.Join(tmpDir, "existing-worktree")
	os.Mkdir(existingPath, 0755)

	// Create a marker file to verify we got the same directory back
	markerFile := filepath.Join(existingPath, ".marker")
	os.WriteFile(markerFile, []byte("marker"), 0644)

	// Call createWorktree - should return existing path without creating new one
	result, err := createWorktree("existing-worktree")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	if result != existingPath {
		t.Errorf("expected path %s, got %s", existingPath, result)
	}

	// Verify marker file still exists (we reused the directory)
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("marker file missing - directory was not reused")
	}
}

func TestCreateWorktree_Fresh(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	// Create a temp git repo
	repoDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	cmd.Run()

	// Create initial commit (required for worktree)
	testFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = repoDir
	cmd.Run()

	// Set up worktree directory inside the temp repo
	worktreeDir = filepath.Join(repoDir, ".worktrees")

	// Save and change directory
	oldDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(oldDir)

	// Call createWorktree with fresh branch name
	result, err := createWorktree("fresh-branch")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	expectedPath := filepath.Join(worktreeDir, "fresh-branch")
	if result != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, result)
	}

	// Verify worktree was created
	if _, err := os.Stat(result); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}

	// Verify branch was created
	cmd = exec.Command("git", "rev-parse", "--verify", "fresh-branch")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Error("branch was not created")
	}
}

func TestCreateWorktree_AttachLocalBranch(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	// Create a temp git repo
	repoDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	cmd.Run()

	// Create initial commit
	testFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = repoDir
	cmd.Run()

	// Create a local branch (without worktree)
	cmd = exec.Command("git", "branch", "local-only-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %v\n%s", err, out)
	}

	// Set up worktree directory
	worktreeDir = filepath.Join(repoDir, ".worktrees")

	// Save and change directory
	oldDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(oldDir)

	// Call createWorktree - should attach to existing local branch
	result, err := createWorktree("local-only-branch")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	expectedPath := filepath.Join(worktreeDir, "local-only-branch")
	if result != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, result)
	}

	// Verify worktree was created
	if _, err := os.Stat(result); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}
}

func TestHandleWorktreeCheckAPI(t *testing.T) {
	// Save original worktreeDir and restore after test
	originalWorktreeDir := worktreeDir
	defer func() { worktreeDir = originalWorktreeDir }()

	t.Run("missing name parameter returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/worktree/check", nil)
		w := httptest.NewRecorder()

		handleWorktreeCheckAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/worktree/check?name=test", nil)
		w := httptest.NewRecorder()

		handleWorktreeCheckAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("no conflict returns exists false", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		req := httptest.NewRequest(http.MethodGet, "/api/worktree/check?name=new-branch", nil)
		w := httptest.NewRecorder()

		handleWorktreeCheckAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result["exists"] != false {
			t.Errorf("expected exists=false, got %v", result["exists"])
		}
	})

	t.Run("worktree exists returns type worktree", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		// Create worktree directory
		os.Mkdir(filepath.Join(tmpDir, "existing-worktree"), 0755)

		req := httptest.NewRequest(http.MethodGet, "/api/worktree/check?name=existing-worktree", nil)
		w := httptest.NewRecorder()

		handleWorktreeCheckAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result["exists"] != true {
			t.Errorf("expected exists=true, got %v", result["exists"])
		}
		if result["type"] != "worktree" {
			t.Errorf("expected type=worktree, got %v", result["type"])
		}
		if result["branch"] != "existing-worktree" {
			t.Errorf("expected branch=existing-worktree, got %v", result["branch"])
		}
	})

	t.Run("empty name returns exists false", func(t *testing.T) {
		tmpDir := t.TempDir()
		worktreeDir = tmpDir

		req := httptest.NewRequest(http.MethodGet, "/api/worktree/check?name=", nil)
		w := httptest.NewRecorder()

		handleWorktreeCheckAPI(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})
}

// TestWorktreeMerge_InvalidPath tests security check for paths outside worktreeDir
func TestWorktreeMerge_InvalidPath(t *testing.T) {
	// Test that paths outside worktreeDir are rejected
	tests := []struct {
		name string
		path string
	}{
		{"path traversal", "/worktrees/../../../etc/passwd"},
		{"absolute path outside", "/tmp/malicious"},
		{"relative path", "worktrees/test"},
		{"empty path", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidWorktreePath(tt.path)
			if result {
				t.Errorf("isValidWorktreePath(%q) = true, want false", tt.path)
			}
		})
	}
}

// TestWorktreeMerge_ValidPath tests that valid worktree paths are accepted
func TestWorktreeMerge_ValidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"valid worktree path", "/worktrees/fix-bug"},
		{"valid worktree path with uuid", "/worktrees/fix-bug-abc12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidWorktreePath(tt.path)
			if !result {
				t.Errorf("isValidWorktreePath(%q) = false, want true", tt.path)
			}
		})
	}
}
