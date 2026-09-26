// git_run.go -- the one way swe-swe-server runs git
// (tasks/2026-09-25-git-one-at-a-time.md, phase C).
//
// On a large repo the New Session dialog once started ~180 git commands at
// once and exhausted the box's open files. So:
//
//   - Every git command goes through runGit. git_run_lint_test.go fails the
//     build on any other way of starting git.
//   - Per project, git commands run strictly one at a time. A project and all
//     its branch folders share one git store, so they share one line. Each
//     project has two lines: local (reads/writes the repo on this box) and
//     network (fetch, ls-remote, clone, push), so a slow remote never holds up
//     a local check or delete.
//   - A caller waits its turn with its ctx. If ctx ends while waiting, it
//     leaves the line and its command never runs.
//   - Every call needs a ctx with a deadline, so no command can hold a line
//     forever. When the deadline passes, git and its helpers are killed.
//
// Only swe-swe-server's own git calls are covered. Git run by agents inside
// sessions is not queued.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// gitCall is one git command for runGit.
type gitCall struct {
	// Dir is where git runs (passed as `-C Dir`). Required. It also picks
	// the line: the project whose git store Dir belongs to.
	Dir  string
	Args []string
	// Network joins the project's network line and never prompts for a
	// login (GIT_TERMINAL_PROMPT=0).
	Network bool
	// Env replaces the environment; nil inherits the server's.
	Env   []string
	Stdin io.Reader
	// Combined returns stderr mixed into stdout (CombinedOutput); otherwise
	// only stdout is returned and stderr is logged on failure.
	Combined bool
	// OnStart runs once git has started, before waiting on it; the returned
	// func (if any) runs after git exits. Used to register the pid with the
	// credential broker.
	OnStart func(pid int) func()
}

var errGitNoDeadline = errors.New("runGit: ctx has no deadline")

// gitSlowWait / gitSlowRun: calls that wait or run longer than these are
// logged, so a pile-up shows in the logs.
const (
	gitSlowWait = time.Second
	gitSlowRun  = 5 * time.Second
)

var (
	gitLinesMu sync.Mutex
	gitLines   = map[string]chan struct{}{}
)

func gitLine(key string) chan struct{} {
	gitLinesMu.Lock()
	defer gitLinesMu.Unlock()
	line, ok := gitLines[key]
	if !ok {
		line = make(chan struct{}, 1)
		gitLines[key] = line
	}
	return line
}

// runGit waits for its turn on the project's line, then runs git. See the
// file comment for the rules.
func runGit(ctx context.Context, c gitCall) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		_, file, line, _ := runtime.Caller(1)
		log.Printf("runGit refused %v from %s:%d: ctx has no deadline", gitArgsHead(c.Args), filepath.Base(file), line)
		return nil, errGitNoDeadline
	}
	if c.Dir == "" {
		return nil, errors.New("runGit: Dir is required")
	}
	key := gitLineKey(c.Dir)
	if c.Network {
		key += " (network)"
	} else {
		key += " (local)"
	}

	waitStart := time.Now()
	line := gitLine(key)
	select {
	case line <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-line }()
	wait := time.Since(waitStart)

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", c.Dir}, c.Args...)...)
	killGitGroupOnCancel(cmd)
	cmd.Env = c.Env
	if c.Network {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	}
	cmd.Stdin = c.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if c.Combined {
		cmd.Stderr = &stdout
	}

	runStart := time.Now()
	err := cmd.Start()
	if err == nil {
		var done func()
		if c.OnStart != nil {
			done = c.OnStart(cmd.Process.Pid)
		}
		err = cmd.Wait()
		if done != nil {
			done()
		}
	}
	run := time.Since(runStart)

	if err != nil || wait > gitSlowWait || run > gitSlowRun {
		// Only the subcommand is logged: later args can hold URLs, and Env
		// can hold credentials.
		msg := fmt.Sprintf("git %v in %s: waited %v, ran %v", gitArgsHead(c.Args), key, wait.Round(time.Millisecond), run.Round(time.Millisecond))
		if err != nil {
			msg += fmt.Sprintf(", failed: %v", err)
			if !c.Combined {
				if s := strings.TrimSpace(stderr.String()); s != "" {
					msg += fmt.Sprintf(", stderr: %.500s", s)
				}
			}
		}
		log.Print(msg)
	}
	return stdout.Bytes(), err
}

func gitArgsHead(args []string) []string {
	if len(args) > 2 {
		return args[:2]
	}
	return args
}

// gitLineKey picks the line dir joins: its repo's common git dir, found
// from the filesystem without running git (running git to find the line
// would itself need a line). Falls back to the cleaned dir (a clone target,
// a folder about to be `git init`ed).
func gitLineKey(dir string) string {
	dir = filepath.Clean(dir)
	for d := dir; ; d = filepath.Dir(d) {
		if key, ok := gitStoreAt(d); ok {
			return key
		}
		if d == filepath.Dir(d) {
			return dir
		}
	}
}

// gitStoreAt reports the common git dir of the repo rooted at d, if any.
func gitStoreAt(d string) (string, bool) {
	dotGit := filepath.Join(d, ".git")
	info, err := os.Stat(dotGit)
	if err == nil && info.IsDir() {
		return dotGit, true
	}
	if err == nil {
		// A branch folder (worktree): ".git" is a file "gitdir: X", where X
		// is <common>/worktrees/<name>.
		b, err := os.ReadFile(dotGit)
		if err != nil {
			return "", false
		}
		gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if !ok {
			return "", false
		}
		gitdir = strings.TrimSpace(gitdir)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(d, gitdir)
		}
		if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
			common := strings.TrimSpace(string(c))
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitdir, common)
			}
			return filepath.Clean(common), true
		}
		if i := strings.LastIndex(gitdir, string(filepath.Separator)+"worktrees"+string(filepath.Separator)); i >= 0 {
			return filepath.Clean(gitdir[:i]), true
		}
		return filepath.Clean(gitdir), true
	}
	// A bare repo: HEAD and objects/ right in d.
	if _, err := os.Stat(filepath.Join(d, "HEAD")); err == nil {
		if info, err := os.Stat(filepath.Join(d, "objects")); err == nil && info.IsDir() {
			return d, true
		}
	}
	return "", false
}

// killGitGroupOnCancel makes a cancelled context actually stop git. Killing
// only the direct child is not enough: `git fetch` spawns helpers
// (git-remote-https and its curl) that inherit the output pipes, so Wait would
// keep blocking on a stalled grandchild long after git itself is gone. Put the
// whole thing in its own process group, SIGKILL the group on cancel, and cap
// the post-kill wait so an inherited pipe can never hold the caller.
// No-op for a background (never-cancelled) context.
func killGitGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid: signal the group. Setpgid above makes pgid == pid.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
