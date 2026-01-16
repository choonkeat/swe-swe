# Add `--with-docker` flag to `swe-swe init`

## Goal

Add a `--with-docker` flag to `swe-swe init` that configures the container with Docker CLI access to the host's Docker daemon, enabling AI agents to run Docker commands for integration testing, building images, and managing containers.

## Security Note

Mounting the Docker socket is essentially root access to the host. The container can:
- Mount host filesystem via `docker run -v /:/host`
- Run privileged containers
- Access other containers' data

This is an opt-in feature. Users must explicitly request `--with-docker`.

---

## Phases

| Phase | Description |
|-------|-------------|
| Phase 0 | Capture golden files for existing `swe-swe init` output variants |
| Phase 1 | Add `--with-docker` flag parsing and validation |
| Phase 2 | Modify Dockerfile template to conditionally install Docker CLI |
| Phase 3 | Modify docker-compose.yml template to conditionally mount Docker socket |
| Phase 4 | Handle permissions (docker group for app user) |
| Phase 5 | Update help text and documentation |
| Phase 6 | End-to-end integration testing |

---

## Phase 0: Capture golden files for existing output variants

### What will be achieved
A `testdata/golden/` directory with complete captured output for all flag variants, providing full regression baselines.

### Small steps

1. Create `cmd/swe-swe/testdata/golden/` directory structure

2. Add Makefile target for golden file generation:
   ```makefile
   GOOS := $(shell go env GOOS)
   GOARCH := $(shell go env GOARCH)
   SWE_SWE_CLI := ./dist/swe-swe.$(GOOS)-$(GOARCH)$(if $(filter windows,$(GOOS)),.exe,)

   TESTDATA := ./cmd/swe-swe/testdata/golden

   .PHONY: golden-update
   golden-update: build-cli
   	@rm -rf /tmp/swe-swe-golden
   	@ln -sfn $(abspath $(TESTDATA)) /tmp/swe-swe-golden
   	@$(MAKE) _golden-variant NAME=default FLAGS=
   	@$(MAKE) _golden-variant NAME=claude-only FLAGS="--agents claude"
   	@$(MAKE) _golden-variant NAME=aider-only FLAGS="--agents aider"
   	@$(MAKE) _golden-variant NAME=goose-only FLAGS="--agents goose"
   	@$(MAKE) _golden-variant NAME=nodejs-agents FLAGS="--agents claude,gemini,codex"
   	@$(MAKE) _golden-variant NAME=exclude-aider FLAGS="--exclude aider"
   	@$(MAKE) _golden-variant NAME=with-apt FLAGS="--apt-get-install vim,curl"
   	@$(MAKE) _golden-variant NAME=with-npm FLAGS="--npm-install typescript"
   	@$(MAKE) _golden-variant NAME=with-both-packages FLAGS="--apt-get-install vim --npm-install typescript"

   _golden-variant:
   	@rm -rf $(TESTDATA)/$(NAME)/home $(TESTDATA)/$(NAME)/target
   	@mkdir -p $(TESTDATA)/$(NAME)/home $(TESTDATA)/$(NAME)/target
   	HOME=/tmp/swe-swe-golden/$(NAME)/home $(SWE_SWE_CLI) init $(FLAGS) /tmp/swe-swe-golden/$(NAME)/target
   ```

3. Capture 9 variants (entire subdirectory each):
   - `default/` - no flags (all agents)
   - `claude-only/` - `--agents claude`
   - `aider-only/` - `--agents aider`
   - `goose-only/` - `--agents goose`
   - `nodejs-agents/` - `--agents claude,gemini,codex`
   - `exclude-aider/` - `--exclude aider`
   - `with-apt/` - `--apt-get-install vim,curl`
   - `with-npm/` - `--npm-install typescript`
   - `with-both-packages/` - `--apt-get-install vim --npm-install typescript`

4. Golden file structure per variant:
   ```
   testdata/golden/{variant}/
   ├── home/
   │   └── .swe-swe/
   │       └── projects/
   │           └── {hash}/           # deterministic hash based on fixed path
   │               ├── Dockerfile
   │               ├── docker-compose.yml
   │               ├── entrypoint.sh
   │               ├── traefik-dynamic.yml
   │               ├── nginx-vscode.conf
   │               ├── auth/
   │               ├── chrome/
   │               └── swe-swe-server/
   └── target/
       ├── .mcp.json
       └── .swe-swe/
           └── browser-automation.md
   ```

5. Add golden file comparison test in `main_test.go`:
   ```go
   func TestInitGoldenFiles(t *testing.T) {
       variants := []struct {
           name  string
           flags []string
       }{
           {"default", []string{}},
           {"claude-only", []string{"--agents", "claude"}},
           // ... etc
       }
       for _, v := range variants {
           t.Run(v.name, func(t *testing.T) {
               // Run init with temp $HOME pointing to temp dir
               // Compare generated directory tree against testdata/golden/{v.name}/
           })
       }
   }
   ```

