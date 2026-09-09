package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Agent View is the one pane with a heavy, non-bundleable dependency
// (Xvfb/chromium/x11vnc/websockify), so it is relocatable: a lean swe-swe host
// runs `swe-swe up --agent-view=<url>` and this command runs the allocation
// service at <url> on a machine that does have the display stack.
//
// Until now that service was only reachable two ways, both awkward for the
// machine that is supposed to be the easy half of the deal: build
// docker/browser-backend yourself from a source checkout, or find
// swe-swe-server inside a project's metadata dir and know to pass
// `-mode browser-backend`. This command is the third way -- no Docker, no
// checkout, no project:
//
//	swe-swe browser-backend [server flags...]
//
// It dumps swe-swe-server into a project-independent dir under ~/.swe-swe and
// execs it in browser-backend mode, forwarding every argument through.

// browserBackendDefaultPort mirrors `ENV SWE_PORT=9333` in
// docker/browser-backend/Dockerfile. Without it the server would fall through
// resolveListenAddr to :1977 -- or, worse, to whatever PORT the host already
// exports for its own service, which is how a real deployment lost a boot to
// "address already in use". The image supplies this default; off Docker,
// nothing does, so the wrapper must.
const browserBackendDefaultPort = "9333"

// browserBackendHomeDir is where the standalone service keeps its copy of
// swe-swe-server. Deliberately NOT under ~/.swe-swe/projects/: this machine
// runs no sessions and has no project, so it must not appear in `swe-swe list`
// or be pruned as a stale project.
func browserBackendHomeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}
	return filepath.Join(homeDir, ".swe-swe", "browser-backend"), nil
}

// browserBackendDisplayStack is the set of programs the service spawns per
// allocated browser. swe-swe-server refuses to start without them; we check
// first only so the message can name the missing ones and the packages that
// carry them.
var browserBackendDisplayStack = []string{"Xvfb", "x11vnc", "websockify"}

// browserBackendChromiumNames are the accepted chromium binary names: Debian
// ships `chromium`, Ubuntu has historically shipped `chromium-browser`.
var browserBackendChromiumNames = []string{"chromium", "chromium-browser"}

// missingDisplayStack returns the display programs that are not on PATH, using
// the injected lookup so the check is testable. Chromium counts as present if
// either accepted name resolves.
func missingDisplayStack(lookPath func(string) (string, error)) []string {
	var missing []string
	for _, name := range browserBackendDisplayStack {
		if _, err := lookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	chromiumFound := false
	for _, name := range browserBackendChromiumNames {
		if _, err := lookPath(name); err == nil {
			chromiumFound = true
			break
		}
	}
	if !chromiumFound {
		missing = append(missing, "chromium")
	}
	return missing
}

// browserBackendMissingStackError renders the missing-programs failure with the
// Debian/Ubuntu install line. Package names, not binary names: `Xvfb` comes
// from `xvfb`, and noVNC's static assets ride along with websockify.
func browserBackendMissingStackError(missing []string) error {
	return fmt.Errorf("browser-backend needs the display stack; not found on PATH: %s\n"+
		"  Debian/Ubuntu: sudo apt-get install -y chromium xvfb x11vnc novnc websockify\n"+
		"  Other distributions carry the same programs under different package names",
		strings.Join(missing, " "))
}

// browserBackendAddrFlag reports whether the caller passed an explicit listen
// address on the command line. Only then does the wrapper stand aside; with no
// flag it imposes 9333 and clears the environment's say in the matter.
//
// SWE_BIND, SWE_PORT and PORT are all deliberately ignored, even though
// swe-swe-server honors each of them. None is evidence that THIS caller picked
// a port for THIS service, and all three are routinely exported by something
// else and inherited: a host's own PORT once cost a deployment a boot with
// "address already in use", and the first two live smokes of this command
// silently landed on :1977 because the launching shell had SWE_PORT and
// SWE_BIND set for an unrelated swe-swe. A command line flag cannot be
// inherited by accident, so it is the only signal worth trusting.
func browserBackendAddrFlag(args []string) bool {
	for _, a := range args {
		if a == "-bind" || a == "--bind" || a == "-addr" || a == "--addr" ||
			strings.HasPrefix(a, "-bind=") || strings.HasPrefix(a, "--bind=") ||
			strings.HasPrefix(a, "-addr=") || strings.HasPrefix(a, "--addr=") {
			return true
		}
	}
	return false
}

// browserBackendListenEnvKeys are every variable swe-swe-server consults when
// resolving its listen address. All three are cleared before exec so an
// inherited one cannot move this service.
var browserBackendListenEnvKeys = []string{"SWE_BIND", "SWE_PORT", "PORT"}

// browserBackendPortEnv returns env with every inherited listen variable
// dropped and SWE_PORT set to port. Dropping rather than appending matters:
// duplicate keys in the exec environment resolve unpredictably, and SWE_BIND
// outranks SWE_PORT inside the server, so appending alone would not win. The
// caller only reaches here when no listen flag was given, so nothing
// intentional is discarded.
func browserBackendPortEnv(env []string, port string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		drop := false
		for _, key := range browserBackendListenEnvKeys {
			if strings.HasPrefix(e, key+"=") {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		out = append(out, e)
	}
	return append(out, "SWE_PORT="+port)
}

// extractBrowserBackendServer dumps just swe-swe-server (not the whole
// dockerless payload -- this machine runs no sessions, so the credential
// broker, tunnel client and mcp proxies have nothing to do here) into binDir.
func extractBrowserBackendServer(binDir, goos, goarch string) (string, error) {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("create %s: %w", binDir, err)
	}
	src := filepath.Join(dockerlessPayloadBinDir(goos, goarch), "swe-swe-server")
	data, err := dockerlessPayload.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read embedded swe-swe-server (is the %s/%s payload built? run `make dockerless-payload` on this host): %w", goos, goarch, err)
	}
	dst := filepath.Join(binDir, "swe-swe-server")
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return "", fmt.Errorf("write %s: %w", dst, err)
	}
	// WriteFile honors the mode only on creation; force it in case a previous
	// run left the file with a different mode.
	if err := os.Chmod(dst, 0755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", dst, err)
	}
	return dst, nil
}

