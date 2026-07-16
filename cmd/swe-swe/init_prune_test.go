package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestInitPrunesStaleServerSourceOrphan guards the build-context mirror
// invariant: the dumped swe-swe-server/ tree must exactly mirror the embedded
// templates, never accumulate leftovers. `init` used to be append-only (it
// wrote templates but never reconciled deletions), so a renamed/removed
// template left a stale .go behind. The Dockerfile's `COPY swe-swe-server/*.go`
// glob then compiled the orphan alongside its replacement, redeclaring symbols
// and breaking the image build (real incident: tailscale.go -> listen.go).
//
// This drives the real `init` end to end: run once, plant an orphaning .go that
// redeclares a symbol from a shipped file, run again, and assert the orphan is
// gone while the legitimate file survives.
func TestInitPrunesStaleServerSourceOrphan(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping init integration test in short mode")
	}

	tmp := t.TempDir()
	cliBin := filepath.Join(tmp, "swe-swe-test-cli")
	// Build the CLI from the current package (test cwd is the package dir).
	build := exec.Command("go", "build", "-o", cliBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build CLI: %v\n%s", err, out)
	}

	home := filepath.Join(tmp, "home")
	proj := filepath.Join(tmp, "proj")
	meta := filepath.Join(tmp, "meta")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	runInit := func(extra ...string) {
		t.Helper()
		args := append([]string{"init", "--project-directory", proj, "--metadata-dir", meta}, extra...)
		cmd := exec.Command(cliBin, args...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
	}

	// First init establishes the tree. The renamed-from file must be present;
	// it is the symbol home the orphan would collide with.
	runInit()
	shipped := filepath.Join(meta, "swe-swe-server", "listen.go")
	if _, err := os.Stat(shipped); err != nil {
		t.Fatalf("expected shipped listen.go after init, got: %v", err)
	}

	// Simulate a previous init that shipped a since-removed template: plant a
	// stale .go redeclaring a symbol that listen.go already defines. If this
	// survived the next init, `go build` on the COPY'd *.go glob would fail.
	orphan := filepath.Join(meta, "swe-swe-server", "tailscale.go")
	if err := os.WriteFile(orphan, []byte("package main\n\nfunc firstNonEmpty(vals ...string) string { return \"\" }\n"), 0644); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	// Re-init (the real upgrade path: `--previous-init-flags=reuse`) must prune
	// the orphan (whole tree regenerated as a mirror)...
	runInit("--previous-init-flags=reuse")
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("stale orphan tailscale.go was not pruned on re-init (err=%v)", err)
	}
	// ...while the legitimate file is regenerated, not collateral damage.
	if _, err := os.Stat(shipped); err != nil {
		t.Fatalf("listen.go missing after re-init: %v", err)
	}
}
