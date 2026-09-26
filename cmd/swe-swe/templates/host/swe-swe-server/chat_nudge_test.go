package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// nudgeSession returns a Session whose PTY is a pipe, plus a func that reads
// everything written to it. A real agent's terminal is the only place a nudge
// can land, so the pipe is what proves it was typed.
func nudgeSession(t *testing.T) (*Session, func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { w.Close(); r.Close() })

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	sess := &Session{UUID: "sess-1", AgentChatPort: 4001, PTY: w}
	return sess, func() string {
		// Give the nudge goroutine room to run, then close the write end so
		// the reader returns whatever was actually typed.
		time.Sleep(chatNudgeDelay + 400*time.Millisecond)
		w.Close()
		return <-done
	}
}

// A message pushed with no agent parked would sit unread forever, because the
// wake-up is normally typed by the browser and an MCP caller has no browser.
func TestWakeAgentForQueuedChatTypesNudgeWhenNobodyIsWaiting(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		switch tool {
		case "agent_waiting":
			return `{"waiting":false}`, nil
		case "get_chat_history":
			return historyWithUnreadMessage, nil
		}
		t.Errorf("unexpected orchestrator tool %q", tool)
		return "", nil
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	got := collect()
	if !strings.Contains(got, chatNudgeText) {
		t.Errorf("nudge text was not typed into the terminal, got %q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Errorf("nudge was not submitted with Enter, got %q", got)
	}
}

// An agent parked in send_message receives the message through that call. A
// nudge here would type stray text into a session that is already handling it.
func TestWakeAgentForQueuedChatStaysQuietWhenAgentIsParked(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(int, string, any) (string, error) {
		return `{"waiting":true}`, nil
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	if got := collect(); got != "" {
		t.Errorf("nudged a parked agent, typed %q", got)
	}
}

const historyWithUnreadMessage = `[` +
	`{"type":"userMessage","seq":1,"id":"u1","text":"hi"},` +
	`{"type":"userMessagesConsumed","seq":2,"ids":["u1"]},` +
	`{"type":"userMessage","seq":3,"id":"u2","text":"run /ck:run-marp"}]`

// The smoke-test failure: a message pushed while the agent was parked is handed
// to it at once, so 4s later the agent is busy working on it and no longer
// parked. Typing check_messages then made pi drop the task to report "No
// pending chat messages". What matters is whether the message is still unread.
func TestWakeAgentForQueuedChatStaysQuietWhenMessageWasAlreadyPickedUp(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		if tool == "get_chat_history" {
			return `[` +
				`{"type":"userMessage","seq":1,"id":"u1","text":"run /ck:run-marp"},` +
				`{"type":"userMessagesConsumed","seq":2,"ids":["u1"]},` +
				`{"type":"agentMessage","seq":3,"text":"on it"}]`, nil
		}
		return `{"waiting":false}`, nil
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	if got := collect(); got != "" {
		t.Errorf("nudged an agent already working on the message, typed %q", got)
	}
}

// A message the user took back is not waiting for anyone either.
func TestWakeAgentForQueuedChatIgnoresDeletedMessages(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		if tool == "get_chat_history" {
			return `[{"type":"userMessage","seq":1,"id":"u1","text":"oops"},` +
				`{"type":"userMessageDeleted","seq":2,"id":"u1"}]`, nil
		}
		return `{"waiting":false}`, nil
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	if got := collect(); got != "" {
		t.Errorf("nudged for a withdrawn message, typed %q", got)
	}
}

// Unreadable history must not silence the nudge, for the same reason as an
// unknown wait state below.
func TestWakeAgentForQueuedChatNudgesWhenHistoryIsUnavailable(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(_ int, tool string, _ any) (string, error) {
		if tool == "get_chat_history" {
			return "", errors.New("connection refused")
		}
		return `{"waiting":false}`, nil
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	if got := collect(); !strings.Contains(got, chatNudgeText) {
		t.Errorf("unavailable history must still nudge, got %q", got)
	}
}

// An orchestrator too old to know agent_waiting must not silence the nudge: a
// redundant check_messages costs nothing, a stranded message has no recovery.
func TestWakeAgentForQueuedChatNudgesWhenWaitStateIsUnknown(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(int, string, any) (string, error) {
		return "", errors.New("unknown tool: agent_waiting")
	})

	sess, collect := nudgeSession(t)
	wakeAgentForQueuedChat(sess)

	if got := collect(); !strings.Contains(got, chatNudgeText) {
		t.Errorf("an unknown wait state must still nudge, got %q", got)
	}
}

// A session with no agent chat has no queue to wake anyone for.
func TestWakeAgentForQueuedChatIgnoresSessionsWithoutAgentChat(t *testing.T) {
	chatNudgeDelay = 10 * time.Millisecond
	t.Cleanup(func() { chatNudgeDelay = 4 * time.Second })

	swapOrchestrator(t, func(int, string, any) (string, error) {
		t.Error("must not query the orchestrator without an agent-chat port")
		return "", nil
	})

	sess, collect := nudgeSession(t)
	sess.AgentChatPort = 0
	wakeAgentForQueuedChat(sess)

	if got := collect(); got != "" {
		t.Errorf("typed %q into a session with no agent chat", got)
	}
}

// agentIsParkedOnChat drives the decision, so garbage from the orchestrator
// must resolve to "nudge" rather than to a silent drop.
func TestAgentIsParkedOnChatTreatsUnparseableRepliesAsNotWaiting(t *testing.T) {
	swapOrchestrator(t, func(int, string, any) (string, error) {
		return "message pushed", nil
	})
	if agentIsParkedOnChat(&Session{UUID: "sess-1", AgentChatPort: 4001}) {
		t.Error("an unparseable reply was read as a parked agent")
	}
}
