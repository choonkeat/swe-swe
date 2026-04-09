// backfill-recording-summaries decompresses each existing session-<uuid>.log.gz
// recording, extracts a one-line summary from the tail, and writes it back into
// the sibling session-<uuid>.metadata.json under "summary_line".
//
// Run after upgrading swe-swe-server to the version that caches summaries at
// compression time. New recordings get the cache for free; this tool fills in
// historical recordings so the homepage no longer needs to gunzip them.
//
// Usage:
//
//	go run ./cmd/backfill-recording-summaries [-dir /workspace/.swe-swe/recordings] [-dry-run]
//
// Safety:
//   - Skips active sessions: any recording whose plain .log or .log.pipe still
//     exists alongside the .log.gz is treated as in-progress and skipped.
//     (The server only writes .log.gz after the recording has ended and the
//     plain .log has been removed.)
//   - Skips recordings whose metadata.json already has a non-empty summary_line.
//   - Writes metadata via temp-file + rename for atomicity.
//   - Best-effort: errors on individual files are logged and the tool continues.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func main() {
	dir := flag.String("dir", "/workspace/.swe-swe/recordings", "recordings directory")
	dryRun := flag.Bool("dry-run", false, "print what would change without modifying any files")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("ReadDir %s: %v", *dir, err)
	}

	// Build a set of files in the directory for quick lookup of "is the plain
	// .log still here?" — that's our "active session" guard.
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			present[e.Name()] = true
		}
	}

	var (
		processed int
		updated   int
		skipped   int
		failed    int
	)
	start := time.Now()

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".log.gz") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimPrefix(name, "session-"), ".log.gz")
		// Only handle root recordings. Filenames are either "<uuid>" (root) or
		// "<parent-uuid>-<child-uuid>" (child); children have no metadata.json
		// of their own.
		if !isRootRecordingStem(stem) {
			continue
		}
		// Active-session guard: if a plain .log or .log.pipe FIFO is still
		// present alongside, the recording may still be in flight.
		if present["session-"+stem+".log"] || present["session-"+stem+".log.pipe"] {
			log.Printf("SKIP active: session-%s", stem)
			skipped++
			continue
		}

		processed++
		metadataPath := filepath.Join(*dir, "session-"+stem+".metadata.json")
		metaBytes, err := os.ReadFile(metadataPath)
		if err != nil {
			log.Printf("SKIP no-metadata: session-%s (%v)", stem, err)
			skipped++
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(metaBytes, &m); err != nil {
			log.Printf("SKIP bad-metadata: session-%s (%v)", stem, err)
			skipped++
			continue
		}
		if existing, _ := m["summary_line"].(string); existing != "" {
			skipped++
			continue
		}

		gzPath := filepath.Join(*dir, name)
		summary, err := tailSummaryFromGzip(gzPath)
		if err != nil {
			log.Printf("FAIL %s: %v", name, err)
			failed++
			continue
		}
		if summary == "" {
			log.Printf("EMPTY %s (no usable tail line)", name)
			skipped++
			continue
		}

		if *dryRun {
			log.Printf("WOULD UPDATE session-%s -> %q", stem, truncate(summary, 80))
			updated++
			continue
		}

		m["summary_line"] = summary
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			log.Printf("FAIL marshal %s: %v", stem, err)
			failed++
			continue
		}
		tmp := metadataPath + ".tmp"
		if err := os.WriteFile(tmp, out, 0644); err != nil {
			log.Printf("FAIL write %s: %v", stem, err)
			failed++
			continue
		}
		if err := os.Rename(tmp, metadataPath); err != nil {
			os.Remove(tmp)
			log.Printf("FAIL rename %s: %v", stem, err)
			failed++
			continue
		}
		log.Printf("OK   session-%s -> %q", stem, truncate(summary, 80))
		updated++
	}

	fmt.Printf("\nDone in %s. processed=%d updated=%d skipped=%d failed=%d\n",
		time.Since(start), processed, updated, skipped, failed)
}

// tailSummaryFromGzip streams the gzip-compressed file, retains only the last
// 8 KB of decompressed bytes, and runs the same extraction the server uses.
func tailSummaryFromGzip(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	const tailSize = 8192
	buf := make([]byte, 4096)
	ring := make([]byte, 0, tailSize)
	for {
		n, err := gz.Read(buf)
		if n > 0 {
			ring = append(ring, buf[:n]...)
			if len(ring) > tailSize {
				ring = ring[len(ring)-tailSize:]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return extractSummaryFromBytes(ring), nil
}

// extractSummaryFromBytes mirrors the server's logic in main.go: strip ANSI,
// drop control bytes, walk lines from the end, skip prompt/script noise and
// garbled TUI fragments, return the first usable line prefixed with "Agent: ".
func extractSummaryFromBytes(buf []byte) string {
	clean := ansiEscapeRe.ReplaceAll(buf, nil)
	var filtered []byte
	for _, b := range clean {
		if b == '\n' || (b >= 32 && b < 127) {
			filtered = append(filtered, b)
		}
	}
	lines := bytes.Split(bytes.TrimRight(filtered, "\n"), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(string(lines[i]))
		if trimmed == "" || trimmed == "$" || trimmed == "%" {
			continue
		}
		if strings.HasPrefix(trimmed, "Script done") || strings.HasPrefix(trimmed, "Script started") {
			continue
		}
		wordChars := 0
		for _, r := range trimmed {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '.' || r == ',' || r == '-' || r == '\'' || r == '"' || r == ':' || r == '!' || r == '?' {
				wordChars++
			}
		}
		if len(trimmed) > 10 && wordChars*2 < len(trimmed) {
			continue
		}
		if len(trimmed) < 8 {
			continue
		}
		return "Agent: " + sanitizeSummaryText(trimmed)
	}
	return ""
}

func sanitizeSummaryText(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// isRootRecordingStem returns true if `stem` is a 36-char UUID (root recording).
// Child recordings are formatted as "<parent-uuid>-<child-uuid>" and have length
// 73; we skip those because they share the parent's metadata.json.
func isRootRecordingStem(stem string) bool {
	const uuidLen = 36
	if len(stem) != uuidLen {
		return false
	}
	// Cheap shape check: 8-4-4-4-12 hex with dashes at the right positions.
	for i, b := range []byte(stem) {
		switch i {
		case 8, 13, 18, 23:
			if b != '-' {
				return false
			}
		default:
			if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) {
				return false
			}
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
