package main

// Cross-binary integration: the real `mcp` client CLI talking to the real
// `mcp-cli-proxy` daemon (separate processes) over a unix socket, in front of
// the fake stdio MCP child. This is the end-to-end proof that the proxy's
// internal id-remapping interoperates with the client's fixed request id, that
// blocking tools work with no client-side timeout, and that a dying child
// surfaces as a clean non-zero exit rather than a hang.

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goBuild compiles pkg (a package path relative to this test's dir) to out.
func goBuild(t *testing.T, out, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s failed: %v\n%s", pkg, err, o)
	}
}

// runMCP invokes the built mcp client with SWE_MCP_DIR pointed at sockDir. It
// returns stdout (the tool result / rendered help) on success; stderr carries
// side-channel notices (the -h tip, blocking-call warnings) that must not
// pollute result assertions, so it is folded in only on error, for diagnostics.
func runMCP(bin, sockDir string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "SWE_MCP_DIR="+sockDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}

func TestIntegrationRealBinaries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration build in -short mode")
	}
	childBin := buildFakeServer(t)

	binDir := t.TempDir()
	proxyBin := filepath.Join(binDir, "mcp-cli-proxy")
	mcpBin := filepath.Join(binDir, "mcp")
	goBuild(t, proxyBin, ".")    // this package
	goBuild(t, mcpBin, "../mcp") // the client CLI

	sockDir := t.TempDir()
	sock := filepath.Join(sockDir, "svc.sock")
	proxy := exec.Command(proxyBin, "--name", "svc", "--socket", sock, "--", childBin)
	proxy.Stderr = os.Stderr
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Process.Kill()

	// Wait for the proxy socket to accept connections.
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("proxy socket never became ready")
	}

	t.Run("discovers server and tools", func(t *testing.T) {
		out, err := runMCP(mcpBin, sockDir)
		if err != nil {
			t.Fatalf("mcp (top help): %v\n%s", err, out)
		}
		if !strings.Contains(out, "svc") {
			t.Errorf("top help did not list server svc: %q", out)
		}
		out, err = runMCP(mcpBin, sockDir, "svc")
		if err != nil {
			t.Fatalf("mcp svc: %v\n%s", err, out)
		}
		if !strings.Contains(out, "echo") {
			t.Errorf("server help did not list echo tool: %q", out)
		}
	})

	t.Run("calls a tool through both binaries", func(t *testing.T) {
		out, err := runMCP(mcpBin, sockDir, "svc", "echo", "--text", "hi there")
		if err != nil {
			t.Fatalf("mcp svc echo: %v\n%s", err, out)
		}
		if strings.TrimSpace(out) != "hi there" {
			t.Errorf("echo returned %q", out)
		}
	})

	t.Run("blocking call does not stall a concurrent fast call", func(t *testing.T) {
		go runMCP(mcpBin, sockDir, "svc", "block") // ~400ms in the child
		time.Sleep(80 * time.Millisecond)          // ensure the block call is in flight
		t0 := time.Now()
		out, err := runMCP(mcpBin, sockDir, "svc", "echo", "--text", "quick")
		if err != nil {
			t.Fatalf("mcp svc echo (fast): %v\n%s", err, out)
		}
		if d := time.Since(t0); d > 300*time.Millisecond {
			t.Errorf("fast call was head-of-line blocked by the slow call: %v", d)
		}
		if strings.TrimSpace(out) != "quick" {
			t.Errorf("fast call returned %q", out)
		}
	})

	t.Run("dying child surfaces as non-zero exit, not a hang", func(t *testing.T) {
		done := make(chan error, 1)
		go func() {
			_, err := runMCP(mcpBin, sockDir, "svc", "crash")
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Error("expected non-zero exit when the child crashes mid-call")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("mcp hung after the child crashed instead of erroring out")
		}
	})
}
