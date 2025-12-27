package main

import (
	"os"
	"strings"
	"testing"
)

// TestSanitizePathConsistent verifies sanitizePath returns the same hash for same path
func TestSanitizePathConsistent(t *testing.T) {
	path := "/Users/alice/projects/myapp"
	result1 := sanitizePath(path)
	result2 := sanitizePath(path)

	if result1 != result2 {
		t.Errorf("sanitizePath not consistent: %q != %q", result1, result2)
	}
}

// TestSanitizePathDifferentForDifferentPaths verifies different paths get different hashes
func TestSanitizePathDifferentForDifferentPaths(t *testing.T) {
	path1 := "/Users/alice/projects/myapp"
	path2 := "/Users/bob/projects/myapp"

	result1 := sanitizePath(path1)
	result2 := sanitizePath(path2)

	if result1 == result2 {
		t.Errorf("sanitizePath should differ for different paths, but got same: %q", result1)
	}
}

// TestSanitizePathReplacesSpecialChars verifies special characters are replaced
func TestSanitizePathReplacesSpecialChars(t *testing.T) {
	path := "/Users/alice/my-app@v2.0"
	result := sanitizePath(path)

	// Result should not contain special characters (except hyphens)
	if strings.Contains(result, "@") || strings.Contains(result, ".") {
		t.Errorf("sanitizePath failed to replace special chars: %q", result)
	}

	// Should contain hyphens and alphanumeric chars
	if !strings.Contains(result, "-") {
		t.Errorf("sanitizePath should contain hyphens: %q", result)
	}
}

// TestSanitizePathEndsWithHash verifies hash is appended
func TestSanitizePathEndsWithHash(t *testing.T) {
	path := "/Users/alice/projects/myapp"
	result := sanitizePath(path)

	// Should have at least one hyphen (separating name from hash)
	parts := strings.Split(result, "-")
	if len(parts) < 2 {
		t.Errorf("sanitizePath should end with hash: %q", result)
	}

	// Last part should be hex hash (8 chars)
	lastPart := parts[len(parts)-1]
	if len(lastPart) != 8 {
		t.Errorf("hash should be 8 chars, got %d: %q", len(lastPart), lastPart)
	}

	// All chars should be hex digits
	for _, c := range lastPart {
		if !strings.ContainsAny(string(c), "0123456789abcdef") {
			t.Errorf("hash contains non-hex character: %q", lastPart)
		}
	}
}

// TestGetMetadataDirReturnsPath verifies getMetadataDir returns valid path
func TestGetMetadataDirReturnsPath(t *testing.T) {
	path := "/tmp/test-project"
	result, err := getMetadataDir(path)

	if err != nil {
		t.Errorf("getMetadataDir should not fail: %v", err)
	}

	if result == "" {
		t.Errorf("getMetadataDir should return non-empty path")
	}
}

// TestGetMetadataDirUnderHome verifies metadata dir is under $HOME/.swe-swe/projects/
func TestGetMetadataDirUnderHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Skipping: cannot get home directory: %v", err)
	}

	path := "/tmp/test-project"
	result, err := getMetadataDir(path)

	if err != nil {
		t.Errorf("getMetadataDir should not fail: %v", err)
	}

	expectedPrefix := homeDir + "/.swe-swe/projects"
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("metadata dir should be under %q, got %q", expectedPrefix, result)
	}
}

// TestGetMetadataDirDifferentForDifferentPaths verifies different paths get different metadata dirs
func TestGetMetadataDirDifferentForDifferentPaths(t *testing.T) {
	path1 := "/tmp/test-project-1"
	path2 := "/tmp/test-project-2"

	result1, err1 := getMetadataDir(path1)
	result2, err2 := getMetadataDir(path2)

	if err1 != nil || err2 != nil {
		t.Errorf("getMetadataDir should not fail: %v, %v", err1, err2)
	}

	if result1 == result2 {
		t.Errorf("different paths should get different metadata dirs: %q", result1)
	}
}
