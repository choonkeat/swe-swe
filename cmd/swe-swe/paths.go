package main

import (
	"crypto/md5"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// expandTilde expands ~ to the user's home directory
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// extractProjectDirectory parses args for --project-directory flag
// Returns (projectDir, remainingArgs)
func extractProjectDirectory(args []string) (string, []string) {
	projectDir := "."
	var remaining []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle --project-directory=value
		if strings.HasPrefix(arg, "--project-directory=") {
			projectDir = strings.TrimPrefix(arg, "--project-directory=")
			continue
		}

		// Handle --project-directory value
		if arg == "--project-directory" {
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++ // Skip the value
				continue
			}
		}

		remaining = append(remaining, arg)
	}

	// Expand ~ in projectDir
	projectDir = expandTilde(projectDir)

	return projectDir, remaining
}

// copyFile copies a single file, preserving permissions.
// Removes existing destination file first to handle read-only files (e.g., .git/objects).
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// Remove existing file first to handle read-only files
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	return os.WriteFile(dst, data, srcInfo.Mode())
}

// copyDir recursively copies a directory tree, preserving permissions
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory with same permissions
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// sanitizeProjectName converts a directory name into a sanitized project name
// suitable for use as Docker label values and Traefik router names.
// It lowercases, replaces non-alphanumeric chars with hyphens, and truncates to 32 chars.
// Example: "My Project.Name" -> "my-project-name"
func sanitizeProjectName(dirName string) string {
	// Lowercase the name
	name := strings.ToLower(dirName)

	// Replace non-alphanumeric chars with hyphens
	re := regexp.MustCompile(`[^a-z0-9]+`)
	name = re.ReplaceAllString(name, "-")

	// Remove leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Truncate to 32 characters
	if len(name) > 32 {
		name = name[:32]
		// Remove trailing hyphen if truncation created one
		name = strings.TrimRight(name, "-")
	}

	// Default to "swe-swe" if empty
	if name == "" {
		name = "swe-swe"
	}

	return name
}

// sanitizePath converts an absolute path into a sanitized directory name suitable
// for use under $HOME/.swe-swe/projects/. It replaces non-alphanumeric characters
// (except separators) with hyphens and appends an MD5 hash of the full absolute path.
// Example: /Users/alice/projects/my-app -> users-alice-projects-my-app-{md5-first-8-chars}
func sanitizePath(absPath string) string {
	// Replace path separators and non-alphanumeric chars with hyphens
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	sanitized := re.ReplaceAllString(absPath, "-")
	// Remove leading/trailing hyphens
	sanitized = strings.Trim(sanitized, "-")

	// Compute MD5 hash of absolute path
	hash := md5.Sum([]byte(absPath))
	hashStr := fmt.Sprintf("%x", hash)[:8] // First 8 chars of hex hash

	return fmt.Sprintf("%s-%s", sanitized, hashStr)
}

// getMetadataDir returns the metadata directory path for a given project absolute path.
// Metadata is stored in $HOME/.swe-swe/projects/{sanitized-path}/
func getMetadataDir(absPath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}

	sanitized := sanitizePath(absPath)
	return filepath.Join(homeDir, ".swe-swe", "projects", sanitized), nil
}
