package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ansiReset ends a colored prefix.
const ansiReset = "\x1b[0m"

// prefixColors are the per-service ANSI foreground colors, cycled by service
// index. Chosen to be readable on both light and dark terminals.
var prefixColors = []string{
	"\x1b[36m", // cyan
	"\x1b[33m", // yellow
	"\x1b[32m", // green
	"\x1b[35m", // magenta
	"\x1b[34m", // blue
	"\x1b[31m", // red
}

// nameWidth returns the longest service name length, used to align prefixes.
func nameWidth(services []Service) int {
	w := 0
	for _, s := range services {
		if len(s.Name) > w {
			w = len(s.Name)
		}
	}
	return w
}

// colorFor returns the ANSI color escape for the i-th service, or "" when
// noColor is set (NO_COLOR honored).
func colorFor(i int, noColor bool) string {
	if noColor {
		return ""
	}
	return prefixColors[i%len(prefixColors)]
}

// servicePrefix builds the aligned log prefix "name<pad> | " for a service,
// optionally wrapped in the given ANSI color (with reset before the "| ").
func servicePrefix(name string, width int, color string) string {
	padded := name
	if len(name) < width {
		padded = name + strings.Repeat(" ", width-len(name))
	}
	if color == "" {
		return fmt.Sprintf("%s | ", padded)
	}
	return fmt.Sprintf("%s%s%s | ", color, padded, ansiReset)
}

// streamLines reads r line by line and writes each line to w prefixed by
// prefix, holding mu across each full "prefix + line + \n" write so concurrent
// streams never interleave within a line. A final unterminated fragment is
// still emitted (with a trailing newline appended). Read errors terminate the
// loop silently -- the caller's cmd.Wait reports the real failure.
func streamLines(w io.Writer, mu *sync.Mutex, prefix string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		mu.Lock()
		fmt.Fprintf(w, "%s%s\n", prefix, line)
		mu.Unlock()
	}
}
