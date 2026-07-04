package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseToolArgs(t *testing.T) {
	s := parseSchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"text":{"type":"string"},
			"count":{"type":"integer"},
			"ratio":{"type":"number"},
			"flag":{"type":"boolean"},
			"tags":{"type":"array"},
			"mode":{"type":"string","enum":["a","b"]}
		},
		"required":["text"]
	}`))

	t.Run("happy path with type coercion", func(t *testing.T) {
		got, err := parseToolArgs(s, []string{
			"--text", "hi", "--count", "3", "--ratio", "1.5",
			"--flag", "--tags", `["x","y"]`, "--mode", "a",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got["text"] != "hi" {
			t.Errorf("text=%v", got["text"])
		}
		if got["count"] != int64(3) {
			t.Errorf("count=%v (%T)", got["count"], got["count"])
		}
		if got["ratio"] != 1.5 {
			t.Errorf("ratio=%v", got["ratio"])
		}
		if got["flag"] != true {
			t.Errorf("flag=%v", got["flag"])
		}
		if arr, ok := got["tags"].([]any); !ok || len(arr) != 2 {
			t.Errorf("tags=%v", got["tags"])
		}
	})

	t.Run("bare boolean is true", func(t *testing.T) {
		got, _ := parseToolArgs(s, []string{"--text", "x", "--flag"})
		if got["flag"] != true {
			t.Errorf("bare --flag should be true, got %v", got["flag"])
		}
	})

	t.Run("--flag=value form", func(t *testing.T) {
		got, err := parseToolArgs(s, []string{"--text=hello world"})
		if err != nil {
			t.Fatal(err)
		}
		if got["text"] != "hello world" {
			t.Errorf("text=%v", got["text"])
		}
	})

	t.Run("missing required", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--count", "1"}); err == nil {
			t.Error("expected missing-required error")
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--text", "x", "--nope", "y"}); err == nil {
			t.Error("expected unknown-flag error")
		}
	})

	t.Run("enum violation", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--text", "x", "--mode", "c"}); err == nil {
			t.Error("expected enum error")
		}
	})

	t.Run("bad integer", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--text", "x", "--count", "notanint"}); err == nil {
			t.Error("expected integer parse error")
		}
	})
}

// fakeSocketServer answers tools/list and tools/call on a unix socket, standing
// in for an mcp-cli-proxy without needing a child process.
func fakeSocketServer(t *testing.T, server string, handler func(method string, params json.RawMessage) json.RawMessage) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, server+".sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sc := bufio.NewScanner(c)
				sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
				for sc.Scan() {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params json.RawMessage `json:"params"`
					}
					if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
						continue
					}
					result := handler(req.Method, req.Params)
					resp := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": json.RawMessage(result)}
					b, _ := json.Marshal(resp)
					c.Write(append(b, '\n'))
				}
			}(c)
		}
	}()
	return dir
}

const echoToolsList = `{"tools":[{"name":"echo","description":"Echo text back.\nSecond line.","inputSchema":{"type":"object","properties":{"text":{"type":"string","description":"what to echo"}},"required":["text"]}}]}`

func runCapture(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("SWE_MCP_DIR", dir)
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	code := run(args, outW, errW)
	outW.Close()
	errW.Close()
	outB, _ := readAll(outR)
	errB, _ := readAll(errR)
	return outB, errB, code
}

func readAll(f *os.File) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String(), nil
		}
	}
}

func TestRunCallText(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		switch method {
		case "tools/list":
			return json.RawMessage(echoToolsList)
		case "tools/call":
			var p struct {
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			json.Unmarshal(params, &p)
			return json.RawMessage(`{"content":[{"type":"text","text":"` + p.Arguments.Text + `"}]}`)
		}
		return json.RawMessage(`{}`)
	})

	out, errStr, code := runCapture(t, dir, "svc", "echo", "--text", "hello")
	if code != 0 {
		t.Fatalf("exit %d, stderr=%s", code, errStr)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("out=%q", out)
	}
}

func TestRunCallStructuredAndImage(t *testing.T) {
	// base64 of "PNGDATA"
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		if method == "tools/list" {
			return json.RawMessage(echoToolsList)
		}
		return json.RawMessage(`{"content":[{"type":"image","mimeType":"image/png","data":"UE5HREFUQQ=="}],"structuredContent":{"ok":true}}`)
	})
	imgDir := t.TempDir()
	t.Setenv("SWE_MCP_IMAGE_DIR", imgDir)

	out, errStr, code := runCapture(t, dir, "svc", "echo", "--text", "x")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errStr)
	}
	if !strings.Contains(out, "image/png saved to") {
		t.Errorf("expected image path in output, got %q", out)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("expected structured JSON, got %q", out)
	}
	// confirm a file was actually written
	entries, _ := os.ReadDir(imgDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 image file, got %d", len(entries))
	}
}

func TestRunServerHelpListsTools(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(echoToolsList)
	})
	out, _, code := runCapture(t, dir, "svc")
	if code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out, "echo") || !strings.Contains(out, "Echo text back.") {
		t.Errorf("server help missing tool listing: %q", out)
	}
	// description should be first-line only
	if strings.Contains(out, "Second line.") {
		t.Errorf("server help should show only first description line: %q", out)
	}
}

func TestRunToolHelpShowsFlags(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(echoToolsList)
	})
	out, _, code := runCapture(t, dir, "svc", "echo", "-h")
	if code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out, "--text") || !strings.Contains(out, "(required)") {
		t.Errorf("tool help missing flag: %q", out)
	}
}

func TestRunUnavailableServer(t *testing.T) {
	dir := t.TempDir() // no sockets
	_, errStr, code := runCapture(t, dir, "nope", "tool", "--x", "1")
	if code == 0 {
		t.Error("expected non-zero exit for missing server")
	}
	if !strings.Contains(errStr, "unavailable") {
		t.Errorf("expected 'unavailable' error, got %q", errStr)
	}
}
