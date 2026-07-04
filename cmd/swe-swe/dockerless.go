package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// dockerlessGOOSGuard reports whether a dockerless init is allowed for the
// given GOOS. The embedded payload binaries are static-Linux only (Phase 1),
// and the server still relies on two Linux-only couplings -- the abstract
// unix socket `@swe-swe-broker` and util-linux `script -T/-I/-O` recording
// flags -- so dumping them on macOS/Windows would produce a broken setup.
// Mac-native dockerless (darwin binaries + portable couplings) is Phase 6.
func dockerlessGOOSGuard(goos string) error {
	if goos != "linux" {
		return fmt.Errorf("swe-swe init --dockerless is supported on a Linux host only for now (this is a %s build); use Docker mode here, or see Phase 6 for native macOS support", goos)
	}
	return nil
}

// extractDockerlessBinaries writes the embedded static-Linux binaries for the
// given GOARCH into destDir, each as an executable (0755) file. destDir is
// created if missing. embed.FS strips the executable bit, so we restore it.
func extractDockerlessBinaries(destDir, goarch string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	srcDir := dockerlessPayloadBinDir(goarch)
	for _, name := range dockerlessBinaries {
		data, err := dockerlessPayload.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return fmt.Errorf("read embedded %s (is the %s payload built for this arch?): %w", name, goarch, err)
		}
		dst := filepath.Join(destDir, name)
		if err := os.WriteFile(dst, data, 0755); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		// WriteFile honors the mode only on creation; force it in case the
		// file pre-existed with a different mode (re-init).
		if err := os.Chmod(dst, 0755); err != nil {
			return fmt.Errorf("chmod %s: %w", dst, err)
		}
	}
	return nil
}
