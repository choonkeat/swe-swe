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
// Files live under /tmp/swe-swe-session-gitconfig/<sid>. Tmpfs-friendly
// and works in containers regardless of /home/app permissions. Cleared
// in killSessionProcessGroup on session end; orphaned files survive
// server restarts but are overwritten when the sid is next saved.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// var (not const) so tests can redirect to a temp dir.
var sessionGitconfigDir = "/tmp/swe-swe-session-gitconfig"

// sessionGitconfigLocks serializes the assemble+write sequence per sid.
// The per-session gitconfig and its sibling <sid>.allowed_signers must
// stay a mutually consistent pair: the gitconfig's allowedSignersFile
// line points at the allowed_signers file, and the principal email in
// that file must match the [user] email in the gitconfig. Multiple WS
// connections to one sid each run their read loop on a separate
// goroutine, so writeSessionGitconfig can be called concurrently for
// the same sid. Holding this lock across both file writes guarantees
// the final on-disk pair comes from a single write call (no interleave).
//
// Keyed by sid (not by path) because the allowed_signers path is always
// derived from the sid even when callers pass an explicit gitconfig path.
var (
	sessionGitconfigLocks   = map[string]*sync.Mutex{}
	sessionGitconfigLocksMu sync.Mutex
)

func sessionGitconfigLock(sid string) *sync.Mutex {
	sessionGitconfigLocksMu.Lock()
	defer sessionGitconfigLocksMu.Unlock()
	mu, ok := sessionGitconfigLocks[sid]
	if !ok {
		mu = &sync.Mutex{}
		sessionGitconfigLocks[sid] = mu
	}
	return mu
}

// atomicWriteFile writes data to <path>.tmp then renames it onto path.
// os.Rename is atomic on the same filesystem, so a concurrent reader
// (the agent's git) sees either the old complete file or the new
// complete file, never a truncated one. os.WriteFile alone opens with
// O_TRUNC and would expose a zero-length / partial window.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func sessionGitconfigPath(sid string) string {
	return filepath.Join(sessionGitconfigDir, sid)
}

// sessionAllowedSignersPath sits next to the per-session gitconfig.
// gpg.ssh.allowedSignersFile in the gitconfig points here so that
// `git verify-commit` (and friends) can verify signatures produced by
// the in-session broker without needing a process-wide ~/.ssh/allowed_signers.
func sessionAllowedSignersPath(sid string) string {
	return filepath.Join(sessionGitconfigDir, sid+".allowed_signers")
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
	// Serialize the whole assemble + write allowed_signers + write
	// gitconfig sequence per sid so the two files are always a
	// consistent pair (see sessionGitconfigLocks). Any subprocess-derived
	// input must be resolved by the caller and passed in -- never shell
	// out while holding this lock.
	mu := sessionGitconfigLock(sid)
	mu.Lock()
	defer mu.Unlock()

	home, _ := os.UserHomeDir()
	body := "# managed by swe-swe-server; edits will be overwritten\n"
	if home != "" {
		body += fmt.Sprintf("[include]\n\tpath = %s/.gitconfig\n", home)
	}

	// [user] takes both author identity and the SSH signing pubkey
	// (literal, not a path -- supported by git >= 2.34 since the
	// value starts with a key type token).
	a, hasAuthor := getAuthor(sid)
	signPub := signingKeyPublicAuthorized(sid)
	if hasAuthor || signPub != "" {
		body += "[user]\n"
		if hasAuthor {
			if a.Name != "" {
				body += fmt.Sprintf("\tname = %s\n", a.Name)
			}
			if a.Email != "" {
				body += fmt.Sprintf("\temail = %s\n", a.Email)
			}
		}
		if signPub != "" {
			body += fmt.Sprintf("\tsigningkey = %s\n", signPub)
		}
	}

	// SSH commit/tag signing wiring. Only emit when a signing key
	// is actually set so users without one see no behavior change.
	// gpg.format = ssh switches git off the openpgp default;
	// gpg.ssh.program points it at our broker-backed wrapper.
	if signPub != "" {
		body += "[gpg]\n"
		body += "\tformat = ssh\n"
		body += "[gpg \"ssh\"]\n"
		body += "\tprogram = git-sign-swe-swe\n"

		// Emit allowedSignersFile only when we can also write a usable
		// file: it needs a principal, and the author email is the obvious
		// choice. Without this, `git log --show-signature` and
		// `git verify-commit` fail with "gpg.ssh.allowedSignersFile needs
		// to be configured" or similar.
		// Write + rename the allowed_signers file BEFORE the gitconfig
		// that references it, so the file always exists by the time the
		// allowedSignersFile line goes live.
		if hasAuthor && a.Email != "" {
			signersPath := sessionAllowedSignersPath(sid)
			line := fmt.Sprintf("%s %s\n", a.Email, signPub)
			if err := atomicWriteFile(signersPath, []byte(line), 0600); err == nil {
				body += fmt.Sprintf("\tallowedSignersFile = %s\n", signersPath)
			}
		}

		body += "[commit]\n"
		body += "\tgpgsign = true\n"
		body += "[tag]\n"
		body += "\tgpgsign = true\n"
	}

	return atomicWriteFile(path, []byte(body), 0600)
}

