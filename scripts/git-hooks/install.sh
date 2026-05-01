#!/bin/bash
# One-time hook installer. Run after a fresh clone:
#     bash scripts/git-hooks/install.sh
#
# Idempotent: safe to re-run.

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
HOOK_SRC="$ROOT/scripts/git-hooks/pre-commit-no-leaks.sh"
HOOK_DST="$ROOT/.git/hooks/pre-commit"

chmod +x "$HOOK_SRC"
ln -sf "$HOOK_SRC" "$HOOK_DST"

echo "Installed: ${HOOK_DST} -> ${HOOK_SRC}"
