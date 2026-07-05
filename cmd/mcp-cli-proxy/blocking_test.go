package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBlockingTools(t *testing.T) {
	cfg, err := parseArgs([]string{"--name", "x", "--socket", "/s", "--blocking-tools", "send_message, send_verbal_reply ,", "--", "cmd"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.blockingTools["send_message"] || !cfg.blockingTools["send_verbal_reply"] {
		t.Fatalf("want both tools, got %v", cfg.blockingTools)
	}
	if len(cfg.blockingTools) != 2 {
		t.Fatalf("empty entries should be dropped, got %v", cfg.blockingTools)
	}
}

func TestBlockingToolName(t *testing.T) {
	set := map[string]bool{"send_message": true}
	call := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_message","arguments":{}}}`)
	if got := blockingToolName("tools/call", call, set); got != "send_message" {
		t.Fatalf("want send_message, got %q", got)
	}
	// non-blocking tool
	other := []byte(`{"method":"tools/call","params":{"name":"check_messages"}}`)
	if got := blockingToolName("tools/call", other, set); got != "" {
		t.Fatalf("check_messages must not match, got %q", got)
	}
	// wrong method
	if got := blockingToolName("tools/list", call, set); got != "" {
		t.Fatalf("tools/list must not match, got %q", got)
	}
	// empty set
	if got := blockingToolName("tools/call", call, nil); got != "" {
		t.Fatalf("nil set must not match, got %q", got)
	}
}

func TestBlockingNoticeFrame(t *testing.T) {
	b := blockingNotice("swe-swe-agent-chat", "send_message")
	var m struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Params struct {
			Data string `json:"data"`
		} `json:"params"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("frame not valid JSON: %v", err)
	}
	if m.Method != "notifications/message" {
		t.Fatalf("want notifications/message, got %q", m.Method)
	}
	if len(m.ID) != 0 {
		t.Fatalf("notification must have no id, got %s", m.ID)
	}
	if !strings.Contains(m.Params.Data, "BLOCKING") || !strings.Contains(m.Params.Data, "send_message") {
		t.Fatalf("data missing warning text: %q", m.Params.Data)
	}
}
