package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
)

// TestRpcRelaysNotificationThenReturns simulates the proxy: it sends an interim
// notifications/message frame BEFORE the real response. rpc must relay the
// notification text to stderr and return the response result, not mistake the
// notification for the result.
func TestRpcRelaysNotificationThenReturns(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	// Redirect os.Stderr to capture the relayed notification.
	origErr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr
	defer func() { os.Stderr = origErr }()

	go func() {
		br := bufio.NewReader(server)
		_, _ = br.ReadString('\n') // consume the request
		// interim notification (no id), then the real response (id 1)
		server.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"warning","data":"<mcp>BLOCKING: waiting for reply</mcp>"}}` + "\n"))
		server.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n"))
		server.Close()
	}()

	res, err := rpc(client, "tools/call", map[string]any{"name": "send_message"})
	wErr.Close()
	os.Stderr = origErr
	if err != nil {
		t.Fatalf("rpc error: %v", err)
	}
	var got map[string]bool
	if e := json.Unmarshal(res, &got); e != nil || !got["ok"] {
		t.Fatalf("want result {ok:true}, got %s (err %v)", res, e)
	}
	stderrBytes, _ := io.ReadAll(rErr)
	if !strings.Contains(string(stderrBytes), "BLOCKING: waiting for reply") {
		t.Fatalf("notification not relayed to stderr, got: %q", string(stderrBytes))
	}
}
