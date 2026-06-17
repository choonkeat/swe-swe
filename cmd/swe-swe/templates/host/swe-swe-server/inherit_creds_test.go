package main

import (
	"crypto/ed25519"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestInheritSessionCredentials verifies that a child session inherits its
// parent's HTTPS credentials, author identity, and SSH signing key, and
// that its per-session gitconfig is regenerated with the inherited values
// so commit signing works immediately.
func TestInheritSessionCredentials(t *testing.T) {
	origDir := sessionGitconfigDir
	sessionGitconfigDir = t.TempDir()
	defer func() { sessionGitconfigDir = origDir }()

	parent := "parent-sess"
	child := "child-sess"
	defer clearSessionCredentials(parent)
	defer clearSessionCredentials(child)

	// Seed the parent with all three kinds of git state.
	setCredential(parent, "github.com", CredentialBag{Username: "x-access-token", Token: "ghp_secret"})
	setAuthor(parent, AuthorIdent{Name: "Ada Lovelace", Email: "ada@example.com"})

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	setSigningKey(parent, SigningKey{
		Signer:      signer,
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		Label:       "parent-key",
	})

	inheritSessionCredentials(parent, child, "")

	// HTTPS credentials copied per host.
	c, ok := getCredential(child, "github.com")
	if !ok || c.Token != "ghp_secret" || c.Username != "x-access-token" {
		t.Fatalf("child did not inherit HTTPS credential: %+v ok=%v", c, ok)
	}
	// Author identity copied.
	a, ok := getAuthor(child)
	if !ok || a.Name != "Ada Lovelace" || a.Email != "ada@example.com" {
		t.Fatalf("child did not inherit author: %+v ok=%v", a, ok)
	}
	// Signing key copied (compare the resolved public key).
	if got, want := signingKeyPublicAuthorized(child), signingKeyPublicAuthorized(parent); got == "" || got != want {
		t.Fatalf("child did not inherit signing key: got %q want %q", got, want)
	}

	// Child gitconfig regenerated with the inherited signing + author.
	data, err := os.ReadFile(sessionGitconfigPath(child))
	if err != nil {
		t.Fatalf("child gitconfig not written: %v", err)
	}
	cfg := string(data)
	for _, want := range []string{"ada@example.com", "signingkey = ", "gpgsign = true"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("child gitconfig missing %q\n---\n%s", want, cfg)
		}
	}
}

// TestInheritSessionCredentialsNoParent confirms an empty or unknown parent
// is a safe no-op: the child inherits nothing and no panic occurs.
func TestInheritSessionCredentialsNoParent(t *testing.T) {
	origDir := sessionGitconfigDir
	sessionGitconfigDir = t.TempDir()
	defer func() { sessionGitconfigDir = origDir }()

	inheritSessionCredentials("", "child-empty", "")
	if _, ok := getAuthor("child-empty"); ok {
		t.Fatal("child inherited author from an empty parent")
	}

	inheritSessionCredentials("ghost-parent", "child-ghost", "")
	if _, ok := getAuthor("child-ghost"); ok {
		t.Fatal("child inherited author from an unknown parent")
	}

	// Same parent and child is a no-op (no self-copy, no panic).
	setAuthor("self", AuthorIdent{Name: "Self", Email: "self@example.com"})
	defer clearSessionCredentials("self")
	inheritSessionCredentials("self", "self", "")
}
