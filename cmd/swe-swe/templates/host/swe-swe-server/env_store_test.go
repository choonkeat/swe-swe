package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSessionEnvVars_ExpandsAndKeeps verifies the repo env-vars store parses
// its raw blob into KEY=VALUE entries, expanding $VAR against the supplied
// session-env lookup (mirroring .swe-swe/env semantics).
func TestSessionEnvVars_ExpandsAndKeeps(t *testing.T) {
	sid := "sid-keep"
	defer clearSessionEnv(sid)
	setSessionEnv(sid, "OPENAI_API_KEY=sk-123\nDB_PASSWORD=hunter2\nDATABASE_URL=postgres://app:$DB_PASSWORD@db/prod\n")

	lookup := envLookup([]string{"DB_PASSWORD=from-session"})
	kept, dropped := sessionEnvVars(sid, lookup)

	if len(dropped) != 0 {
		t.Fatalf("no reserved keys expected, got dropped=%v", dropped)
	}
	if v, ok := envValue(kept, "OPENAI_API_KEY"); !ok || v != "sk-123" {
		t.Errorf("OPENAI_API_KEY = %q (present=%v), want sk-123", v, ok)
	}
	// Earlier line DB_PASSWORD=hunter2 wins over the session lookup for the
	// later $DB_PASSWORD reference (local-refs-win, same as loadEnvFile).
	if v, _ := envValue(kept, "DATABASE_URL"); v != "postgres://app:hunter2@db/prod" {
		t.Errorf("DATABASE_URL = %q, want expansion against the earlier local line", v)
	}
}

// TestSessionEnvVars_DropsReservedKeys verifies that keys managed by swe-swe
// are refused (and reported) so the textarea can never clobber the credential
// broker, proxies, or ports. A dropped reserved key must also not leak into
// later $VAR expansion.
func TestSessionEnvVars_DropsReservedKeys(t *testing.T) {
	sid := "sid-reserved"
	defer clearSessionEnv(sid)
	setSessionEnv(sid, "PATH=/evil\nGH_TOKEN=stolen\nGIT_CONFIG_COUNT=99\nAPP_ENV=prod\nWHERE=$PATH\n")

	kept, dropped := sessionEnvVars(sid, envLookup([]string{"PATH=/real/session/path"}))

	for _, k := range []string{"PATH", "GH_TOKEN", "GIT_CONFIG_COUNT"} {
		if _, ok := envValue(kept, k); ok {
			t.Errorf("reserved key %s must not appear in kept env", k)
		}
		if !strSliceHas(dropped, k) {
			t.Errorf("reserved key %s must be reported in dropped, got %v", k, dropped)
		}
	}
	if v, ok := envValue(kept, "APP_ENV"); !ok || v != "prod" {
		t.Errorf("non-reserved APP_ENV = %q (present=%v), want prod", v, ok)
	}
	// The dropped PATH=/evil must not have entered the local map, so $PATH in
	// WHERE resolves to the session PATH, not /evil.
	if v, _ := envValue(kept, "WHERE"); v != "/real/session/path" {
		t.Errorf("WHERE = %q, want session PATH (dropped reserved key must not leak into expansion)", v)
	}
}

// TestSessionEnvVars_Unset returns nil for a session with no stored env.
func TestSessionEnvVars_Unset(t *testing.T) {
	kept, dropped := sessionEnvVars("sid-none", nil)
	if kept != nil || dropped != nil {
		t.Errorf("unset session: got kept=%v dropped=%v, want nil,nil", kept, dropped)
	}
}

// TestSetSessionEnv_BlankClears verifies an all-whitespace blob clears the
// store rather than storing an empty entry.
func TestSetSessionEnv_BlankClears(t *testing.T) {
	sid := "sid-blank"
	setSessionEnv(sid, "FOO=bar\n")
	setSessionEnv(sid, "   \n\n")
	if _, ok := getSessionEnvRaw(sid); ok {
		t.Errorf("blank blob must clear the store")
	}
	if n := sessionEnvCount(sid); n != 0 {
		t.Errorf("sessionEnvCount after clear = %d, want 0", n)
	}
}

// TestSessionEnvCount counts kept (non-reserved) vars for the cred-state badge.
func TestSessionEnvCount(t *testing.T) {
	sid := "sid-count"
	defer clearSessionEnv(sid)
	setSessionEnv(sid, "A=1\nB=2\nPATH=/nope\n# comment\n\nC=3\n")
	if n := sessionEnvCount(sid); n != 3 {
		t.Errorf("sessionEnvCount = %d, want 3 (A,B,C; PATH reserved, comment/blank skipped)", n)
	}
}

