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
	"strings"
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

// readLocalGitUser reads <workDir>/.git/config and returns local
// user.name and user.email if set. Used by the session-page template
// to surface "this repo overrides the per-session identity" in the
// Settings UI: when local user.* is set, the form's Author Name and
// Email fields render as readonly with an explainer, since git's
// resolution order means local beats global and the per-session
// identity won't apply for that repo.
//
// Returns empty strings on any error (no .git/config, no [user]
// section, parse failure). The form is editable in those cases.
func readLocalGitUser(workDir string) (name, email string) {
	if workDir == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".git", "config"))
	if err != nil {
		return "", ""
	}
	inUserSection := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inUserSection = strings.EqualFold(trimmed, "[user]")
			continue
		}
		if !inUserSection {
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		switch strings.ToLower(key) {
		case "name":
			name = val
		case "email":
			email = val
		}
	}
	return name, email
}
