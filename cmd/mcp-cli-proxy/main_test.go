package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServerSrc is a minimal stdio MCP server for tests. It answers initialize,
// tools/list, and tools/call. Behaviour by tool name:
//   - "block" sleeps 400ms before replying (exercises concurrent multiplexing).
//   - "crash" calls os.Exit(1) with no reply (exercises restart / fail-pending).
//   - anything else echoes its "text" argument, or its name when text is absent.
//
// It also records any notifications/cancelled it receives: the requestId is
// appended to the file named by $CANCEL_LOG, so tests can observe both
// proxy-generated cancels (client disconnect) and plain notification
// passthrough. Params are parsed generically to avoid struct tags (backticks
// can't nest in this raw-string literal).
const fakeServerSrc = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

var outMu sync.Mutex

func emit(s string) {
	outMu.Lock()
	fmt.Print(s)
	outMu.Unlock()
}

func recordCancel(reqID string) {
	path := os.Getenv("CANCEL_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(reqID + "\n")
}

func handle(line []byte) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	var method string
	json.Unmarshal(m["method"], &method)
	id := m["id"]
	switch method {
	case "initialize":
		emit(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"serverInfo\":{\"name\":\"fake\",\"version\":\"1\"}}}\n", id))
	case "notifications/initialized":
	case "notifications/cancelled":
		var params map[string]json.RawMessage
		json.Unmarshal(m["params"], &params)
		recordCancel(string(params["requestId"]))
	case "tools/list":
		emit(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"echo\",\"inputSchema\":{\"type\":\"object\",\"properties\":{\"text\":{\"type\":\"string\"}}}},{\"name\":\"block\"},{\"name\":\"crash\"}]}}\n", id))
	case "tools/call":
		var params map[string]json.RawMessage
		json.Unmarshal(m["params"], &params)
		var name string
		json.Unmarshal(params["name"], &name)
		var args map[string]json.RawMessage
		json.Unmarshal(params["arguments"], &args)
		var text string
		json.Unmarshal(args["text"], &text)
		if name == "crash" {
			os.Exit(1)
		}
		if name == "block" {
			time.Sleep(400 * time.Millisecond)
		}
		if text == "" {
			text = name
		}
		emit(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":%q}]}}\n", id, text))
	}
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		go handle(line)
	}
}
`

// buildFakeServer compiles the fake MCP server into a temp binary.
func buildFakeServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(fakeServerSrc), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fakemcp")
	runGo(t, dir, "mod", "init", "fakemcp")
	runGo(t, dir, "build", "-o", bin, ".")
	return bin
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v failed: %v\n%s", args, err, out)
	}
}

// startProxy launches a proxy fronting the fake server binary on a temp socket.
func startProxy(t *testing.T, bin string) string {
	return startProxyCmd(t, []string{bin}, defaultMaxRestarts)
}

// startProxyCmd launches a proxy fronting an arbitrary command with a chosen
// crash-loop cap, and waits for its socket to accept connections. The socket is
// bound before the child is supervised, so it becomes ready even when the child
// cannot start (used by the crash-loop-cap test).
func startProxyCmd(t *testing.T, command []string, maxRestarts int) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "mcp.sock")
	cfg := config{
		name:            "fake",
		socketPath:      sock,
		protocolVersion: defaultProtocolVersion,
		maxRestarts:     maxRestarts,
		command:         command,
	}
	go run(cfg, log.New(io.Discard, "", 0))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("proxy socket never became ready")
	return ""
}

// call sends one request and reads one response line.
func call(t *testing.T, sock, request string) map[string]json.RawMessage {
	t.Helper()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "%s\n", request)
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		t.Fatalf("no response for %s", request)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
		t.Fatalf("bad response %q: %v", sc.Text(), err)
	}
	return m
}

func TestToolsListRoundTrip(t *testing.T) {
	sock := startProxy(t, buildFakeServer(t))
	resp := call(t, sock, `{"jsonrpc":"2.0","id":42,"method":"tools/list"}`)
	if string(resp["id"]) != "42" {
		t.Errorf("id not restored: got %s want 42", resp["id"])
	}
	if _, ok := resp["result"]; !ok {
		t.Errorf("expected result, got %v", resp)
	}
}

func TestConcurrentClientsNoHeadOfLineBlock(t *testing.T) {
	sock := startProxy(t, buildFakeServer(t))

	var wg sync.WaitGroup
	fastDone := make(chan time.Duration, 1)
	wg.Add(2)
	// A slow "block" call and a fast call issued concurrently: the fast call
	// must not wait on the slow one (proves id-multiplexing, not serialization).
	go func() {
		defer wg.Done()
		call(t, sock, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"block","arguments":{}}}`)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // ensure the block call is in flight
		t0 := time.Now()
		resp := call(t, sock, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"quick","arguments":{}}}`)
		fastDone <- time.Since(t0)
		if string(resp["id"]) != "2" {
			t.Errorf("fast call id mismatch: %s", resp["id"])
		}
	}()
	fastLatency := <-fastDone
	if fastLatency > 300*time.Millisecond {
		t.Errorf("fast call was head-of-line blocked: %v", fastLatency)
	}
	wg.Wait()
}

func TestParseArgs(t *testing.T) {
	cfg, err := parseArgs([]string{"--name", "x", "--socket", "/tmp/x.sock", "--", "echo", "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.name != "x" || cfg.socketPath != "/tmp/x.sock" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.command) != 2 || cfg.command[0] != "echo" {
		t.Errorf("unexpected command: %v", cfg.command)
	}
	if _, err := parseArgs([]string{"--name", "x", "--socket", "/tmp/x.sock"}); err == nil {
		t.Error("expected error for missing -- command")
	}
}

// pollCall retries the same request until the response is not an error, or the
// deadline passes. It returns the last response seen.
func pollCall(t *testing.T, sock, request string, timeout time.Duration) map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]json.RawMessage
	for time.Now().Before(deadline) {
		last = call(t, sock, request)
		if _, isErr := last["error"]; !isErr {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

// TestChildCrashRestart proves that when the child dies mid-request the caller
// gets an error (not a hang), and the proxy restarts the child so a later call
// succeeds on the same socket.
func TestChildCrashRestart(t *testing.T) {
	sock := startProxy(t, buildFakeServer(t))

	resp := call(t, sock, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"crash","arguments":{}}}`)
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error response after child crash, got %v", resp)
	}

	recovered := pollCall(t, sock, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{}}}`, 3*time.Second)
	if _, ok := recovered["result"]; !ok {
		t.Fatalf("proxy did not recover after child crash: %v", recovered)
	}
	if string(recovered["id"]) != "2" {
		t.Errorf("id not restored after restart: got %s want 2", recovered["id"])
	}
}

// TestCrashLoopCap proves that a child which never starts trips the crash-loop
// cap and the proxy reports the server as permanently unavailable (not merely
// "restarting").
func TestCrashLoopCap(t *testing.T) {
	sock := startProxyCmd(t, []string{"sh", "-c", "exit 1"}, 0)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp := call(t, sock, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		var e struct {
			Message string `json:"message"`
		}
		if raw, ok := resp["error"]; ok {
			json.Unmarshal(raw, &e)
			if strings.Contains(e.Message, "unavailable") {
				return // success
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("proxy never reported the crash-looped server as unavailable")
}

// waitForFile polls until path exists and is non-empty, returning its contents.
func waitForFile(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never became non-empty", path)
	return ""
}

// TestCancelOnDisconnect proves that when a client disconnects with a request in
// flight, the proxy forwards notifications/cancelled (with the internal request
// id) to the child.
func TestCancelOnDisconnect(t *testing.T) {
	cancelLog := filepath.Join(t.TempDir(), "cancels.log")
	t.Setenv("CANCEL_LOG", cancelLog)
	sock := startProxy(t, buildFakeServer(t))

	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	// First request on a fresh proxy gets internal id 1. Fire the slow "block"
	// call, let it reach the child, then disconnect before it returns.
	fmt.Fprintf(c, "%s\n", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"block","arguments":{}}}`)
	time.Sleep(80 * time.Millisecond)
	c.Close()

	got := strings.TrimSpace(waitForFile(t, cancelLog, 2*time.Second))
	if got != "1" {
		t.Errorf("expected cancelled requestId 1, got %q", got)
	}
}

