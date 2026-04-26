package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLocalGitUser_Basic(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `[core]
	repositoryformatversion = 0
[user]
	name = Alice Author
	email = alice@example.com
[remote "origin"]
	url = https://github.com/example/repo.git
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	name, email := readLocalGitUser(tmp)
	if name != "Alice Author" {
		t.Errorf("name: got %q, want %q", name, "Alice Author")
	}
	if email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", email, "alice@example.com")
	}
}

func TestReadLocalGitUser_NoUserSection(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = https://github.com/example/repo.git
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	name, email := readLocalGitUser(tmp)
	if name != "" || email != "" {
		t.Errorf("expected empty, got name=%q email=%q", name, email)
	}
}

func TestReadLocalGitUser_PartialUserSection(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `[user]
	name = Bob
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	name, email := readLocalGitUser(tmp)
	if name != "Bob" {
		t.Errorf("name: got %q, want Bob", name)
	}
	if email != "" {
		t.Errorf("email: expected empty, got %q", email)
	}
}

func TestReadLocalGitUser_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	name, email := readLocalGitUser(tmp)
	if name != "" || email != "" {
		t.Errorf("expected empty for missing file, got name=%q email=%q", name, email)
	}
}

func TestReadLocalGitUser_EmptyWorkDir(t *testing.T) {
	name, email := readLocalGitUser("")
	if name != "" || email != "" {
		t.Errorf("expected empty for empty workdir, got name=%q email=%q", name, email)
	}
}

func TestReadLocalGitUser_CommentsAndWhitespace(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `# header comment
; alt comment
[user]
	# inline-style comment
	name = Carol Crypto

	email = carol@example.com
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	name, email := readLocalGitUser(tmp)
	if name != "Carol Crypto" {
		t.Errorf("name: got %q, want Carol Crypto", name)
	}
	if email != "carol@example.com" {
		t.Errorf("email: got %q, want carol@example.com", email)
	}
}

func TestSessionGitconfig_RoundTrip(t *testing.T) {
	// Smoke test: setAuthor + writeSessionGitconfigFile produce a file
	// containing an [include] line and a [user] section with the saved
	// name/email. Uses writeSessionGitconfigFile directly (with a tmp
	// path) so we don't need to override the production sessionGitconfigDir.
	tmp := t.TempDir()
	sid := "test-sid-12345"
	path := filepath.Join(tmp, sid)
	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile (no author): %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(body), "[include]") {
		t.Errorf("expected [include] in file, got:\n%s", body)
	}
	if strings.Contains(string(body), "[user]") {
		t.Errorf("did not expect [user] before setAuthor, got:\n%s", body)
	}

	setAuthor(sid, AuthorIdent{Name: "Dave Diff", Email: "dave@example.com"})
	defer clearSessionCredentials(sid)
	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile (with author): %v", err)
	}
	body, _ = os.ReadFile(path)
	if !strings.Contains(string(body), "name = Dave Diff") {
		t.Errorf("expected name in file, got:\n%s", body)
	}
	if !strings.Contains(string(body), "email = dave@example.com") {
		t.Errorf("expected email in file, got:\n%s", body)
	}
}
