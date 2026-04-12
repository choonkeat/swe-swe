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
		"swe-swe/setup",
	} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("expected %s at destDir root: %v", want, err)
		}
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
		"swe-swe/setup",
	} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("expected %s at destDir root: %v", want, err)
		}
	}
}