### Verification
- Run `make golden-update` to generate initial baselines
- Run `go test ./cmd/swe-swe/...` to verify against baselines
- Any diff = test failure with clear output showing what changed

---

## Phase 1: Add `--with-docker` flag parsing and validation

### What will be achieved
The `swe-swe init` command will accept a new `--with-docker` boolean flag.

### Small steps

1. Add `withDocker` boolean variable in `handleInit()` function:
   ```go
   withDocker := fs.Bool("with-docker", false, "Mount Docker socket to allow container to run Docker commands on host")
   ```

2. Pass `withDocker` value to template processing functions (used in later phases)

### Verification (TDD style)

**Red:**
```go
func TestParseWithDockerFlag(t *testing.T) {
    // Test: --with-docker flag is recognized and parsed correctly
    // Test: default value is false when flag not provided
    // Test: value is true when flag is provided
}
```

**Green:**
- Implement the flag parsing logic
- Run tests to confirm they pass

**Regression guarantee:**
- Run existing `go test ./cmd/swe-swe/...`
- Verify `swe-swe init --help` still works
- Verify `swe-swe init` without the flag works identically to before

---

## Phase 2: Modify Dockerfile template to conditionally install Docker CLI

### What will be achieved
The Dockerfile template will have a new conditional section `{{IF DOCKER}}` that installs the Docker CLI.

### Small steps

1. Add `DOCKER` to the list of conditional markers in `processDockerfileTemplate()`

2. Add conditional Docker CLI installation block to `templates/host/Dockerfile`:
   ```dockerfile
   # {{IF DOCKER}}
   # Install Docker CLI only (talks to host Docker via mounted socket)
   RUN ARCH="$(dpkg --print-architecture)" && \
       case "${ARCH}" in \
           amd64) DOCKER_ARCH="x86_64" ;; \
           arm64) DOCKER_ARCH="aarch64" ;; \
           armhf) DOCKER_ARCH="armhf" ;; \
           *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
       esac && \
       curl -fsSL "https://download.docker.com/linux/static/stable/${DOCKER_ARCH}/docker-27.4.1.tgz" | \
       tar xz -C /usr/local/bin --strip-components=1 docker/docker
   # {{ENDIF}}
   ```

3. Update `processDockerfileTemplate()` to accept `withDocker bool` parameter

4. Wire the `withDocker` flag from Phase 1 into the template processor

### Verification (TDD style)

**Red:**
```go
func TestProcessDockerfileTemplateWithDocker(t *testing.T) {
    // Test: DOCKER section is REMOVED when withDocker=false
    // Test: DOCKER section is KEPT when withDocker=true
    // Test: DOCKER section works independently of other flags
}
```

**Green:**
- Implement the conditional logic
- Run tests

**Regression guarantee:**
- Golden file tests pass
- `swe-swe init` without `--with-docker` produces identical Dockerfile to before

---

## Phase 3: Modify docker-compose.yml template to conditionally mount Docker socket

### What will be achieved
The docker-compose.yml template will conditionally include the Docker socket volume mount.

### Small steps

1. Add conditional marker support to docker-compose.yml template

2. Add socket mount inside conditional block in `templates/host/docker-compose.yml`:
   ```yaml
   volumes:
     - ${WORKSPACE_DIR:-.}:/workspace
     - ./home:/home/app
     - ./certs:/swe-swe/certs:ro
     # {{IF DOCKER}}
     - /var/run/docker.sock:/var/run/docker.sock
     # {{ENDIF}}
   ```

3. Create `processDockerComposeTemplate()` function (similar to `processDockerfileTemplate()`)

4. Wire `withDocker` flag through to template processing

### Verification (TDD style)

**Red:**
```go
func TestProcessDockerComposeTemplateWithDocker(t *testing.T) {
    // Test: socket mount line is ABSENT when withDocker=false
    // Test: socket mount line is PRESENT when withDocker=true
    // Test: all other content unchanged
}
```

**Green:**
- Implement `processDockerComposeTemplate()`
- Run tests

**Regression guarantee:**
- Golden file tests pass
- Validate generated docker-compose.yml is valid YAML: `docker-compose config`

---

## Phase 4: Handle permissions (docker group for app user)

### What will be achieved
The `app` user inside the container will have permission to use the Docker socket via runtime GID detection.

### Small steps

1. Add conditional block to `templates/host/entrypoint.sh`:
   ```bash
   # {{IF DOCKER}}
   # Add app user to docker socket's group for permission to use Docker CLI
   if [ -S /var/run/docker.sock ]; then
       DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)
       if ! getent group $DOCKER_GID > /dev/null 2>&1; then
           groupadd -g $DOCKER_GID docker-host
       fi
       usermod -aG $DOCKER_GID app
   fi
   # {{ENDIF}}
   ```

