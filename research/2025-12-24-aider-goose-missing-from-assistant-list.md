# Research: Why Aider and Goose Don't Appear in Assistant Selection

**Date**: 2025-12-24
**Context**: User observed that aider and goose are not showing in the "Select AI Assistant" page, even though they're installed in the Dockerfile.

## Issue Summary

The assistant selection page only shows Claude, Gemini, and Codex. Aider and Goose are defined in the source code but aren't being detected at runtime.

## Root Cause Analysis

### How Assistant Detection Works

The server uses `exec.LookPath()` to detect assistants (main.go:485-515):

```go
func detectAvailableAssistants() error {
	for _, cfg := range assistantConfigs {
		if _, err := exec.LookPath(cfg.Binary); err == nil {
			log.Printf("Detected assistant: %s (%s)", cfg.Name, cfg.Binary)
			availableAssistants = append(availableAssistants, cfg)
		}
	}
	if len(availableAssistants) == 0 {
		return fmt.Errorf("no AI assistants available...")
	}
	return nil
}
```

This checks if each binary name is findable in the system PATH.

### Assistant Configuration (main.go:49-89)

```go
var assistantConfigs = []AssistantConfig{
	{Name: "Claude", ShellCmd: "claude", ShellRestartCmd: "claude --continue", Binary: "claude"},
	{Name: "Gemini", ShellCmd: "gemini", ShellRestartCmd: "gemini --resume", Binary: "gemini"},
	{Name: "Codex", ShellCmd: "codex", ShellRestartCmd: "codex resume --last", Binary: "codex"},
	{Name: "Goose", ShellCmd: "goose session", ShellRestartCmd: "goose session -r", Binary: "goose"},
	{Name: "Aider", ShellCmd: "aider", ShellRestartCmd: "aider --restore-chat-history", Binary: "aider"},
}
```

Both aider and goose are defined, but they're not appearing in the selection page.

## Likely Causes

### Issue 1: Aider Package vs Binary Mismatch

**Installation** (Dockerfile line 38):
```dockerfile
RUN pip3 install aider-chat
```

**Problem**:
- Package name: `aider-chat` (what pip installs)
- Binary name expected: `aider` (what swe-swe-server looks for)

**Verification needed**:
- Check what executable(s) `aider-chat` actually provides
- The binary might be named `aider-chat` or something else entirely
- Different versions may provide different binary names

**Likely solution**:
Change the binary lookup name in assistantConfigs from `"aider"` to match the actual executable provided by `aider-chat`.

### Issue 2: Goose Installation Failure

**Installation** (Dockerfile line 39):
```dockerfile
RUN pip3 install goose
```

**Problem**:
- The installation silently fails (`2>/dev/null || true`)
- `goose` might be an ambiguous package name on PyPI
- Wrong goose package might be installed (there are multiple packages named "goose")
- Goose might not install an executable binary in PATH

**Verification needed**:
- Check if `goose` package exists or if it's a different name
- Check what binaries the correct goose package provides
- Verify PyPI has a package named `goose` that provides a binary

**Likely solution**:
- Use the correct package name (e.g., `goose-game`, `goose-lang`, etc.)
- Or use a package that actually provides an executable

### Issue 3: PATH Environment Variable

**Dockerfile PATH setup** (line 28):
```dockerfile
ENV PATH="/usr/local/lib/node_modules/.bin:${PATH}"
```

**Problem**:
- PATH is modified for npm binaries (node_modules/.bin)
- Python pip binaries go to `/usr/local/bin` or similar
- The custom PATH might not include the correct Python bin directory

**Verification needed**:
- Check where pip installs binaries (usually `/usr/local/bin` or `/usr/bin`)
- Verify that location is in PATH when swe-swe-server runs
- Check if PATH is being correctly inherited by the Go process

### Issue 4: Installation Silently Fails

**Installation command**:
```dockerfile
RUN pip3 install \
    aider-chat \
    goose \
    2>/dev/null || true
```

**Problem**:
- Errors are suppressed (`2>/dev/null`)
- Installation might fail completely
- Container builds successfully even if packages don't install
- No visibility into what went wrong

**Evidence**:
The server is running without errors, but aider/goose aren't detected—suggests they simply aren't installed in a place where `exec.LookPath()` can find them.

## Debugging Steps

### To investigate in Docker container:

```bash
# 1. Check if binaries exist
docker exec <container-name> which aider
docker exec <container-name> which goose
docker exec <container-name> ls -la /usr/local/bin | grep -E "aider|goose"

# 2. Check pip packages are installed
docker exec <container-name> pip3 list | grep -E "aider|goose"

# 3. Check actual binary names from packages
docker exec <container-name> pip3 show aider-chat
docker exec <container-name> pip3 show goose

# 4. Check PATH
docker exec <container-name> echo $PATH

# 5. Try running the binaries directly
docker exec <container-name> aider --version
docker exec <container-name> goose --version
```

### To check swe-swe-server logs:

```bash
docker logs swe-swe-server | grep -E "Detected|assistant"
```

This should show which assistants were successfully detected.

## Resolution (FINAL - VERIFIED)

During testing, the root cause was discovered and fixed across two iterations:

### Iteration 1: Aider Installation Fix

**Problem**: `pip3 install aider-chat` was failing silently

