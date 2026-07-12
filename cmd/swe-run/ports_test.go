package main

import (
	"fmt"
	"testing"
)

// reservedBandsForTest mirrors the derived-port bands swe-swe-server allocates
// off each session's preview base (main.go offsets). Used by the invariant
// tests to prove assignPorts never lands on one.
func inReservedBandForTest(p int) bool {
	// offset 0 = preview base itself; others = agent-chat/public/cdp/vnc/files/proxy.
	for _, off := range []int{0, 1000, 2000, 3000, 4000, 6000, 20000} {
		lo := 3000 + off
		hi := 3019 + off
		if p >= lo && p <= hi {
			return true
		}
	}
	return false
}

func TestAssignPorts_PrimarySelection(t *testing.T) {
	tests := []struct {
		name        string
		services    []Service
		primary     string
		wantPrimary string
	}{
		{
			name:        "web wins by default",
			services:    []Service{{Name: "worker", Command: "a"}, {Name: "web", Command: "b"}},
			wantPrimary: "web",
		},
		{
			name:        "first line when no web",
			services:    []Service{{Name: "api", Command: "a"}, {Name: "worker", Command: "b"}},
			wantPrimary: "api",
		},
		{
			name:        "explicit override",
			services:    []Service{{Name: "web", Command: "a"}, {Name: "worker", Command: "b"}},
			primary:     "worker",
			wantPrimary: "worker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports, err := assignPorts(3000, tt.services, tt.primary)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ports[tt.wantPrimary] != 3000 {
				t.Fatalf("primary %q got port %d, want base 3000; full map=%v", tt.wantPrimary, ports[tt.wantPrimary], ports)
			}
		})
	}
}

func TestAssignPorts_NonPrimaryFormula(t *testing.T) {
	services := []Service{
		{Name: "web", Command: "a"},
		{Name: "worker", Command: "b"},
		{Name: "db", Command: "c"},
	}
	ports, err := assignPorts(3000, services, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// web is primary -> base. worker is 0th non-primary, db is 1st.
	if ports["web"] != 3000 {
		t.Errorf("web=%d want 3000", ports["web"])
	}
	if ports["worker"] != 3000+5000+0*20 {
		t.Errorf("worker=%d want 8000", ports["worker"])
	}
	if ports["db"] != 3000+5000+1*20 {
		t.Errorf("db=%d want 8020", ports["db"])
	}
}

func TestAssignPorts_Invariants(t *testing.T) {
	// A representative multi-service Procfile.
	services := []Service{
		{Name: "web", Command: "a"},
		{Name: "worker", Command: "b"},
		{Name: "db", Command: "c"},
		{Name: "cache", Command: "d"},
	}
	// (a) session-unique across all 20 bases: no port assigned in base B collides
	// with any port assigned in base B' (B != B').
	all := map[int]string{}
	for base := 3000; base <= 3019; base++ {
		ports, err := assignPorts(base, services, "")
		if err != nil {
			t.Fatalf("base %d: unexpected error: %v", base, err)
		}
		for name, p := range ports {
			key := fmt.Sprintf("base%d/%s", base, name)
			if prev, ok := all[p]; ok {
				t.Fatalf("port %d collides: %s and %s", p, prev, key)
			}
			all[p] = key
			// (b) avoids reserved bands, except the primary which is intentionally the base.
			if name != "web" && inReservedBandForTest(p) {
				t.Fatalf("%s port %d lands in a reserved band", key, p)
			}
			// (c) range bounds.
			if p < 1024 || p > 65535 {
				t.Fatalf("%s port %d out of [1024,65535]", key, p)
			}
		}
	}
}

func TestAssignPorts_Errors(t *testing.T) {
	services := []Service{{Name: "web", Command: "a"}}
	if _, err := assignPorts(3000, services, "nope"); err == nil {
		t.Error("expected error for unknown primary name")
	}
	if _, err := assignPorts(3000, nil, ""); err == nil {
		t.Error("expected error for empty services")
	}
	// Too many non-primary services would push a port into the files band (9000+).
	var many []Service
	many = append(many, Service{Name: "web", Command: "a"})
	for i := 0; i < 60; i++ {
		many = append(many, Service{Name: fmt.Sprintf("svc%d", i), Command: "x"})
	}
	if _, err := assignPorts(3000, many, ""); err == nil {
		t.Error("expected error when non-primary ports overflow the free band")
	}
}
