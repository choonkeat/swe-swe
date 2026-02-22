package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestPromptAgentsDefault(t *testing.T) {
	input := "\n" // press Enter → use default
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	agents, err := promptAgents(scanner, &out, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No detected agents → default is allAgents
	if len(agents) != len(allAgents) {
		t.Errorf("expected all agents, got %v", agents)
	}
}

func TestPromptAgentsDetected(t *testing.T) {
	input := "\n" // press Enter → use detected
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	detected := []string{"claude", "codex"}
	agents, err := promptAgents(scanner, &out, detected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 2 || agents[0] != "claude" || agents[1] != "codex" {
		t.Errorf("expected %v, got %v", detected, agents)
	}
}

func TestPromptAgentsExplicit(t *testing.T) {
	input := "claude,gemini\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	agents, err := promptAgents(scanner, &out, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 2 || agents[0] != "claude" || agents[1] != "gemini" {
		t.Errorf("expected [claude gemini], got %v", agents)
	}
}

func TestPromptAgentsInvalid(t *testing.T) {
	input := "invalid\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	_, err := promptAgents(scanner, &out, nil)
	if err == nil {
		t.Fatal("expected error for invalid agent")
	}
}

func TestPromptDockerYes(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"\n", false},
		{"no\n", false},
	}

	for _, tt := range tests {
		scanner := bufio.NewScanner(strings.NewReader(tt.input))
		var out bytes.Buffer
		got := promptDocker(scanner, &out)
		if got != tt.want {
			t.Errorf("promptDocker(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPromptAccessLocalOnly(t *testing.T) {
	input := "\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "no" {
		t.Errorf("expected ssl='no', got %q", ssl)
	}
	if email != "" {
		t.Errorf("expected empty email, got %q", email)
	}
}

func TestPromptAccessSelfsignLocalhost(t *testing.T) {
	// s then Enter → selfsign (localhost only)
	input := "s\n\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "selfsign" {
		t.Errorf("expected ssl='selfsign', got %q", ssl)
	}
	if email != "" {
		t.Errorf("expected empty email, got %q", email)
	}
}

func TestPromptAccessSelfsignWithHost(t *testing.T) {
	// s then hostname → selfsign@hostname
	input := "s\n192.168.1.100\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "selfsign@192.168.1.100" {
		t.Errorf("expected ssl='selfsign@192.168.1.100', got %q", ssl)
	}
	if email != "" {
		t.Errorf("expected empty email, got %q", email)
	}
}

func TestPromptAccessLetsencrypt(t *testing.T) {
	input := "l\nexample.com\nadmin@example.com\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "letsencrypt-staging@example.com" {
		t.Errorf("expected ssl='letsencrypt-staging@example.com', got %q", ssl)
	}
	if email != "admin@example.com" {
		t.Errorf("expected email='admin@example.com', got %q", email)
	}
	// Should include upgrade instructions
	if !strings.Contains(out.String(), "letsencrypt@example.com") {
		t.Error("expected upgrade instructions in output")
	}
}

func TestPromptAccessLetsencryptNoEmail(t *testing.T) {
	// User provides hostname but empty email → falls back to "no"
	input := "l\nexample.com\n\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "no" {
		t.Errorf("expected ssl='no', got %q", ssl)
	}
	if email != "" {
		t.Errorf("expected empty email, got %q", email)
	}
}

func TestPromptAccessLetsencryptNoHostname(t *testing.T) {
	// User chooses l but provides empty hostname → falls back to "no"
	input := "l\n\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	var out bytes.Buffer

	ssl, email := promptAccess(scanner, &out)
	if ssl != "no" {
		t.Errorf("expected ssl='no', got %q", ssl)
	}
	if email != "" {
		t.Errorf("expected empty email, got %q", email)
	}
}

func TestDetectInstalledAgents(t *testing.T) {
	// detectInstalledAgents should return a subset of allAgents
	detected := detectInstalledAgents()
	agentSet := make(map[string]bool)
	for _, a := range allAgents {
		agentSet[a] = true
	}
	for _, a := range detected {
		if !agentSet[a] {
			t.Errorf("detected agent %q not in allAgents", a)
		}
	}
}

func TestParseSSLFlagValue(t *testing.T) {
	tests := []struct {
		input      string
		wantMode   string
		wantHost   string
		wantDomain string
		wantErr    bool
	}{
		{"no", "no", "", "", false},
		{"selfsign", "selfsign", "", "", false},
		{"selfsign@192.168.1.1", "selfsign", "192.168.1.1", "", false},
		{"letsencrypt@example.com", "letsencrypt", "", "example.com", false},
		{"letsencrypt-staging@test.com", "letsencrypt-staging", "", "test.com", false},
		{"invalid", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mode, host, domain, err := parseSSLFlagValue(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSSLFlagValue(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if domain != tt.wantDomain {
				t.Errorf("domain = %q, want %q", domain, tt.wantDomain)
			}
		})
	}
}
