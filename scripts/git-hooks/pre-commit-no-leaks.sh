#!/bin/bash
# Refuse commits whose staged diff contains a value from .swe-swe/env.
#
# Generic by design: this script does not contain any literal secrets.
# The list of values to protect is whatever has been put in .swe-swe/env
# (which is gitignored). Add a new line to .swe-swe/env and the hook
# starts protecting that value automatically.
#
# Install: bash scripts/git-hooks/install.sh

set -euo pipefail

ENV_FILE="$(git rev-parse --show-toplevel)/.swe-swe/env"
[ -f "$ENV_FILE" ] || exit 0

STAGED="$(git diff --cached --no-color --unified=0)"
[ -z "$STAGED" ] && exit 0

EXIT=0

while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|\#*) continue ;; esac
    value="${line#*=}"
    [ -z "$value" ] && continue
    # Strip optional surrounding quotes.
    value="${value%\"}"; value="${value#\"}"
    value="${value%\'}"; value="${value#\'}"
    # Skip values that look like PATH-style or system paths to avoid
    # tripping on every commit that mentions /usr/local/, etc.
    case "$value" in
        '$'*|*':$'*|/usr/*|/bin/*|/opt/*|/etc/*|/var/*) continue ;;
    esac
    # Skip very short values (high false-positive risk).
    [ "${#value}" -lt 8 ] && continue

    if printf '%s\n' "$STAGED" | grep --fixed-strings --quiet -- "$value"; then
        # Don't echo the value -- the whole point is to keep it out of
        # logs and terminal scrollback. Name the key so the developer
        # knows which entry tripped.
        key="${line%%=*}"
        echo "ERROR: staged change contains the value of \$${key} from .swe-swe/env" >&2
        echo "       (value not echoed; check .swe-swe/env or your staged diff)" >&2
        EXIT=1
    fi
done < "$ENV_FILE"

exit $EXIT
