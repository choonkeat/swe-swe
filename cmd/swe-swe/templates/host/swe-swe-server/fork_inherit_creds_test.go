package main

import (
	"crypto/ed25519"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestForkSessionParamsInheritsFromSource is the repro + spec for the bug where
// a forked session dropped the source session's git HTTPS credentials, SSH
// signing key, git author, and repo env vars. handleForkExecute must stamp
// InheritCredsFrom = source UUID onto the staged params so the spawn path
// (inheritSessionEnv before buildSessionEnv, inheritSessionCredentials after)
// runs -- exactly like MCP create_session and the terminal-child path.
//
// Before the fix InheritCredsFrom was empty, so a fork carried only the browser
// localStorage env blob (EnvRaw) and none of the git auth state.
func TestForkSessionParamsInheritsFromSource(t *testing.T) {
	src := &hydratedForkSource{
		UUID:        "source-session",
		Assistant:   "claude",
		WorkDir:     "/work",
		Theme:       "dark",
		ChatLogPath: "/rec/chat.events.jsonl",
	}
	p := buildForkSessionParams("new-session", src, "--resume abc", "fork: demo", "REPO_VAR=v\n")

	if p.InheritCredsFrom != "source-session" {
		t.Errorf("fork params InheritCredsFrom = %q, want source-session; the fork spawns without the source's git creds/signing", p.InheritCredsFrom)
	}
	// The localStorage env blob is still passed through as the fallback for
	// ended sources whose in-memory env store is already cleared.
	if p.EnvRaw != "REPO_VAR=v\n" {
		t.Errorf("fork params EnvRaw = %q, want the env blob passed through", p.EnvRaw)
	}
	// The rest of the fork wiring must be intact.
	if p.UUID != "new-session" || p.Assistant != "claude" || p.WorkDir != "/work" ||
		p.SessionMode != "chat" || p.ExtraArgs != "--resume abc" || p.Name != "fork: demo" ||
		p.Theme != "dark" || p.PrepopulateChatLog != "/rec/chat.events.jsonl" {
		t.Errorf("fork params base fields wrong: %+v", p)
	}
}

// TestForkInheritanceEndToEnd proves the fork's params actually drive git-auth
// and env inheritance end to end: it seeds a source session with every setting a
// user configures (HTTPS PAT, author, SSH signing key, repo env), builds the
// fork params, then replays getOrCreateSession's spawn order against those
// params and asserts the child ends up with all four. This is the behavioral
// regression guard for the reported bug.
func TestForkInheritanceEndToEnd(t *testing.T) {
	origDir := sessionGitconfigDir
	sessionGitconfigDir = t.TempDir()
	defer func() { sessionGitconfigDir = origDir }()

	source := "fork-source"
	defer clearSessionCredentials(source)
	defer clearSessionEnv(source)

	setCredential(source, "github.com", CredentialBag{Username: "x-access-token", Token: "ghp_source"})
	setAuthor(source, AuthorIdent{Name: "Ada Lovelace", Email: "ada@example.com"})

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	setSigningKey(source, SigningKey{
		Signer:      signer,
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		Label:       "source-key",
	})
	setSessionEnv(source, "REPO_VAR=source-value\n")

	workDir := t.TempDir()
	src := &hydratedForkSource{UUID: source, Assistant: "claude", WorkDir: workDir}
	p := buildForkSessionParams("fork-child", src, "--resume abc", "fork: demo", "")
	defer clearSessionCredentials(p.UUID)
	defer clearSessionEnv(p.UUID)

	// Replay the spawn wiring in getOrCreateSession's order.
	if p.InheritCredsFrom != "" {
		inheritSessionEnv(p.InheritCredsFrom, p.UUID)
	}
	childEnv := buildSessionEnv(SessionEnvParams{SID: p.UUID, WorkDir: workDir, SessionMode: "chat"})
	if p.InheritCredsFrom != "" {
		inheritSessionCredentials(p.InheritCredsFrom, p.UUID, workDir)
	}

	// 1) Repo env var baked into the child's frozen process env.
	if v, ok := envValue(childEnv, "REPO_VAR"); !ok || v != "source-value" {
		t.Errorf("REPO_VAR = %q (present=%v), want source-value baked into the fork's process env", v, ok)
	}
	// 2) HTTPS credential copied to the child store.
	if c, ok := getCredential(p.UUID, "github.com"); !ok || c.Token != "ghp_source" {
		t.Errorf("fork credential = %+v (present=%v), want inherited ghp_source", c, ok)
	}
	// 3) Git author copied.
	if a, ok := getAuthor(p.UUID); !ok || a.Email != "ada@example.com" {
		t.Errorf("fork author = %+v (present=%v), want inherited ada@example.com", a, ok)
	}
	// 4) SSH signing key copied.
	if got, want := signingKeyPublicAuthorized(p.UUID), signingKeyPublicAuthorized(source); got == "" || got != want {
		t.Errorf("fork signing key = %q, want inherited %q", got, want)
	}
	// 5) Child gitconfig regenerated with the inherited author + signing key.
	cfg, err := os.ReadFile(sessionGitconfigPath(p.UUID))
	if err != nil {
		t.Fatalf("fork gitconfig not written: %v", err)
	}
	for _, want := range []string{"ada@example.com", "signingkey = ", "gpgsign = true"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("fork gitconfig missing %q\n---\n%s", want, string(cfg))
		}
	}
}
