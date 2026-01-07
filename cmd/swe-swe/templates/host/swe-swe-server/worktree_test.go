package main

import (
	"testing"
)

func TestDeriveBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Basic transformations
		{"Fix Login Bug", "fix-login-bug"},
		{"  spaces  around  ", "spaces-around"},
		{"UPPERCASE", "uppercase"},
		{"with_underscores", "with_underscores"},

		// Special characters
		{"special!@#chars", "special-chars"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"test@#$%^&*()test", "test-test"},

		// Unicode handling (diacritics removed)
		{"émojis and üñíçödé", "emojis-and-unicode"},
		{"café résumé", "cafe-resume"},
		{"naïve coöperate", "naive-cooperate"},

		// Edge cases
		{"", ""},
		{"a", "a"},
		{"123-numbers-456", "123-numbers-456"},
		{"---leading-trailing---", "leading-trailing"},
		{"   ", ""},

		// Real-world examples
		{"20260107-143052", "20260107-143052"},
		{"Fix: user login #123", "fix-user-login-123"},
		{"feat/add-new-feature", "feat-add-new-feature"},
		{"bug_fix_issue", "bug_fix_issue"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := deriveBranchName(tt.input)
			if result != tt.expected {
				t.Errorf("deriveBranchName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
