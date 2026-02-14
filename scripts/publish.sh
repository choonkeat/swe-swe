#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/npm-platforms"
DRY_RUN="${DRY_RUN:-true}"
OTP="${NPM_OTP:-}"

if [ "$DRY_RUN" = "true" ]; then
  PUBLISH_ARGS="--dry-run"
  echo "Dry-run mode (set DRY_RUN=false to publish for real)"
else
  PUBLISH_ARGS=""
  if [ -n "$OTP" ]; then
    PUBLISH_ARGS="--otp=$OTP"
  fi
  echo "Publishing for real!"
fi

echo ""

# ── 1. Publish platform packages ─────────────────────────────────────────
PLATFORMS=(linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64)

for platform in "${PLATFORMS[@]}"; do
  pkg_dir="$OUT_DIR/$platform"
  if [ ! -d "$pkg_dir" ]; then
    echo "ERROR: Missing $pkg_dir — run scripts/build-platforms.sh first"
    exit 1
  fi
  echo "→ Publishing @choonkeat/swe-swe-${platform}…"
  npm publish "$pkg_dir" --access public $PUBLISH_ARGS
done

# ── 2. Publish the main package ───────────────────────────────────────────
echo "→ Publishing swe-swe…"
npm publish "$REPO_ROOT" --access public $PUBLISH_ARGS

echo ""
echo "Done."
