package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Agent session id capture
//
// Each chat-mode agent (claude, codex, pi, ...) persists its conversation to a
// per-session file on disk. /api/fork needs to know which file belongs to a
// given swe-swe session in order to fork at a specific anchor. Until this
// capture machinery existed, the fork handler relied on a "latest mtime in
// workdir" heuristic that quietly forked the wrong conversation whenever more
// than one chat had ever lived in the same workdir.
//
// Capture strategy per agent:
//   - claude: claude supports `--session-id <uuid>`. We pre-generate a uuid,
//     inject the flag at spawn, and stamp Session.AgentSessionID synchronously.
//     No filesystem watch required.
//   - codex/pi: no equivalent fresh-session flag exists. We snapshot the
//     agent's session directory just before spawn (under a per-binary mutex)
//     and poll for new files for a short window after spawn. The first new
//     file's filename yields the session id.
//
// If the agent invocation was already supplied with an id-carrying flag
// (--resume <uuid>, --session-id <uuid>, codex resume <uuid>, etc.) we parse
// the id out of argv and skip both injection and the watch.

// agentSpawnMu serializes snapshot+spawn windows on a per-assistant basis so a
// watch-based capture sees exactly one new file. Claude doesn't need this --
// its capture is synchronous via --session-id.
var (
	agentSpawnMuMu sync.Mutex
	agentSpawnMus  = map[string]*sync.Mutex{}
)

func acquireAgentSpawnLock(assistant string) func() {
	agentSpawnMuMu.Lock()
	m, ok := agentSpawnMus[assistant]
	if !ok {
		m = &sync.Mutex{}
		agentSpawnMus[assistant] = m
	}
	agentSpawnMuMu.Unlock()
	m.Lock()
	return m.Unlock
}

// parseKnownAgentSessionID returns a session id if argv already specifies one
// via a resume/fork/session flag. Empty string means "unknown -- caller should
// inject or watch".
func parseKnownAgentSessionID(assistant string, argv []string) string {
	// Flags that carry a session id as their immediate argument.
	idFlags := map[string]bool{
		"--session-id": true, // claude
		"--resume":     true, // claude, pi, gemini
		"--session":    true, // pi (path or uuid)
		"--fork":       true, // pi
	}
	for i, a := range argv {
		if idFlags[a] && i+1 < len(argv) {
			return strings.TrimSuffix(filepath.Base(argv[i+1]), ".jsonl")
		}
	}
	// Codex uses subcommands: `codex resume <id>`, `codex fork <id>`. The first
	// positional after the subcommand is the id (UUID form). Only treat the
	// next arg as an id if it looks like a UUID; codex also accepts thread
	// names which we shouldn't conflate.
	if assistant == "codex" {
		for i, a := range argv {
			if (a == "resume" || a == "fork") && i+1 < len(argv) {
				next := argv[i+1]
				if looksLikeUUID(next) {
					return next
				}
			}
		}
	}
	return ""
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// hasRestartFlag reports whether argv contains a flag that resumes or
// continues a prior conversation. Such flags conflict with an injected
// --session-id (they pick an existing file; we'd be telling claude to use a
// different file).
func hasRestartFlag(argv []string) bool {
	for _, a := range argv {
		switch a {
		case "--continue", "-c", "--from-pr", "--fork-session":
			return true
		}
	}
	return false
}

// injectAgentSessionID modifies argv to embed an explicit session id for
// agents that support it on fresh starts. Returns (newArgv, id) on success;
// (argv, "") when the agent doesn't support pre-generated ids or when the
// caller has already specified an id-carrying flag.
func injectAgentSessionID(assistant string, argv []string) ([]string, string) {
	if parseKnownAgentSessionID(assistant, argv) != "" {
		return argv, ""
	}
	if hasRestartFlag(argv) {
		return argv, ""
	}
	switch assistant {
	case "claude":
		id := uuid.NewString()
		return append(argv, "--session-id", id), id
	}
	// Other agents (codex, pi, gemini, ...) fall through to watch-based capture.
	return argv, ""
}

// agentSessionDir returns the filesystem directory where the agent writes its
// session-state file for `workDir`, or "" if we don't have a watcher path for
// the agent. The returned directory may not yet exist (e.g. codex creates a
// date-stamped subdir on first run).
// encodeClaudeProjectDir encodes a workdir the way Claude Code names its
// ~/.claude/projects/<dir> rollout folder: every path separator AND dot is
// replaced with a dash (e.g. /repos/github.com-x/workspace ->
// -repos-github-com-x-workspace). Replacing only "/" left dots intact, so any
// workdir containing "." (notably github.com-... repo paths) never matched the
// real folder and fork/resume could not locate the rollout.
func encodeClaudeProjectDir(workDir string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(workDir)
}

func agentSessionDir(assistant, workDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch assistant {
	case "claude":
		if v := os.Getenv("CLAUDE_HOME"); v != "" {
			return filepath.Join(v, "projects", encodeClaudeProjectDir(workDir))
		}
		return filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(workDir))
	case "codex":
		if v := os.Getenv("CODEX_HOME"); v != "" {
			return filepath.Join(v, "sessions")
		}
		return filepath.Join(home, ".codex", "sessions")
	case "pi":
		if v := os.Getenv("PI_HOME"); v != "" {
			return filepath.Join(v, "agent", "sessions")
		}
		return filepath.Join(home, ".pi", "agent", "sessions")
	}
	return ""
}

