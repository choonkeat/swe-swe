package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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

// extractDockerlessBinaries dumps the embedded static-Linux binaries onto
// disk; init --dockerless calls it to populate .swe-swe/bin. Each file must
// land executable (0755) and byte-identical to the embed.
func TestExtractDockerlessBinaries(t *testing.T) {
	// The Makefile builds the host arch into the embed; on this Linux CI
	// host that is runtime.GOARCH.
	dest := t.TempDir()
	if err := extractDockerlessBinaries(dest, runtime.GOARCH); err != nil {
		t.Fatalf("extractDockerlessBinaries: %v", err)
	}
	for _, name := range dockerlessBinaries {
		p := filepath.Join(dest, name)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: not extracted: %v", name, err)
			continue
		}
		if info.Mode().Perm()&0111 == 0 {
			t.Errorf("%s: mode %v is not executable", name, info.Mode().Perm())
		}
		want, err := dockerlessPayload.ReadFile(filepath.Join(dockerlessPayloadBinDir(runtime.GOARCH), name))
		if err != nil {
			t.Fatalf("read embed %s: %v", name, err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read extracted %s: %v", name, err)
		}
		if len(got) != len(want) {
			t.Errorf("%s: extracted %d bytes, embed has %d", name, len(got), len(want))
		}
	}
}

// The mode marker is how `swe-swe up` decides to run the host-native server
// instead of docker compose. A fresh metadata dir is not dockerless; one
// written by writeDockerlessMarker is.
func TestDockerlessMarker(t *testing.T) {
	sweDir := t.TempDir()
	if isDockerlessProject(sweDir) {
		t.Errorf("fresh metadata dir reported as dockerless")
	}
	if err := writeDockerlessMarker(sweDir); err != nil {
		t.Fatalf("writeDockerlessMarker: %v", err)
	}
	if !isDockerlessProject(sweDir) {
		t.Errorf("after writeDockerlessMarker, isDockerlessProject = false")
	}
	// A metadata dir that does not exist is not dockerless (no panic).
	if isDockerlessProject(filepath.Join(sweDir, "does-not-exist")) {
		t.Errorf("missing dir reported as dockerless")
	}
}
