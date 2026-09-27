package main

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func stubChatPush(t *testing.T, fn func(tool string) (string, error)) {
	t.Helper()
	orig, origWait, origEvery := orchestratorCall, chatPushWait, chatPushRetryEvery
	orchestratorCall = func(port int, tool string, args any) (string, error) { return fn(tool) }
	chatPushWait, chatPushRetryEvery = 200*time.Millisecond, time.Millisecond
	t.Cleanup(func() { orchestratorCall, chatPushWait, chatPushRetryEvery = orig, origWait, origEvery })
}

// The shape net/http returns when nothing listens on the agent-chat port yet.
var errChatNotListening = &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}

// A message sent before the new session's agent-chat is listening arrives once
// it is, exactly once.
func TestPushChatMessageWaitsForAgentChat(t *testing.T) {
	calls := 0
	stubChatPush(t, func(tool string) (string, error) {
		calls++
		if tool != "send_chat_message" {
			t.Errorf("tool = %q", tool)
		}
		if calls < 3 {
			return "", errChatNotListening
		}
		return "message pushed", nil
	})
	out, err := pushChatMessage(4001, map[string]string{"text": "hi"})
	if err != nil || out != "message pushed" {
		t.Fatalf("got (%q, %v)", out, err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (two refused, one delivered)", calls)
	}
}

// Any other failure may mean the message was delivered, so it is not retried.
func TestPushChatMessageDoesNotRetryOtherErrors(t *testing.T) {
	calls := 0
	stubChatPush(t, func(string) (string, error) {
		calls++
		return "", errors.New("HTTP 500: boom")
	})
	if _, err := pushChatMessage(4001, nil); err == nil {
		t.Fatal("want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// A chat that never comes up (e.g. agent stuck on a trust screen) gives up
// with an error that says where to look.
func TestPushChatMessageGivesUp(t *testing.T) {
	stubChatPush(t, func(string) (string, error) { return "", errChatNotListening })
	_, err := pushChatMessage(4001, nil)
	if err == nil || !strings.Contains(err.Error(), "get_session_output") {
		t.Fatalf("err = %v", err)
	}
}
