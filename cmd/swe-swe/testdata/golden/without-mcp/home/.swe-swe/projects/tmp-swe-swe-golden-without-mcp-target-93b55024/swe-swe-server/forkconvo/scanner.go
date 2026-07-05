package forkconvo

import (
	"bufio"
	"io"
)

// newBigScanner returns a bufio.Scanner with a generous line buffer so that
// long agent events (which can include large tool inputs/outputs) don't get
// rejected as ErrTooLong. 16 MiB matches the largest single Claude tool_use
// payload I've observed; bump if it ever bites.
func newBigScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	const maxLine = 16 * 1024 * 1024
	s.Buffer(make([]byte, 1024*1024), maxLine)
	return s
}
