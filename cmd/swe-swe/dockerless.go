package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// dockerlessMarkerFile names the sentinel inside the metadata dir that marks a
// project as host-native. `swe-swe up` reads it to decide between exec-ing the
// dumped swe-swe-server and shelling out to docker compose.
const dockerlessMarkerFile = "mode"

// writeDockerlessMarker records that the project at sweDir is dockerless.
func writeDockerlessMarker(sweDir string) error {
	if err := os.MkdirAll(sweDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sweDir, dockerlessMarkerFile), []byte("dockerless\n"), 0644)
}

// isDockerlessProject reports whether sweDir holds a dockerless mode marker.
// Missing dir/file or any read error reports false (treat as compose mode).
func isDockerlessProject(sweDir string) bool {
	data, err := os.ReadFile(filepath.Join(sweDir, dockerlessMarkerFile))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "dockerless"
}

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

// executeDockerlessInit performs a host-native (no-Docker) init: it dumps the
// embedded binaries into the metadata dir, writes the mode marker + project
// records that `swe-swe list`/`up` rely on, and prints next steps. It does NOT
// generate a Dockerfile or compose file. The GOOS guard in handleInit has
// already rejected non-Linux callers before we get here.
func executeDockerlessInit(absPath, sweDir string, config InitConfig) {
	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create metadata directory: %v", err)
	}
	config.HostUID = os.Getuid()
	config.HostGID = os.Getgid()

	// Dump the prebuilt host-native binaries (server + helpers) into the
	// metadata dir's bin/, which `swe-swe up` puts on PATH and exec's.
	binDir := filepath.Join(sweDir, "bin")
	if err := extractDockerlessBinaries(binDir, runtime.GOARCH); err != nil {
		log.Fatalf("Failed to extract dockerless binaries: %v", err)
	}
	fmt.Printf("Extracted %d host-native binaries to %s\n", len(dockerlessBinaries), binDir)

	if err := writeDockerlessMarker(sweDir); err != nil {
		log.Fatalf("Failed to write dockerless marker: %v", err)
	}

	// Record the project path (used by `swe-swe list`) and save config so
	// `swe-swe up` can detect the CLI-vs-config version skew.
	if err := os.WriteFile(filepath.Join(sweDir, ".path"), []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}
	if err := saveInitConfig(sweDir, config); err != nil {
		log.Fatalf("Failed to save init config: %v", err)
	}

	fmt.Printf("\nInitialized dockerless swe-swe project at %s\n", absPath)
	fmt.Printf("View all projects: swe-swe list\n")
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
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