// TestInheritSessionEnv copies a parent's raw env onto a fresh child, one-way.
func TestInheritSessionEnv(t *testing.T) {
	parent, child := "sid-parent", "sid-child"
	defer clearSessionEnv(parent)
	defer clearSessionEnv(child)
	setSessionEnv(parent, "SHARED=yes\n")

	inheritSessionEnv(parent, child)
	if raw, ok := getSessionEnvRaw(child); !ok || raw != "SHARED=yes\n" {
		t.Errorf("child raw = %q (present=%v), want inherited parent blob", raw, ok)
	}
	// Same-session and empty are no-ops.
	inheritSessionEnv(parent, parent)
	inheritSessionEnv("", child)
}

// TestInheritedEnvReachesChildProcess is a REGRESSION test for the spawn-time
// ordering bug where a var saved for one session did not reach a newly spawned
// session's PROCESS env -- only its in-memory store. The store must be
// populated BEFORE buildSessionEnv (which bakes the result into cmd.Env that
// pty.Start freezes); getOrCreateSession now does exactly that.
//
// This test mirrors that fixed order and asserts the var lands in the built
// env for BOTH delivery paths:
//   - inheritSessionEnv (MCP create_session / fork copies the parent's blob)
//   - SessionParams.EnvRaw (browser new-session blob staged on the POST)
//
// The pre-fix code ran inheritSessionEnv AFTER buildSessionEnv, so the env was
// already frozen and the child launched without the var. The neighbouring
// TestInheritSessionEnv stayed green throughout because it only checks the
// store copy (getSessionEnvRaw), never the built process env -- the same blind
// spot the e2e suite had (no test spawned a second session and ran printenv).
func TestInheritedEnvReachesChildProcess(t *testing.T) {
	// Path 1: inherited blob (create_session / fork).
	t.Run("inherit", func(t *testing.T) {
		parent, child := "sid-inherit-parent", "sid-inherit-child"
		defer clearSessionEnv(parent)
		defer clearSessionEnv(child)
		setSessionEnv(parent, "REPRO_ABC=1234\n")

		// Fixed order, as in getOrCreateSession: inherit into the store first,
		// THEN build the process env.
		inheritSessionEnv(parent, child)
		childProcessEnv := buildSessionEnv(SessionEnvParams{SID: child, SessionMode: "chat"})

		if v, ok := envValue(childProcessEnv, "REPRO_ABC"); !ok || v != "1234" {
			t.Fatalf("child process env REPRO_ABC = %q (present=%v), want 1234 -- inherited env did not reach the spawned process", v, ok)
		}
	})

	// Path 2: EnvRaw staged on the browser creation intent.
	t.Run("staged EnvRaw", func(t *testing.T) {
		child := "sid-staged-child"
		defer clearSessionEnv(child)

		// getOrCreateSession applies p.EnvRaw to the store before buildSessionEnv.
		p := SessionParams{UUID: child, EnvRaw: "REPRO_XYZ=hello\n"}
		if p.EnvRaw != "" {
			setSessionEnv(p.UUID, p.EnvRaw)
		}
		childProcessEnv := buildSessionEnv(SessionEnvParams{SID: child, SessionMode: "chat"})

		if v, ok := envValue(childProcessEnv, "REPRO_XYZ"); !ok || v != "hello" {
			t.Fatalf("child process env REPRO_XYZ = %q (present=%v), want hello -- staged EnvRaw did not reach the spawned process", v, ok)
		}
	})
}

// TestBuildSessionEnv_FileWinsOverStore locks in the precedence rule: the
// checked-in .swe-swe/env file overrides the in-memory repo env-vars store on
// key collisions, while non-colliding store vars still flow through.
func TestBuildSessionEnv_FileWinsOverStore(t *testing.T) {
	sid := "sid-precedence"
	defer clearSessionEnv(sid)

	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".swe-swe"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".swe-swe", "env"), []byte("SHARED=from-file\n"), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	setSessionEnv(sid, "SHARED=from-store\nSTORE_ONLY=yes\n")

	env := buildSessionEnv(SessionEnvParams{SID: sid, WorkDir: workDir, SessionMode: "terminal"})

	if v, _ := envValue(env, "SHARED"); v != "from-file" {
		t.Errorf("SHARED = %q, want from-file (.swe-swe/env must win the collision)", v)
	}
	if v, ok := envValue(env, "STORE_ONLY"); !ok || v != "yes" {
		t.Errorf("STORE_ONLY = %q (present=%v), want yes (non-colliding store var must survive)", v, ok)
	}
}

func strSliceHas(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
