package main

import (
	"net/http"
	"net/url"
	"strings"
)

// agentChatUploadRedirect returns where a "/uploads/..." request really
// belongs when it came from an Agent Chat pane served in the path form.
//
// agent-chat hands out root-anchored "/uploads/<name>" addresses for
// attachments. That is right when agent-chat owns the whole origin (its own
// port), but at /proxy/{uuid}/agentchat/ the browser sends the request here,
// where no such file exists, and the image in the chat bubble breaks. The chat
// page's address arrives as the Referer, and names the session whose
// agent-chat holds the file. The redirect target is an ordinary /proxy/ path,
// so it goes through the same auth and share-scope checks as the pane itself.
func agentChatUploadRedirect(r *http.Request) (string, bool) {
	if !strings.HasPrefix(r.URL.Path, "/uploads/") {
		return "", false
	}
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host == "" || ref.Host != r.Host {
		return "", false
	}
	rest, ok := strings.CutPrefix(ref.Path, "/proxy/")
	if !ok {
		return "", false
	}
	uuid, pane, ok := strings.Cut(rest, "/")
	if !ok || uuid == "" || !strings.HasPrefix(pane, "agentchat/") {
		return "", false
	}
	sessionsMu.RLock()
	_, exists := sessions[uuid]
	sessionsMu.RUnlock()
	if !exists {
		return "", false
	}
	target := "/proxy/" + uuid + "/agentchat" + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	return target, true
}
