package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// dockerlessMarkerFile names the sentinel inside the metadata dir that marks a
// project as host-native. `swe-swe up` reads it to decide between exec-ing the
// dumped swe-swe-server and shelling out to docker compose.
const dockerlessMarkerFile = "mode"

// writeDockerlessMarker records that the project at sweDir is dockerless.
func writeDockerlessMarker(sweDir string) error {
	if err := os.MkdirAll(sweDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sweDir, dockerlessMarkerFile), []byte("dockerless\n"), 0644)
}

// isDockerlessProject reports whether sweDir holds a dockerless mode marker.
// Missing dir/file or any read error reports false (treat as compose mode).
func isDockerlessProject(sweDir string) bool {
	data, err := os.ReadFile(filepath.Join(sweDir, dockerlessMarkerFile))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "dockerless"
}

// dockerlessGOOSGuard reports whether a dockerless init is allowed for the
// given GOOS. The embedded payload binaries are static-Linux only (Phase 1),
// and the server still relies on two Linux-only couplings -- the abstract
// unix socket `@swe-swe-broker` and util-linux `script -T/-I/-O` recording
// flags -- so dumping them on macOS/Windows would produce a broken setup.
// Mac-native dockerless (darwin binaries + portable couplings) is Phase 6.
func dockerlessGOOSGuard(goos string) error {
	switch goos {
	case "linux":
		return nil
	case "darwin":
		// macOS is supported but the runtime ports are still in progress
		// (Phase 6): the per-session credential broker and PTY recording are
		// Linux-specific and degrade on darwin -- see dockerlessDarwinWarning.
		return nil
	default:
		return fmt.Errorf("swe-swe init --dockerless supports Linux and macOS hosts (this is a %s build); use Docker mode here", goos)
	}
}

// dockerlessDarwinWarning is printed on a macOS dockerless init so the user
// knows which subsystems are not yet ported (Phase 6).
const dockerlessDarwinWarning = "Note: macOS dockerless is experimental. The per-session git credential " +
	"broker and PTY session recording are not yet ported to macOS and will be " +
	"inactive; all six tabs otherwise work. Track: tasks/2026-06-27-dockerless-single-binary.md (Phase 6)."

// executeDockerlessInit performs a host-native (no-Docker) init: it dumps the
// embedded binaries into the metadata dir, writes the mode marker + project
// records that `swe-swe list`/`up` rely on, and prints next steps. It does NOT
// generate a Dockerfile or compose file. The GOOS guard in handleInit has
// already rejected non-Linux callers before we get here.
func executeDockerlessInit(absPath, sweDir string, config InitConfig) {
	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create metadata directory: %v", err)
	}
	config.HostUID = os.Getuid()
	config.HostGID = os.Getgid()

	// Dump the prebuilt host-native binaries (server + helpers) into the
	// metadata dir's bin/, which `swe-swe up` puts on PATH and exec's.
	binDir := filepath.Join(sweDir, "bin")
	if err := extractDockerlessBinaries(binDir, runtime.GOOS, runtime.GOARCH); err != nil {
		log.Fatalf("Failed to extract dockerless binaries: %v", err)
	}
	fmt.Printf("Extracted %d host-native binaries to %s\n", len(dockerlessBinaries), binDir)
	if runtime.GOOS == "darwin" {
		fmt.Println(dockerlessDarwinWarning)
	}

	// Emit the swe-swe-open shim + xdg-open/open/... symlinks into bin/ so the
	// agent's URL-open habits route into the Preview pane (entrypoint.sh does
	// this in the container).
	if err := writeDockerlessOpenShim(binDir); err != nil {
		log.Fatalf("Failed to write swe-swe-open shim: %v", err)
	}

	// Project-scoped MCP config (option ii): no global ~/.claude.json
	// pollution. Claude reads .mcp.json from the project root at launch.
	if err := writeDockerlessMCPConfig(absPath); err != nil {
		log.Fatalf("Failed to write .mcp.json: %v", err)
	}
	fmt.Printf("Wrote MCP config to %s\n", filepath.Join(absPath, ".mcp.json"))

	// Claude hook guards (AskUserQuestion + silent-stop), project-scoped so
	// the host user's global ~/.claude is never touched.
	if err := writeDockerlessHooks(absPath, sweDir); err != nil {
		log.Fatalf("Failed to write Claude hook guards: %v", err)
	}
	fmt.Printf("Wrote Claude hook guards to %s\n", filepath.Join(absPath, ".claude", "settings.local.json"))

	if err := writeDockerlessMarker(sweDir); err != nil {
		log.Fatalf("Failed to write dockerless marker: %v", err)
	}

	// Record the project path (used by `swe-swe list`) and save config so
	// `swe-swe up` can detect the CLI-vs-config version skew.
	if err := os.WriteFile(filepath.Join(sweDir, ".path"), []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}
	if err := saveInitConfig(sweDir, config); err != nil {
		log.Fatalf("Failed to save init config: %v", err)
	}

	fmt.Printf("\nInitialized dockerless swe-swe project at %s\n", absPath)
	fmt.Printf("View all projects: swe-swe list\n")
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
}

