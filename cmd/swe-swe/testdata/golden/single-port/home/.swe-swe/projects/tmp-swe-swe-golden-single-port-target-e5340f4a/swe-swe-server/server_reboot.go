package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// composeProject returns the docker-compose project this server's container
// belongs to, read from the container's own compose label. It is empty (with an
// error) when we are not running under compose -- dockerless mode, or a bare
// process -- in which case a rebuild-reboot is not available and the caller
// should say so rather than pretend.
//
// os.Hostname() is the container's short id by default, which `docker inspect`
// accepts as a lookup key.
func composeProject() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	out, err := exec.Command("docker", "inspect", host,
		"--format", `{{index .Config.Labels "com.docker.compose.project"}}`).Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect %s: %w", host, err)
	}
	project := strings.TrimSpace(string(out))
	if project == "" {
		return "", fmt.Errorf("container %s has no compose project label (not running under docker compose)", host)
	}
	return project, nil
}

// triggerRebuildReboot tears down this server's compose project. Bringing the
// stack back up -- init, then build --no-cache picking up the latest templates,
// then up -- is the host restart loop's job, which fires the moment the
// project's `up` exits. We only pull the trigger here; we do NOT rebuild.
//
// `docker compose -p <project> down` works off the running containers' compose
// labels, so it needs neither the compose file (a host path we cannot see from
// in here) nor a particular working directory.
//
// The teardown is started detached from the request that asked for it: compose
// down then stops this very container, so nothing sequenced after the exec is
// guaranteed to run.
//
// This intentionally runs no tests or build first. A broken working tree makes
// the host loop's rebuild fail and leaves the stack DOWN, so the caller must
// verify the tree (make test) before invoking -- exactly as the manual reboot
// runbook's step 0 does.
func triggerRebuildReboot(project string) error {
	if project == "" {
		return fmt.Errorf("no compose project: a rebuild-reboot needs the docker compose stack")
	}
	// down -t 20: SIGTERM every service with a 20s grace before SIGKILL, so the
	// server's own shutdown path can close sessions cleanly on the way out.
	cmd := exec.Command("docker", "compose", "-p", project, "down", "-t", "20")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start `docker compose -p %s down`: %w", project, err)
	}
	pid := cmd.Process.Pid
	log.Printf("Rebuild-reboot: issued `docker compose -p %s down` (pid %d); host restart loop will redeploy", project, pid)
	go func() {
		defer recoverGoroutine("reboot compose down")
		// Log the exit status rather than discard it. This process lives inside
		// a container the command is tearing down, so it is usually SIGKILLed
		// before Wait returns -- an error here is the normal case, not a fault.
		if err := cmd.Wait(); err != nil {
			log.Printf("Rebuild-reboot: `docker compose down` (pid %d) exited: %v (expected if our own container was stopped first)", pid, err)
		} else {
			log.Printf("Rebuild-reboot: `docker compose down` (pid %d) completed cleanly", pid)
		}
	}()
	return nil
}

// handleServerRebootAPI handles POST /api/server/reboot: the homepage-driven
// counterpart to the reboot_server MCP tool. It resolves the compose project
// BEFORE acknowledging so a dockerless server returns a clear "unavailable"
// instead of a silent no-op, then flushes the acknowledgment before triggering
// teardown so the caller always sees it.
//
// Behind the auth cookie (not in authMiddleware's exemption list) and denied to
// shared-session guests via scopedPathAllowed, same as /api/server/shutdown.
func handleServerRebootAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	project, err := composeProject()
	if err != nil {
		http.Error(w, "Reboot unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf(`{"status":"rebooting","project":%q}`, project)))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	log.Printf("Reboot requested via web UI from %s (compose project %s)", r.RemoteAddr, project)
	if err := triggerRebuildReboot(project); err != nil {
		log.Printf("Reboot trigger failed: %v", err)
	}
}
