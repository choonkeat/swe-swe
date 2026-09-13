package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	for _, want := range []string{"commit-session-chat-log", "end_session"} {
		if !strings.Contains(sentText, want) {
			t.Errorf("instruction is missing %q; the agent cannot finish the job without it.\ngot: %s", want, sentText)
		}
	}
	// The advice is the only way a user learns this behaviour is theirs to
	// change -- nothing else in the product mentions the command.
	if !strings.Contains(sentText, chatLogCommitThenEndCommand) {
		t.Errorf("fallback must tell the user they can define their own %q command.\ngot: %s", chatLogCommitThenEndCommand, sentText)
	}
}

// A user who has defined commit-log-then-end has said what this button should
// do. Sending our instructions alongside it would override the thing we just
// offered them control over, so the command goes out alone.
func TestCommitThenEndPrefersUserCommand(t *testing.T) {
	home := t.TempDir()
	cmdDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, chatLogCommitThenEndCommand+".md"), []byte("mine\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		UUID:            "s",
		AgentChatPort:   4001,
		Assistant:       "claude",
		WorkDir:         home,
		AssistantConfig: AssistantConfig{SlashCmdFormat: SlashCmdMD},
	}
	var sentText string
	swapOrchestrator(t, func(_ int, tool string, args any) (string, error) {
		if tool == "send_chat_message" {
			if m, ok := args.(map[string]any); ok {
				sentText, _ = m["text"].(string)
			}
		}
		return "message pushed", nil
	})

	if err := requestChatLogCommitThenEnd(sess); err != nil {
		t.Fatalf("requestChatLogCommitThenEnd: %v", err)
	}
	if want := "/" + chatLogCommitThenEndCommand; sentText != want {
		t.Errorf("want exactly %q, got %q", want, sentText)
	}
}

// An agent with no slash command convention at all (aider, shell) must never be
// handed a slash name it cannot resolve -- it would read as literal text and
// the log would never be committed.
func TestCommitThenEndAgentWithoutSlashCommands(t *testing.T) {
	sess := &Session{
		UUID:            "s",
		AgentChatPort:   4001,
		Assistant:       "aider",
		WorkDir:         t.TempDir(),
		AssistantConfig: AssistantConfig{SlashCmdFormat: SlashCmdNone},
	}
	if sessionHasSlashCommand(sess, chatLogCommitThenEndCommand) {
		t.Error("an agent with no command convention can never have the command")
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

// newChatLogRepo makes a git repo at a temp path with one empty commit on main,
// and returns the path plus a git runner rooted there.
func newChatLogRepo(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("commit", "-q", "--allow-empty", "-m", "init")
	return repo, run
}

// A log committed on THIS tree's branch is committed. The baseline case.
func TestGitHasCommittedFileOnCurrentBranch(t *testing.T) {
	repo, run := newChatLogRepo(t)
	logPath := filepath.Join(repo, "agent-chats", "log.md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("# log\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "agent-chats/log.md")
	run("commit", "-q", "-m", "add log")

	if !gitHasCommittedFile(repo, logPath) {
		t.Error("a log committed on the checked-out branch must report committed")
	}
}

// The regression this function exists for: the log is committed on a branch
// inside a throwaway worktree, then the worktree is removed. This tree's index
// has never seen the file -- `git ls-files` says "never committed" -- but the
// commit is still reachable through refs/heads, so the End dialog must not
// offer to commit it again.
func TestGitHasCommittedFileOnBranchFromRemovedWorktree(t *testing.T) {
	repo, run := newChatLogRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	run("worktree", "add", "-q", "-b", "chatlog/export", wt)

	wtLog := filepath.Join(wt, "agent-chats", "log.md")
	if err := os.MkdirAll(filepath.Dir(wtLog), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(wtLog, []byte("# log\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	wtRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in worktree failed: %v\n%s", args, err, out)
		}
	}
	wtRun("add", "agent-chats/log.md")
	wtRun("commit", "-q", "-m", "chat log")
	run("worktree", "remove", "--force", wt)

	// Precondition: the old probe really does get this wrong.
	lsFiles := exec.Command("git", "ls-files", "--error-unmatch", "agent-chats/log.md")
	lsFiles.Dir = repo
	if lsFiles.Run() == nil {
		t.Fatal("precondition: ls-files should not find a file committed only on another branch")
	}

	sessionLog := filepath.Join(repo, "agent-chats", "log.md")
	if !gitHasCommittedFile(repo, sessionLog) {
		t.Error("a log committed on a branch from a removed worktree must report committed")
	}
}

// A log that exists on disk but in no commit anywhere is not committed -- this
// is the case that must keep offering the commit/discard choice.
func TestGitHasCommittedFileUncommitted(t *testing.T) {
	repo, _ := newChatLogRepo(t)
	logPath := filepath.Join(repo, "agent-chats", "log.md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("# log\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if gitHasCommittedFile(repo, logPath) {
		t.Error("an untracked log must report NOT committed")
	}
}

// Anything we cannot answer counts as "not committed": an empty workDir, an
// empty path, and a directory that is not a repo at all.
func TestGitHasCommittedFileUnanswerable(t *testing.T) {
	repo, _ := newChatLogRepo(t)
	cases := []struct {
		name    string
		workDir string
		path    string
	}{
		{"no workDir", "", filepath.Join(repo, "agent-chats", "log.md")},
		{"no path", repo, ""},
		{"not a repo", t.TempDir(), "agent-chats/log.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if gitHasCommittedFile(tc.workDir, tc.path) {
				t.Error("want NOT committed")
			}
		})
	}
}
