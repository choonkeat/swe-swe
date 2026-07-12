package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBasePortFromEnv(t *testing.T) {
	cases := []struct {
		val     string
		want    int
		wantOK  bool
	}{
		{"3000", 3000, true},
		{"", fallbackBasePort, false},
		{"notanumber", fallbackBasePort, false},
		{"70000", fallbackBasePort, false}, // out of range
		{"0", fallbackBasePort, false},
	}
	for _, c := range cases {
		getenv := func(k string) string {
			if k == "PORT" {
				return c.val
			}
			return ""
		}
		got, ok := basePortFromEnv(getenv)
		if got != c.want || ok != c.wantOK {
			t.Errorf("basePortFromEnv(PORT=%q)=(%d,%v) want (%d,%v)", c.val, got, ok, c.want, c.wantOK)
		}
	}
}

func TestPrintPortTable(t *testing.T) {
	var buf bytes.Buffer
	svcs := []Service{{Name: "web"}, {Name: "worker"}, {Name: "db"}}
	ports := map[string]int{"web": 3000, "worker": 8000, "db": 8020}
	printPortTable(&buf, svcs, ports, "web")
	out := buf.String()
	for _, want := range []string{"web", "3000", "primary", "worker", "8000", "PORT_WORKER", "db", "8020", "PORT_DB"} {
		if !strings.Contains(out, want) {
			t.Errorf("port table missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunMain_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	procfile := "web: echo hello-web\nworker: echo hello-worker\n"
	if err := os.WriteFile(filepath.Join(dir, "Procfile"), []byte(procfile), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	getenv := func(k string) string {
		if k == "PORT" {
			return "3000"
		}
		return ""
	}
	code := runMain(context.Background(), nil, &stdout, &stderr, getenv, os.Environ(), dir)
	if code != 0 {
		t.Errorf("exit code=%d want 0; stderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "hello-web") {
		t.Errorf("expected service output 'hello-web'; got:\n%s", out)
	}
	if !strings.Contains(out, "3000") {
		t.Errorf("expected port table with base 3000; got:\n%s", out)
	}
}

func TestRunMain_MissingProcfile(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), nil, &stdout, &stderr, func(string) string { return "" }, nil, dir)
	if code == 0 {
		t.Errorf("expected non-zero exit for missing Procfile")
	}
	if !strings.Contains(stderr.String(), "Procfile") {
		t.Errorf("expected error mentioning Procfile; got:\n%s", stderr.String())
	}
}

func TestRunMain_CustomProcfileFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Procfile.dev"), []byte("only: echo devmode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	getenv := func(k string) string { return "" } // no PORT -> fallback
	code := runMain(context.Background(), []string{"-f", "Procfile.dev"}, &stdout, &stderr, getenv, nil, dir)
	if code != 0 {
		t.Errorf("exit=%d want 0; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "devmode") {
		t.Errorf("expected 'devmode' output; got:\n%s", stdout.String())
	}
}
