#!/bin/bash
# Check source files for non-ASCII characters, respecting per-file allowlist.
# Run via: make ascii-check
#
# Each non-ASCII character is checked against scripts/ascii-allowlist.txt.
# A character is allowed only if the file's basename has an entry AND
# the character appears in that entry's allowed set.
set -euo pipefail

ALLOWLIST="scripts/ascii-allowlist.txt"
TMPOUT=$(mktemp)
trap 'rm -f "$TMPOUT"' EXIT

# Load per-file allowlist: basename -> allowed chars
declare -A ALLOWED
while IFS=$'\t' read -r glob chars; do
    [[ -z "$glob" || "$glob" == \#* ]] && continue
    ALLOWED["$glob"]="$chars"
done < "$ALLOWLIST"

echo "Checking for non-ASCII characters in source files..."

# Find all non-ASCII lines in source files
grep -rPn '[^\x00-\x7F]' \
    --include='*.go' --include='*.js' --include='*.ts' --include='*.sh' \
    --include='*.html' --include='*.css' \
    --exclude-dir=dist --exclude-dir=node_modules --exclude-dir=vendor --exclude-dir=.git \
    --exclude-dir=testdata --exclude-dir=research --exclude-dir=.playwright-mcp --exclude-dir=.claude \
    --exclude-dir=tmp --exclude-dir=agent-chat-dist --exclude-dir=.swe-swe \
    --exclude=clipboard.min.js --exclude=elm.js --exclude=elm-doc-preview.css --exclude=xterm.js \
    . 2>/dev/null | grep -v '^\./\.test-home' > "$TMPOUT" || true

FAILED=0
while IFS= read -r line; do
    [[ -z "$line" ]] && continue

    # Extract file path and basename
    filepath="${line%%:*}"
    basename="${filepath##*/}"

    # Get allowed chars for this file (if any)
    allowed="${ALLOWED[$basename]:-}"

    # Extract the content part (after file:lineno:)
    rest="${line#*:}"
    content="${rest#*:}"

    # Extract only the non-ASCII characters from this line
    non_ascii=$(LC_ALL=C.utf8 grep -oP '[^\x00-\x7F]' <<< "$content" 2>/dev/null || true)

    # Check each non-ASCII character against the allowlist
    line_ok=true
    while IFS= read -r char; do
        [[ -z "$char" ]] && continue
        if [[ -z "$allowed" ]] || ! LC_ALL=C.utf8 grep -qF "$char" <<< "$allowed" 2>/dev/null; then
            line_ok=false
            break
        fi
    done <<< "$non_ascii"

    if ! $line_ok; then
        echo "$line"
        FAILED=1
    fi
done < "$TMPOUT"

if [ "$FAILED" = "1" ]; then
    echo ""
    echo "ERROR: Non-ASCII characters found above. Run 'make ascii-fix' to auto-replace."
    exit 1
fi

echo "ASCII check passed."
