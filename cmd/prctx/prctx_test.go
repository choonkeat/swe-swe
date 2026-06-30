package main

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want PRRef
	}{
		{"https://github.com/owner/repo/pull/42", PRRef{"github.com", "owner", "repo", 42}},
		{"https://gitlab.com/group/sub/repo/-/merge_requests/7", PRRef{"gitlab.com", "group/sub", "repo", 7}},
	}
	for _, c := range cases {
		got, err := parseRef(c.in)
		if err != nil {
			t.Fatalf("parseRef(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseRef(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseRefRejectsGarbage(t *testing.T) {
	if _, err := parseRef("not-a-url-or-number"); err == nil {
		t.Fatal("expected error for non-url non-number arg")
	}
}

func TestMapThreads(t *testing.T) {
	var n0, n1 gqlThread
	n0.ID = "PRRT_aaa"
	n0.IsResolved = true
	n0.Path = "a.go"
	n0.Line = 10
	n0.DiffSide = "RIGHT"
	c0 := gqlComment{DatabaseID: 1, Body: "root one"}
	c0.Commit.Oid = "abc"
	c1 := gqlComment{DatabaseID: 2, Body: "reply to one"}
	n0.Comments.Nodes = []gqlComment{c0, c1}

	n1.ID = "PRRT_bbb"
	n1.Path = "b.go"
	n1.OriginalLine = 5
	n1.DiffSide = "LEFT"
	c2 := gqlComment{DatabaseID: 3, Body: "root two"}
	c2.Commit.Oid = "def"
	n1.Comments.Nodes = []gqlComment{c2}

	threads := mapThreads([]gqlThread{n0, n1})
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
	if threads[0].ID != "PRRT_aaa" || !threads[0].Resolved || len(threads[0].Comments) != 2 {
		t.Errorf("thread 0: id=%s resolved=%v comments=%d, want id=PRRT_aaa resolved=true comments=2",
			threads[0].ID, threads[0].Resolved, len(threads[0].Comments))
	}
	// Line falls back to OriginalLine when Line is 0.
	if threads[1].Line != 5 {
		t.Errorf("thread 1 line = %d, want 5 (from OriginalLine)", threads[1].Line)
	}
}
