package main

// session_address.go -- cross-box session addresses.
//
// Every session is addressable as <unique>/<uuid>. unique is this box's
// tunnel name (SWE_TUNNEL_UNIQUE, the same label it registers with tunneld).
// A bare <uuid> means "this box". Every session-addressed MCP tool resolves
// its target through resolveLocalSession, the ONE branch that decides local
// (in-memory sessions) versus foreign (relay in a later phase; until then,
// unreachable). Local is the degenerate cluster-of-one.
//
// See tasks/2026-09-18-cross-box-session-comms.md.

import (
	"errors"
	"fmt"
	"strings"
)

// sessionAddr is a parsed <unique>/<uuid> or bare <uuid>.
type sessionAddr struct {
	Unique string
	UUID   string
}

// parseSessionAddr accepts "<uuid>" or "<unique>/<uuid>".
func parseSessionAddr(s string) (sessionAddr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return sessionAddr{}, errors.New("uuid is required")
	}
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return sessionAddr{UUID: s}, nil
	}
	unique, id := s[:i], s[i+1:]
	if unique == "" || id == "" || strings.ContainsAny(id, "/ \t") || strings.ContainsAny(unique, " \t.") {
		return sessionAddr{}, fmt.Errorf("invalid session address %q: want <unique>/<uuid> or <uuid>", s)
	}
	return sessionAddr{Unique: unique, UUID: id}, nil
}

// String renders the address; a box with no unique renders the bare uuid.
func (a sessionAddr) String() string {
	if a.Unique == "" {
		return a.UUID
	}
	return a.Unique + "/" + a.UUID
}

// isLocal reports whether the address names a session on this box. A bare
// uuid is always local; a unique matching ours is local too.
func (a sessionAddr) isLocal() bool {
	return a.Unique == "" || a.Unique == localUnique()
}

// localUnique returns this box's tunnel unique, or "" when no tunnel is
// configured. Read from the live runtime so a unique set later from the
// Settings dialog is honoured without a restart.
func localUnique() string {
	rt := liveTunnelRuntime
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.unique != "" {
		return rt.unique
	}
	return rt.base.Unique
}

// sessionAddress renders a local session's fully qualified address.
func sessionAddress(uuid string) string {
	return sessionAddr{Unique: localUnique(), UUID: uuid}.String()
}

// resolveLocalSession is the one branch every session-addressed tool goes
// through. It returns the uuid to look up in the in-memory session table, or
// an error when the address names another box. Relaying to another box is a
// later phase; until then a foreign unique is unreachable by definition.
func resolveLocalSession(raw string) (string, error) {
	a, err := parseSessionAddr(raw)
	if err != nil {
		return "", err
	}
	if !a.isLocal() {
		return "", fmt.Errorf("unreachable: %s (no relay configured on this box)", a.Unique)
	}
	return a.UUID, nil
}

// chatSignoff is what the SENDING server appends to every send_chat_message
// payload so the receiving agent knows which session to reply to. It is
// derived from the caller's per-session MCP auth key, never from agent text,
// so a session cannot impersonate another. Empty when the caller is unknown.
func chatSignoff(tool, callerUUID string) string {
	if callerUUID == "" {
		return ""
	}
	return "\n\n(via " + tool + " from " + sessionAddress(callerUUID) + ")"
}

// pushChatMessageFrom sends text to a session's agent-chat as a message from
// callerUUID's session. The sender goes as from_name / from_session fields,
// so the text arrives as written and the receiver's chat can show who sent it.
// An unknown caller is sent unstamped.
//
// An agent-chat that predates those fields rejects the call before delivering
// anything; that exact rejection is retried once with the old text signoff.
func pushChatMessageFrom(port int, text, callerUUID string) (string, error) {
	if callerUUID == "" {
		return pushChatMessage(port, map[string]string{"text": text})
	}
	addr := sessionAddress(callerUUID)
	name := ""
	sessionsMu.RLock()
	if s, ok := sessions[callerUUID]; ok {
		name = s.Name
	}
	sessionsMu.RUnlock()
	if name == "" {
		name = addr
	}
	out, err := pushChatMessage(port, map[string]string{"text": text, "from_name": name, "from_session": addr})
	if err != nil && strings.Contains(err.Error(), "unexpected additional properties") {
		return pushChatMessage(port, map[string]string{"text": text + chatSignoff("send_chat_message", callerUUID)})
	}
	return out, err
}
