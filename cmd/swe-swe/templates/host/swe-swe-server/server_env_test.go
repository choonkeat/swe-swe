package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetServerEnv(t *testing.T) {
	t.Helper()
	prev := getServerEnvRaw()
	setServerEnv("")
	t.Cleanup(func() { setServerEnv(prev) })
}

// Server-wide vars reach sessions, reserved keys and the whole SWE_TUNNEL_*
// namespace do not, and $VAR expands against the session env being built.
func TestServerEnvVars_DropsReservedAndTunnelKeys(t *testing.T) {
	resetServerEnv(t)
	setServerEnv("FOO=bar\nPATH=/evil\nSWE_TUNNEL_IDENTITY_KEY=secret\nSWE_TUNNEL_UNIQUE=x\nDERIVED=$BASE/sub\n")
	lookup := func(k string) string {
		if k == "BASE" {
			return "/root"
		}
		return ""
	}
	kept, dropped := serverEnvVars(lookup)
	joined := strings.Join(kept, "\n")
	for _, want := range []string{"FOO=bar", "DERIVED=/root/sub"} {
		if !strings.Contains(joined, want) {
			t.Errorf("kept missing %q: %v", want, kept)
		}
	}
	for _, bad := range []string{"PATH=", "SWE_TUNNEL_"} {
		if strings.Contains(joined, bad) {
			t.Errorf("reserved key leaked into kept: %v", kept)
		}
	}
	droppedJoined := strings.Join(dropped, ",")
	for _, want := range []string{"PATH", "SWE_TUNNEL_IDENTITY_KEY", "SWE_TUNNEL_UNIQUE"} {
		if !strings.Contains(droppedJoined, want) {
			t.Errorf("dropped missing %q: %v", want, dropped)
		}
	}
}

// A blank blob clears the store; unset yields nil,nil.
func TestServerEnv_BlankClears(t *testing.T) {
	resetServerEnv(t)
	setServerEnv("A=1")
	if serverEnvCount() != 1 {
		t.Fatalf("count = %d, want 1", serverEnvCount())
	}
	setServerEnv("  \n\t")
	if kept, dropped := serverEnvVars(func(string) string { return "" }); kept != nil || dropped != nil {
		t.Errorf("blank blob should clear: kept=%v dropped=%v", kept, dropped)
	}
}

// The API never returns values: GET reports a count, POST stores and reports
// the count plus which keys were ignored.
func TestHandleServerEnvAPI(t *testing.T) {
	resetServerEnv(t)

	rr := httptest.NewRecorder()
	handleServerEnvAPI(rr, httptest.NewRequest(http.MethodGet, "/api/server/env", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"count":0`) {
		t.Fatalf("GET empty: %d %s", rr.Code, rr.Body.String())
	}

	body := strings.NewReader(`{"raw":"TOKEN=abc\nSWE_TUNNEL_UNIQUE=nope\n"}`)
	rr = httptest.NewRecorder()
	handleServerEnvAPI(rr, httptest.NewRequest(http.MethodPost, "/api/server/env", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Count   int      `json:"count"`
		Dropped []string `json:"dropped"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 1 || len(resp.Dropped) != 1 || resp.Dropped[0] != "SWE_TUNNEL_UNIQUE" {
		t.Errorf("POST resp = %+v, want count 1 dropped [SWE_TUNNEL_UNIQUE]", resp)
	}
	if strings.Contains(rr.Body.String(), "abc") {
		t.Error("response leaked a value")
	}

	rr = httptest.NewRecorder()
	handleServerEnvAPI(rr, httptest.NewRequest(http.MethodPost, "/api/server/env", strings.NewReader("not json")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	handleServerEnvAPI(rr, httptest.NewRequest(http.MethodDelete, "/api/server/env", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: %d", rr.Code)
	}
}

// Shared-session guests are denied both homepage-settings endpoints.
func TestScopedPathDeniesServerSettings(t *testing.T) {
	for _, p := range []string{"/api/server/tunnel", "/api/server/env"} {
		if scopedPathAllowed("some-uuid", p) {
			t.Errorf("guest must be denied %s", p)
		}
	}
}
