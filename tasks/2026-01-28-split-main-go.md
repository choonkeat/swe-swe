# Split cmd/swe-swe/main.go into Multiple Files

**Status**: Planning
**Created**: 2026-01-28
**Reason**: main.go is 1834 lines - too large for agents to handle effectively

## Background

The `cmd/swe-swe/main.go` file has grown to 1834 lines, making it difficult for AI agents to work with. Go supports multiple files in the same package (same directory with `package main`), which is already used for `proxy.go`, `color.go`, and `ansi.go`.

## Current State

```
cmd/swe-swe/
├── main.go              # 1834 lines - TOO BIG
├── main_test.go
├── proxy.go             # Already split out
├── proxy_unix.go
├── proxy_windows.go
├── proxy_test.go
├── proxy_integration_test.go
├── color.go             # Already split out
├── color_test.go
├── ansi.go              # Already split out
├── ansi_test.go
└── templates/
```

## Proposed Split

### New Files

| File | Contents | ~Lines |
|------|----------|--------|
| **main.go** | `main()`, `printUsage()`, version vars | 100 |
| **docker.go** | `dockerComposeCmd` struct/methods, `getDockerComposeCmd()`, `handlePassthrough()` | 140 |
| **init.go** | `handleInit()`, `InitConfig`, `//go:embed` vars, agent parsing, slash command parsing, `saveInitConfig()`, `loadInitConfig()` | 700 |
| **templates.go** | `processDockerfileTemplate()`, `processSimpleTemplate()`, `processEntrypointTemplate()`, `processTerminalUITemplate()` | 250 |
| **certs.go** | `generateSelfSignedCert()`, `handleCertificatesAndEnv()` | 150 |
| **paths.go** | `expandTilde()`, `extractProjectDirectory()`, `sanitizePath()`, `sanitizeProjectName()`, `getMetadataDir()`, `copyFile()`, `copyDir()` | 120 |
| **list.go** | `handleList()` | 120 |

### Function Mapping

#### main.go (keep)
- `main()`
- `printUsage()`
- Version vars (`Version`, `GitCommit`, `BuildTime`)

#### docker.go (new)
- `type dockerComposeCmd struct`
- `getDockerComposeCmd()`
- `(dc *dockerComposeCmd) buildArgs()`
- `(dc *dockerComposeCmd) command()`
- `(dc *dockerComposeCmd) execArgs()`
- `handlePassthrough()`

#### init.go (new)
- `//go:embed all:templates` (assets)
- `//go:embed all:slash-commands` (slashCommandsFS)
- `writeBundledSlashCommands()`
- `var allAgents`
- `var slashCmdAgents`
- `type SlashCommandsRepo struct`
- `type InitConfig struct`
- `(c *InitConfig) HasNonSlashAgents()`
- `(c *InitConfig) HasSlashAgents()`
- `saveInitConfig()`
- `loadInitConfig()`
- `deriveAliasFromURL()`
- `parseSlashCommandsEntry()`
- `parseSlashCommandsFlag()`
- `parseAgentList()`
- `resolveAgents()`
- `agentInList()`
- `handleInit()`

#### templates.go (new)
- `processDockerfileTemplate()`
- `processSimpleTemplate()`
- `processEntrypointTemplate()`
- `processTerminalUITemplate()`

#### certs.go (new)
- `generateSelfSignedCert()`
- `handleCertificatesAndEnv()`

#### paths.go (new)
- `expandTilde()`
- `extractProjectDirectory()`
- `sanitizeProjectName()`
- `sanitizePath()`
- `getMetadataDir()`
- `copyFile()`
- `copyDir()`

#### list.go (new)
- `handleList()`

## Documentation Updates Required

### README.md (line 510)

**Before:**
```
│   └── swe-swe/              # CLI tool
│       ├── main.go
│       └── templates/
```

**After:**
```
│   └── swe-swe/              # CLI tool
│       ├── *.go              # main, init, docker, templates, certs, paths, list
│       └── templates/
```

### docs/cli-commands-and-binary-management.md (line 483)

**Before:**
```
- `cmd/swe-swe/main.go` — CLI command implementations
```

**After:**
```
- `cmd/swe-swe/*.go` — CLI command implementations (main, init, docker, templates, certs, paths, list)
```

## What Doesn't Need Changes

- **Makefile**: Uses `./cmd/swe-swe` (directory path), Go compiles all `*.go` files automatically
- **Tests**: `main_test.go` uses `package main`, tests functions regardless of which file they're in
- **Build process**: `go build ./cmd/swe-swe` works unchanged
- **Golden tests**: Output is unchanged, no golden file updates needed

## Historical Documentation (No Updates Needed)

Files in `tasks/`, `research/`, and `docs/adr/` contain historical line number references to `cmd/swe-swe/main.go`. These are snapshots and don't need updating.

## Implementation Steps

### Phase 1: Create New Files
- [x] Create `docker.go` with docker-related code
- [ ] Create `init.go` with init command code and embeds
- [x] Create `templates.go` with template processing
- [x] Create `certs.go` with certificate handling
- [ ] Create `paths.go` with path utilities
- [ ] Create `list.go` with list command

### Phase 2: Trim main.go
- [ ] Remove moved functions from main.go
- [ ] Verify main.go only contains: `main()`, `printUsage()`, version vars

### Phase 3: Verify
- [ ] Run `make test` - all tests pass
- [ ] Run `make build` - builds successfully
- [ ] Run `make golden-update` - no changes (output unchanged)

### Phase 4: Update Documentation
- [ ] Update README.md line 510
- [ ] Update docs/cli-commands-and-binary-management.md line 483

## Notes

- All files use `package main` - no import changes needed
- Functions are visible across all files in the same package
- Existing `proxy.go`, `color.go`, `ansi.go` demonstrate this pattern works
- Consider adding file header comments to explain each file's purpose