// dockerlessServerInvocation builds the command to run the dumped server for a
// dockerless project: the server binary path, its args (project as working
// dir, loopback bind on the chosen port), and the environment with the dumped
// bin/ prepended to PATH so the git credential/signing helpers resolve. Pure
// for testability; the actual exec lives in handleDockerlessCommand.
func dockerlessServerInvocation(sweDir, absPath, port string, baseEnv []string, tunnel tunnelConfig) (bin string, args, env []string) {
	binDir := filepath.Join(sweDir, "bin")
	bin = filepath.Join(binDir, "swe-swe-server")
	// Host-native paths: the project is the workspace; the dumped sweDir is
	// the .swe-swe home (sweDir/bin holds the helpers + swe-swe-open shim);
	// worktrees/repos live under sweDir. Loopback bind = no LAN exposure
	// (also exactly what tunnel mode wants: the tunnel client dials loopback).
	args = []string{
		"-working-directory", absPath,
		"-workspace", absPath,
		"-swe-home", sweDir,
		"-worktrees", filepath.Join(sweDir, "worktrees"),
		"-repos", filepath.Join(sweDir, "repos"),
		"-bind", "127.0.0.1:" + port,
	}
	// Tunnel mode (no Docker): point the server at the embedded tunnel client
	// dumped into bin/ and pass through the saved tunnel config.
	if tunnel.serverURL != "" {
		args = append(args,
			"-tunnel-server-url", tunnel.serverURL,
			"-tunnel-bin", filepath.Join(binDir, "swe-swe-tunnel"),
		)
		if tunnel.clientCert != "" {
			args = append(args, "-tunnel-client-cert", tunnel.clientCert)
		}
		if tunnel.localPorts {
			args = append(args, "-tunnel-local-ports")
		}
	}

	env = make([]string, 0, len(baseEnv)+2)
	pathSet := false
	for _, e := range baseEnv {
		if strings.HasPrefix(e, "PATH=") {
			env = append(env, "PATH="+binDir+string(os.PathListSeparator)+strings.TrimPrefix(e, "PATH="))
			pathSet = true
			continue
		}
		env = append(env, e)
	}
	if !pathSet {
		env = append(env, "PATH="+binDir)
	}
	// The swe-swe-open shim (and other per-session wiring) reads
	// SWE_SERVER_PORT; the server passes it through to sessions. In the
	// container the entrypoint exports it; here `swe-swe up` does.
	env = append(env, "SWE_SERVER_PORT="+port)
	return bin, args, env
}

// dockerlessOpenShimNames are the browser-launcher command names symlinked to
// swe-swe-open so an agent's `xdg-open`/`open`/etc. route URLs to the Preview
// pane (mirrors entrypoint.sh).
var dockerlessOpenShimNames = []string{"xdg-open", "open", "x-www-browser", "www-browser", "sensible-browser"}

// dockerlessOpenShim is the swe-swe-open script: it POSTs the URL to the
// per-session preview reverse-proxy so links open in the Preview pane. It
// reads SWE_SERVER_PORT/SESSION_UUID/MCP_AUTH_KEY from the session env the
// server sets. Identical in spirit to the entrypoint.sh heredoc.
const dockerlessOpenShim = `#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:$SWE_SERVER_PORT/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)&key=$MCP_AUTH_KEY" >/dev/null 2>&1 &
echo "-> Preview: $URL" >&2
`

// writeDockerlessOpenShim writes swe-swe-open (0755) into binDir and creates
// the xdg-open/open/... symlinks pointing at it. Existing symlinks are
// replaced so re-init is idempotent.
func writeDockerlessOpenShim(binDir string) error {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", binDir, err)
	}
	shim := filepath.Join(binDir, "swe-swe-open")
	if err := os.WriteFile(shim, []byte(dockerlessOpenShim), 0755); err != nil {
		return fmt.Errorf("write swe-swe-open: %w", err)
	}
	if err := os.Chmod(shim, 0755); err != nil {
		return fmt.Errorf("chmod swe-swe-open: %w", err)
	}
	for _, name := range dockerlessOpenShimNames {
		link := filepath.Join(binDir, name)
		_ = os.Remove(link)
		if err := os.Symlink("swe-swe-open", link); err != nil {
			return fmt.Errorf("symlink %s: %w", name, err)
		}
	}
	return nil
}