// agentSessionFileSnapshot returns a snapshot of every *.jsonl file under
// dir (recursively, so codex's YYYY/MM/DD tree is covered), keyed by absolute
// path. Used to compute the "new files" set after spawn.
func agentSessionFileSnapshot(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	if dir == "" {
		return out
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jsonl") {
			out[p] = struct{}{}
		}
		return nil
	})
	return out
}

// extractAgentSessionIDFromPath converts a newly-created jsonl path into the
// agent's session id, applying each agent's naming convention.
func extractAgentSessionIDFromPath(assistant, path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	switch assistant {
	case "claude", "pi":
		return base
	case "codex":
		// rollout-<YYYY-MM-DD>T<HH-MM-SS>-<uuid>.jsonl
		if !strings.HasPrefix(base, "rollout-") {
			return ""
		}
		// The uuid is the trailing 36-char chunk after the final '-' boundary
		// of the timestamp portion. Splitting on '-' and taking the last five
		// parts and rejoining gives the uuid (which itself contains four
		// dashes).
		parts := strings.Split(base, "-")
		if len(parts) < 5 {
			return ""
		}
		candidate := strings.Join(parts[len(parts)-5:], "-")
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	return ""
}

// captureAgentSessionIDViaWatch polls dir for a new *.jsonl file that wasn't
// in preSnapshot, then stamps it onto sess.AgentSessionID. Runs in its own
// goroutine; logs and exits on timeout. Intended for codex and pi, which
// don't accept a pre-generated session id on fresh starts.
func captureAgentSessionIDViaWatch(sess *Session, dir string, preSnapshot map[string]struct{}) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current := agentSessionFileSnapshot(dir)
		var candidates []string
		for p := range current {
			if _, existed := preSnapshot[p]; !existed {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) > 0 {
			// Pick the newest by mtime. With the per-assistant spawn mutex
			// held until the first new file appears, candidates is usually a
			// single path; the mtime tiebreaker is defense in depth.
			best := candidates[0]
			var bestMtime time.Time
			if info, err := os.Stat(best); err == nil {
				bestMtime = info.ModTime()
			}
			for _, p := range candidates[1:] {
				info, err := os.Stat(p)
				if err != nil {
					continue
				}
				if info.ModTime().After(bestMtime) {
					best = p
					bestMtime = info.ModTime()
				}
			}
			id := extractAgentSessionIDFromPath(sess.Assistant, best)
			if id != "" {
				sess.mu.Lock()
				sess.AgentSessionID = id
				if sess.Metadata != nil {
					sess.Metadata.AgentSessionID = id
				}
				sess.mu.Unlock()
				if err := sess.saveMetadata(); err != nil {
					log.Printf("session %s: saveMetadata after agent session id capture failed: %v", sess.UUID, err)
				}
				log.Printf("session %s: captured %s agent session id %s via filesystem watch (%s)", sess.UUID, sess.Assistant, id, filepath.Base(best))
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("WARN: session %s: failed to capture %s agent session id within 10s (watch dir=%s)", sess.UUID, sess.Assistant, dir)
}
