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
	"sync"
	"testing"
	"time"
)

// fakeServerSrc is a minimal stdio MCP server for tests. It answers initialize,
// tools/list, and tools/call. A tools/call with name "block" sleeps before
// replying (to exercise concurrent multiplexing); anything else echoes its name.
// Params are parsed generically to avoid struct tags (backticks can't nest in
// this raw-string literal).
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
	case "tools/list":
		emit(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"echo\"}]}}\n", id))
	case "tools/call":
		var params map[string]json.RawMessage
		json.Unmarshal(m["params"], &params)
		var name string
		json.Unmarshal(params["name"], &name)
		if name == "block" {
			time.Sleep(400 * time.Millisecond)
		}
		emit(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":%q}]}}\n", id, name))
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

// startProxy launches a proxy fronting the fake server on a temp socket.
func startProxy(t *testing.T, bin string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "mcp.sock")
	cfg := config{
		name:            "fake",
		socketPath:      sock,
		protocolVersion: defaultProtocolVersion,
		maxRestarts:     defaultMaxRestarts,
		command:         []string{bin},
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
