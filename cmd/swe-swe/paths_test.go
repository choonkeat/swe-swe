package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCopyDirSkipsSocket verifies that copyDir does not fail when a
// Unix domain socket is encountered (e.g. ~/.ssh/agent/*.agent.* from
// macOS SSH agents). Regression for the "operation not supported on
// socket" init crash.
func TestCopyDirSkipsSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix domain sockets not supported on Windows")
	}

	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dst")

	// Regular file that should be copied.
	if err := os.WriteFile(filepath.Join(srcDir, "config"), []byte("Host *\n  User me\n"), 0644); err != nil {
		t.Fatalf("seed regular file: %v", err)
	}

	// Unix domain socket that should be skipped.
	sockPath := filepath.Join(srcDir, "agent.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	defer l.Close()

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Regular file copied.
	got, err := os.ReadFile(filepath.Join(dstDir, "config"))
	if err != nil {
		t.Fatalf("regular file not copied: %v", err)
	}
	if string(got) != "Host *\n  User me\n" {
		t.Errorf("regular file content: got %q", got)
	}

	// Socket not copied.
	if _, err := os.Stat(filepath.Join(dstDir, "agent.sock")); err == nil {
		t.Errorf("socket was copied; expected it to be skipped")
	}
}

// TestCopyDirPreservesSymlink verifies that copyDir recreates symlinks
// instead of dereferencing them. This matters for dotfiles like
// .ssh/config -> ../dotfiles/ssh/config.
func TestCopyDirPreservesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlink semantics differ on Windows")
	}

	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dst")

	if err := os.WriteFile(filepath.Join(srcDir, "real"), []byte("data"), 0644); err != nil {
		t.Fatalf("seed real file: %v", err)
	}
	if err := os.Symlink("real", filepath.Join(srcDir, "link")); err != nil {
		t.Fatalf("seed symlink: %v", err)
	}

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dstDir, "link"))
	if err != nil {
		t.Fatalf("lstat copied link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("copied entry is not a symlink; got mode %v", info.Mode())
	}

	target, err := os.Readlink(filepath.Join(dstDir, "link"))
	if err != nil {
		t.Fatalf("readlink copied link: %v", err)
	}
	if target != "real" {
		t.Errorf("symlink target: got %q, want %q", target, "real")
	}
}
