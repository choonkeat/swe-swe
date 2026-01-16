package playback

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseTimingFile parses a Linux script timing file and log content into playback frames.
// The timing file format is: "delay_seconds byte_count\n" per line.
// Each line indicates how long to wait before outputting the next N bytes from the log.
func ParseTimingFile(logContent, timingContent []byte) ([]PlaybackFrame, error) {
	// Find where the header ends (after "Script started on..." line)
	headerEndOffset := findHeaderEnd(logContent)

	var frames []PlaybackFrame
	var cumulativeTime float64
	var byteOffset int
	var headerSkipped bool
	var timeOffset float64 // Time to subtract once we start real content

	scanner := bufio.NewScanner(bytes.NewReader(timingContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		delay, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			continue
		}

		byteCount, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		cumulativeTime += delay

		// Extract the content for this frame
		endOffset := byteOffset + byteCount
		if endOffset > len(logContent) {
			endOffset = len(logContent)
		}

		// Skip frames that are part of the header
		if byteOffset < headerEndOffset {
			if endOffset >= headerEndOffset {
				// This frame spans the header boundary - take only content after header
				if !headerSkipped {
					timeOffset = cumulativeTime
					headerSkipped = true
				}
				content := logContent[headerEndOffset:endOffset]
				if len(content) > 0 {
					frames = append(frames, PlaybackFrame{
						Timestamp: cumulativeTime - timeOffset,
						Content:   cleanContent(content),
					})
				}
			}
			// else: entirely within header, skip
		} else {
			// Past the header
			if !headerSkipped {
				timeOffset = cumulativeTime - delay
				headerSkipped = true
			}
			content := logContent[byteOffset:endOffset]
			frames = append(frames, PlaybackFrame{
				Timestamp: cumulativeTime - timeOffset,
				Content:   cleanContent(content),
			})
		}

		byteOffset = endOffset
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return frames, nil
}

// findHeaderEnd returns the byte offset where the header ends.
// The header is the "Script started on..." line plus any metadata lines.
func findHeaderEnd(content []byte) int {
	// Look for the first newline after "Script started on"
	idx := bytes.Index(content, []byte("Script started on"))
	if idx == -1 {
		return 0 // No header found
	}

	// Find the end of this line
	newlineIdx := bytes.IndexByte(content[idx:], '\n')
	if newlineIdx == -1 {
		return len(content)
	}

	return idx + newlineIdx + 1
}

// cleanContent sanitizes terminal content for safe display.
// Removes problematic control sequences and fixes invalid UTF-8.
func cleanContent(content []byte) string {
	// Convert to string, replacing invalid UTF-8 with empty string
	s := sanitizeUTF8(content)

	// Remove OSC sequences (Operating System Commands) that might not render well
	// These are \x1b]...\x07 or \x1b]...\x1b\\
	oscRegex := regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	s = oscRegex.ReplaceAllString(s, "")

	// Remove DCS sequences (Device Control String)
	// These are \x1bP...\x1b\\
	dcsRegex := regexp.MustCompile(`\x1bP[^\x1b]*\x1b\\`)
	s = dcsRegex.ReplaceAllString(s, "")

	// Remove APC sequences (Application Program Command)
	// These are \x1b_...\x1b\\
	apcRegex := regexp.MustCompile(`\x1b_[^\x1b]*\x1b\\`)
	s = apcRegex.ReplaceAllString(s, "")

	return s
}

// sanitizeUTF8 converts bytes to a valid UTF-8 string,
// removing any invalid sequences instead of replacing with U+FFFD.
func sanitizeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}

	// Build a new string with only valid UTF-8 sequences
	var result strings.Builder
	result.Grow(len(b))

	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			// Invalid byte, skip it
			b = b[1:]
			continue
		}
		result.WriteRune(r)
		b = b[size:]
	}

	return result.String()
}

// StripScriptMetadata removes script command header/footer from log content.
// Linux script adds lines like "Script started on..." and "Script done on..."
func StripScriptMetadata(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))

	startIndex := 0
	endIndex := len(lines)

	// Skip header lines (first few lines that are script metadata)
	for i := 0; i < len(lines) && i < 5; i++ {
		line := string(lines[i])
		if strings.HasPrefix(line, "Script started on") ||
			strings.HasPrefix(line, "Command:") {
			startIndex = i + 1
		}
	}

	// Skip footer lines
	for i := len(lines) - 1; i >= 0; i-- {
		line := string(lines[i])
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "Script done on") ||
			strings.Contains(line, "Command exit status") ||
			strings.Contains(line, "Saving session") {
			endIndex = i
		} else if trimmed != "" && endIndex < len(lines) {
			// Found content before footer, stop
			break
		}
	}

	// Trim trailing empty lines
	for endIndex > startIndex && strings.TrimSpace(string(lines[endIndex-1])) == "" {
		endIndex--
	}

	if startIndex >= len(lines) || startIndex >= endIndex {
		return content // Return original if nothing to strip
	}

	return bytes.Join(lines[startIndex:endIndex], []byte("\n"))
}
