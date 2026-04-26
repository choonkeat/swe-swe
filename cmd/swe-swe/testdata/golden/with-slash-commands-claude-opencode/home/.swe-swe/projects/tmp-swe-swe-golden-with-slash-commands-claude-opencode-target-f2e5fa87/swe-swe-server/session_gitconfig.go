// session_gitconfig.go -- per-session GIT_CONFIG_GLOBAL file management.
//
// Each session gets its own gitconfig file at sessionGitconfigPath(sid).
// The file [include]s the user's ~/.gitconfig as a baseline and adds
// a [user] section populated from sessionAuthor[sid]. The session shell
// is spawned with GIT_CONFIG_GLOBAL pointing at this file, so:
//
//   - settings the user has in ~/.gitconfig (safe.directory, init, etc.)
//     remain in effect via the [include]
//   - per-session author identity is layered on top
//   - mid-session updates from the WS set_credentials handler take
//     effect on the next git invocation (git re-parses gitconfig per
//     run; no shell restart needed)
//
// Files live under /home/app/.swe-swe/session-gitconfig/<sid>. Cleared
// in clearSessionCredentials's caller path on session end.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const sessionGitconfigDir = "/home/app/.swe-swe/session-gitconfig"

func sessionGitconfigPath(sid string) string {
	return filepath.Join(sessionGitconfigDir, sid)
}

// ensureSessionGitconfig creates the file if missing, populating it
// from any already-stored author identity for the sid. Returns the
// path on success.
func ensureSessionGitconfig(sid string) (string, error) {
	if sid == "" {
		return "", fmt.Errorf("empty sid")
	}
	if err := os.MkdirAll(sessionGitconfigDir, 0700); err != nil {
		return "", err
	}
	path := sessionGitconfigPath(sid)
	if err := writeSessionGitconfigFile(path, sid); err != nil {
		return "", err
	}
	return path, nil
}

// writeSessionGitconfig (re)writes the per-session gitconfig file with
// the current sessionAuthor[sid] values. Safe to call any time the
// author identity changes.
func writeSessionGitconfig(sid string) error {
	if sid == "" {
		return nil
	}
	path := sessionGitconfigPath(sid)
	return writeSessionGitconfigFile(path, sid)
}

func writeSessionGitconfigFile(path, sid string) error {
	home, _ := os.UserHomeDir()
	body := "# managed by swe-swe-server; edits will be overwritten\n"
	if home != "" {
		body += fmt.Sprintf("[include]\n\tpath = %s/.gitconfig\n", home)
	}
	if a, ok := getAuthor(sid); ok {
		body += "[user]\n"
		if a.Name != "" {
			body += fmt.Sprintf("\tname = %s\n", a.Name)
		}
		if a.Email != "" {
			body += fmt.Sprintf("\temail = %s\n", a.Email)
		}
	}
	return os.WriteFile(path, []byte(body), 0600)
}

func removeSessionGitconfig(sid string) {
	if sid == "" {
		return
	}
	_ = os.Remove(sessionGitconfigPath(sid))
}
