package main

import "testing"

func TestCredentialUpdatesPrimaryOnly(t *testing.T) {
	got := credentialUpdates("github.com", " x-access-token ", "ghp_1", nil)
	if len(got) != 1 {
		t.Fatalf("want 1 update, got %d (%+v)", len(got), got)
	}
	if got[0].Host != "github.com" {
		t.Errorf("host = %q, want github.com", got[0].Host)
	}
	if got[0].Bag.Username != "x-access-token" {
		t.Errorf("username = %q, want trimmed x-access-token", got[0].Bag.Username)
	}
	if got[0].Bag.Token != "ghp_1" {
		t.Errorf("token = %q", got[0].Bag.Token)
	}
}

func TestCredentialUpdatesEmptyPrimaryHostDefaultsToGithub(t *testing.T) {
	got := credentialUpdates("  ", "u", "t", nil)
	if len(got) != 1 || got[0].Host != "github.com" {
		t.Fatalf("want github.com default, got %+v", got)
	}
}

// The browser now re-sends EVERY host it holds a token for on session start,
// not just the one matching the workspace origin remote. One message carries
// them all so the author identity and the gitconfig rewrite happen once.
func TestCredentialUpdatesCarriesAdditionalHosts(t *testing.T) {
	got := credentialUpdates("gitlab.example.com", "oauth2", "glpat", []additionalHostCred{
		{Host: "github.com", Username: "x-access-token", Token: "ghp_1"},
		{Host: " bitbucket.org ", Username: "bb", Token: "bbt"},
	})
	if len(got) != 3 {
		t.Fatalf("want 3 updates, got %d (%+v)", len(got), got)
	}
	if got[0].Host != "gitlab.example.com" {
		t.Errorf("primary host must stay first, got %q", got[0].Host)
	}
	if got[1].Host != "github.com" || got[1].Bag.Token != "ghp_1" {
		t.Errorf("second update = %+v", got[1])
	}
	if got[2].Host != "bitbucket.org" {
		t.Errorf("host must be trimmed, got %q", got[2].Host)
	}
}

func TestCredentialUpdatesSkipsEmptyAndDuplicateAdditionalHosts(t *testing.T) {
	got := credentialUpdates("github.com", "u", "t", []additionalHostCred{
		{Host: "", Username: "u", Token: "t"},                    // no host
		{Host: "gitlab.com", Username: "u", Token: ""},           // no token
		{Host: "github.com", Username: "other", Token: "other"},  // duplicate of primary
		{Host: "gitlab.com", Username: "u", Token: "g"},          // keeps this one
		{Host: "gitlab.com", Username: "u", Token: "g2"},         // duplicate, first wins
	})
	if len(got) != 2 {
		t.Fatalf("want 2 updates, got %d (%+v)", len(got), got)
	}
	if got[0].Host != "github.com" || got[0].Bag.Token != "t" {
		t.Errorf("primary must not be overwritten by a duplicate: %+v", got[0])
	}
	if got[1].Host != "gitlab.com" || got[1].Bag.Token != "g" {
		t.Errorf("second update = %+v", got[1])
	}
}

func TestCredentialUpdatesCapsAdditionalHosts(t *testing.T) {
	var many []additionalHostCred
	for i := 0; i < maxAdditionalCredHosts+10; i++ {
		many = append(many, additionalHostCred{
			Host:     string(rune('a'+i%26)) + "-host.example.com",
			Username: "u",
			Token:    "t",
		})
	}
	got := credentialUpdates("github.com", "u", "t", many)
	if len(got) > maxAdditionalCredHosts+1 {
		t.Fatalf("want at most %d updates, got %d", maxAdditionalCredHosts+1, len(got))
	}
}
