package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	in := `# a comment
FOO=bar

  BAZ = qux
QUOTED="hello world"
SINGLE='single quoted'
EMPTY=
WITHEQ=a=b=c
`
	got, err := parseEnvFile(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"FOO":    "bar",
		"BAZ":    "qux",
		"QUOTED": "hello world",
		"SINGLE": "single quoted",
		"EMPTY":  "",
		"WITHEQ": "a=b=c",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseEnvFile() =\n  %v\nwant\n  %v", got, want)
	}
}

func TestParseEnvFile_BadLine(t *testing.T) {
	if _, err := parseEnvFile(strings.NewReader("NOTANASSIGNMENT\n")); err == nil {
		t.Fatal("expected error for line without '='")
	}
}

func TestNormalizeEnvName(t *testing.T) {
	cases := map[string]string{
		"web":      "WEB",
		"web-1":    "WEB_1",
		"back_end": "BACK_END",
		"DB":       "DB",
		"a.b":      "A_B",
	}
	for in, want := range cases {
		if got := normalizeEnvName(in); got != want {
			t.Errorf("normalizeEnvName(%q)=%q want %q", in, got, want)
		}
	}
}

// envToMap turns a []string of "K=V" back into a map for assertions.
func envToMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestBuildServiceEnv_DiscoveryAndOwnPort(t *testing.T) {
	inherited := []string{"PATH=/usr/bin", "PORT=3000"}
	ports := map[string]int{"web": 3000, "db": 8000, "worker": 8020}

	env := buildServiceEnv(inherited, nil, nil, ports, "db")
	m := envToMap(env)

	// Discovery: PORT_<NAME> for ALL services, visible to every service.
	if m["PORT_WEB"] != "3000" {
		t.Errorf("PORT_WEB=%q want 3000", m["PORT_WEB"])
	}
	if m["PORT_DB"] != "8000" {
		t.Errorf("PORT_DB=%q want 8000", m["PORT_DB"])
	}
	if m["PORT_WORKER"] != "8020" {
		t.Errorf("PORT_WORKER=%q want 8020", m["PORT_WORKER"])
	}
	// Own PORT = this service's assigned port (foreman parity), overriding
	// the inherited base PORT=3000.
	if m["PORT"] != "8000" {
		t.Errorf("PORT=%q want 8000 (db's own)", m["PORT"])
	}
	// Inherited passthrough preserved.
	if m["PATH"] != "/usr/bin" {
		t.Errorf("PATH=%q want /usr/bin", m["PATH"])
	}
}

func TestBuildServiceEnv_Precedence(t *testing.T) {
	inherited := []string{"SHARED=inherited", "ONLYINHERIT=x"}
	sweEnv := map[string]string{"SHARED": "swe", "FROMSWE": "y"}
	dotEnv := map[string]string{"SHARED": "dotenv", "FROMDOT": "z"}
	ports := map[string]int{"web": 3000}

	m := envToMap(buildServiceEnv(inherited, sweEnv, dotEnv, ports, "web"))

	// later wins: inherited < .swe-swe/env < .env
	if m["SHARED"] != "dotenv" {
		t.Errorf("SHARED=%q want dotenv (.env wins)", m["SHARED"])
	}
	if m["ONLYINHERIT"] != "x" || m["FROMSWE"] != "y" || m["FROMDOT"] != "z" {
		t.Errorf("missing merged keys: %v", m)
	}
}

func TestBuildServiceEnv_RunnerPortsAlwaysWin(t *testing.T) {
	inherited := []string{"PORT=3000"}
	// A user .env that tries to pin PORT and PORT_DB must NOT override the
	// runner's authoritative discovery values.
	dotEnv := map[string]string{"PORT": "9999", "PORT_DB": "1"}
	ports := map[string]int{"web": 3000, "db": 8000}

	m := envToMap(buildServiceEnv(inherited, nil, dotEnv, ports, "db"))
	if m["PORT"] != "8000" {
		t.Errorf("PORT=%q want 8000 (runner wins over .env)", m["PORT"])
	}
	if m["PORT_DB"] != "8000" {
		t.Errorf("PORT_DB=%q want 8000 (runner wins over .env)", m["PORT_DB"])
	}
}
