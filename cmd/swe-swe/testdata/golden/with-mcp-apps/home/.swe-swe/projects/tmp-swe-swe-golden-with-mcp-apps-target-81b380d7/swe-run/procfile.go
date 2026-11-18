package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Service is one line of a Procfile: a name and the shell command to run.
type Service struct {
	Name    string
	Command string
}

// validName reports whether s matches [A-Za-z0-9_-]+ (the Procfile name rule).
func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// parseProcfile reads a foreman-compatible Procfile: one `name: command` per
// line. Blank lines and `#` comment lines are ignored. Names must match
// [A-Za-z0-9_-]+ and be unique; commands must be non-empty. Returns services in
// file order, or an error describing the first malformed / empty input.
func parseProcfile(r io.Reader) ([]Service, error) {
	var services []Service
	seen := make(map[string]bool)

	sc := bufio.NewScanner(r)
	// Allow long command lines.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			return nil, fmt.Errorf("line %d: missing ':' separator: %q", lineNo, line)
		}
		name := strings.TrimSpace(line[:idx])
		command := strings.TrimSpace(line[idx+1:])
		if !validName(name) {
			return nil, fmt.Errorf("line %d: invalid service name %q (allowed: A-Za-z0-9_-)", lineNo, name)
		}
		if command == "" {
			return nil, fmt.Errorf("line %d: service %q has an empty command", lineNo, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("line %d: duplicate service name %q", lineNo, name)
		}
		seen[name] = true
		services = append(services, Service{Name: name, Command: command})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no services found in Procfile")
	}
	return services, nil
}
