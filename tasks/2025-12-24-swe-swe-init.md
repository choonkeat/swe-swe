# Task: Implement `swe-swe init` and `swe-swe run` commands

## Overview
Create a CLI tool (`cmd/swe-swe`) that initializes and runs a development environment with:
- Docker-compose setup with Traefik routing
- Embedded swe-swe-server binary deployed to `.swe-swe/bin/`
- Persistent auth/config in `.swe-swe/home/`
- VSCode (code-server) + swe-swe-server accessible via subdomains

## Implementation Steps

### Phase 1: Embed binary and generate template files

#### Step 1.1: Set up binary embedding in swe-swe CLI
- [x] Create `cmd/swe-swe/main.go` with basic CLI structure
- [x] Add `//go:embed` directives for static files (Dockerfile, docker-compose.yml, traefik-dynamic.yml)
- [x] Create `cmd/swe-swe-server/` subdirectory in build artifacts
- [x] Implement `swe-swe init --path` command to extract files
- **Test**: ✅ `swe-swe init /tmp/test-init && test -f /tmp/test-init/.swe-swe/Dockerfile`

#### Step 1.2: Create template Dockerfile
- [x] Write `.swe-swe/Dockerfile.template` with:
  - Ubuntu base image
  - Go, Node.js installation
  - Non-root `app` user with home at `/home/app`
  - COPY swe-swe-server binary
  - CMD to run swe-swe-server
- [x] Template should support easy extension (comments for adding Rust, Python, Erlang)
- **Test**: ✅ Verified template is readable and has expected structure

#### Step 1.3: Create template docker-compose.yml
- [x] Write `.swe-swe/docker-compose.yml.template` with:
  - Traefik service configuration
  - swe-swe-server service (mounts project + `.swe-swe/home/`)
  - code-server service (mounts project + `.swe-swe/home/`)
  - Environment variable section with ANTHROPIC_API_KEY example
- [x] Shared `.swe-swe/home/` volume for both services
- **Test**: ✅ YAML syntax validation passes with `docker-compose config`

#### Step 1.4: Create template traefik-dynamic.yml
- [x] Write with optional auth middleware (commented out)
- [x] Router rules for `swe-swe.*` and `vscode.*` subdomains
- **Test**: ✅ YAML syntax validation

#### Step 1.5: Implement file extraction in init command
- [x] Extract embedded files to `path/.swe-swe/`
- [x] Copy swe-swe-server binary to `path/.swe-swe/bin/swe-swe-server`
- [x] Create `path/.swe-swe/home/` directory
- [x] Fail fast with clear error if path creation fails
- **Test**: ✅
  - `make swe-swe-test` passes all checks
  - Error handling verified for nonexistent paths

---

### Phase 2: Implement run command and docker integration

#### Step 2.1: Implement `swe-swe run --path` command
- [x] Accept optional `--path` (defaults to current directory)
- [x] Validate path exists and contains `.swe-swe/` directory
- [x] Check if docker/docker-compose is installed (fail fast with helpful message)
- [x] Construct docker-compose command with correct file path
- **Test**: ✅
  - `./dist/swe-swe run --path /tmp/test` validates .swe-swe exists
  - `./dist/swe-swe run --path /nonexistent` shows clear error
  - Error handling verified

#### Step 2.2: Environment variable injection
- [x] If `ANTHROPIC_API_KEY` env var is set, pass to docker-compose
- [x] Auto-set WORKSPACE_DIR for docker-compose
- [x] Env vars passed to containers for auth/config
- **Test**: ✅ docker-compose config validates vars are available

#### Step 2.3: User experience improvements
- [x] Clear startup message showing:
  - Path being used
  - URL to access swe-swe (swe-swe.lvh.me:9899)
  - URL to access VSCode (vscode.lvh.me:9899)
- [x] Handle docker-compose errors gracefully with hints
- **Test**: ✅ Output clarity verified

#### Step 2.4: Implement `swe-swe stop --path` command
- [x] Accept optional `--path` (defaults to current directory)
- [x] Validate path exists and contains `.swe-swe/` directory
- [x] Run `docker-compose down` to properly stop all services
- [x] Prevent orphaned processes on ungraceful shutdown
- **Test**: ✅ Error handling verified

