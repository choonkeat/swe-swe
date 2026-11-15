// upload_limit.go -- per-session upload size limit and streamed terminal uploads.
//
// SWE_SWE_UPLOAD_MAX_MB (from the session env: Settings textarea, .swe-swe/env,
// or the server env) caps the size of a single uploaded file. Empty, 0, or
// unparseable means no limit. The same value is copied to the session's
// AGENT_CHAT_UPLOAD_MAX_MB so agent-chat's paperclip enforces the same cap
// (see uploadLimitChatEnv).
//
// Terminal paperclip uploads are streamed: the browser sends an upload_start
// JSON message, then the file as a series of 0x03-prefixed binary chunks, then
// upload_end. Each chunk is written straight to a temp file in the uploads dir,
// so the server never holds a whole file in memory (the legacy single-message
// 0x01 upload did, which made "no limit" unsafe).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	uploadMaxEnvKey     = "SWE_SWE_UPLOAD_MAX_MB"
	chatUploadMaxEnvKey = "AGENT_CHAT_UPLOAD_MAX_MB"

	// opcodeUploadChunk prefixes a binary websocket frame carrying one piece
	// of the file announced by the preceding upload_start.
	opcodeUploadChunk = 0x03
)

// parseUploadMaxBytes converts a megabyte count to bytes. "" and "0" mean no
// limit (0). Decimals are allowed ("0.5"). Anything else is an error and the
// caller treats it as no limit.
func parseUploadMaxBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mb, err := strconv.ParseFloat(s, 64)
	if err != nil || mb < 0 || mb != mb {
		return 0, fmt.Errorf("%s=%q is not a number of megabytes", uploadMaxEnvKey, s)
	}
	return int64(mb * 1024 * 1024), nil
}

// formatUploadSize renders a byte count the way the UI does ("65.2 MB").
func formatUploadSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}

// uploadTooLargeError is the user-facing refusal text.
func uploadTooLargeError(name string, size, max int64) error {
	return fmt.Errorf("%s is too large (%s); the upload limit is %s (%s)",
		name, formatUploadSize(size), formatUploadSize(max), uploadMaxEnvKey)
}

// uploadLimitChatEnv appends AGENT_CHAT_UPLOAD_MAX_MB, copied from
// SWE_SWE_UPLOAD_MAX_MB, unless the env already sets it (an explicit
// agent-chat value wins) or swe-swe has no value to copy.
func uploadLimitChatEnv(env []string) []string {
	if envHas(env, chatUploadMaxEnvKey) {
		return env
	}
	v := strings.TrimSpace(envLookup(env)(uploadMaxEnvKey))
	if v == "" {
		return env
	}
	return append(env, chatUploadMaxEnvKey+"="+v)
}

// uploadStream is one in-progress streamed upload on a websocket connection.
// Not safe for concurrent use; the websocket read loop owns it.
type uploadStream struct {
	maxBytes int64 // 0 = no limit

	f        *os.File
	tmpPath  string
	dir      string
	name     string
	expected int64
	written  int64
}

func (u *uploadStream) active() bool { return u.f != nil }

// start opens a temp file for name in dir. An upload already in progress is
// abandoned. Size is the browser-reported total, checked against the limit up
// front so a too-large file is refused before any bytes are sent.
func (u *uploadStream) start(dir, name string, size int64) error {
	u.abort()
	if name == "" {
		return errors.New("Invalid filename")
	}
	if size < 0 {
		return errors.New("Invalid file size")
	}
	if u.maxBytes > 0 && size > u.maxBytes {
		return uploadTooLargeError(name, size, u.maxBytes)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("Failed to create uploads directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".upload-*.partial")
	if err != nil {
		return err
	}
	u.f, u.tmpPath, u.dir, u.name, u.expected, u.written = f, f.Name(), dir, name, size, 0
	return nil
}

// write appends one chunk. Any error abandons the upload.
func (u *uploadStream) write(p []byte) error {
	if !u.active() {
		return errors.New("no upload in progress")
	}
	n := u.written + int64(len(p))
	if n > u.expected {
		name := u.name
		u.abort()
		return fmt.Errorf("%s: received more data than announced", name)
	}
	if u.maxBytes > 0 && n > u.maxBytes {
		name := u.name
		u.abort()
		return uploadTooLargeError(name, n, u.maxBytes)
	}
	if _, err := u.f.Write(p); err != nil {
		u.abort()
		return err
	}
	u.written = n
	return nil
}

// finish closes the temp file and moves it to its final name, replacing any
// earlier upload of the same name. Returns the final path and size.
func (u *uploadStream) finish() (string, int64, error) {
	if !u.active() {
		return "", 0, errors.New("no upload in progress")
	}
	if u.written != u.expected {
		name, got, want := u.name, u.written, u.expected
		u.abort()
		return "", 0, fmt.Errorf("%s: upload incomplete (%d of %d bytes)", name, got, want)
	}
	if err := u.f.Close(); err != nil {
		u.f = nil
		os.Remove(u.tmpPath)
		return "", 0, err
	}
	u.f = nil
	final := filepath.Join(u.dir, u.name)
	if err := os.Chmod(u.tmpPath, 0644); err != nil {
		os.Remove(u.tmpPath)
		return "", 0, err
	}
	if err := os.Rename(u.tmpPath, final); err != nil {
		os.Remove(u.tmpPath)
		return "", 0, err
	}
	return final, u.written, nil
}

// abort drops any in-progress upload and deletes its temp file.
func (u *uploadStream) abort() {
	if u.f == nil {
		return
	}
	u.f.Close()
	os.Remove(u.tmpPath)
	u.f = nil
}
