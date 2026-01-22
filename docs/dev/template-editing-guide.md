# Template Editing Guide

How to modify swe-swe container templates and test changes.

**Related**: See [test-container-workflow.md](test-container-workflow.md) for running test containers.

## Architecture Overview

```
Template files                    Generated files
(source code)                     (runtime)
─────────────────                 ─────────────────
cmd/swe-swe/templates/host/       ~/.swe-swe/projects/<name>/
├── Dockerfile                    ├── Dockerfile
├── docker-compose.yml            ├── docker-compose.yml
├── auth/main.go                  ├── auth/main.go
├── traefik-dynamic.yml           ├── traefik-dynamic.yml
└── ...                           └── ...
        │                                 ↑
        │  make build                     │
        ▼                                 │
   dist/swe-swe.linux-amd64 ─────────────┘
                              swe-swe init
```

**Key concept**: Templates are embedded in the `swe-swe` binary at build time. Running `swe-swe init` extracts and processes them.

## Template Syntax

Templates use two syntaxes:

### 1. Go template variables
```
{{UID}}     → User ID (e.g., 1000)
{{GID}}     → Group ID (e.g., 1000)
```

### 2. Conditional blocks (custom syntax)
```
# {{IF SSL}}
... SSL-only content ...
# {{ENDIF}}

# {{IF NO_SSL}}
... HTTP-only content ...
# {{ENDIF}}

# {{IF DOCKER}}
... Docker-in-Docker content ...
# {{ENDIF}}
```

These are processed based on flags passed to `swe-swe init`.

## Editing Workflow

### For template changes:

```bash
# 1. Edit the template source file
vim cmd/swe-swe/templates/host/docker-compose.yml

# 2. Rebuild the swe-swe binary
make build

# 3. Test with test container workflow
./scripts/test-container/01-init.sh
./scripts/test-container/02-build.sh
./scripts/test-container/03-run.sh

# 4. After testing, run golden tests
make golden-update
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```

### For runtime debugging (quick iteration):

You can temporarily edit generated files for debugging:

```bash
# Find generated project path
cat /workspace/.test-repos/swe-swe-test-0/.swe-test-project

# Edit generated file (temporary - will be overwritten by next init)
vim $(cat /workspace/.test-repos/swe-swe-test-0/.swe-test-project)/docker-compose.yml

# Rebuild just containers (faster than full re-init)
./scripts/test-container/02-build.sh
./scripts/test-container/03-run.sh
```

**Remember**: Changes to generated files are lost when you run `01-init.sh` again. Always port fixes back to template files.

## Common Mistakes

### 1. Copying template files directly

**Wrong**:
```bash
cp cmd/swe-swe/templates/host/docker-compose.yml ~/.swe-swe/projects/myproject/
```
Result: `{{IF SSL}}`, `{{UID}}` etc. appear literally in the file.

**Right**:
```bash
make build && swe-swe init --project-directory=/path/to/project
```

### 2. Editing generated files and expecting persistence

**Wrong**:
```bash
vim ~/.swe-swe/projects/myproject/docker-compose.yml
# ... make changes ...
# ... later run swe-swe init again ...
# ... changes are gone!
```

**Right**:
```bash
vim cmd/swe-swe/templates/host/docker-compose.yml
make build
swe-swe init ...
```

### 3. Forgetting to rebuild binary after template changes

**Wrong**:
```bash
vim cmd/swe-swe/templates/host/Dockerfile
swe-swe init ...  # Uses old templates embedded in binary!
```

**Right**:
```bash
vim cmd/swe-swe/templates/host/Dockerfile
make build        # Embeds new templates
swe-swe init ...  # Now uses updated templates
```

## File Reference

| Template Path | Purpose |
|--------------|---------|
| `docker-compose.yml` | Service definitions (traefik, auth, chrome, swe-swe, vscode) |
| `Dockerfile` | Main swe-swe container |
| `auth/main.go` | Authentication service (login, session cookies) |
| `traefik-dynamic.yml` | Traefik routing and middleware config |
| `entrypoint.sh` | Container startup script |
| `chrome-screencast/Dockerfile` | Browser container for MCP Playwright |
| `code-server/Dockerfile` | VS Code server container |

## Testing Tips

- Use `NO_CACHE=1 ./scripts/test-container/02-build.sh` to force full rebuild
- Check container logs: `docker compose -f $(cat /workspace/.test-repos/swe-swe-test-0/.swe-test-project)/docker-compose.yml logs -f`
- The test container uses `--agents=opencode` by default for simpler auth (uses `ANTHROPIC_API_KEY`)
