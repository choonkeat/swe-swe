package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUploadMaxBytes(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"  ", 0, false},
		{"0", 0, false},
		{"50", 50 << 20, false},
		{" 200 ", 200 << 20, false},
		{"0.5", 512 << 10, false},
		{"-1", 0, true},
		{"fifty", 0, true},
		{"NaN", 0, true},
	}
	for _, c := range cases {
		got, err := parseUploadMaxBytes(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseUploadMaxBytes(%q) = %d, %v; want %d, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestUploadLimitChatEnv(t *testing.T) {
	t.Setenv(uploadMaxEnvKey, "")
	t.Setenv(chatUploadMaxEnvKey, "")

	// Copied from the swe-swe setting.
	got := uploadLimitChatEnv([]string{"A=1", uploadMaxEnvKey + "=75"})
	if !envHas(got, chatUploadMaxEnvKey) || envLookup(got)(chatUploadMaxEnvKey) != "75" {
		t.Errorf("expected %s=75, got %v", chatUploadMaxEnvKey, got)
	}

	// An explicit agent-chat value wins, even empty.
	got = uploadLimitChatEnv([]string{uploadMaxEnvKey + "=75", chatUploadMaxEnvKey + "="})
	if envLookup(got)(chatUploadMaxEnvKey) != "" || len(got) != 2 {
		t.Errorf("explicit agent-chat value must win, got %v", got)
	}

	// Nothing to copy: unchanged (no limit).
	got = uploadLimitChatEnv([]string{"A=1"})
	if envHas(got, chatUploadMaxEnvKey) {
		t.Errorf("expected no %s, got %v", chatUploadMaxEnvKey, got)
	}
}

func TestUploadStreamHappyPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")
	u := &uploadStream{}
	if err := u.start(dir, "a.bin", 6); err != nil {
		t.Fatal(err)
	}
	if err := u.write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := u.write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	path, n, err := u.finish()
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 || path != filepath.Join(dir, "a.bin") {
		t.Errorf("finish = %q, %d", path, n)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "abcdef" {
		t.Errorf("content = %q", b)
	}
	assertNoPartials(t, dir)
}

func TestUploadStreamRefusesTooLargeUpFront(t *testing.T) {
	dir := t.TempDir()
	u := &uploadStream{maxBytes: 10}
	err := u.start(dir, "big.zip", 11)
	if err == nil || !strings.Contains(err.Error(), "too large") || !strings.Contains(err.Error(), uploadMaxEnvKey) {
		t.Fatalf("expected too-large error naming %s, got %v", uploadMaxEnvKey, err)
	}
	if u.active() {
		t.Error("refused upload must not be active")
	}
	assertNoPartials(t, dir)
}

func TestUploadStreamEnforcesLimitOnBytes(t *testing.T) {
	// The browser under-reports its size; the byte count still enforces the cap.
	dir := t.TempDir()
	u := &uploadStream{maxBytes: 4}
	if err := u.start(dir, "x", 4); err != nil {
		t.Fatal(err)
	}
	if err := u.write([]byte("abcde")); err == nil {
		t.Fatal("expected error writing past the announced size")
	}
	if u.active() {
		t.Error("upload must be abandoned")
	}
	assertNoPartials(t, dir)
}

func TestUploadStreamIncompleteAndAbort(t *testing.T) {
	dir := t.TempDir()
	u := &uploadStream{}
	if err := u.start(dir, "x", 5); err != nil {
		t.Fatal(err)
	}
	u.write([]byte("ab"))
	if _, _, err := u.finish(); err == nil {
		t.Fatal("expected incomplete error")
	}
	assertNoPartials(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "x")); !os.IsNotExist(err) {
		t.Error("incomplete upload must not produce the final file")
	}

	// A new start abandons the previous upload.
	u.start(dir, "y", 5)
	u.write([]byte("ab"))
	if err := u.start(dir, "z", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.finish(); err != nil {
		t.Fatalf("empty file should finish: %v", err)
	}
	u.abort()
	assertNoPartials(t, dir)
}

func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, ".upload-*.partial"))
	if len(matches) != 0 {
		t.Errorf("leftover temp files: %v", matches)
	}
}
