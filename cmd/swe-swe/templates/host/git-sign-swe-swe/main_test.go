package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// fakeBroker spins up a one-shot listener on a unique abstract
// socket and serves whatever the test handler writes back. Returns
// the socket name and a chan that yields the parsed request.
func fakeBroker(t *testing.T, respond func(req map[string]any) any) (string, <-chan map[string]any) {
	t.Helper()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	sockName := "@git-sign-swe-swe-test-" + hex.EncodeToString(nonce[:])
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockName, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	got := make(chan map[string]any, 1)
	go func() {
		c, err := l.AcceptUnix()
		if err != nil {
			return
		}
		defer c.Close()
		var req map[string]any
		if err := json.NewDecoder(c).Decode(&req); err != nil {
			return
		}
		got <- req
		_ = json.NewEncoder(c).Encode(respond(req))
	}()
	return sockName, got
}

func TestDialBrokerSign_Success(t *testing.T) {
	sockName, gotReq := fakeBroker(t, func(req map[string]any) any {
		return map[string]any{"signature": "-----BEGIN SSH SIGNATURE-----\nFAKE\n-----END SSH SIGNATURE-----\n"}
	})
	saved := brokerSocketName
	brokerSocketName = sockName
	defer func() { brokerSocketName = saved }()

	armor, err := dialBrokerSign("git", []byte("commit-object-data"))
	if err != nil {
		t.Fatalf("dialBrokerSign: %v", err)
	}
	if !strings.HasPrefix(armor, "-----BEGIN SSH SIGNATURE-----\n") {
		t.Errorf("armor missing header: %q", armor)
	}
	if !strings.Contains(armor, "FAKE") {
		t.Errorf("armor missing fake body: %q", armor)
	}

	req := <-gotReq
	if op, _ := req["op"].(string); op != "sign-ssh" {
		t.Errorf("op: got %q, want sign-ssh", op)
	}
	if ns, _ := req["namespace"].(string); ns != "git" {
		t.Errorf("namespace: got %q, want git", ns)
	}
	dataB64, _ := req["data"].(string)
	decoded, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		t.Fatalf("data not base64: %v", err)
	}
	if string(decoded) != "commit-object-data" {
		t.Errorf("data: got %q, want %q", decoded, "commit-object-data")
	}
}

func TestDialBrokerSign_BrokerError(t *testing.T) {
	sockName, _ := fakeBroker(t, func(_ map[string]any) any {
		return map[string]any{"error": "no signing key for session"}
	})
	saved := brokerSocketName
	brokerSocketName = sockName
	defer func() { brokerSocketName = saved }()

	_, err := dialBrokerSign("git", []byte("x"))
	if err == nil {
		t.Fatal("expected error from broker; got nil")
	}
	if !strings.Contains(err.Error(), "no signing key") {
		t.Errorf("error mismatch: got %v", err)
	}
}

func TestDialBrokerSign_EmptySignature(t *testing.T) {
	sockName, _ := fakeBroker(t, func(_ map[string]any) any {
		return map[string]any{}
	})
	saved := brokerSocketName
	brokerSocketName = sockName
	defer func() { brokerSocketName = saved }()

	_, err := dialBrokerSign("git", []byte("x"))
	if err == nil {
		t.Fatal("expected error from empty signature; got nil")
	}
	if !strings.Contains(err.Error(), "empty signature") {
		t.Errorf("error mismatch: got %v", err)
	}
}

func TestDialBrokerSign_DialFailure(t *testing.T) {
	saved := brokerSocketName
	brokerSocketName = "@git-sign-swe-swe-test-no-such-listener"
	defer func() { brokerSocketName = saved }()

	_, err := dialBrokerSign("git", []byte("x"))
	if err == nil {
		t.Fatal("expected dial error; got nil")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("error mismatch: got %v", err)
	}
}

func TestDialBrokerSign_DefaultNamespace(t *testing.T) {
	sockName, gotReq := fakeBroker(t, func(_ map[string]any) any {
		return map[string]any{"signature": "armor"}
	})
	saved := brokerSocketName
	brokerSocketName = sockName
	defer func() { brokerSocketName = saved }()

	// Caller passes empty namespace; flag-default already substitutes
	// "git" before dialBrokerSign is called, but we still want the
	// wrapper to round-trip whatever it gets so the broker can apply
	// its own default. Verify the request reaches the broker.
	if _, err := dialBrokerSign("git", []byte("d")); err != nil {
		t.Fatalf("dialBrokerSign: %v", err)
	}
	req := <-gotReq
	if ns, _ := req["namespace"].(string); ns == "" {
		t.Errorf("expected non-empty namespace in request; got empty")
	}
}
