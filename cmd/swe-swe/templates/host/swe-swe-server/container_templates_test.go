package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDumpContainerTemplatesNoRootWrapper guards against the bug where
// fs.WalkDir's root entry (path == "container-templates", no trailing slash)
// caused an empty <destDir>/container-templates/ directory to be created
// alongside the actual extracted files.
func TestDumpContainerTemplatesNoRootWrapper(t *testing.T) {
	destDir := t.TempDir()

	if err := dumpContainerTemplates(destDir); err != nil {
		t.Fatalf("dumpContainerTemplates: %v", err)
	}

	wrapper := filepath.Join(destDir, "container-templates")
	if _, err := os.Stat(wrapper); err == nil {
		t.Errorf("dumpContainerTemplates created stray %q wrapper directory; expected files at destDir root", wrapper)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error on %q: %v", wrapper, err)
	}

	// Sanity-check that the actual templates landed at the destination root.
	for _, want := range []string{
		".swe-swe/docs/AGENTS.md",
		".swe-swe/docs/app-preview.md",
		".swe-swe/docs/browser-automation.md",
		".swe-swe/docs/docker.md",
	} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("expected %s at destDir root: %v", want, err)
		}
	}

	// The embedded payload no longer includes a swe-swe/ subtree. Assert that
	// setupSweSweFiles does not create one at the destination root either:
	// "reduce the dirtying to just .swe-swe" is the invariant here.
	if _, err := os.Stat(filepath.Join(destDir, "swe-swe")); err == nil {
		t.Errorf("dumpContainerTemplates created a swe-swe/ directory; expected only .swe-swe/ at destDir root")
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error on swe-swe/: %v", err)
	}
}

// TestSetupSweSweFilesNoRootWrapper is the same regression check for the
// session-prepare extraction path. Without the root-skip guard, every
// prepared workspace ended up with an empty container-templates/ at its root.
func TestSetupSweSweFilesNoRootWrapper(t *testing.T) {
	destDir := t.TempDir()

	if err := setupSweSweFiles(destDir); err != nil {
		t.Fatalf("setupSweSweFiles: %v", err)
	}

	wrapper := filepath.Join(destDir, "container-templates")
	if _, err := os.Stat(wrapper); err == nil {
		t.Errorf("setupSweSweFiles created stray %q wrapper directory; expected files at destDir root", wrapper)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error on %q: %v", wrapper, err)
	}

	for _, want := range []string{
		".swe-swe/docs/AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("expected %s at destDir root: %v", want, err)
		}
	}

	if _, err := os.Stat(filepath.Join(destDir, "swe-swe")); err == nil {
		t.Errorf("setupSweSweFiles created a swe-swe/ directory; expected only .swe-swe/ at destDir root")
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error on swe-swe/: %v", err)
	}
}

// TestSetupSweSweFilesMigratesEnvFile verifies the one-shot os.Rename of
// swe-swe/env -> .swe-swe/env for workspaces that were prepared by an
// earlier swe-swe version.
func TestSetupSweSweFilesMigratesEnvFile(t *testing.T) {
	destDir := t.TempDir()

	// Seed an old-style env file.
	if err := os.MkdirAll(filepath.Join(destDir, "swe-swe"), 0755); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	oldPath := filepath.Join(destDir, "swe-swe", "env")
	if err := os.WriteFile(oldPath, []byte("PATH=/usr/local/go/bin:$PATH\n"), 0644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	if err := setupSweSweFiles(destDir); err != nil {
		t.Fatalf("setupSweSweFiles: %v", err)
	}

	// Old path should be gone.
	if _, err := os.Stat(oldPath); err == nil {
		t.Errorf("old swe-swe/env still exists; expected os.Rename to have moved it")
	}

	// New path should exist with the original content.
	newPath := filepath.Join(destDir, ".swe-swe", "env")
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read migrated .swe-swe/env: %v", err)
	}
	if want := "PATH=/usr/local/go/bin:$PATH\n"; string(got) != want {
		t.Errorf("migrated .swe-swe/env content: got %q, want %q", got, want)
	}
}

// TestSetupSweSweFilesNoEnvMigrationWhenNewExists verifies the migration is
// a no-op when .swe-swe/env already exists (we must not clobber it).
func TestSetupSweSweFilesNoEnvMigrationWhenNewExists(t *testing.T) {
	destDir := t.TempDir()

	// Seed both paths with different contents.
	if err := os.MkdirAll(filepath.Join(destDir, "swe-swe"), 0755); err != nil {
		t.Fatalf("seed mkdir swe-swe: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destDir, ".swe-swe"), 0755); err != nil {
		t.Fatalf("seed mkdir .swe-swe: %v", err)
	}
	oldPath := filepath.Join(destDir, "swe-swe", "env")
	newPath := filepath.Join(destDir, ".swe-swe", "env")
	if err := os.WriteFile(oldPath, []byte("OLD=1\n"), 0644); err != nil {
		t.Fatalf("seed write old: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("NEW=1\n"), 0644); err != nil {
		t.Fatalf("seed write new: %v", err)
	}

	if err := setupSweSweFiles(destDir); err != nil {
		t.Fatalf("setupSweSweFiles: %v", err)
	}

	// New path is untouched.
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read .swe-swe/env: %v", err)
	}
	if string(got) != "NEW=1\n" {
		t.Errorf(".swe-swe/env was clobbered: got %q, want %q", got, "NEW=1\n")
	}

	// Old path is left in place (migration is only a rename when new is missing).
	got, err = os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read swe-swe/env: %v", err)
	}
	if string(got) != "OLD=1\n" {
		t.Errorf("swe-swe/env was unexpectedly modified: got %q, want %q", got, "OLD=1\n")
	}
}
