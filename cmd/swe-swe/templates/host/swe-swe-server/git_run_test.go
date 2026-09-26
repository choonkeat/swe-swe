package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGitLineKey(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitT(t, root, "init", "-q", "-b", "main", repo)
	gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt")
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", wt)
	nested := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(root, "bare.git")
	gitT(t, root, "init", "-q", "--bare", bare)
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}

	store := filepath.Join(repo, ".git")
	for _, tc := range []struct{ dir, want string }{
		{repo, store},
		{repo + "/", store},
		{wt, store},     // a branch folder shares its project's line
		{nested, store}, // any folder inside it too
		{bare, bare},
		{plain, plain}, // no repo: the folder itself
	} {
		if got := gitLineKey(tc.dir); got != tc.want {
			t.Errorf("gitLineKey(%s) = %s, want %s", tc.dir, got, tc.want)
		}
	}
}

// loggingFakeGit puts a `git` on PATH that logs "start <label>" / "end <label>"
// (label = the last argument) and sleeps for the second-to-last argument's
// seconds. Returns a func reading the log lines.
func loggingFakeGit(t *testing.T) func() []string {
	t.Helper()
	bin := t.TempDir()
	logFile := filepath.Join(bin, "log")
	script := `#!/bin/sh
for a; do prev=$last; last=$a; done
echo "start $last" >> "` + logFile + `"
sleep "$prev"
echo "end $last" >> "` + logFile + `"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		b, _ := os.ReadFile(logFile)
		return strings.Fields(strings.ReplaceAll(string(b), "start ", "start:"))
	}
}

func withDeadline(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Runs a, b at the same time (b starts 50ms later) and returns the log.
func runTwo(t *testing.T, a, b gitCall) {
	t.Helper()
	var wg sync.WaitGroup
	for i, c := range []gitCall{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			if _, err := runGit(withDeadline(t), c); err != nil {
				t.Errorf("runGit %v: %v", c.Args, err)
			}
		}()
	}
	wg.Wait()
}

func TestRunGitOneAtATimePerProject(t *testing.T) {
	read := loggingFakeGit(t)
	a := t.TempDir()

	runTwo(t, gitCall{Dir: a, Args: []string{"0.3", "a1"}}, gitCall{Dir: a, Args: []string{"0.3", "a2"}})
	if got := strings.Join(read(), " "); got != "start:a1 end a1 start:a2 end a2" {
		t.Errorf("same project: %s, want one after the other", got)
	}
}

func TestRunGitProjectsAndLinesRunTogether(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sameDir    bool
		bIsNetwork bool
	}{
		{"different projects", false, false},
		{"local and network of one project", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := loggingFakeGit(t)
			a := gitCall{Dir: t.TempDir(), Args: []string{"0.3", "x"}}
			b := gitCall{Dir: t.TempDir(), Args: []string{"0.3", "y"}, Network: tc.bIsNetwork}
			if tc.sameDir {
				b.Dir = a.Dir
			}
			runTwo(t, a, b)
			if got := strings.Join(read(), " "); got != "start:x start:y end x end y" {
				t.Errorf("%s, want side by side", got)
			}
		})
	}
}

// A caller that gives up while waiting leaves the line; its git never runs.
func TestRunGitGivingUpWhileWaiting(t *testing.T) {
	read := loggingFakeGit(t)
	dir := t.TempDir()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runGit(withDeadline(t), gitCall{Dir: dir, Args: []string{"0.5", "first"}})
	}()
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := runGit(ctx, gitCall{Dir: dir, Args: []string{"0", "never"}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
	<-done
	if got := strings.Join(read(), " "); got != "start:first end first" {
		t.Errorf("log %s, want only first", got)
	}
}

func TestRunGitRefusesNoDeadline(t *testing.T) {
	read := loggingFakeGit(t)
	if _, err := runGit(context.Background(), gitCall{Dir: t.TempDir(), Args: []string{"0", "x"}}); !errors.Is(err, errGitNoDeadline) {
		t.Errorf("err = %v, want errGitNoDeadline", err)
	}
	if got := read(); len(got) != 0 {
		t.Errorf("git ran: %v", got)
	}
}

// A timed-out git is killed and frees the line for the next caller.
func TestRunGitTimeoutFreesLine(t *testing.T) {
	read := loggingFakeGit(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runGit(ctx, gitCall{Dir: dir, Args: []string{"5", "stuck"}}); err == nil {
		t.Error("stuck git: no error")
	}
	if _, err := runGit(withDeadline(t), gitCall{Dir: dir, Args: []string{"0", "next"}}); err != nil {
		t.Errorf("next: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v: the stuck git was not killed", time.Since(start))
	}
	if got := strings.Join(read(), " "); got != "start:stuck start:next end next" {
		t.Errorf("log %s", got)
	}
}

func TestRunGitOutput(t *testing.T) {
	repo, _ := newCheckRepo(t)
	out, err := runGit(withDeadline(t), gitCall{Dir: repo, Args: []string{"rev-parse", "--abbrev-ref", "HEAD"}})
	if err != nil || strings.TrimSpace(string(out)) != "main" {
		t.Errorf("out %q err %v, want main", out, err)
	}
	out, err = runGit(withDeadline(t), gitCall{Dir: repo, Args: []string{"rev-parse", "--verify", "nope"}, Combined: true})
	if err == nil || !strings.Contains(string(out), "fatal") {
		t.Errorf("Combined: out %q err %v, want git's error text", out, err)
	}
}
