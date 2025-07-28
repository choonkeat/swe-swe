package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestLineScanning(t *testing.T) {
	input := `2025/07/28 14:05:28 [STDOUT] starting session | provider: claude-code model: sonnet
2025/07/28 14:05:28 [STDOUT]     logging to /Users/choonkeatchew/.local/share/goose/sessions/20250728_140528.jsonl
2025/07/28 14:05:28 [STDOUT]     working directory: /Users/choonkeatchew/git/choonkeat/swe-swe`

	// Test with bufio.Scanner (current implementation)
	scanner := bufio.NewScanner(strings.NewReader(input))
	var lines1 []string
	for scanner.Scan() {
		lines1 = append(lines1, scanner.Text())
	}
	if len(lines1) != 3 {
		t.Errorf("Expected 3 lines with Scanner, got %d", len(lines1))
	}

	// Test alternative implementation using bufio.Reader
	reader := bufio.NewReader(strings.NewReader(input))
	var lines2 []string
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// Handle the last line without newline
			if len(line) > 0 {
				line = bytes.TrimSuffix(line, []byte{'\r'})
				lines2 = append(lines2, string(line))
			}
			break
		}
		// Trim the trailing newline
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		lines2 = append(lines2, string(line))
	}
	if len(lines2) != 3 {
		t.Errorf("Expected 3 lines with Reader, got %d", len(lines2))
	}
}