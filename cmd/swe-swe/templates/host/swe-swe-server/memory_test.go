package main

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
		{8589934592, "8.0 GB"},
	}
	for _, tc := range tests {
		result := formatBytes(tc.input)
		if result != tc.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestGetAvailableMemory(t *testing.T) {
	avail := getAvailableMemory()
	if avail <= 0 {
		t.Errorf("getAvailableMemory() = %d, want > 0", avail)
	}
	// Sanity: should be at least 100MB on any test machine
	if avail < 100*1024*1024 {
		t.Errorf("getAvailableMemory() = %d bytes, suspiciously low", avail)
	}
}

func TestGetProcessTreeRSS(t *testing.T) {
	// Our own process should have nonzero RSS
	rss := getProcessTreeRSS(1) // PID 1 always exists
	if rss <= 0 {
		t.Errorf("getProcessTreeRSS(1) = %d, want > 0", rss)
	}
}

func TestCheckMemoryForNewSession(t *testing.T) {
	// With no active sessions, should always allow
	err := checkMemoryForNewSession()
	if err != nil {
		t.Errorf("checkMemoryForNewSession() with no sessions = %v, want nil", err)
	}
}
