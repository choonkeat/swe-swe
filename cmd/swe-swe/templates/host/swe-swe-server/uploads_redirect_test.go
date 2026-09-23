package main

import (
	"net/http/httptest"
	"testing"
)

// TestAgentChatUploadRedirect -- agent-chat hands out "/uploads/<name>" for an
// attachment. In the path form the chat page lives at /proxy/{uuid}/agentchat/,
// so the browser asks THIS server for /uploads/<name> and the image breaks.
// The chat page's own address, sent as the Referer, says which session's
// agent-chat holds the file.
func TestAgentChatUploadRedirect(t *testing.T) {
	const uuid = "upload-redirect-sess"
	registerTestSession(t, uuid, &Session{Assistant: "claude"})

	cases := []struct {
		name    string
		path    string
		referer string
		want    string
	}{
		{"from the chat pane", "/uploads/abc-red.png", "http://box:1977/proxy/" + uuid + "/agentchat/?parent_url=x", "/proxy/" + uuid + "/agentchat/uploads/abc-red.png"},
		{"query survives", "/uploads/abc-red.png?v=1", "http://box:1977/proxy/" + uuid + "/agentchat/", "/proxy/" + uuid + "/agentchat/uploads/abc-red.png?v=1"},
		{"no referer", "/uploads/abc-red.png", "", ""},
		{"referer from another host", "/uploads/abc-red.png", "http://evil:1977/proxy/" + uuid + "/agentchat/", ""},
		{"referer from another pane", "/uploads/abc-red.png", "http://box:1977/proxy/" + uuid + "/preview/", ""},
		{"unknown session", "/uploads/abc-red.png", "http://box:1977/proxy/nope/agentchat/", ""},
		{"not an upload", "/other/abc-red.png", "http://box:1977/proxy/" + uuid + "/agentchat/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://box:1977"+tc.path, nil)
			if tc.referer != "" {
				r.Header.Set("Referer", tc.referer)
			}
			got, ok := agentChatUploadRedirect(r)
			if ok != (tc.want != "") || got != tc.want {
				t.Errorf("agentChatUploadRedirect = (%q, %v), want %q", got, ok, tc.want)
			}
		})
	}
}