2. Add entrypoint.sh to the template processing pipeline

3. Create `processEntrypointTemplate()` or extend existing logic

### How it works
- Entrypoint runs as root before switching to `app` user
- Detects the GID of the mounted Docker socket
- Creates a group inside the container with that GID
- Adds `app` user to that group
- Now `app` can access the socket (GID numbers match)
- No host files are modified

### Verification (TDD style)

**Red:**
```go
func TestEntrypointTemplateWithDocker(t *testing.T) {
    // Test: docker group block is ABSENT when withDocker=false
    // Test: docker group block is PRESENT when withDocker=true
}
```

**Green:**
- Implement conditional processing for entrypoint.sh
- Run tests

**Regression guarantee:**
- Golden file tests pass
- End-to-end test (Phase 6) verifies `docker ps` works inside container

---

## Phase 5: Update help text and documentation

### What will be achieved
The `--with-docker` flag will be documented in help text.

### Small steps

1. Add `--with-docker` to the flag set with descriptive help text:
   ```go
   withDocker := fs.Bool("with-docker", false, "Mount Docker socket to allow container to run Docker commands on host")
   ```

2. Update `swe-swe init --help` output (automatic from flag definition)

3. Add informational note in generated output when `--with-docker` is used:
   ```
   Docker access: enabled (container can run Docker commands on host)
   ```

### Verification
- Run `swe-swe init --help`, verify `--with-docker` appears with description
- Run `swe-swe init --with-docker`, verify informational message is printed
- Golden file tests pass

---

## Phase 6: End-to-end integration testing

### What will be achieved
Verify the complete `--with-docker` feature works end-to-end.

### Small steps

1. Create a test project with `--with-docker`:
   ```bash
   swe-swe init --with-docker /tmp/test-docker-feature
   ```

2. Verify generated files contain expected Docker-related content:
   - Dockerfile has Docker CLI installation block
   - docker-compose.yml has socket mount
   - entrypoint.sh has group permission block

3. Build and start the container:
   ```bash
   cd /tmp/test-docker-feature && swe-swe up
   ```

4. **[Run from host]** Exec into container and test Docker access:
   ```bash
   docker exec -it <container> docker --version   # CLI installed
   docker exec -it <container> docker ps          # Can talk to host daemon
   docker exec -it <container> docker run --rm hello-world  # Can run containers
   ```

5. Verify containers started by swe-swe container appear on host

6. Clean up:
   ```bash
   swe-swe down
   ```

### Test matrix

| Test | Expected Result |
|------|-----------------|
| `docker --version` | Shows Docker CLI version |
| `docker ps` | Lists host's running containers |
| `docker run hello-world` | Runs successfully |
| Container visibility | hello-world visible in `docker ps -a` on host |

### Regression: Test without `--with-docker`

- Run `swe-swe init` WITHOUT `--with-docker`, verify:
  - No socket mount in docker-compose.yml
  - No Docker CLI in container (`docker` command not found)
  - Container still works normally

---

## Implementation Checklist

- [x] Phase 0: Golden files infrastructure
  - [x] Add Makefile `golden-update` target
  - [x] Generate 9 variant golden files
  - [x] Add golden comparison test in `main_test.go`
  - [x] Run `make golden-update` and commit baseline

- [x] Phase 1: Flag parsing
  - [x] Add `--with-docker` flag to `handleInit()`
  - [x] Add DOCKER condition to `processDockerfileTemplate()`

- [x] Phase 2: Dockerfile template
  - [x] Add `{{IF DOCKER}}` block with Docker CLI installation
  - [x] Update `processDockerfileTemplate()` for `withDocker` parameter
  - [x] Add unit test for template processing

- [ ] Phase 3: docker-compose.yml template
  - [ ] Add `{{IF DOCKER}}` block with socket mount
  - [ ] Create `processDockerComposeTemplate()` function
  - [ ] Add unit test for template processing

- [ ] Phase 4: entrypoint.sh permissions
  - [ ] Add `{{IF DOCKER}}` block with GID detection
  - [ ] Create `processEntrypointTemplate()` function
  - [ ] Add unit test for template processing

- [ ] Phase 5: Help text
  - [ ] Verify `--help` shows new flag
  - [ ] Add informational output when flag is used

- [ ] Phase 6: End-to-end testing
  - [ ] Test `--with-docker` variant builds and runs
  - [ ] Test `docker` commands work inside container
  - [ ] Test regression: non-docker variant still works

- [ ] Add `with-docker` golden file variant
  - [ ] Run `make golden-update` with new variant
  - [ ] Commit updated golden files
