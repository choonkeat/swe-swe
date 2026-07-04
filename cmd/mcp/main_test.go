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
			"opts":{"type":["null","array"],"items":{"type":"string"}},
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

	t.Run("array flag with invalid JSON", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--text", "x", "--tags", "notjson"}); err == nil {
			t.Error("expected JSON parse error for array flag")
		}
	})

	// Regression: real MCP schemas express nullable/optional array fields as a
	// UNION type -- `"type":["null","array"]` -- not a bare "array". The client
	// must recognize "array" inside the union and JSON-decode the value; the bug
	// was that a union type failed to parse into a plain string field, silently
	// became "", defaulted to string, and forwarded the raw text uncoerced, so
	// the server rejected it (agent-chat send_message --more_quick_replies).
	t.Run("union nullable-array coerces JSON to array", func(t *testing.T) {
		got, err := parseToolArgs(s, []string{"--text", "x", "--opts", `["a","b"]`})
		if err != nil {
			t.Fatal(err)
		}
		arr, ok := got["opts"].([]any)
		if !ok || len(arr) != 2 {
			t.Errorf("opts should decode to a 2-element array, got %#v (%T)", got["opts"], got["opts"])
		}
	})

	t.Run("union nullable-array rejects raw string", func(t *testing.T) {
		if _, err := parseToolArgs(s, []string{"--text", "x", "--opts", "notjson"}); err == nil {
			t.Error("expected JSON parse error for union array flag, got nil")
		}
	})
}

// TestToolHelpUnionType proves a union `["null","array"]` renders as "array" in
// help, not "string", so the agent knows to pass JSON.
func TestToolHelpUnionType(t *testing.T) {
	const list = `{"tools":[{"name":"send","description":"send","inputSchema":{"type":"object","properties":{"opts":{"type":["null","array"],"items":{"type":"string"}}},"required":[]}}]}`
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(list)
	})
	out, _, code := runCapture(t, dir, "svc", "send", "-h")
	if code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out, "--opts array") {
		t.Errorf("union type should display as array, got %q", out)
	}
}

// richToolsList exercises everything hi-def help must surface: annotations
// (title + readOnlyHint), schema defaults, multi-line flag descriptions, array
// item types, and exotic schema keywords (minimum) that must never be dropped.
const richToolsList = `{"tools":[
  {"name":"shoot","description":"Take a screenshot.\n\nSecond paragraph of tool docs.",
   "annotations":{"title":"Take a screenshot","readOnlyHint":true},
   "inputSchema":{"type":"object","properties":{
     "scale":{"type":"string","enum":["css","device"],"default":"css","description":"Image resolution scale.\nSecond line of flag docs."},
     "tags":{"type":"array","items":{"type":"string"},"description":"Labels."},
     "depth":{"type":"integer","minimum":1,"description":"How deep."}
   },"required":["scale"]}},
  {"name":"nuke","description":"Delete everything.","annotations":{"destructiveHint":true},
   "inputSchema":{"type":"object","properties":{}}}
]}`

// TestToolHelpFullFidelity proves -h renders the full tool definition a native
// MCP client would inject: no truncation, defaults, annotations, item types,
// required-first ordering, and a raw-JSON fallback for unmodeled keywords.
func TestToolHelpFullFidelity(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(richToolsList)
	})
	out, _, code := runCapture(t, dir, "svc", "shoot", "-h")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{
		"Take a screenshot  [read-only]",       // annotations title + hint
		"Second paragraph of tool docs.",       // full tool description
		"--scale string (required, default: css)", // default from schema, not prose
		"Second line of flag docs.",            // full multi-line flag description
		"one of: css, device",
		"--tags array of string", // item type expanded
		`schema: {"minimum":1}`,  // exotic keyword surfaced, not dropped
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tool help missing %q:\n%s", want, out)
		}
	}
	// Required flags come first: --scale before optional --depth and --tags.
	if strings.Index(out, "--scale") > strings.Index(out, "--depth") {
		t.Errorf("required flag should be listed first:\n%s", out)
	}
}

// TestToolHelpDestructiveHint proves destructiveHint=true is marked even when
// the tool has no title.
func TestToolHelpDestructiveHint(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(richToolsList)
	})
	out, _, code := runCapture(t, dir, "svc", "nuke", "-h")
	if code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out, "[destructive]") {
		t.Errorf("expected [destructive] marker:\n%s", out)
	}
}

// TestServerFullDumpsAllTools proves --full prints every tool's full docs in
// one shot -- the MCP-less equivalent of native tool-definition injection.
func TestServerFullDumpsAllTools(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(richToolsList)
	})
	out, _, code := runCapture(t, dir, "svc", "--full")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{
		"mcp svc shoot [flags]",
		"Second paragraph of tool docs.",
		"mcp svc nuke [flags]",
		"Delete everything.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--full missing %q:\n%s", want, out)
		}
	}
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

// TestRunIsErrorExit proves a tool result flagged isError still prints its
// content but exits non-zero, so the agent's shell sees the failure.
func TestRunIsErrorExit(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		if method == "tools/list" {
			return json.RawMessage(echoToolsList)
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"boom"}],"isError":true}`)
	})
	out, _, code := runCapture(t, dir, "svc", "echo", "--text", "x")
	if code != 1 {
		t.Errorf("expected exit 1 on isError result, got %d", code)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("expected error content printed, got %q", out)
	}
}

// TestRunUnknownTool proves calling a tool the server does not expose fails
// cleanly with a helpful message.
func TestRunUnknownTool(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(echoToolsList)
	})
	_, errStr, code := runCapture(t, dir, "svc", "missing", "--x", "1")
	if code == 0 {
		t.Error("expected non-zero exit for unknown tool")
	}
	if !strings.Contains(errStr, "no tool") {
		t.Errorf("expected 'no tool' error, got %q", errStr)
	}
}

// TestTopHelpListsServers proves the socket directory is the registry: every
// <name>.sock is surfaced as a server, sorted.
func TestTopHelpListsServers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"beta.sock", "alpha.sock", "notasocket"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	out, _, code := runCapture(t, dir)
	if code != 0 {
		t.Fatalf("top help exit %d", code)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("top help missing discovered servers: %q", out)
	}
	if strings.Contains(out, "notasocket") {
		t.Errorf("top help listed a non-socket file: %q", out)
	}
	if strings.Index(out, "alpha") > strings.Index(out, "beta") {
		t.Errorf("servers not sorted: %q", out)
	}
}
