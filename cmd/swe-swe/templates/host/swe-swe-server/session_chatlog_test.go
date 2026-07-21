package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// swapOrchestrator stubs the agent-chat orchestrator call so tests can drive
// the chat-log paths without a live agent-chat process.
func swapOrchestrator(t *testing.T, fn func(port int, tool string, args any) (string, error)) *[]string {
	t.Helper()
	var calls []string
	orig := orchestratorCall
	orchestratorCall = func(port int, tool string, args any) (string, error) {
		calls = append(calls, tool)
		return fn(port, tool, args)
	}
	t.Cleanup(func() { orchestratorCall = orig })
	return &calls
}

const statusJSON = `{"enabled":true,"path":"/w/agent-chats/2026-07-21-01-untitled-u.md",` +
	`"dir":"/w/agent-chats","slug":"untitled-u","titled":false,"stopped":false,` +
	`"optedOut":false,"exists":true}`

// A session with no agent-chat port has no chat log to offer -- asking the
// orchestrator would just fail on a dial to port 0.
func TestSessionChatLogNoAgentChatPort(t *testing.T) {
	calls := swapOrchestrator(t, func(int, string, any) (string, error) {
		t.Error("must not call the orchestrator when there is no agent-chat port")
		return "", nil
	})
	info, err := sessionChatLog(&Session{UUID: "x"})
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if info.Enabled {
		t.Error("a session without an agent-chat port reports no chat log")
	}
	if len(*calls) != 0 {
		t.Errorf("expected zero orchestrator calls, got %v", *calls)
	}
}

func TestSessionChatLogParsesStatus(t *testing.T) {
	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		if tool != "chatlog_status" {
			t.Errorf("called %q, want chatlog_status", tool)
		}
		return statusJSON, nil
	})

	info, err := sessionChatLog(&Session{UUID: "x", AgentChatPort: 4001})
	if err != nil {
		t.Fatalf("sessionChatLog: %v", err)
	}
	if !info.Enabled || !info.Exists {
		t.Errorf("want enabled+exists, got %+v", info)
	}
	if info.Titled {
		t.Error("this fixture is untitled")
	}
	if info.Path != "/w/agent-chats/2026-07-21-01-untitled-u.md" {
		t.Errorf("path = %q", info.Path)
	}
}

// A dead or wedged agent-chat must not make ending a session impossible: the
// caller falls back to "no chat log to offer" rather than surfacing an error
// that would block the End button.
func TestSessionChatLogOrchestratorFailureIsNotFatal(t *testing.T) {
	swapOrchestrator(t, func(int, string, any) (string, error) {
		return "", errors.New("connection refused")
	})
	info, err := sessionChatLog(&Session{UUID: "x", AgentChatPort: 4001})
	if err != nil {
		t.Fatalf("an unreachable orchestrator must not be a hard error, got %v", err)
	}
	if info.Enabled {
		t.Error("an unreachable orchestrator reports no chat log")
	}
}

func TestChatLogAPIServesStatus(t *testing.T) {
	swapSessions(t, map[string]*Session{"s": {UUID: "s", AgentChatPort: 4001}})
	swapOrchestrator(t, func(int, string, any) (string, error) { return statusJSON, nil })

	w := httptest.NewRecorder()
	handleSessionChatLogAPI(w, httptest.NewRequest(http.MethodGet, "/api/session/s/chatlog", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var got chatLogInfo
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled || !got.Exists {
		t.Errorf("want enabled+exists, got %+v", got)
	}
}

// chatlog=discard must delete the log BEFORE teardown starts. Once the agent
// process is killed the orchestrator is gone with it, so a discard attempted
// afterwards would silently leave the file on disk.
func TestEndAPIDiscardsChatLogBeforeTeardown(t *testing.T) {
	swapSessions(t, map[string]*Session{"s": {UUID: "s", AgentChatPort: 4001}})

	order := make(chan string, 4)
	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		order <- tool
		return "chat log discarded", nil
	})
	swapEndTeardown(t, func(string) error {
		order <- "teardown"
		return nil
	})

	w := httptest.NewRecorder()
	handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/s/end?chatlog=discard", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	if got := <-order; got != "chatlog_optout" {
		t.Fatalf("first action was %q, want chatlog_optout", got)
	}
	if got := <-order; got != "teardown" {
		t.Fatalf("second action was %q, want teardown", got)
	}
}

// If the discard itself fails, the session must NOT be torn down: the user
// asked for the log to be gone, and ending anyway would strand the file with
// no agent left to delete it.
func TestEndAPIDiscardFailureAbortsEnd(t *testing.T) {
	sess := &Session{UUID: "s", AgentChatPort: 4001}
	swapSessions(t, map[string]*Session{"s": sess})
	swapOrchestrator(t, func(int, string, any) (string, error) {
		return "", errors.New("orchestrator down")
	})
	swapEndTeardown(t, func(string) error {
		t.Error("teardown must not run when the discard failed")
		return nil
	})

	w := httptest.NewRecorder()
	handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/s/end?chatlog=discard", nil))
	if w.Code == http.StatusAccepted {
		t.Errorf("want a failure status, got %d", w.Code)
	}
	if sess.isEnding() {
		t.Error("the session must not be latched as ending when the discard failed")
	}
}

// chatlog=commit hands the work to the agent: it needs its tools and its
// working tree, so the session must stay alive. The agent ends the session
// itself once the commit lands -- there is nothing here to poll.
func TestEndAPICommitKeepsSessionAlive(t *testing.T) {
	sess := &Session{UUID: "s", AgentChatPort: 4001}
	swapSessions(t, map[string]*Session{"s": sess})

	var sentText string
	swapOrchestrator(t, func(_ int, tool string, args any) (string, error) {
		if tool == "send_chat_message" {
			if m, ok := args.(map[string]any); ok {
				sentText, _ = m["text"].(string)
			}
		}
		return "message pushed", nil
	})
	swapEndTeardown(t, func(string) error {
		t.Error("commit mode must not tear the session down -- the agent still has work to do")
		return nil
	})

	w := httptest.NewRecorder()
	handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/s/end?chatlog=commit", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	if sess.isEnding() {
		t.Error("commit mode must NOT latch the session as ending -- it stays joinable while the agent works")
	}
	if sentText == "" {
		t.Fatal("commit mode must push an instruction into the agent's chat")
	}
	for _, want := range []string{"commit-session-chat-log", "end_session", sess.UUID} {
		if !strings.Contains(sentText, want) {
			t.Errorf("instruction is missing %q; the agent cannot finish the job without it.\ngot: %s", want, sentText)
		}
	}
}

// No chatlog param keeps today's behavior: end, leave the file alone.
func TestEndAPIWithoutChatLogParamJustEnds(t *testing.T) {
	swapSessions(t, map[string]*Session{"s": {UUID: "s", AgentChatPort: 4001}})
	calls := swapOrchestrator(t, func(int, string, any) (string, error) {
		t.Error("plain end must not touch the chat log")
		return "", nil
	})
	done := make(chan struct{}, 1)
	swapEndTeardown(t, func(string) error { done <- struct{}{}; return nil })

	w := httptest.NewRecorder()
	handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/s/end", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	<-done
	if len(*calls) != 0 {
		t.Errorf("expected no orchestrator calls, got %v", *calls)
	}
}
