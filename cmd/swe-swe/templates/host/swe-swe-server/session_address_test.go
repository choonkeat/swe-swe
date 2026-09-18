package main

import (
	"strings"
	"testing"
)

func withLocalUnique(t *testing.T, unique string) {
	t.Helper()
	rt := liveTunnelRuntime
	rt.mu.Lock()
	prev := rt.unique
	rt.unique = unique
	rt.mu.Unlock()
	t.Cleanup(func() {
		rt.mu.Lock()
		rt.unique = prev
		rt.mu.Unlock()
	})
}

func TestParseSessionAddr(t *testing.T) {
	cases := []struct {
		in     string
		unique string
		uuid   string
		bad    bool
	}{
		{in: "abc-123", uuid: "abc-123"},
		{in: "  abc-123 ", uuid: "abc-123"},
		{in: "box1/abc-123", unique: "box1", uuid: "abc-123"},
		{in: "", bad: true},
		{in: "/abc", bad: true},
		{in: "box1/", bad: true},
		{in: "box1/a/b", bad: true},
		{in: "bo x/abc", bad: true},
	}
	for _, c := range cases {
		got, err := parseSessionAddr(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("parseSessionAddr(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSessionAddr(%q): %v", c.in, err)
			continue
		}
		if got.Unique != c.unique || got.UUID != c.uuid {
			t.Errorf("parseSessionAddr(%q) = %+v, want unique=%q uuid=%q", c.in, got, c.unique, c.uuid)
		}
	}
}

func TestResolveLocalSession(t *testing.T) {
	withLocalUnique(t, "here")
	cases := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "u1", want: "u1"},
		{in: "here/u1", want: "u1"},
		{in: "there/u1", wantErr: "unreachable: there"},
		{in: "", wantErr: "uuid is required"},
	}
	for _, c := range cases {
		got, err := resolveLocalSession(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("resolveLocalSession(%q): err=%v, want containing %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("resolveLocalSession(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestResolveLocalSessionWithoutUnique(t *testing.T) {
	withLocalUnique(t, "")
	if _, err := resolveLocalSession("anybox/u1"); err == nil || !strings.Contains(err.Error(), "unreachable: anybox") {
		t.Fatalf("no-unique box must treat every foreign unique as unreachable, got %v", err)
	}
	if got, err := resolveLocalSession("u1"); err != nil || got != "u1" {
		t.Fatalf("bare uuid must stay local, got %q %v", got, err)
	}
}

func TestSessionAddressAndSignoff(t *testing.T) {
	withLocalUnique(t, "here")
	if got := sessionAddress("u1"); got != "here/u1" {
		t.Fatalf("sessionAddress = %q", got)
	}
	if got := chatSignoff("send_chat_message", "u1"); got != "\n\n(via send_chat_message from here/u1)" {
		t.Fatalf("chatSignoff = %q", got)
	}
	if got := chatSignoff("send_chat_message", ""); got != "" {
		t.Fatalf("unknown caller must not be stamped, got %q", got)
	}
	withLocalUnique(t, "")
	if got := sessionAddress("u1"); got != "u1" {
		t.Fatalf("no-unique address = %q", got)
	}
	if got := chatSignoff("send_chat_message", "u1"); got != "\n\n(via send_chat_message from u1)" {
		t.Fatalf("no-unique signoff = %q", got)
	}
}