---

### Phase 3: Build system integration

#### Step 3.1: Update Makefile
- [x] Add `build` target to compile `swe-swe` CLI with embedded files
- [x] Ensure swe-swe-server is built first and available for embedding
- [x] Binaries placed in ./dist/ directory
- **Test**: ✅ `make swe-swe-test` builds and validates everything

#### Step 3.2: Makefile targets
- [x] swe-swe-init: Initialize new project (default SWE_SWE_PATH=./tmp)
- [x] swe-swe-test: Run comprehensive validation tests
- [x] swe-swe-run: Start docker-compose environment
- [x] swe-swe-stop: Stop docker-compose environment
- [x] swe-swe-clean: Remove .swe-swe directory
- **Test**: ✅ All targets tested and working

---

### Phase 4: Quick start & documentation

#### Current Usage
```bash
# Initialize new project at ./tmp
make swe-swe-init SWE_SWE_PATH=./my-project

# Or manually with the CLI
./dist/swe-swe init --path ./my-project

# Test the initialization
make swe-swe-test SWE_SWE_PATH=./my-project

# Run the environment (requires Docker)
make swe-swe-run SWE_SWE_PATH=./my-project

# Access the environment
# - swe-swe terminal: swe-swe.lvh.me:9899
# - VSCode: vscode.lvh.me:9899
# - Traefik dashboard: traefik.lvh.me:9899

# Stop the environment
make swe-swe-stop SWE_SWE_PATH=./my-project

# Clean up
make swe-swe-clean SWE_SWE_PATH=./my-project
```

#### Files Generated in `.swe-swe/`
- `Dockerfile`: Minimal Ubuntu with Go, Node.js (easily extensible)
- `docker-compose.yml`: Traefik, swe-swe-server, code-server services
- `traefik-dynamic.yml`: Optional auth configuration (commented out)
- `bin/swe-swe-server`: Embedded binary from build
- `home/`: Persistent storage for auth/config (mounted in containers)

---

## Summary of Changes

### New Files Created
- `cmd/swe-swe/main.go` - CLI application with init, run, stop commands
- `cmd/swe-swe/templates/Dockerfile` - Base image with Go, Node.js, extensible for more tools
- `cmd/swe-swe/templates/docker-compose.yml` - Multi-service setup with Traefik routing
- `cmd/swe-swe/templates/traefik-dynamic.yml` - Optional auth middleware configuration
- `tasks/2025-12-24-114101-swe-swe-init.md` - This detailed task file

### Files Modified
- `Makefile` - Refactored with new targets and ./dist/ directory structure
- `.gitignore` - Added dist/, tmp/ exclusions

### Commits Made
1. `5ae499a` - feat: implement swe-swe CLI init command with embedded templates
2. `562cec1` - fix: update docker-compose port and add swe-swe test target
3. `b77c185` - refactor: move server code to cmd/swe-swe-server directory
4. `d8b71a9` - chore: update .gitignore to exclude dist/ and tmp/ directories

## Progress Tracking

- [x] Phase 1 complete (embedding & templates)
  - All steps 1.1-1.5 implemented and tested
  - Binary embedding with go:embed directives
  - Dockerfile template with comments for extensions
  - docker-compose.yml with shared .swe-swe/home/ volume
  - traefik-dynamic.yml with optional auth

- [x] Phase 2 complete (run & stop commands)
  - init command extracts templates and binary
  - run command starts docker-compose with env var injection
  - stop command gracefully shuts down services
  - Comprehensive error handling and validation

- [x] Phase 3 complete (build system)
  - Makefile refactored to use ./dist/ directory
  - All convenience targets working (swe-swe-init, swe-swe-test, swe-swe-run, swe-swe-stop, swe-swe-clean)
  - Binaries properly isolated and gitignored

- [x] Phase 4 complete (quick start & usage)
  - Clear usage documentation in task file
  - Generated file structure documented
  - Ready for end-user documentation (README.md next phase)

## Notes
- Use stdlib for CLI parsing (flag package) to minimize dependencies
- Per CLAUDE.md: Fail fast with clear error messages
- All test output should be visible (no hidden command output)
- Keep Dockerfile editable by users - just provide well-commented template
