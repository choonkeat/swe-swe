package main

import (
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
	if goos != "linux" {
		return fmt.Errorf("swe-swe init --dockerless is supported on a Linux host only for now (this is a %s build); use Docker mode here, or see Phase 6 for native macOS support", goos)
	}
	return nil
}

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
	if err := extractDockerlessBinaries(binDir, runtime.GOARCH); err != nil {
		log.Fatalf("Failed to extract dockerless binaries: %v", err)
	}
	fmt.Printf("Extracted %d host-native binaries to %s\n", len(dockerlessBinaries), binDir)

	// Emit the swe-swe-open shim + xdg-open/open/... symlinks into bin/ so the
	// agent's URL-open habits route into the Preview pane (entrypoint.sh does
	// this in the container).
	if err := writeDockerlessOpenShim(binDir); err != nil {
		log.Fatalf("Failed to write swe-swe-open shim: %v", err)
	}

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
func dockerlessServerInvocation(sweDir, absPath, port string, baseEnv []string) (bin string, args, env []string) {
	binDir := filepath.Join(sweDir, "bin")
	bin = filepath.Join(binDir, "swe-swe-server")
	// Host-native paths: the project is the workspace; the dumped sweDir is
	// the .swe-swe home (sweDir/bin holds the helpers + swe-swe-open shim);
	// worktrees/repos live under sweDir. Loopback bind = no LAN exposure.
	args = []string{
		"-working-directory", absPath,
		"-workspace", absPath,
		"-swe-home", sweDir,
		"-worktrees", filepath.Join(sweDir, "worktrees"),
		"-repos", filepath.Join(sweDir, "repos"),
		"-bind", "127.0.0.1:" + port,
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
		bin, sargs, env := dockerlessServerInvocation(sweDir, absPath, port, os.Environ())
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

// extractDockerlessBinaries writes the embedded static-Linux binaries for the
// given GOARCH into destDir, each as an executable (0755) file. destDir is
// created if missing. embed.FS strips the executable bit, so we restore it.
func extractDockerlessBinaries(destDir, goarch string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	srcDir := dockerlessPayloadBinDir(goarch)
	for _, name := range dockerlessBinaries {
		data, err := dockerlessPayload.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return fmt.Errorf("read embedded %s (is the %s payload built for this arch?): %w", name, goarch, err)
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