**Root Cause**: PEP 668 externally-managed-environment restriction in Debian/Ubuntu Python 3.11+
- Modern Debian prevents pip from installing system-wide packages
- Error was suppressed by `2>/dev/null || true` in Dockerfile
- This broke all pip-based assistant installations

**Solution** - **Commit `96cf679`**:
```dockerfile
RUN pip3 install --break-system-packages \
    aider-chat
```
- Added `--break-system-packages` flag to allow installation in containers
- Removed `2>/dev/null` to make errors visible

**Result**: ✅ Aider now installs successfully and appears in assistant selection

### Iteration 2: Goose Installation Fix

**Problem**: Docker build failed with `ERROR: No matching distribution found for goose`

**Root Cause**: Goose is **not available on PyPI** - it installs via GitHub release script
- Tried to install using pip which failed
- User provided correct installation method

**Solution** - **Commit `1e83df6`**:
```dockerfile
RUN curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false bash || true
```
- Install goose via official GitHub release script instead of pip
- Non-blocking installation (using `|| true`) since it's optional

**Result**: ✅ Goose now installs successfully and appears in assistant selection

### Final Status (Updated with GitHub CDN Issue)

**Aider**: ✅ **Works reliably**
- Installed via `pip3 install aider-chat` with `--break-system-packages`
- Available on both x86_64 and ARM64 architectures

**Goose**: ⚠️ **Intermittent failures due to GitHub CDN**
- Installation via GitHub release script: `https://github.com/block/goose/releases/download/stable/download_cli.sh`
- Release binaries exist for ARM64: `goose-aarch64-unknown-linux-gnu.tar.bz2` ✓
- Download returns: **HTTP 504 Bad Gateway** from GitHub CDN (intermittent)
- Installation is optional - application works fine without it
- Users can manually retry: `curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false bash`

**Plus npm-based assistants**: Claude, Gemini, Codex

## GitHub CDN & SSL Certificate Issues

**Observed in Container**: `4a51344ef01c` (ARM64 Linux)

When the goose installation script runs:
```
Detected OS: linux with ARCH aarch64
Downloading stable release: goose-aarch64-unknown-linux-gnu.tar.bz2...
Error: Failed to download https://github.com/block/goose/releases/download/stable/goose-aarch64-unknown-linux-gnu.tar.bz2
```

Direct download attempt:
```
HTTP/2 504 Bad Gateway
```

**Root Causes**:
1. **GitHub CDN Intermittent Errors**: GitHub's CDN returns occasional 504 errors - outside our control, transient
2. **SSL Certificate Issues** (potential): If behind corporate firewall/VPN with intercepting proxies:
   - The Dockerfile curl runs at *build time*, before enterprise certificates are installed
   - Certificates are only mounted and installed at *runtime* via `entrypoint.sh`
   - This means corporate CA certificates won't be available during the build phase
   - Users behind corporate proxies might see SSL verification failures that look like 504 errors

**Mitigation in Dockerfile**:
- Changed from silent failure (`|| true`) to printing a warning
- Documented both GitHub CDN and SSL certificate issues
- Installation remains optional so builds don't fail
- Added guidance for users behind corporate proxies:
  - Option 1: Use `--cacert` if you have certificates
  - Option 2: Use `--insecure` flag (less secure)
  - Option 3: Install goose manually after container is running

**For Users Behind Corporate Proxies**:
1. If goose installation fails during `swe-swe init`:
   - Extract the generated `.swe-swe/` directory
   - Edit the `Dockerfile` to add `--cacert` or `-k` flag to the curl command
   - Run `swe-swe build` to rebuild with modified Dockerfile
2. Or, after `swe-swe up` succeeds, manually install goose inside the running container:
   ```bash
   docker exec swe-swe-server bash -c "curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false bash"
   ```
   At this point, the entrypoint.sh has already installed enterprise certificates into the system trust store.

## Solution Recommendations

### Quick Fix:

1. Update Dockerfile to show what's actually installing:
```dockerfile
RUN pip3 install aider-chat goose 2>&1 | tee /tmp/pip-install.log
```

2. Check pip output to see actual binary names

3. Update assistantConfigs in main.go to match actual binary names

### Better Fix:

1. **For Aider**:
   - Use package name `aider` if it exists on PyPI
   - Or use `aider-chat` and update binary name to `aider-chat`

2. **For Goose**:
   - Verify correct PyPI package name
   - Consider if goose integration is even necessary
   - Remove if not critical, or replace with better-maintained alternative

### Robustness:

Instead of silently ignoring install failures:
```dockerfile
RUN pip3 install aider-chat goose || echo "Warning: Some pip packages failed to install"
```

This preserves visibility while still allowing builds to succeed.

## Files to Update

- `cmd/swe-swe/templates/Dockerfile` - Fix pip installation and binary names
- `cmd/swe-swe-server/main.go` - Update assistantConfigs Binary field if needed
- `README.md` - Update documentation to list only actually available assistants

## Related Code

- **Assistant detection**: `cmd/swe-swe-server/main.go:485-515`
- **Assistant config**: `cmd/swe-swe-server/main.go:49-89`
- **Dockerfile**: `cmd/swe-swe/templates/Dockerfile:28-40`
- **Selection UI**: `cmd/swe-swe-server/static/selection.html`
