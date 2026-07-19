package main

import (
	"fmt"
	"os"
)

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)



func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		handleInit()
	case "list":
		handleList()
	case "proxy":
		handleProxy()
	case "-h", "--help":
		printUsage()
	default:
		handlePassthrough(command, os.Args[2:])
	}
}


func printUsage() {
	fmt.Fprintf(os.Stderr, "swe-swe %s (%s)\n\n", Version, GitCommit)
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options]

Native Commands:
  init [options]                         Initialize a new swe-swe project
  list                                   List all initialized swe-swe projects (auto-prunes stale ones)
  proxy <command>                        Proxy host commands to containers with real-time streaming

Pass-through Commands:
  All other commands (up, down, build, ps, logs, exec, etc.) are passed directly
  to docker compose with the project's docker-compose.yml. Use --project-directory
  to specify which project, or run from the project directory.

  In a project initialized with --runtime=host there is no docker compose: 'up'
  runs swe-swe-server directly on the host in the foreground (Ctrl-C to stop),
  and compose-only commands (build, ps, logs, exec, ...) do not apply.

Global Option (for pass-through commands):
  --project-directory PATH               Project directory (defaults to current directory)

Init Options:
  --project-directory PATH               Project directory (defaults to current directory)
  --previous-init-flags=reuse            Reapply saved configuration from previous init
  --previous-init-flags=ignore           Ignore saved configuration, use provided flags
  --runtime MODE                         Where the environment runs (default: container)
                                           container                     docker compose
                                           container-with-docker-socket  ...plus the host docker socket
                                           host                          no containers at all; runs on the
                                                                         host. Linux, or macOS experimentally.
                                                                         See docs/dockerless.md
  --ask [DIR]                            Interactive init; optional value overrides the metadata directory
  --metadata-dir DIR                     Override metadata directory (default: ~/.swe-swe/projects/<derived>)
  --agents AGENTS                        Comma-separated agents: claude,gemini,codex,aider,goose,opencode,pi
                                         (default: all)
  --exclude-agents AGENTS                Comma-separated agents to exclude
  --apt-get-install PACKAGES             Additional apt packages to install (comma or space separated)
  --npm-install PACKAGES                 Additional npm packages to install globally (comma or space separated)
  --with-slash-commands REPOS            Git repos to clone as slash commands for Claude/Codex/OpenCode
                                         Format: [alias@]<git-url> (space-separated)
  --with-skills REPOS                    Git repos to clone as agent skills
                                         Format: [alias@]<git-url> (space-separated)
  --ssl MODE                             SSL mode: 'no' (default), 'selfsign', 'selfsign@<host>',
                                         'letsencrypt@<domain>', or 'letsencrypt-staging@<domain>'
                                         Use selfsign@<ip-or-hostname> for remote access
  --email EMAIL                          Contact email for Let's Encrypt expiry notices (required with letsencrypt)
  --copy-home-paths PATHS                Comma-separated paths relative to $HOME to copy into container
                                         (e.g., .gitconfig,.ssh/config)
  --preview-ports RANGE                  App preview port range (default: 3000-3019)
  --public-ports RANGE                   Public (no-auth) port range (default: 5000-5019)
  --proxy-port-offset OFFSET             Offset for per-session proxy ports (default: 20000)
                                         e.g., app port 3000 -> proxy port 23000
  --repos-dir DIR                        Host directory mounted at /repos for external repo clones
                                         (default: .swe-swe/repos in the project)
  --terminal-font-size SIZE              Terminal font size in pixels (default: 14)
  --terminal-font-family FONT            Terminal font family (default: Menlo, Monaco, "Courier New", monospace)
  --status-bar-font-size SIZE            Status bar font size in pixels (default: 12)
  --status-bar-font-family FONT          Status bar font family (default: system sans-serif)

Tunnel Options (see docs/tunnel-explained.md):
  --tunnel-server-url URL                Tunnel server to dial out to (e.g. https://tunnel.example.com).
                                         Drops Traefik / Let's Encrypt; the server binds 127.0.0.1 only
  --tunnel-unique LABEL                  Bare unique label for the registration; public hostname becomes
                                         <label>-tunnel.<server-suffix>. Overridable at runtime via
                                         SWE_TUNNEL_UNIQUE
  --tunnel-client-cert PATH              PEM mTLS client certificate to present to the tunnel server
  --tunnel-local-ports                   Also publish SWE_PORT and the port ranges on the host's 127.0.0.1,
                                         so the 'swe-swe up' machine can reach the containers directly

Available Agents:
  claude, gemini, codex, aider, goose, opencode, pi

Services (defined in docker-compose.yml after init):
  swe-swe (always), traefik (only in --ssl modes)

Examples:
  swe-swe init                                   Initialize current directory with all agents
  swe-swe init --agents=claude                   Initialize current directory with Claude only
  swe-swe init --agents=claude,gemini            Initialize current directory with Claude and Gemini
  swe-swe init --exclude-agents=aider,goose      Initialize current directory without Aider and Goose
  swe-swe init --apt-get-install="vim htop"      Initialize current directory with custom apt packages
  swe-swe init --npm-install="typescript tsx"    Initialize current directory with custom npm packages
  swe-swe init --runtime=container-with-docker-socket
                                                 Initialize with host Docker socket access
  swe-swe init --runtime=host                    Initialize a host-native setup with no Docker
  swe-swe init --with-skills=ck@https://github.com/choonkeat/skills.git
                                                 Initialize current directory with external skills
  swe-swe init --with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git
                                                 Initialize current directory with slash commands
  swe-swe init --ssl=selfsign                    Initialize with self-signed HTTPS certificate
  swe-swe init --copy-home-paths=.gitconfig,.ssh/config
                                                 Copy git and SSH config from host
  swe-swe up                                     Start all services
  swe-swe up -d                                  Start all services in background
  swe-swe down                                   Stop all services
  swe-swe down --remove-orphans                  Stop and remove orphan containers
  swe-swe ps                                     List running containers
  swe-swe logs -f swe-swe                        Follow logs for swe-swe service
  swe-swe exec swe-swe bash                      Open shell in swe-swe container
  swe-swe build --no-cache                       Rebuild all images without cache
  swe-swe up --project-directory /path            Run command for project at /path

Environment Variables:
  SWE_SWE_PASSWORD                       Authentication password (defaults to changeme)
  SWE_SWE_AUTO_UPGRADE                   Non-interactive: auto-regenerate config and add --build
                                         when CLI version is newer than container config
  NODE_EXTRA_CA_CERTS                    Enterprise CA certificate path (auto-copied during init)
  SSL_CERT_FILE                          SSL certificate file path (auto-copied during init)
  NODE_EXTRA_CA_CERTS_BUNDLE             CA certificate bundle path (auto-copied during init)

Requires: Docker with Compose plugin (docker compose) or standalone docker-compose.
          Not needed for projects initialized with --runtime=host.
`)
}


