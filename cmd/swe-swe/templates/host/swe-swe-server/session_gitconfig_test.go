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

func TestSessionGitconfig_SigningKey(t *testing.T) {
	// When a signing key is registered, writeSessionGitconfigFile
	// must emit:
	//   - user.signingkey with the OpenSSH-authorized-key literal
	//   - [gpg] format = ssh
	//   - [gpg "ssh"] program = git-sign-swe-swe
	//   - [commit] gpgsign = true
	//   - [tag] gpgsign = true
	// Without a signing key these blocks must be absent so users
	// who haven't opted in see no behavior change.
	tmp := t.TempDir()
	sid := "test-sid-sign-gitconfig"
	path := filepath.Join(tmp, sid)
	defer clearSessionCredentials(sid)

	// No signing key yet -- the signing blocks must not appear.
	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile (no key): %v", err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "gpgsign") {
		t.Errorf("did not expect gpgsign before signing key, got:\n%s", body)
	}
	if strings.Contains(string(body), "format = ssh") {
		t.Errorf("did not expect format=ssh before signing key, got:\n%s", body)
	}

	// Register a signing key and re-emit.
	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{
		Signer:      signer,
		Fingerprint: "SHA256:test",
		Label:       "test",
	})
	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile (with key): %v", err)
	}
	body, _ = os.ReadFile(path)
	got := string(body)

	wantSubstrings := []string{
		"signingkey = ssh-ed25519 ",
		"[gpg]",
		"format = ssh",
		"[gpg \"ssh\"]",
		"program = git-sign-swe-swe",
		"[commit]",
		"gpgsign = true",
		"[tag]",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in gitconfig; got:\n%s", want, got)
		}
	}
}

// TestSessionGitconfig_AllowedSigners verifies that when both a signing
// key and an author email are present, writeSessionGitconfigFile writes
// the per-session allowed_signers file with `<email> <pubkey>` and emits
// `allowedSignersFile = <path>` under `[gpg "ssh"]`. Without this, git's
// signature verification path (`git log --show-signature`, `verify-commit`)
// fails because the signature has no recognized principal.
func TestSessionGitconfig_AllowedSigners(t *testing.T) {
	tmp := t.TempDir()
	savedDir := sessionGitconfigDir
	sessionGitconfigDir = tmp
	defer func() { sessionGitconfigDir = savedDir }()

	sid := "test-sid-allowed-signers"
	path := filepath.Join(tmp, sid)
	defer clearSessionCredentials(sid)

	setAuthor(sid, AuthorIdent{Name: "Eve Email", Email: "eve@example.com"})
	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{
		Signer:      signer,
		Fingerprint: "SHA256:test",
		Label:       "test",
	})

	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitconfig: %v", err)
	}
	if !strings.Contains(string(body), "allowedSignersFile = "+sessionAllowedSignersPath(sid)) {
		t.Errorf("expected allowedSignersFile pointing at session path; got:\n%s", body)
	}

	signers, err := os.ReadFile(sessionAllowedSignersPath(sid))
	if err != nil {
		t.Fatalf("read allowed_signers: %v", err)
	}
	got := string(signers)
	if !strings.HasPrefix(got, "eve@example.com ssh-ed25519 ") {
		t.Errorf("allowed_signers line: got %q, want %q...", got, "eve@example.com ssh-ed25519 ")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("allowed_signers must end with newline; got %q", got)
	}
}

// TestSessionGitconfig_AllowedSigners_SkippedWithoutEmail verifies that
// when a signing key is set but no author email is recorded, we skip the
// allowed_signers file and do not emit `allowedSignersFile`. Signing still
// works (no behavior regression); only local verification is skipped.
func TestSessionGitconfig_AllowedSigners_SkippedWithoutEmail(t *testing.T) {
	tmp := t.TempDir()
	savedDir := sessionGitconfigDir
	sessionGitconfigDir = tmp
	defer func() { sessionGitconfigDir = savedDir }()

	sid := "test-sid-no-email"
	path := filepath.Join(tmp, sid)
	defer clearSessionCredentials(sid)

	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{
		Signer:      signer,
		Fingerprint: "SHA256:test",
		Label:       "test",
	})
	// Author not set: signing still wires up, but no allowed_signers.

	if err := writeSessionGitconfigFile(path, sid); err != nil {
		t.Fatalf("writeSessionGitconfigFile: %v", err)
	}

	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "allowedSignersFile") {
		t.Errorf("did not expect allowedSignersFile without author email; got:\n%s", body)
	}
	if _, err := os.Stat(sessionAllowedSignersPath(sid)); !os.IsNotExist(err) {
		t.Errorf("did not expect allowed_signers file to be written; stat err=%v", err)
	}
}

// TestRemoveSessionGitconfig_RemovesAllowedSigners verifies that
// session teardown also removes the sibling allowed_signers file so
// teardown is symmetric with creation.
func TestRemoveSessionGitconfig_RemovesAllowedSigners(t *testing.T) {
	tmp := t.TempDir()
	savedDir := sessionGitconfigDir
	sessionGitconfigDir = tmp
	defer func() { sessionGitconfigDir = savedDir }()

	sid := "test-sid-remove-signers"
	defer clearSessionCredentials(sid)

	setAuthor(sid, AuthorIdent{Name: "Frank Foo", Email: "frank@example.com"})
	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{
		Signer:      signer,
		Fingerprint: "SHA256:test",
		Label:       "test",
	})
	if _, err := ensureSessionGitconfig(sid); err != nil {
		t.Fatalf("ensureSessionGitconfig: %v", err)
	}

	if _, err := os.Stat(sessionAllowedSignersPath(sid)); err != nil {
		t.Fatalf("expected allowed_signers to exist after ensure; got %v", err)
	}

	removeSessionGitconfig(sid)

	if _, err := os.Stat(sessionAllowedSignersPath(sid)); !os.IsNotExist(err) {
		t.Errorf("expected allowed_signers gone after remove; stat err=%v", err)
	}
	if _, err := os.Stat(sessionGitconfigPath(sid)); !os.IsNotExist(err) {
		t.Errorf("expected gitconfig gone after remove; stat err=%v", err)
	}
}
