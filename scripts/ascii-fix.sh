#!/bin/bash
# Replace accidental non-ASCII characters in source files.
# Run via: make ascii-fix
set -euo pipefail

PRUNE=(-name dist -o -name node_modules -o -name vendor -o -name .git \
       -o -name testdata -o -name research -o -name .playwright-mcp -o -name .claude \
       -o -name tmp -o -name agent-chat-dist -o -name .swe-swe)
EXCLUDE=(-not -name 'clipboard.min.js' -not -name 'elm.js' -not -name 'elm-doc-preview.css' \
         -not -path './.test-home*')

echo "Fixing non-ASCII characters in source files..."

# Pass 1: Replace em-dash, ellipsis, leq, box-drawing, checkmark, copyright in all source files
find . \( "${PRUNE[@]}" \) -prune \
    -o \( -name '*.go' -o -name '*.js' -o -name '*.ts' -o -name '*.sh' -o -name '*.html' -o -name '*.css' \) \
    "${EXCLUDE[@]}" \
    -type f -print0 | xargs -0 sed -i \
    -e $'s/\xe2\x80\x94/--/g' \
    -e $'s/\xe2\x80\xa6/.../g' \
    -e $'s/\xe2\x89\xa4/<=/g' \
    -e $'s/\xe2\x94\x80/-/g' \
    -e $'s/\xe2\x9c\x93/[ok]/g' \
    -e $'s/\xc2\xa9/(c)/g'

# Pass 2: Replace right-arrow only in .go and .sh (intentional in JS/HTML UI buttons)
find . \( "${PRUNE[@]}" \) -prune \
    -o \( -name '*.go' -o -name '*.sh' \) \
    -not -path './.test-home*' \
    -type f -print0 | xargs -0 sed -i \
    -e $'s/\xe2\x86\x92/->/g'

echo "Done. Run 'make ascii-check' to verify."
