package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectDebugScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "inject after <head>",
			input:    `<!DOCTYPE html><html><head><title>Test</title></head><body></body></html>`,
			expected: `<!DOCTYPE html><html><head><script src="/__swe-swe-debug__/inject.js"></script><title>Test</title></head><body></body></html>`,
		},
		{
			name:     "inject after <head> with attributes",
			input:    `<html><head lang="en"><title>Test</title></head></html>`,
			expected: `<html><head lang="en"><script src="/__swe-swe-debug__/inject.js"></script><title>Test</title></head></html>`,
		},
		{
			name:     "inject after <body> if no head",
			input:    `<!DOCTYPE html><html><body><p>Hello</p></body></html>`,
			expected: `<!DOCTYPE html><html><body><script src="/__swe-swe-debug__/inject.js"></script><p>Hello</p></body></html>`,
		},
		{
			name:     "case insensitive HEAD",
			input:    `<HTML><HEAD><TITLE>Test</TITLE></HEAD></HTML>`,
			expected: `<HTML><HEAD><script src="/__swe-swe-debug__/inject.js"></script><TITLE>Test</TITLE></HEAD></HTML>`,
		},
		{
			name:     "case insensitive BODY",
			input:    `<HTML><BODY><P>Hello</P></BODY></HTML>`,
			expected: `<HTML><BODY><script src="/__swe-swe-debug__/inject.js"></script><P>Hello</P></BODY></HTML>`,
		},
		{
			name:     "mixed case hEaD",
			input:    `<html><hEaD><title>Test</title></hEaD></html>`,
			expected: `<html><hEaD><script src="/__swe-swe-debug__/inject.js"></script><title>Test</title></hEaD></html>`,
		},
		{
			name:     "no head or body - unchanged",
			input:    `<html><div>content</div></html>`,
			expected: `<html><div>content</div></html>`,
		},
		{
			name:     "head comes before body - only first injected",
			input:    `<head></head><body></body>`,
			expected: `<head><script src="/__swe-swe-debug__/inject.js"></script></head><body></body>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectDebugScript([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("injectDebugScript(%q)\ngot:  %q\nwant: %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestModifyCSPHeader(t *testing.T) {
	tests := []struct {
		name     string
		csp      string
		expected string
	}{
		{
			name:     "empty CSP unchanged",
			csp:      "",
			expected: "",
		},
		{
			name:     "adds to existing script-src and adds connect-src",
			csp:      "script-src 'unsafe-inline'",
			expected: "script-src 'self' 'unsafe-inline'; connect-src ws: wss:",
		},
		{
			name:     "adds script-src and connect-src if both missing",
			csp:      "default-src 'self'",
			expected: "default-src 'self'; script-src 'self'; connect-src ws: wss:",
		},
		{
			name:     "adds to existing connect-src and adds script-src",
			csp:      "connect-src https://api.example.com",
			expected: "connect-src ws: wss: https://api.example.com; script-src 'self'",
		},
		{
			name:     "modifies both existing script-src and connect-src",
			csp:      "script-src 'self'; connect-src https://api.example.com",
			expected: "script-src 'self' 'self'; connect-src ws: wss: https://api.example.com",
		},
		{
			name:     "handles full CSP with all directives",
			csp:      "default-src 'self'; script-src 'unsafe-inline'; connect-src https://api.example.com",
			expected: "default-src 'self'; script-src 'self' 'unsafe-inline'; connect-src ws: wss: https://api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.csp != "" {
				h.Set("Content-Security-Policy", tt.csp)
			}
			modifyCSPHeader(h)
			result := h.Get("Content-Security-Policy")
			if result != tt.expected {
				t.Errorf("modifyCSPHeader with CSP %q\ngot:  %q\nwant: %q", tt.csp, result, tt.expected)
			}
		})
	}
}

func TestDebugInjectJSEndpoint(t *testing.T) {
	// Create a simple handler to test the inject.js endpoint
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__swe-swe-debug__/inject.js" {
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(debugInjectJS))
			return
		}
		http.NotFound(w, r)
	})

	req := httptest.NewRequest("GET", "/__swe-swe-debug__/inject.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("expected Content-Type application/javascript, got %q", ct)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "swe-swe-debug") {
		t.Errorf("expected body to contain 'swe-swe-debug', got %q", body)
	}
}

// TestProxyIntegration tests the full proxy with a mock upstream server
func TestProxyHTMLInjection(t *testing.T) {
	// Create mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<!DOCTYPE html><html><head><title>Test</title></head><body>Hello</body></html>`))
		case "/html-no-head":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!DOCTYPE html><html><body>Hello</body></html>`))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok"}`))
		case "/css":
			w.Header().Set("Content-Type", "text/css")
			w.Write([]byte(`body { color: red; }`))
		case "/gzip-html":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "gzip")
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			gz.Write([]byte(`<!DOCTYPE html><html><head><title>Gzipped</title></head><body>Compressed</body></html>`))
			gz.Close()
			w.Write(buf.Bytes())
		case "/csp":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Security-Policy", "script-src 'unsafe-inline'")
			w.Write([]byte(`<html><head></head></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	// The actual proxy test would require starting the proxy server
	// For unit testing, we test the components directly above
	// Here we document what a full integration test would verify:
	t.Run("documents integration test expectations", func(t *testing.T) {
		// Integration tests would verify:
		// 1. HTML responses get script injected
		// 2. Non-HTML responses pass through unchanged
		// 3. Gzip HTML is decompressed, injected, and served uncompressed
		// 4. CSP headers are modified
		// 5. inject.js endpoint serves the placeholder script

		// For now, we test the helper functions directly
		// Full integration testing will be in Phase 4
	})
}

// TestGzipDecompression tests that gzip content is properly decompressed
func TestGzipDecompression(t *testing.T) {
	original := `<!DOCTYPE html><html><head><title>Test</title></head><body>Hello</body></html>`

	// Compress the HTML
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte(original))
	gz.Close()

	// Decompress it (simulating what ModifyResponse does)
	gr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	gr.Close()
	if err != nil {
		t.Fatalf("failed to read gzip: %v", err)
	}

	if string(decompressed) != original {
		t.Errorf("gzip roundtrip failed\ngot:  %q\nwant: %q", decompressed, original)
	}

	// Now inject
	injected := injectDebugScript(decompressed)
	if !strings.Contains(string(injected), debugScriptTag) {
		t.Errorf("injection into decompressed content failed")
	}
}
