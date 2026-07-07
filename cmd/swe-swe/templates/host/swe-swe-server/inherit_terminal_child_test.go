package main

import (
	"crypto/ed25519"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestTerminalChildInheritsParentSettings is the end-to-end spec for the fix
// that lets a plain Terminal tab benefit from the same session settings as the
// Agent Terminal it was opened from. A terminal child carries
// InheritCredsFrom = <parent agent UUID> (set in the handleWebSocket
// SessionParams block in main.go), which drives the same inheritance path MCP
// create_session uses.
//
// The test replays getOrCreateSession's spawn order EXACTLY so it exercises the
// real wiring rather than the helpers in isolation:
//
//	main.go ~5053: inheritSessionEnv(parent, child)      // BEFORE buildSessionEnv
//	main.go ~5059: env := buildSessionEnv(...)           // freezes cmd.Env
//	main.go ~5141: registerSessionPid(...)               // (pty.Start, then register)
//	main.go ~5215: inheritSessionCredentials(parent, ...) // AFTER, rewrites gitconfig
//
// That ordering asymmetry is load-bearing and is asserted below (step 6): repo
// env vars are baked into the frozen process env, but the GH_TOKEN/GITLAB_TOKEN
// convenience vars are not -- git-over-HTTPS still works because the credential
// helper resolves the child SID at git-run time, not at spawn.
func TestTerminalChildInheritsParentSettings(t *testing.T) {
	origDir := sessionGitconfigDir
	sessionGitconfigDir = t.TempDir()
	defer func() { sessionGitconfigDir = origDir }()

	parent := "agent-parent"
	child := "terminal-child"
	defer clearSessionCredentials(parent)
	defer clearSessionCredentials(child)
	defer clearSessionEnv(parent)
	defer clearSessionEnv(child)

	// Seed the parent agent session with every setting a user configures in the
	// UI: HTTPS PAT, git author, SSH signing key, and a repo env var.
	setCredential(parent, "github.com", CredentialBag{Username: "x-access-token", Token: "ghp_parent"})
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
	setSessionEnv(parent, "REPO_VAR=parent-value\n")

	// Replay the terminal-child spawn wiring in getOrCreateSession's order.
	workDir := t.TempDir()
	p := SessionParams{UUID: child, InheritCredsFrom: parent}

	if p.InheritCredsFrom != "" {
		inheritSessionEnv(p.InheritCredsFrom, p.UUID)
	}
	childEnv := buildSessionEnv(SessionEnvParams{SID: p.UUID, WorkDir: workDir, SessionMode: "terminal"})
	if p.InheritCredsFrom != "" {
		inheritSessionCredentials(p.InheritCredsFrom, p.UUID, workDir)
	}

	// 1) Repo env var reaches the child's frozen process env, because
	//    inheritSessionEnv runs before buildSessionEnv.
	if v, ok := envValue(childEnv, "REPO_VAR"); !ok || v != "parent-value" {
		t.Errorf("REPO_VAR = %q (present=%v), want parent-value baked into the child process env", v, ok)
	}

	// 2) HTTPS credential copied into the child store, so credential.helper=swe-swe
	//    (always present in the env) resolves the child SID to the PAT at git-run
	//    time via the broker.
	if c, ok := getCredential(child, "github.com"); !ok || c.Token != "ghp_parent" || c.Username != "x-access-token" {
		t.Errorf("child credential = %+v (present=%v), want inherited ghp_parent", c, ok)
	}

	// 3) Git author copied.
	if a, ok := getAuthor(child); !ok || a.Name != "Ada Lovelace" || a.Email != "ada@example.com" {
		t.Errorf("child author = %+v (present=%v), want inherited Ada Lovelace <ada@example.com>", a, ok)
	}

	// 4) SSH signing key copied (compare resolved public keys).
	if got, want := signingKeyPublicAuthorized(child), signingKeyPublicAuthorized(parent); got == "" || got != want {
		t.Errorf("child signing key = %q, want inherited %q", got, want)
	}

	// 5) Child gitconfig regenerated with the inherited signing + author so
	//    commits sign immediately (the child's GIT_CONFIG_GLOBAL points here).
	cfg, err := os.ReadFile(sessionGitconfigPath(child))
	if err != nil {
		t.Fatalf("child gitconfig not written: %v", err)
	}
	for _, want := range []string{"ada@example.com", "signingkey = ", "gpgsign = true"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("child gitconfig missing %q\n---\n%s", want, string(cfg))
		}
	}

	// 6) Ordering caveat, pinned so a future refactor can't silently regress it:
	//    credentials inherit AFTER buildSessionEnv, so GH_TOKEN is NOT baked into
	//    the frozen spawn-time env. git-over-HTTPS still works (credential helper
	//    resolves at runtime), but a bare `gh`/`prctx` reading GH_TOKEN would not
	//    see it at spawn. The token IS mappable once creds are inherited -- a
	//    rebuilt env carries it -- proving the only gap is spawn-time freezing.
	if _, ok := envValue(childEnv, "GH_TOKEN"); ok {
		t.Errorf("GH_TOKEN unexpectedly baked into the spawn-time env; the inherit ordering changed -- revisit this caveat and whether credentials should inherit before buildSessionEnv")
	}
	rebuilt := buildSessionEnv(SessionEnvParams{SID: child, WorkDir: workDir, SessionMode: "terminal"})
	if v, ok := envValue(rebuilt, "GH_TOKEN"); !ok || v != "ghp_parent" {
		t.Errorf("GH_TOKEN after inheritance = %q (present=%v), want ghp_parent mapped from the inherited credential", v, ok)
	}
}

// TestTerminalChildNoParentInheritsNothing confirms the top-level Agent tab
// path (no ParentUUID -> empty InheritCredsFrom) is a safe no-op: the session
// starts with none of a would-be parent's settings and nothing panics. This is
// the branch that stops the Agent tab itself, and fork/new-session spawns
// (which replace params entirely), from accidentally inheriting.
func TestTerminalChildNoParentInheritsNothing(t *testing.T) {
	origDir := sessionGitconfigDir
	sessionGitconfigDir = t.TempDir()
	defer func() { sessionGitconfigDir = origDir }()

	child := "solo-agent"
	defer clearSessionCredentials(child)
	defer clearSessionEnv(child)

	p := SessionParams{UUID: child, InheritCredsFrom: ""}
	if p.InheritCredsFrom != "" {
		inheritSessionEnv(p.InheritCredsFrom, p.UUID)
	}
	env := buildSessionEnv(SessionEnvParams{SID: p.UUID, WorkDir: t.TempDir(), SessionMode: "terminal"})
	if p.InheritCredsFrom != "" {
		inheritSessionCredentials(p.InheritCredsFrom, p.UUID, "")
	}

	if _, ok := getAuthor(child); ok {
		t.Error("session with no parent inherited an author")
	}
	if _, ok := getCredential(child, "github.com"); ok {
		t.Error("session with no parent inherited a credential")
	}
	if v, ok := envValue(env, "GH_TOKEN"); ok {
		t.Errorf("session with no parent has GH_TOKEN = %q, want none", v)
	}
}
