package main

import (
	"embed"
	"fmt"
)

// dockerlessPayload carries the prebuilt, static-linux binaries (and, in
// later phases, the helper scripts + config) that `swe-swe init
// --dockerless` dumps onto the host and that the thin Dockerfile COPYs into
// the image. The tree is populated by the Makefile `dockerless-payload`
// target before the CLI is built; a committed `.gitkeep` keeps this embed
// directive compilable on a fresh checkout where the binaries are absent.
//
//go:embed all:dockerless-payload
var dockerlessPayload embed.FS

// dockerlessBinaries is the single source of truth for the compiled helper
// binaries the CLI embeds. The Makefile builds exactly this set and the
// embed verification test asserts each one is present as an ELF for the
// target arch. Keep this list in sync with the Makefile `dockerless-payload`
// target and the Dockerfile COPY steps.
var dockerlessBinaries = []string{
	"swe-swe-server",
	"git-credential-swe-swe",
	"git-sign-swe-swe",
	"mcp-lazy-init",
	"swe-swe-broker-probe",
	"swe-swe-fork-convo",
	// Foreman-compatible Procfile runner for docker-free multi-service dev.
	"swe-run",
	// Registry-resolving exec helper for our distribute-go-bin npm tools
	// (md-serve, agent-chat, whiteboard-mcp, reverse-proxy); replaces the
	// plain-npx spawns of those tools so they need no node.
	"swe-npx",
	// External tunnel client (pinned ref), embedded so `swe-swe up` can run
	// tunnel mode with no Docker. Only spawned when -tunnel-server-url is set.
	"swe-swe-tunnel",
}

// dockerlessPayloadBinDir returns the embed path holding the static host
// binaries for the given GOOS/GOARCH, e.g. "dockerless-payload/bin/linux-amd64"
// or "dockerless-payload/bin/darwin-arm64". The dockerless payload carries
// host binaries only (Docker mode builds in-container), so the CLI embeds the
// set for the OS/arch it was built for.
func dockerlessPayloadBinDir(goos, goarch string) string {
	return fmt.Sprintf("dockerless-payload/bin/%s-%s", goos, goarch)
}
