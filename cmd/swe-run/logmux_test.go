package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestNameWidth(t *testing.T) {
	svcs := []Service{{Name: "web"}, {Name: "worker"}, {Name: "db"}}
	if got := nameWidth(svcs); got != len("worker") {
		t.Fatalf("nameWidth=%d want %d", got, len("worker"))
	}
}

func TestColorFor_NoColor(t *testing.T) {
	// NO_COLOR set -> no escape codes at all.
	if c := colorFor(0, true); c != "" {
		t.Errorf("colorFor with noColor=true returned %q, want empty", c)
	}
	// Colors on -> non-empty and distinct across indices.
	a := colorFor(0, false)
	b := colorFor(1, false)
	if a == "" || b == "" {
		t.Errorf("expected non-empty colors, got %q and %q", a, b)
	}
	if a == b {
		t.Errorf("expected distinct colors for indices 0 and 1, both %q", a)
	}
}

func TestServicePrefix_Aligned(t *testing.T) {
	// Without color, "web" padded to width 6 then "| ".
	p := servicePrefix("web", 6, "")
	if p != "web    | " {
		t.Fatalf("prefix=%q want %q", p, "web    | ")
	}
}

func TestStreamLines_PrefixesCompleteLines(t *testing.T) {
	var buf bytes.Buffer
	mu := &sync.Mutex{}
	r := strings.NewReader("line one\nline two\nno newline tail")
	streamLines(&buf, mu, "web | ", r)
	got := buf.String()
	want := "web | line one\nweb | line two\nweb | no newline tail\n"
	if got != want {
		t.Fatalf("streamLines output=\n%q\nwant\n%q", got, want)
	}
}

func TestStreamLines_NoTornLinesUnderConcurrency(t *testing.T) {
	// Two streams writing many lines concurrently to the same buffer under the
	// same mutex must never interleave within a single line.
	var buf bytes.Buffer
	mu := &sync.Mutex{}
	makeInput := func(tag string, n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(tag)
			sb.WriteString("-payload-payload-payload\n")
		}
		return sb.String()
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); streamLines(&buf, mu, "AAAAA | ", strings.NewReader(makeInput("A", 200))) }()
	go func() { defer wg.Done(); streamLines(&buf, mu, "BBBBB | ", strings.NewReader(makeInput("B", 200))) }()
	wg.Wait()

	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Each emitted line must be exactly one prefix + one payload, no mixing.
		okA := strings.HasPrefix(line, "AAAAA | A-payload")
		okB := strings.HasPrefix(line, "BBBBB | B-payload")
		if !okA && !okB {
			t.Fatalf("torn/interleaved line detected: %q", line)
		}
		if strings.Count(line, "|") != 1 {
			t.Fatalf("line has multiple prefixes (torn): %q", line)
		}
	}
}
