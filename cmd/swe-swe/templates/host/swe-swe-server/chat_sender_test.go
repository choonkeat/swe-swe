package main

import (
	"errors"
	"testing"
	"time"
)

// stubChatArgs records every send_chat_message call's args and answers each
// from replies (the last reply repeats).
func stubChatArgs(t *testing.T, replies ...error) *[]map[string]string {
	t.Helper()
	var got []map[string]string
	orig, origWait, origEvery := orchestratorCall, chatPushWait, chatPushRetryEvery
	orchestratorCall = func(port int, tool string, args any) (string, error) {
		m, _ := args.(map[string]string)
		got = append(got, m)
		i := len(got) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		if replies[i] != nil {
			return "", replies[i]
		}
		return "message pushed", nil
	}
	chatPushWait, chatPushRetryEvery = 200*time.Millisecond, time.Millisecond
	t.Cleanup(func() { orchestratorCall, chatPushWait, chatPushRetryEvery = orig, origWait, origEvery })
	return &got
}

func withSession(t *testing.T, uuid, name string) {
	t.Helper()
	sessionsMu.Lock()
	sessions[uuid] = &Session{UUID: uuid, Name: name}
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		delete(sessions, uuid)
		sessionsMu.Unlock()
	})
}

// The sender travels as fields; the text is the caller's own words, untouched.
func TestSendChatCarriesSenderAsFields(t *testing.T) {
	withLocalUnique(t, "here")
	withSession(t, "u1", "builder")
	got := stubChatArgs(t, nil)

	out, err := pushChatMessageFrom(4001, "hello", "u1")
	if err != nil || out != "message pushed" {
		t.Fatalf("got (%q, %v)", out, err)
	}
	if len(*got) != 1 {
		t.Fatalf("calls = %d, want 1", len(*got))
	}
	want := map[string]string{"text": "hello", "from_session": "here/u1", "from_name": "builder"}
	for k, v := range want {
		if (*got)[0][k] != v {
			t.Errorf("arg %s = %q, want %q", k, (*got)[0][k], v)
		}
	}
}

// A session with no name is still labelled: by its address.
func TestSendChatUnnamedSenderUsesAddress(t *testing.T) {
	withLocalUnique(t, "here")
	withSession(t, "u2", "")
	got := stubChatArgs(t, nil)
	pushChatMessageFrom(4001, "hi", "u2")
	if (*got)[0]["from_name"] != "here/u2" {
		t.Errorf("from_name = %q, want the address", (*got)[0]["from_name"])
	}
}

// An unknown caller is never stamped: text only, no sender fields.
func TestSendChatUnknownCallerHasNoSender(t *testing.T) {
	got := stubChatArgs(t, nil)
	pushChatMessageFrom(4001, "hi", "")
	if len(*got) != 1 || len((*got)[0]) != 1 || (*got)[0]["text"] != "hi" {
		t.Errorf("args = %v, want only text", *got)
	}
}

// An older agent-chat rejects the unknown fields without delivering anything:
// retry once with the text signoff it understands.
func TestSendChatOlderAgentChatGetsTextSignoff(t *testing.T) {
	withLocalUnique(t, "here")
	withSession(t, "u1", "builder")
	got := stubChatArgs(t,
		errors.New(`invalid params: validating "arguments": unexpected additional properties ["from_name" "from_session"]`),
		nil)

	out, err := pushChatMessageFrom(4001, "hello", "u1")
	if err != nil || out != "message pushed" {
		t.Fatalf("got (%q, %v)", out, err)
	}
	if len(*got) != 2 {
		t.Fatalf("calls = %d, want 2", len(*got))
	}
	retry := (*got)[1]
	if retry["text"] != "hello\n\n(via send_chat_message from here/u1)" || len(retry) != 1 {
		t.Errorf("retry args = %v, want text with the old signoff and nothing else", retry)
	}
}

// Any other failure may mean the message was delivered: no retry.
func TestSendChatOtherErrorsAreNotRetried(t *testing.T) {
	withLocalUnique(t, "here")
	withSession(t, "u1", "builder")
	got := stubChatArgs(t, errors.New("HTTP 500: boom"))
	if _, err := pushChatMessageFrom(4001, "hello", "u1"); err == nil {
		t.Fatal("want error")
	}
	if len(*got) != 1 {
		t.Errorf("calls = %d, want 1", len(*got))
	}
}