// tunnelConfig carries the dockerless tunnel settings from init.json through
// to the server flags. Zero value (empty serverURL) = tunnel disabled.
type tunnelConfig struct {
	serverURL  string
	clientCert string
	localPorts bool
}

// loadDockerlessTunnelConfig reads the saved tunnel settings for a dockerless
// project. Missing/unreadable config = tunnel disabled (zero value).
func loadDockerlessTunnelConfig(sweDir string) tunnelConfig {
	cfg, err := loadInitConfig(sweDir)
	if err != nil {
		return tunnelConfig{}
	}
	return tunnelConfig{
		serverURL:  cfg.TunnelServerURL,
		clientCert: cfg.TunnelClientCert,
		localPorts: cfg.TunnelLocalPorts,
	}
}

// handleDockerlessCommand is the dockerless counterpart to the docker-compose
// passthrough: it runs the dumped server directly instead of `docker compose`.
// Supports `up [--open]` (foreground) and `down`.
func handleDockerlessCommand(command, sweDir, absPath string, args []string) {
	open := false
	var rest []string
	for _, a := range args {
		if a == "--open" {
			open = true
			continue
		}
		rest = append(rest, a)
	}

	switch command {
	case "up":
		port := dockerlessPort(os.Getenv)
		tunnel := loadDockerlessTunnelConfig(sweDir)
		bin, sargs, env := dockerlessServerInvocation(sweDir, absPath, port, os.Environ(), tunnel)
		if tunnel.serverURL != "" {
			fmt.Printf("Tunnel mode: connecting via %s\n", tunnel.serverURL)
		}
		if _, err := os.Stat(bin); err != nil {
			log.Fatalf("dockerless server not found at %s -- re-run `swe-swe init --dockerless`: %v", bin, err)
		}
		sargs = append(sargs, rest...)
		url := fmt.Sprintf("http://127.0.0.1:%s/", port)
		fmt.Printf("Starting dockerless swe-swe at %s (Ctrl-C to stop)\n", url)
		if open {
			go openBrowserWhenReady(url, net.JoinHostPort("127.0.0.1", port))
		}
		cmd := exec.Command(bin, sargs...)
		cmd.Env = env
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			log.Fatalf("swe-swe-server: %v", err)
		}
	case "down":
		fmt.Println("Dockerless swe-swe runs in the foreground; stop it with Ctrl-C in the terminal running `swe-swe up`.")
	default:
		log.Fatalf("`swe-swe %s` is not supported in dockerless mode (use: `swe-swe up [--open]` or `swe-swe down`)", command)
	}
}

// openBrowserWhenReady waits for the server to accept connections on addr, then
// best-effort opens the URL in the host browser. Errors are non-fatal.
func openBrowserWhenReady(url, addr string) {
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			_ = exec.Command("xdg-open", url).Start()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dockerlessPort returns the port the dockerless server should bind, honoring
// SWE_PORT/PORT from the environment and defaulting to 1977.
func dockerlessPort(getenv func(string) string) string {
	for _, k := range []string{"SWE_PORT", "PORT"} {
		if v := getenv(k); v != "" {
			return v
		}
	}
	return "1977"
}

// mcpServerSpec is one entry in a Claude Code .mcp.json (stdio transport).
type mcpServerSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// dockerlessMCPServers returns the swe-swe MCP servers, mirroring the
// `claude mcp add` commands the container entrypoint registers
// (templates.go claude_mcp_setup). The `sh -c '... $VAR ...'` form is kept
// verbatim so the session env vars (SWE_SERVER_PORT/SESSION_UUID/
// MCP_AUTH_KEY/BROWSER_CDP_PORT) the server sets expand at agent-launch time.
func dockerlessMCPServers() map[string]mcpServerSpec {
	sh := func(script string) mcpServerSpec { return mcpServerSpec{Command: "sh", Args: []string{"-c", script}} }
	return map[string]mcpServerSpec{
		"swe-swe-agent-chat": sh("exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies \"What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned\" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"),
		"swe-swe-playwright": sh("exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"),
		"swe-swe-preview":    sh("exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"),
		"swe-swe-whiteboard": {Command: "npx", Args: []string{"-y", "@choonkeat/agent-whiteboard"}},
		"swe-swe":            sh("exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"),
	}
}

// writeDockerlessMCPConfig writes a project-scoped .mcp.json into projectDir
// (option ii: no global ~/.claude.json pollution). Claude Code, launched with
// cwd=projectDir by the server, reads it. Overwrites any existing file so
// re-init picks up command changes.
func writeDockerlessMCPConfig(projectDir string) error {
	doc := struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}{MCPServers: dockerlessMCPServers()}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(projectDir, ".mcp.json"), data, 0644); err != nil {
		return fmt.Errorf("write .mcp.json: %w", err)
	}
	return nil
}

