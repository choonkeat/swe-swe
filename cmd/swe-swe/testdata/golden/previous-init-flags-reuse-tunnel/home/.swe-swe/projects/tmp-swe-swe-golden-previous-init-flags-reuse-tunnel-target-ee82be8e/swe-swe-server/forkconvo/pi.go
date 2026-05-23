package forkconvo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// piSessionsRoot is the default location for pi-mono's coding-agent sessions.
// Pi exposes a tree-native fork via runtime.fork(entryId); this implementation
// is for parity and external scripting only.
func piSessionsRoot() string {
	if v := os.Getenv("PI_HOME"); v != "" {
		return filepath.Join(v, "agent", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "sessions")
}

func findPiSession(sessionID string) (string, error) {
	root := piSessionsRoot()
	direct := filepath.Join(root, sessionID+".jsonl")
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}
	var match string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(d.Name(), sessionID) {
			match = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if match == "" {
		return "", fmt.Errorf("pi session %s not found under %s", sessionID, root)
	}
	return match, nil
}

func forkPi(opts Opts) (*Result, error) {
	src, err := findPiSession(opts.SourceSessionID)
	if err != nil {
		return nil, err
	}
	if opts.Anchor == AnchorLastChatReply {
		return nil, fmt.Errorf("pi: last-chat-reply not implemented (use pi's native /fork or pass an explicit entry id)")
	}
	newID := uuid.NewString()
	dst := filepath.Join(filepath.Dir(src), newID+".jsonl")

	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	scanner := newBigScanner(in)
	w := bufio.NewWriter(out)
	defer w.Flush()

	found := false
	for scanner.Scan() {
		line := scanner.Text()
		rewritten := strings.ReplaceAll(line, opts.SourceSessionID, newID)
		if _, err := io.WriteString(w, rewritten); err != nil {
			_ = os.Remove(dst)
			return nil, err
		}
		if _, err := w.WriteString("\n"); err != nil {
			_ = os.Remove(dst)
			return nil, err
		}
		var ev piEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.ID == opts.Anchor {
			found = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		_ = os.Remove(dst)
		return nil, err
	}
	if !found {
		_ = os.Remove(dst)
		return nil, fmt.Errorf("anchor entry id %s not present in %s", opts.Anchor, src)
	}
	return &Result{
		NewSessionID:  newID,
		NewSourcePath: dst,
		AnchorUUID:    opts.Anchor,
	}, nil
}

type piEvent struct {
	ID string `json:"id"`
}
