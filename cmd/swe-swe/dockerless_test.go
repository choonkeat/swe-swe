package main

import "testing"

// Dockerless runs the host-native binaries directly; today those binaries
// are Linux-only (abstract-socket broker + GNU `script` recording flags), so
// `swe-swe init --dockerless` must refuse on a non-Linux CLI rather than
// dump binaries that cannot run. Mac-native support is Phase 6.
func TestDockerlessGOOSGuard(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		if err := dockerlessGOOSGuard(goos); err == nil {
			t.Errorf("dockerlessGOOSGuard(%q) = nil, want error (non-Linux must be refused)", goos)
		}
	}
	if err := dockerlessGOOSGuard("linux"); err != nil {
		t.Errorf("dockerlessGOOSGuard(\"linux\") = %v, want nil", err)
	}
}
