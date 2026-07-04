package main

import (
	"html/template"
	"strings"
	"testing"
)

// renderForkConfirm parses the embedded fork-confirm page (the live main()
// path parses the same bytes into forkConfirmTemplate) and renders it with the
// given data, so the test exercises the real template content.
func renderForkConfirm(t *testing.T, data forkConfirmData) string {
	t.Helper()
	content, err := pageTemplatesFS.ReadFile("page-templates/fork-confirm.html")
	if err != nil {
		t.Fatalf("read fork-confirm template: %v", err)
	}
	tmpl, err := template.New("fork-confirm").Parse(string(content))
	if err != nil {
		t.Fatalf("parse fork-confirm template: %v", err)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		t.Fatalf("execute fork-confirm template: %v", err)
	}
	return sb.String()
}

// TestForkConfirmCarriesRepoEnv locks in that a fork inherits the repo's env
// vars: the confirm page must (a) expose the repo init_sha so the client can
// locate this repo's env-vars blob in localStorage, (b) wire that blob onto the
// fork POST under name="env" (applied to the store before the forked session
// spawns, exactly like the new-session dialog), and (c) tell the user the vars
// will be applied -- a statement, not a choice.
func TestForkConfirmCarriesRepoEnv(t *testing.T) {
	out := renderForkConfirm(t, forkConfirmData{
		SourceUUID: "src-uuid",
		SourceName: "my session",
		Assistant:  "claude",
		InitSha:    "abc123init",
	})

	// (a) init_sha reaches the client so it can find the blob's localStorage key.
	if !strings.Contains(out, "abc123init") {
		t.Errorf("fork confirm page does not expose init_sha; client cannot locate the repo env blob\n%s", out)
	}
	// (b) the client-side env wiring is present: it reads the swe-swe-env store
	// and attaches an env field to the fork POST.
	if !strings.Contains(out, "swe-swe-env:") {
		t.Errorf("fork confirm page has no swe-swe-env localStorage lookup; env blob will not be attached")
	}
	if !strings.Contains(out, `name="env"`) && !strings.Contains(out, "name='env'") {
		t.Errorf("fork confirm page never attaches an env field to the POST; the fork will spawn without repo env vars")
	}
	// (c) the informational line element exists so the user is told the vars
	// will be applied.
	if !strings.Contains(out, "fork-env-note") {
		t.Errorf("fork confirm page has no env-applied notice element")
	}
}

// TestForkConfirmErrorStateHasNoEnvNote makes sure the env wiring lives only on
// the real confirm branch: an error-state render (source could not be
// validated) must not claim env vars will be applied.
func TestForkConfirmErrorStateHasNoEnvNote(t *testing.T) {
	out := renderForkConfirm(t, forkConfirmData{
		SourceUUID: "src-uuid",
		Error:      "source session has no chat event log",
	})
	if strings.Contains(out, "fork-env-note") {
		t.Errorf("error-state fork page must not show the env-applied notice")
	}
}
