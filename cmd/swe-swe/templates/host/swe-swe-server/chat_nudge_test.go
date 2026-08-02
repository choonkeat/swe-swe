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
		if tool != "agent_waiting" {
			t.Errorf("unexpected orchestrator tool %q", tool)
		}
		return `{"waiting":false}`, nil
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
