package main

import "testing"

// TestFilesPortFromPreview verifies the Files port is derived as preview+6000
// and lands inside the dedicated Files port band (9000-9019), mirroring the
// other per-session derived ports (agent-chat=+1000, public=+2000, cdp=+3000,
// vnc=+4000).
func TestFilesPortFromPreview(t *testing.T) {
	for previewPort := previewPortStart; previewPort <= previewPortEnd; previewPort++ {
		filesPort := filesPortFromPreview(previewPort)
		if filesPort != previewPort+6000 {
			t.Errorf("filesPortFromPreview(%d) = %d, want %d", previewPort, filesPort, previewPort+6000)
		}
		if filesPort < filesPortStart || filesPort > filesPortEnd {
			t.Errorf("filesPortFromPreview(%d) = %d, outside files band %d-%d", previewPort, filesPort, filesPortStart, filesPortEnd)
		}
	}
}

// TestFindAvailablePortQuintupleAssignsFilesPort verifies that a freshly
// allocated session derives FilesPort as PreviewPort+6000 and that it falls
// within the Files port band. findAvailablePortQuintuple returns the preview
// port; the session creation path derives FilesPort via filesPortFromPreview,
// so we assert the same invariant here against an allocated preview port.
func TestFindAvailablePortQuintupleAssignsFilesPort(t *testing.T) {
	// Snapshot and restore the package-global sessions map so this test does
	// not leak allocated ports into other tests.
	sessionsMu.Lock()
	saved := sessions
	sessions = make(map[string]*Session)
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = saved
		sessionsMu.Unlock()
	}()

	sessionsMu.Lock()
	previewPort, _, _, _, _, err := findAvailablePortQuintuple()
	sessionsMu.Unlock()
	if err != nil {
		t.Fatalf("findAvailablePortQuintuple() returned error: %v", err)
	}

	sess := &Session{
		PreviewPort: previewPort,
		FilesPort:   filesPortFromPreview(previewPort),
	}

	if sess.FilesPort != sess.PreviewPort+6000 {
		t.Errorf("FilesPort = %d, want PreviewPort+6000 = %d", sess.FilesPort, sess.PreviewPort+6000)
	}
	if sess.FilesPort < filesPortStart || sess.FilesPort > filesPortEnd {
		t.Errorf("FilesPort = %d, outside files band %d-%d", sess.FilesPort, filesPortStart, filesPortEnd)
	}
}
