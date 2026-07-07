package main

import "testing"

// TestCloneNeedsAuth verifies the git-output heuristic that distinguishes an
// authentication failure (which should surface a credential prompt to the
// browser) from other clone errors (which should surface as raw text).
func TestCloneNeedsAuth(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"github https 401", "remote: Support for password authentication was removed.\nfatal: Authentication failed for 'https://github.com/acme/private.git/'", true},
		{"could not read Username", "fatal: could not read Username for 'https://github.com': terminal prompts disabled", true},
		{"could not read Password", "fatal: could not read Password for 'https://x@github.com': terminal prompts disabled", true},
		{"gitlab 403", "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://gitlab.com/acme/private.git/'", true},
		{"repo not found is not auth", "fatal: repository 'https://github.com/acme/nope.git/' not found", false},
		{"dns failure is not auth", "fatal: unable to access 'https://nohost.invalid/x.git/': Could not resolve host: nohost.invalid", false},
		{"empty output not auth", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloneNeedsAuth(tc.out); got != tc.want {
				t.Errorf("cloneNeedsAuth(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}
