package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// POST acknowledges and queues exactly one shutdown reason; non-POST is
// rejected without touching the queue.
func TestServerShutdownAPI(t *testing.T) {
	drain := func() {
		select {
		case <-serverShutdownRequests:
		default:
		}
	}
	drain()
	t.Cleanup(drain)

	rr := httptest.NewRecorder()
	handleServerShutdownAPI(rr, httptest.NewRequest(http.MethodGet, "/api/server/shutdown", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: got %d, want 405", rr.Code)
	}
	select {
	case r := <-serverShutdownRequests:
		t.Fatalf("GET queued a shutdown: %q", r)
	default:
	}

	rr = httptest.NewRecorder()
	handleServerShutdownAPI(rr, httptest.NewRequest(http.MethodPost, "/api/server/shutdown", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: got %d, want 200 (body %q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "shutting_down") {
		t.Errorf("POST body = %q, want shutting_down", rr.Body.String())
	}

	// A second POST while one is pending must not block (buffered send with
	// default) and still acknowledges.
	rr = httptest.NewRecorder()
	handleServerShutdownAPI(rr, httptest.NewRequest(http.MethodPost, "/api/server/shutdown", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("second POST: got %d, want 200", rr.Code)
	}

	select {
	case reason := <-serverShutdownRequests:
		if !strings.Contains(reason, "web UI") {
			t.Errorf("reason = %q, want mention of web UI", reason)
		}
	default:
		t.Fatal("POST did not queue a shutdown request")
	}
	// Exactly one queued despite two POSTs.
	select {
	case r := <-serverShutdownRequests:
		t.Errorf("second shutdown request queued: %q", r)
	default:
	}
}
