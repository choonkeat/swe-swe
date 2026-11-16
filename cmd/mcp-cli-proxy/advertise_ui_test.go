package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAdvertiseUI(t *testing.T) {
	cfg, err := parseArgs([]string{"--name", "x", "--socket", "/tmp/x.sock", "--", "echo"})
	if err != nil || cfg.advertiseUI {
		t.Fatalf("default must be off: cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseArgs([]string{"--name", "x", "--socket", "/tmp/x.sock", "--advertise-ui", "--", "echo"})
	if err != nil || !cfg.advertiseUI {
		t.Fatalf("flag not parsed: cfg=%+v err=%v", cfg, err)
	}
}

// TestInitializeRequestDefaultUnchanged pins the off-by-default handshake to
// the exact bytes the proxy has always sent, so shipping the flag changes
// nothing for servers until swe-swe opts in.
func TestInitializeRequestDefaultUnchanged(t *testing.T) {
	got := string(initializeRequest(config{protocolVersion: "2024-11-05"}))
	want := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mcp-cli-proxy","version":"1"}}}`
	if got != want {
		t.Errorf("default handshake changed:\n got %s\nwant %s", got, want)
	}
}

// startProxyCfg launches a proxy with an explicit config and waits for its
// socket.
func startProxyCfg(t *testing.T, cfg config) string {
	t.Helper()
	cfg.socketPath = filepath.Join(t.TempDir(), "mcp.sock")
	go run(cfg, log.New(io.Discard, "", 0))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", cfg.socketPath); err == nil {
			c.Close()
			return cfg.socketPath
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("proxy socket never became ready")
	return ""
}

// TestAdvertiseUIHandshake proves, end to end through a real child process,
// what the server sees in initialize: the MCP Apps extension only with
// --advertise-ui, empty capabilities without it. Tool calls work either way.
func TestAdvertiseUIHandshake(t *testing.T) {
	bin := buildFakeServer(t)
	for _, on := range []bool{false, true} {
		initLog := filepath.Join(t.TempDir(), "init.json")
		t.Setenv("INIT_LOG", initLog)
		sock := startProxyCfg(t, config{
			name:            "fake",
			protocolVersion: defaultProtocolVersion,
			maxRestarts:     defaultMaxRestarts,
			advertiseUI:     on,
			command:         []string{bin},
		})
		resp := pollCall(t, sock, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, 5*time.Second)
		if _, isErr := resp["error"]; isErr {
			t.Fatalf("advertiseUI=%v: tools/list failed: %s", on, resp["error"])
		}

		raw, err := os.ReadFile(initLog)
		if err != nil {
			t.Fatalf("advertiseUI=%v: child never logged initialize: %v", on, err)
		}
		var params struct {
			Capabilities struct {
				Extensions map[string]struct {
					MimeTypes []string `json:"mimeTypes"`
				} `json:"extensions"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatal(err)
		}
		ui, has := params.Capabilities.Extensions[uiExtensionID]
		if has != on {
			t.Errorf("advertiseUI=%v: extension present=%v, params=%s", on, has, raw)
		}
		if on && (len(ui.MimeTypes) != 1 || ui.MimeTypes[0] != uiMimeType) {
			t.Errorf("mimeTypes=%v", ui.MimeTypes)
		}
	}
}
