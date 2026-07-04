package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

// Remote Agent View client (Phase 5d). When -agent-view points at a
// swe-swe/browser-backend URL, the server allocates a remote browser per
// session and makes it look local to the agent + UI:
//
//   - CDP: a local reverse proxy on sess.CDPPort forwards to the remote
//     chromium's CDP endpoint, and rewrites the host:port in /json* responses
//     back to localhost:CDPPort so the agent's Playwright MCP
//     (--cdp-endpoint http://localhost:CDPPort) follows webSocketDebuggerUrl
//     through this proxy. The agent host only needs to reach localhost.
//   - VNC: the per-session VNC reverse proxy (main.go) targets
//     sess.RemoteVNCTarget instead of localhost.
//
// Reuse a single shared client; never construct a per-request http.Transport
// (see CLAUDE.md memory-leak rule).
var browserBackendClient = &http.Client{Timeout: 30 * time.Second}

// remoteAllocate is indirected so wiring can be tested without a live backend.
var remoteAllocate = allocateRemoteBrowser

// allocateRemoteBrowser POSTs to <backend>/sessions and returns the allocation.
func allocateRemoteBrowser(backendURL, token, sessionID string) (*allocResponse, error) {
	payload := map[string]string{"sessionId": sessionID}
	// Where chromium-on-the-backend should resolve "localhost" (the agent's
	// dev-server URLs). The backend defaults to this request's source address,
	// which is right unless NAT hides us -- then the operator overrides it.
	if v := os.Getenv("SWE_AGENT_VIEW_LOCALHOST"); v != "" {
		payload["resolveLocalhostTo"] = v
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(backendURL, "/")+"/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := browserBackendClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("browser-backend allocate: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var alloc allocResponse
	if err := json.NewDecoder(resp.Body).Decode(&alloc); err != nil {
		return nil, fmt.Errorf("browser-backend allocate: decode: %w", err)
	}
	return &alloc, nil
}

// freeRemoteBrowser DELETEs <backend>/sessions/{id}. Best-effort: logged, not fatal.
func freeRemoteBrowser(backendURL, token, sessionID string) {
	req, err := http.NewRequest(http.MethodDelete, strings.TrimRight(backendURL, "/")+"/sessions/"+sessionID, nil)
	if err != nil {
		log.Printf("browser-backend free %s: %v", sessionID, err)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := browserBackendClient.Do(req)
	if err != nil {
		log.Printf("browser-backend free %s: %v", sessionID, err)
		return
	}
	resp.Body.Close()
}

// remoteHostFor returns the host clients should dial for the remote browser's
// CDP/VNC ports: the advertised host if the backend gave one, else the host
// from the backend URL.
func remoteHostFor(backendURL, advertised string) string {
	if advertised != "" {
		return advertised
	}
	if u, err := url.Parse(backendURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "localhost"
}

// startRemoteAgentView allocates a remote browser and wires the session's CDP +
// VNC to it. Matches the startSessionAgentView dispatch signature.
func startRemoteAgentView(sess *Session) (string, error) {
	alloc, err := remoteAllocate(agentViewBackend, browserBackendToken, sess.UUID)
	if err != nil {
		return "", err
	}
	host := remoteHostFor(agentViewBackend, alloc.Host)
	if err := wireRemoteSession(sess, host, alloc.CDPPort, alloc.VNCPort, alloc.SessionID); err != nil {
		freeRemoteBrowser(agentViewBackend, browserBackendToken, alloc.SessionID)
		return "", err
	}
	log.Printf("Agent View remote: session %s -> %s (cdp %d, vnc %d)", sess.UUID, host, alloc.CDPPort, alloc.VNCPort)
	return "started", nil
}

// wireRemoteSession records the remote VNC target and starts the local CDP
// reverse proxy on sess.CDPPort. Split out so it can be tested without a real
// backend allocation.
func wireRemoteSession(sess *Session, host string, cdpPort, vncPort int, remoteID string) error {
	sess.RemoteBrowserID = remoteID
	sess.RemoteVNCTarget = fmt.Sprintf("%s:%d", host, vncPort)

	remoteCDP := fmt.Sprintf("%s:%d", host, cdpPort)
	localCDP := fmt.Sprintf("localhost:%d", sess.CDPPort)
	target := &url.URL{Scheme: "http", Host: remoteCDP}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = remoteCDP
		// Chromium fills webSocketDebuggerUrl from the request Host, so send
		// the remote host; we rewrite it back to localhost in ModifyResponse.
		req.Host = remoteCDP
	}
	// Rewrite the CDP discovery JSON (/json, /json/version, /json/list) so the
	// debugger URLs point back through this local proxy.
	proxy.ModifyResponse = func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			return nil
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		b = rewriteCDPHosts(b, remoteCDP, localCDP)
		resp.Body = io.NopCloser(bytes.NewReader(b))
		resp.ContentLength = int64(len(b))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(b)))
		return nil
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sess.CDPPort))
	if err != nil {
		return fmt.Errorf("remote CDP proxy listen on %d: %w", sess.CDPPort, err)
	}
	srv := &http.Server{Handler: proxy}
	sess.RemoteCDPProxyServer = srv
	sess.BrowserStarted = true
	go func() {
		defer recoverGoroutine(fmt.Sprintf("remote CDP proxy for session %s", sess.UUID))
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("Session %s: remote CDP proxy error: %v", sess.UUID, err)
		}
	}()
	return nil
}

// rewriteCDPHosts replaces the remote chromium host:port with the local proxy
// host:port in CDP discovery JSON, covering the ws:// and http:// forms plus a
// 127.0.0.1 variant chromium sometimes emits.
func rewriteCDPHosts(body []byte, remoteHostPort, localHostPort string) []byte {
	s := string(body)
	s = strings.ReplaceAll(s, remoteHostPort, localHostPort)
	// chromium may report 127.0.0.1:<port> regardless of the Host header.
	if _, port, err := net.SplitHostPort(remoteHostPort); err == nil {
		s = strings.ReplaceAll(s, "127.0.0.1:"+port, localHostPort)
	}
	return []byte(s)
}

// stopRemoteAgentView shuts the local CDP proxy and frees the remote allocation.
func stopRemoteAgentView(sess *Session) {
	if sess.RemoteCDPProxyServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		sess.RemoteCDPProxyServer.Shutdown(ctx)
		cancel()
		sess.RemoteCDPProxyServer = nil
	}
	if sess.RemoteBrowserID != "" {
		freeRemoteBrowser(agentViewBackend, browserBackendToken, sess.RemoteBrowserID)
		sess.RemoteBrowserID = ""
	}
	sess.RemoteVNCTarget = ""
	sess.BrowserStarted = false
}
