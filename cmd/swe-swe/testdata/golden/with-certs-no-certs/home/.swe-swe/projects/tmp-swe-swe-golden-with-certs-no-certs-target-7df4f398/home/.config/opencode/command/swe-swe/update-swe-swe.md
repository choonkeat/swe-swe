---
description: Update workspace swe-swe files after a version upgrade
---

# Update swe-swe Workspace Files

Update stale swe-swe workspace files after a version upgrade using three-way merge to preserve user customizations.

## Steps

### 1. Extract latest templates

```bash
swe-swe-server --dump-container-templates /workspace/.swe-swe/updated
```

### 2. Three-way merge each file

For each file in `.swe-swe/updated/`, compare three versions:
- **Baseline**: `.swe-swe/baseline/<path>` (snapshot from original init)
- **Current**: `<path>` (what's on disk now, possibly user-modified)
- **New**: `.swe-swe/updated/<path>` (latest from this binary)

Apply these rules:

| Baseline | Current | New | Action |
|----------|---------|-----|--------|
| missing | exists | exists | Show diff current↔new, ask user before replacing |
| = current | any | any | Auto-replace with new (user hasn't modified) |
| = new | any | any | Skip (template unchanged in this version) |
| ≠ current ≠ new | — | — | Three-way merge with your judgment (see notes below) |

### 3. Special merge notes

**`.mcp.json`**: This is a JSON object with `mcpServers`. Merge the objects:
- Keep any user-added server entries
- Update/replace entries that came from swe-swe (compare with baseline)
- Add any new entries from the new template

**`AGENTS.md`**: Has structured sections:
- **Commands table** and **Documentation list**: Replace from new template
- **Current Setup section**: Preserve entirely (agent-written content)

### 4. Update baselines

After merging each file, copy the **new** template version into `.swe-swe/baseline/<path>` so future updates have the correct baseline.

### 5. Clean up

```bash
rm -rf /workspace/.swe-swe/updated
```

### 6. Report

Summarize what was updated, what was skipped (unchanged), and any files that needed manual merge decisions.
