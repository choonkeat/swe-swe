package main

import "testing"

func TestMapDiscussions(t *testing.T) {
	pos := &glPosition{NewPath: "a.go", NewLine: 12, HeadSHA: "abc"}
	inline := glDiscussion{
		ID: "disc1",
		Notes: []glNote{
			{ID: 1, Body: "inline root", Type: "DiffNote", Resolved: true, Position: pos},
			{ID: 2, Body: "inline reply", Type: "DiffNote", Position: pos},
		},
	}
	general := glDiscussion{
		ID:    "disc2",
		Notes: []glNote{{ID: 3, Body: "general note"}},
	}
	// Removed-line comment: NewLine 0 -> falls back to old path/line, side LEFT.
	removed := glDiscussion{
		ID: "disc3",
		Notes: []glNote{
			{ID: 4, Body: "on removed line", Type: "DiffNote",
				Position: &glPosition{OldPath: "b.go", OldLine: 7, HeadSHA: "def"}},
		},
	}

	threads, notes := mapDiscussions([]glDiscussion{inline, general, removed})
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
	if len(notes) != 1 || notes[0].Body != "general note" {
		t.Fatalf("notes = %+v, want one general note", notes)
	}
	if threads[0].ID != "disc1" || !threads[0].Resolved || len(threads[0].Comments) != 2 {
		t.Errorf("thread 0: id=%s resolved=%v comments=%d, want disc1/true/2",
			threads[0].ID, threads[0].Resolved, len(threads[0].Comments))
	}
	if threads[0].Path != "a.go" || threads[0].Line != 12 || threads[0].Side != "RIGHT" {
		t.Errorf("thread 0 anchor = %s:%d %s, want a.go:12 RIGHT", threads[0].Path, threads[0].Line, threads[0].Side)
	}
	if threads[1].Path != "b.go" || threads[1].Line != 7 || threads[1].Side != "LEFT" {
		t.Errorf("thread 1 anchor = %s:%d %s, want b.go:7 LEFT", threads[1].Path, threads[1].Line, threads[1].Side)
	}
}

func TestProviderFor(t *testing.T) {
	cases := []struct {
		host string
		typ  string
		ok   bool
	}{
		{"github.com", "main.githubProvider", true},
		{"gitlab.com", "main.gitlabProvider", true},
		{"gitlab.example.org", "main.gitlabProvider", true},
		{"bitbucket.org", "", false},
	}
	for _, c := range cases {
		p, err := providerFor(PRRef{Host: c.host})
		if c.ok && err != nil {
			t.Errorf("providerFor(%q): unexpected error %v", c.host, err)
		}
		if !c.ok && err == nil {
			t.Errorf("providerFor(%q): expected error, got %T", c.host, p)
		}
	}
}

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
