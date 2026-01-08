package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
		{"feat/add-new-feature", "feat-add-new-feature"},
		{"bug_fix_issue", "bug_fix_issue"},
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
