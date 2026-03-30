package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPprofHeapEndpoint(t *testing.T) {
	// pprof registers on http.DefaultServeMux via the blank import
	req := httptest.NewRequest("GET", "/debug/pprof/heap", nil)
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /debug/pprof/heap returned %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Error("expected Content-Type header on pprof response")
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty pprof heap profile")
	}
}