func removeSessionGitconfig(sid string) {
	if sid == "" {
		return
	}
	// Hold the per-sid lock so removal cannot interleave with an
	// in-flight write. Remove the gitconfig FIRST so a live
	// allowedSignersFile = line never points at an already-deleted
	// file (which would make git verification fail mid-teardown).
	mu := sessionGitconfigLock(sid)
	mu.Lock()
	defer mu.Unlock()
	_ = os.Remove(sessionGitconfigPath(sid))
	_ = os.Remove(sessionAllowedSignersPath(sid))
}

// repoInitSHA returns the root commit (the parent-less "initial" commit)
// of the repo at workDir, or the empty string if the directory is not
// a git repo, has no commits yet, or git is unavailable. The browser
// uses this as a per-repo identity for binding stored signing keys to
// "this is the repo I had auto-restore enabled for last time" -- a
// stronger check than origin alone because a recycled hostname does
// not share the same root commit as the original repo.
//
// `git rev-list --max-parents=0 HEAD` returns one line per root commit;
// some repos have multiple roots after a merge of unrelated histories,
// so we return the lexicographically first hash to keep the choice
// deterministic across machines that clone the same repo.
func repoInitSHA(workDir string) string {
	if workDir == "" {
		return ""
	}
	// Quick early-out: not a git working tree.
	if _, err := os.Stat(filepath.Join(workDir, ".git")); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--max-parents=0", "HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return ""
	}
	// Deterministic choice when multiple roots exist.
	min := strings.TrimSpace(lines[0])
	for _, line := range lines[1:] {
		s := strings.TrimSpace(line)
		if s != "" && (min == "" || s < min) {
			min = s
		}
	}
	return min
}

// readLocalSigningOverrides scans <workDir>/.git/config for any
// signing-related keys set at repo level. Local config wins over the
// per-session GIT_CONFIG_GLOBAL, so any of these silently disable the
// session's signing wiring even when a key has been registered:
//
//	gpg.format
//	gpg.ssh.program
//	gpg.ssh.allowedSignersFile
//	commit.gpgsign
//	tag.gpgsign
//
// The classic trap is `gpg.format = openpgp` left over from a host
// gitconfig: signing flows route to gnupg (which has no key in the
// container) instead of git-sign-swe-swe, and the user sees a generic
// "gpg failed to sign the data".
//
// Returns the matching key=value pairs in declaration order, joined
// with ", " so the UI can render the list verbatim. Empty string when
// no overrides are set (the common case).
func readLocalSigningOverrides(workDir string) string {
	if workDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".git", "config"))
	if err != nil {
		return ""
	}
	// Sections we care about and the keys within them.
	type want struct {
		section string
		keys    []string
	}
	wants := []want{
		{"gpg", []string{"format"}},
		{"gpg \"ssh\"", []string{"program", "allowedsignersfile"}},
		{"commit", []string{"gpgsign"}},
		{"tag", []string{"gpgsign"}},
	}
	wantedKey := func(section, key string) (string, bool) {
		section = strings.ToLower(section)
		key = strings.ToLower(key)
		for _, w := range wants {
			if !strings.EqualFold(w.section, section) {
				continue
			}
			for _, k := range w.keys {
				if k == key {
					// Canonical display form for the user-facing list.
					base := section
					if section == "gpg \"ssh\"" {
						base = "gpg.ssh"
					}
					if key == "allowedsignersfile" {
						key = "allowedSignersFile"
					}
					if key == "gpgsign" {
						key = "gpgSign"
					}
					return base + "." + key, true
				}
			}
		}
		return "", false
	}

	var out []string
	currentSection := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		display, ok := wantedKey(currentSection, key)
		if !ok {
			continue
		}
		out = append(out, display+"="+val)
	}
	return strings.Join(out, ", ")
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
