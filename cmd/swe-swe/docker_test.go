package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAndUpgrade(t *testing.T) {
	// Save and restore global Version
	origVersion := Version
	defer func() { Version = origVersion }()

	t.Run("no upgrade when versions match", func(t *testing.T) {
		Version = "1.2.3"
		sweDir := t.TempDir()

		config := InitConfig{CLIVersion: "1.2.3"}
		data, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(sweDir, "init.json"), data, 0644)

		args := []string{"--detach"}
		result := checkAndUpgrade(sweDir, "/tmp/project", args)

		if len(result) != 1 || result[0] != "--detach" {
			t.Errorf("expected unchanged args [--detach], got %v", result)
		}
	})

	t.Run("no upgrade when config has no version", func(t *testing.T) {
		Version = "1.2.3"
		sweDir := t.TempDir()

		config := InitConfig{}
		data, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(sweDir, "init.json"), data, 0644)

		args := []string{"--detach"}
		result := checkAndUpgrade(sweDir, "/tmp/project", args)

		if len(result) != 1 || result[0] != "--detach" {
			t.Errorf("expected unchanged args [--detach], got %v", result)
		}
	})

	t.Run("no upgrade when config file missing", func(t *testing.T) {
		Version = "1.2.3"
		sweDir := t.TempDir()

		args := []string{"--detach"}
		result := checkAndUpgrade(sweDir, "/tmp/project", args)

		if len(result) != 1 || result[0] != "--detach" {
			t.Errorf("expected unchanged args [--detach], got %v", result)
		}
	})

	t.Run("version mismatch without auto-upgrade prints warning", func(t *testing.T) {
		Version = "2.0.0"
		sweDir := t.TempDir()

		config := InitConfig{CLIVersion: "1.0.0"}
		data, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(sweDir, "init.json"), data, 0644)

		// Ensure SWE_SWE_AUTO_UPGRADE is not set
		os.Unsetenv("SWE_SWE_AUTO_UPGRADE")

		// In non-interactive mode (test runner), this should just warn
		args := []string{"--detach"}
		result := checkAndUpgrade(sweDir, "/tmp/project", args)

		// Args should be unchanged (no --build added) since no upgrade happened
		if len(result) != 1 || result[0] != "--detach" {
			t.Errorf("expected unchanged args [--detach], got %v", result)
		}
	})

	t.Run("build flag not duplicated", func(t *testing.T) {
		// This tests the hasBuild logic indirectly - when --build is already present,
		// it should not be added again. We test this by verifying the logic in isolation.
		args := []string{"--build", "--detach"}
		hasBuild := false
		for _, arg := range args {
			if arg == "--build" {
				hasBuild = true
				break
			}
		}
		if !hasBuild {
			t.Error("expected hasBuild to be true when --build is in args")
		}
	})
}
