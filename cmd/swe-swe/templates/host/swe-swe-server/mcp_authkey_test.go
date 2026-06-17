package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testAPIKey is the deterministic per-session key reused by the handler
// auth tests in this package.
const testAPIKey = "test-api-key"

// registerTestSessionKey wires key->sid directly (bypassing random
// generation) so handler auth tests can use deterministic keys, removing
// the mapping when the test finishes.
func registerTestSessionKey(t *testing.T, sid, key string) {
	t.Helper()
	mcpKeyMu.Lock()
	mcpKeyToSession[key] = sid
	mcpSessionToKey[sid] = key
	mcpKeyMu.Unlock()
	t.Cleanup(func() {
		mcpKeyMu.Lock()
		delete(mcpKeyToSession, key)
		delete(mcpSessionToKey, sid)
		mcpKeyMu.Unlock()
	})
}

func TestIssueAndLookupSessionKey(t *testing.T) {
	sid := "sess-A"
	k1 := issueSessionKey(sid)
	if k1 == "" {
		t.Fatal("issueSessionKey returned empty key")
	}
	defer clearSessionKey(sid)

	// Idempotent per sid: a session recreated under the same UUID keeps
	// a stable key.
	if k2 := issueSessionKey(sid); k2 != k1 {
		t.Fatalf("issueSessionKey not stable: %q vs %q", k1, k2)
	}

	// Reverse lookup resolves the owning session.
	got, ok := sessionForKey(k1)
	if !ok || got != sid {
		t.Fatalf("sessionForKey(%q) = %q,%v; want %q,true", k1, got, ok, sid)
	}

	// Distinct sessions get distinct keys.
	other := issueSessionKey("sess-B")
	defer clearSessionKey("sess-B")
	if other == k1 {
		t.Fatal("distinct sessions got the same key")
	}

	// Unknown and empty keys are rejected.
	if _, ok := sessionForKey("nope"); ok {
		t.Fatal("unknown key accepted")
	}
	if _, ok := sessionForKey(""); ok {
		t.Fatal("empty key accepted")
	}
	if issueSessionKey("") != "" {
		t.Fatal("issueSessionKey(\"\") should return empty")
	}

	// Clearing removes both directions.
	clearSessionKey(sid)
	if _, ok := sessionForKey(k1); ok {
		t.Fatal("key still valid after clearSessionKey")
	}
}

func TestMCPAuthMiddleware(t *testing.T) {
	sid := "caller-123"
	key := issueSessionKey(sid)
	defer clearSessionKey(sid)

	var seen string
	var ran bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = callerSessionFromContext(r.Context())
		ran = true
		w.WriteHeader(http.StatusOK)
	})
	mw := mcpAuthMiddleware(next)

	t.Run("valid key injects caller session", func(t *testing.T) {
		seen, ran = "", false
		req := httptest.NewRequest(http.MethodPost, "/mcp?key="+key, nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !ran || seen != sid {
			t.Fatalf("caller session not injected: ran=%v seen=%q", ran, seen)
		}
	})

	t.Run("unknown key is rejected and downstream never runs", func(t *testing.T) {
		ran = false
		req := httptest.NewRequest(http.MethodPost, "/mcp?key=bogus", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		if ran {
			t.Fatal("downstream handler ran for an unknown key")
		}
	})

	t.Run("missing key is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

func TestCallerSessionFromContextEmpty(t *testing.T) {
	if got := callerSessionFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty caller session, got %q", got)
	}
}

func TestSessionKeyMatchesPath(t *testing.T) {
	sid := "owner-xyz"
	key := issueSessionKey(sid)
	defer clearSessionKey(sid)

	mk := func(uuid, k string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/api/session/"+uuid+"/x?key="+k, nil)
	}

	if !sessionKeyMatchesPath(mk(sid, key), sid) {
		t.Error("a session's own key should authorize its own path")
	}
	// The crux: session A's key must NOT authorize session B's path.
	if sessionKeyMatchesPath(mk("other-sid", key), "other-sid") {
		t.Error("a session's key must not authorize another session's path")
	}
	if sessionKeyMatchesPath(mk(sid, "wrong"), sid) {
		t.Error("a wrong key must be rejected")
	}
	if sessionKeyMatchesPath(mk(sid, ""), sid) {
		t.Error("an empty key must be rejected")
	}
}
