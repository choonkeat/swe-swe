package main

import "testing"

func TestComputeSigningState(t *testing.T) {
	tests := []struct {
		name            string
		hasKey          bool
		emailResolvable bool
		localOverrides  string
		wantActive      bool
		wantReason      string
	}{
		{
			name:       "no key, no email, no override",
			wantActive: false, wantReason: "no signing key",
		},
		{
			name:   "no key but email present -- still needs a key first",
			hasKey: false, emailResolvable: true,
			wantActive: false, wantReason: "no signing key",
		},
		{
			name:   "key + email + no override -> active",
			hasKey: true, emailResolvable: true,
			wantActive: true, wantReason: "",
		},
		{
			name:   "key + email but local override shadows it",
			hasKey: true, emailResolvable: true, localOverrides: "gpg.format=openpgp",
			wantActive: false, wantReason: "local .git/config override",
		},
		{
			name:   "key + override but no email -- override wins as the reason",
			hasKey: true, emailResolvable: false, localOverrides: "gpg.format=openpgp",
			wantActive: false, wantReason: "local .git/config override",
		},
		{
			name:   "key, no email, no override -> no email",
			hasKey: true, emailResolvable: false,
			wantActive: false, wantReason: "no email",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			active, reason := computeSigningState(tc.hasKey, tc.emailResolvable, tc.localOverrides)
			if active != tc.wantActive {
				t.Errorf("active: got %v, want %v", active, tc.wantActive)
			}
			if reason != tc.wantReason {
				t.Errorf("reason: got %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// TestBuildSessionCredState_SnapshotShape verifies the assembled message
// reflects the stores: a registered key + author email + no local
// override yields signing_active=true with the fingerprint and the
// author_email_set flag set.
func TestBuildSessionCredState_SnapshotShape(t *testing.T) {
	sid := "test-sid-credstate"
	defer clearSessionCredentials(sid)

	setAuthor(sid, AuthorIdent{Name: "Ivy Ident", Email: "ivy@example.com"})
	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{Signer: signer, Fingerprint: "SHA256:credstate", Label: "test"})
	setCredential(sid, "gitlab.example.com", CredentialBag{Username: "x", Token: "secret"})

	// workDir "" -> readLocalSigningOverrides returns "" (no override).
	state := buildSessionCredState(sid, "")

	if state["type"] != "session_cred_state" {
		t.Errorf("type: got %v", state["type"])
	}
	if state["author_email_set"] != true {
		t.Errorf("author_email_set: got %v, want true", state["author_email_set"])
	}
	if state["signing_fingerprint"] != "SHA256:credstate" {
		t.Errorf("signing_fingerprint: got %v", state["signing_fingerprint"])
	}
	if state["signing_active"] != true {
		t.Errorf("signing_active: got %v, want true", state["signing_active"])
	}
	if state["signing_inactive_reason"] != "" {
		t.Errorf("signing_inactive_reason: got %v, want empty", state["signing_inactive_reason"])
	}
	hosts, ok := state["hosts"].([]string)
	if !ok || len(hosts) != 1 || hosts[0] != "gitlab.example.com" {
		t.Errorf("hosts: got %v", state["hosts"])
	}
}
