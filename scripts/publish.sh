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

# -- 0. Verify versions match ---------------------------------------------
MAIN_VERSION=$(node -p "require('$REPO_ROOT/package.json').version")
echo "Verifying version consistency (v${MAIN_VERSION})..."

MISMATCH=0
# Check platform package versions
for platform in linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64; do
  pkg_json="$OUT_DIR/$platform/package.json"
  if [ ! -f "$pkg_json" ]; then
    echo "  ERROR: Missing $pkg_json"
    MISMATCH=1
    continue
  fi
  plat_version=$(node -p "require('$pkg_json').version")
  if [ "$plat_version" != "$MAIN_VERSION" ]; then
    echo "  ERROR: $platform version=$plat_version (expected $MAIN_VERSION)"
    MISMATCH=1
  fi
done

# Check optionalDependencies point to the same version
node -e "
  var p = require('$REPO_ROOT/package.json');
  var deps = p.optionalDependencies || {};
  var ok = true;
  for (var k in deps) {
    if (deps[k] !== '$MAIN_VERSION') {
      console.log('  ERROR: optionalDependencies[' + k + ']=' + deps[k] + ' (expected $MAIN_VERSION)');
      ok = false;
    }
  }
  if (!ok) process.exit(1);
" || MISMATCH=1

if [ "$MISMATCH" -ne 0 ]; then
  echo "Version mismatch detected! Run 'make bump NEW_VERSION=x.y.z' first."
  exit 1
fi
echo "  All versions match: v${MAIN_VERSION}"
echo ""

# -- 1. Publish platform packages -----------------------------------------
PLATFORMS=(linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64)

for platform in "${PLATFORMS[@]}"; do
  pkg_dir="$OUT_DIR/$platform"
  if [ ! -d "$pkg_dir" ]; then
    echo "ERROR: Missing $pkg_dir -- run scripts/build-platforms.sh first"
    exit 1
  fi
  echo "-> Publishing @choonkeat/swe-swe-${platform}..."
  npm publish "$pkg_dir" --access public $PUBLISH_ARGS
done

# -- 2. Publish the main package -------------------------------------------
echo "-> Publishing swe-swe..."
npm publish "$REPO_ROOT" --access public $PUBLISH_ARGS

echo ""
echo "Done."