// writeDockerlessHooks installs the Claude hook guards that the container
// entrypoint writes into ~/.claude on every boot. On a host we must NOT touch
// the user's global ~/.claude/settings.json -- the guards would leak into
// their unrelated claude sessions -- so the scripts go into the swe-swe
// metadata dir and the wiring into the PROJECT's .claude/settings.local.json
// (machine-local by convention, so the absolute script paths never get
// committed). Merge is idempotent: prior swe-swe entries are dropped and
// re-appended; every other key and hook entry is preserved. The guard scripts
// self-exempt sessions without an agent-chat channel, so host TUI runs are
// unaffected even though the hooks are installed.
func writeDockerlessHooks(projectDir, sweDir string) error {
	hooksDir := filepath.Join(sweDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("hooks dir: %w", err)
	}
	stopPath := filepath.Join(hooksDir, "swe-swe-stop-guard.sh")
	askPath := filepath.Join(hooksDir, "swe-swe-ask-guard.sh")
	if err := os.WriteFile(stopPath, []byte(stopGuardScript), 0o755); err != nil {
		return fmt.Errorf("write stop guard: %w", err)
	}
	if err := os.WriteFile(askPath, []byte(askGuardScript), 0o755); err != nil {
		return fmt.Errorf("write ask guard: %w", err)
	}

	settingsPath := filepath.Join(projectDir, ".claude", "settings.local.json")
	root := map[string]any{}
	if b, err := os.ReadFile(settingsPath); err == nil {
		// Invalid JSON starts fresh, mirroring the entrypoint's jq fallback.
		if json.Unmarshal(b, &root) != nil {
			root = map[string]any{}
		}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	cmdEntry := func(matcher, cmd string) map[string]any {
		e := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": cmd}},
		}
		if matcher != "" {
			e["matcher"] = matcher
		}
		return e
	}
	// Drop any prior swe-swe guard entries so re-init never duplicates.
	keep := func(list []any, ours func(map[string]any) bool) []any {
		var out []any
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && ours(m) {
				continue
			}
			out = append(out, item)
		}
		return out
	}
	pre, _ := hooks["PreToolUse"].([]any)
	pre = keep(pre, func(m map[string]any) bool { return m["matcher"] == "AskUserQuestion" })
	hooks["PreToolUse"] = append(pre, cmdEntry("AskUserQuestion", askPath))
	stop, _ := hooks["Stop"].([]any)
	stop = keep(stop, func(m map[string]any) bool {
		return strings.Contains(fmt.Sprint(m["hooks"]), "swe-swe-stop-guard")
	})
	hooks["Stop"] = append(stop, cmdEntry("", stopPath))
	root["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("project .claude dir: %w", err)
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings.local.json: %w", err)
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0o644)
}

// extractDockerlessBinaries writes the embedded static-Linux binaries for the
// given GOARCH into destDir, each as an executable (0755) file. destDir is
// created if missing. embed.FS strips the executable bit, so we restore it.
func extractDockerlessBinaries(destDir, goos, goarch string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	srcDir := dockerlessPayloadBinDir(goos, goarch)
	for _, name := range dockerlessBinaries {
		data, err := dockerlessPayload.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return fmt.Errorf("read embedded %s (is the %s/%s payload built? run `make dockerless-payload` on this host): %w", name, goos, goarch, err)
		}
		dst := filepath.Join(destDir, name)
		if err := os.WriteFile(dst, data, 0755); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		// WriteFile honors the mode only on creation; force it in case the
		// file pre-existed with a different mode (re-init).
		if err := os.Chmod(dst, 0755); err != nil {
			return fmt.Errorf("chmod %s: %w", dst, err)
		}
	}
	return nil
}