// handleBrowserBackend runs the standalone Agent View browser backend in the
// foreground, forwarding args to swe-swe-server. It replaces this process so
// Ctrl-C, systemd and `docker stop` reach the server directly rather than a
// wrapper that would have to forward signals and could lose the exit status.
func handleBrowserBackend() {
	args := os.Args[2:]
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			printBrowserBackendUsage()
			return
		}
	}

	if err := dockerlessGOOSGuard(runtime.GOOS); err != nil {
		fmt.Fprintf(os.Stderr, "Error: swe-swe browser-backend needs a host-native build; this is a %s build, which ships no swe-swe-server\n", runtime.GOOS)
		os.Exit(1)
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "Note: the browser backend is only tested on Linux. Its display stack (Xvfb/x11vnc/websockify) is X11, and two peer checks fail open off Linux.")
	}

	if missing := missingDisplayStack(exec.LookPath); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "Error: %v\n", browserBackendMissingStackError(missing))
		os.Exit(1)
	}

	home, err := browserBackendHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	binDir := filepath.Join(home, "bin")
	bin, err := extractBrowserBackendServer(binDir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	env := os.Environ()
	if !browserBackendAddrFlag(args) {
		env = browserBackendPortEnv(env, browserBackendDefaultPort)
	}

	fmt.Printf("swe-swe browser-backend %s\n", Version)
	fmt.Printf("Server: %s\n", bin)
	if os.Getenv("SWE_BROWSER_BACKEND_TOKEN") == "" {
		fmt.Println("Warning: SWE_BROWSER_BACKEND_TOKEN is unset -- anyone who can reach this port can allocate a browser here.")
	}

	execArgs := append([]string{bin, "-mode", "browser-backend"}, args...)
	if err := syscall.Exec(bin, execArgs, env); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start %s: %v\n", bin, err)
		os.Exit(1)
	}
}

// printBrowserBackendUsage documents the wrapper itself. The server's own
// browser-backend flags are forwarded verbatim, so the ones worth knowing are
// named here rather than re-declared, and stay documented in one place.
func printBrowserBackendUsage() {
	fmt.Fprintf(os.Stderr, `Usage: swe-swe browser-backend [options]

Runs the standalone Agent View browser backend on this machine: an allocation
service that hands out one Chromium (on its own hidden X display, watchable
over VNC) per requesting swe-swe session. Foreground; Ctrl-C stops it.

Listens on :%s. Pass -bind/-addr to change that. SWE_BIND, SWE_PORT and PORT
are ignored here: on a shared machine they usually belong to something else,
and inheriting one would quietly move or break this service.

Point a swe-swe host at it with:
  SWE_BROWSER_BACKEND_TOKEN=<shared secret> \
      swe-swe up --agent-view=https://this-machine:%s

Requires on PATH: chromium (or chromium-browser), Xvfb, x11vnc, websockify.
  Debian/Ubuntu: sudo apt-get install -y chromium xvfb x11vnc novnc websockify

Linux only in practice: the display stack is X11, and the per-connection peer
checks the reverse-tunnel mode relies on are Linux-specific.

Reachability: the swe-swe host must reach this machine's API port AND the
CDP/VNC port ranges it hands back. If it cannot reach back, run the swe-swe
host with --agent-view-tunnel so this machine never needs an inbound route.

Options are passed through to swe-swe-server, including:
  -bind ADDR:PORT             Listen address
  -browser-backend-max N      Concurrent browser cap (0 = size of the VNC range)
  -browser-backend-host HOST  Hostname clients should dial for the CDP/VNC ports
  -browser-backend-idle DUR   Free a browser unused for this long (default 30m, 0 disables)

Environment:
  SWE_BROWSER_BACKEND_TOKEN   Shared secret; both sides need it to allocate
  SWE_BROWSER_BACKEND_HOST    Same as -browser-backend-host
  SWE_BROWSER_BACKEND_IDLE    Same as -browser-backend-idle
  SWE_CDP_PORTS               CDP port range (default 6000-6019)
  SWE_VNC_PORTS               VNC port range (default 7000-7039)

See docs/dockerless.md ("Browser stack") and docker/browser-backend/README.md.
`, browserBackendDefaultPort, browserBackendDefaultPort)
}