// TestNotificationPassthrough proves that an id-less notification from a client
// is forwarded verbatim to the child (observed here via the child's cancel log).
func TestNotificationPassthrough(t *testing.T) {
	cancelLog := filepath.Join(t.TempDir(), "notif.log")
	t.Setenv("CANCEL_LOG", cancelLog)
	sock := startProxy(t, buildFakeServer(t))

	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "%s\n", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`)

	got := strings.TrimSpace(waitForFile(t, cancelLog, 2*time.Second))
	if got != "99" {
		t.Errorf("child did not receive forwarded notification: got %q", got)
	}
}

// TestMalformedJSON proves the proxy answers garbage with a parse error and
// keeps the connection alive for the next well-formed request.
func TestMalformedJSON(t *testing.T) {
	sock := startProxy(t, buildFakeServer(t))
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	fmt.Fprintf(c, "not json\n")
	if !sc.Scan() {
		t.Fatal("no response to malformed input")
	}
	var errResp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(sc.Bytes(), &errResp); err != nil {
		t.Fatalf("bad error response %q: %v", sc.Text(), err)
	}
	if errResp.Error == nil || errResp.Error.Code != -32700 {
		t.Errorf("expected parse error -32700, got %s", sc.Text())
	}

	// Connection must survive: a valid request now succeeds.
	fmt.Fprintf(c, "%s\n", `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`)
	if !sc.Scan() {
		t.Fatal("connection did not survive malformed input")
	}
	var ok struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal(sc.Bytes(), &ok)
	if string(ok.ID) != "5" || len(ok.Result) == 0 {
		t.Errorf("expected result for id 5 after recovery, got %s", sc.Text())
	}
}

// TestStaleSocketRemoval proves the proxy removes a leftover socket file from a
// previous run before binding.
func TestStaleSocketRemoval(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "mcp.sock")
	if err := os.WriteFile(sock, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		name:            "fake",
		socketPath:      sock,
		protocolVersion: defaultProtocolVersion,
		maxRestarts:     defaultMaxRestarts,
		command:         []string{buildFakeServer(t)},
	}
	go run(cfg, log.New(io.Discard, "", 0))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Socket binds before the child finishes its handshake, so poll tools/list
	// until the child is initialized and answering.
	resp := pollCall(t, sock, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, 3*time.Second)
	if _, ok := resp["result"]; !ok {
		t.Fatalf("expected result after rebinding stale socket, got %v", resp)
	}
}

// TestLargePayload proves a payload well beyond bufio's 64KB default round-trips
// through both scanners intact (the proxy sets 16MB buffers on each side).
func TestLargePayload(t *testing.T) {
	sock := startProxy(t, buildFakeServer(t))

	big := strings.Repeat("a", 2*1024*1024) // 2MB, far above the 64KB default
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": "echo", "arguments": map[string]any{"text": big}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := call(t, sock, string(req))
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp["result"], &result); err != nil {
		t.Fatalf("parsing large result: %v", err)
	}
	if len(result.Content) != 1 || len(result.Content[0].Text) != len(big) {
		got := 0
		if len(result.Content) == 1 {
			got = len(result.Content[0].Text)
		}
		t.Errorf("large payload not round-tripped: got %d bytes want %d", got, len(big))
	}
}
