package main

import (
	"sort"
	"testing"
)

// TestSessionTokenEnv pins the swe-swe-only names the saved HTTPS tokens are
// exported under. The conventional GH_TOKEN/GITLAB_TOKEN must never be claimed:
// third-party CLIs read them, and the user must stay free to set their own.
func TestSessionTokenEnv(t *testing.T) {
	sid := "sid-token-env"
	defer clearSessionCredentials(sid)
	setCredential(sid, "github.com", CredentialBag{Username: "x-access-token", Token: "ghp_x"})
	setCredential(sid, "gitlab.example.com", CredentialBag{Username: "oauth2", Token: "glpat_x"})
	setCredential(sid, "git.corp.example", CredentialBag{Username: "u", Token: "corp_x"})
	setCredential(sid, "gitlab.com", CredentialBag{Username: "oauth2"}) // no token

	got := sessionTokenEnv(sid)
	sort.Strings(got)
	want := []string{
		"SWE_SWE_GITHUB_HTTPS_TOKEN=ghp_x",
		"SWE_SWE_GITLAB_HTTPS_TOKEN=glpat_x",
	}
	if len(got) != len(want) {
		t.Fatalf("sessionTokenEnv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sessionTokenEnv[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	env := buildSessionEnv(SessionEnvParams{SID: sid, WorkDir: t.TempDir(), SessionMode: "terminal"})
	for _, k := range []string{"GH_TOKEN", "GITLAB_TOKEN"} {
		if v, ok := envValue(env, k); ok && (v == "ghp_x" || v == "glpat_x") {
			t.Errorf("%s = %q: saved HTTPS token leaked into a conventional CLI var", k, v)
		}
	}
}
