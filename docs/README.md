# Documentation

## Getting Started

- [Configuration Reference](configuration.md) — all `swe-swe init` flags, environment variables, and config files
- [CLI Commands and Build Architecture](cli-commands-and-binary-management.md) — full command reference, troubleshooting, build system

## Features

- [Browser Automation](browser-automation.md) — Chrome CDP and MCP Playwright
- [Multi-service Apps](multi-service.md) -- Procfile + `swe-run` + App Preview, docker-free
- [Mobile & Touch Support](mobile-keyboard-ui.md) — mobile-optimized UI for touch devices
- [Connection Lifecycle](connection-lifecycle.md) — WebSocket states and reconnection behavior
- [Session and Recording Summaries](session-and-recording-summaries.md) -- how sessions and recordings are summarized

## Deployment

- [Dockerless](dockerless.md) -- run host-native with no Docker daemon or compose stack
- [Dockerless on a Mac](dockerless-mac-vm.md) -- Linux VM + browser-backend container runbook
- [Tunnel Mode Explained](tunnel-explained.md) -- what tunnel mode is, the identity model, env vars, troubleshooting
- [Tunnel Mode on Your Laptop](tunnel-laptop.md) -- runbook for local docker
- [Tunnel Mode on a PaaS](tunnel-paas.md) -- generic runbook for any container PaaS
- [Tunnel Mode on Fly.io](tunnel-fly.md) -- concrete Fly walkthrough

## Internals

- [WebSocket Protocol](websocket-protocol.md) — terminal communication protocol
- [Architecture Decision Records](adr/) — design decisions and rationale
