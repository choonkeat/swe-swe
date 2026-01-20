#!/bin/bash
# Prompt user for hardening level and git URL
# Can skip prompts if env vars are already set: ENABLE_HARDENING, GIT_CLONE_URL
# Password is auto-generated at boot and shown in MOTD
# Returns values separated by newline

set -e

# Phase 1: OS hardening prompt (skip if ENABLE_HARDENING is set)
if [ -z "${ENABLE_HARDENING:-}" ]; then
    echo "Enable OS hardening? (UFW firewall, Fail2ban, SSH hardening, auto-updates)" >&2

    while true; do
        read -p "Enable hardening? (y/n, default y): " HARDENING_CHOICE
        HARDENING_CHOICE=${HARDENING_CHOICE:-y}

        case "$HARDENING_CHOICE" in
            y|Y|yes|Yes|YES)
                HARDENING_LEVEL="yes"
                break
                ;;
            n|N|no|No|NO)
                HARDENING_LEVEL="no"
                break
                ;;
            *)
                echo "ERROR: Please enter 'y' or 'n'." >&2
                echo ""
                continue
                ;;
        esac
    done
else
    HARDENING_LEVEL="$ENABLE_HARDENING"
    echo "Enable OS hardening? (UFW firewall, Fail2ban, SSH hardening, auto-updates)" >&2
    echo "Enable hardening? (y/n, default y): $HARDENING_LEVEL [from ENABLE_HARDENING]" >&2
    echo "" >&2
fi

# Phase 2: Git clone URL (skip if GIT_CLONE_URL is set, even if empty)
if [ -z "${GIT_CLONE_URL+set}" ]; then
    echo "Optionally clone a git repository to /workspace" >&2
    read -p "Git repository URL (optional, leave empty to skip): " GIT_URL
    GIT_URL=${GIT_URL:-}
else
    GIT_URL="$GIT_CLONE_URL"
    echo "Optionally clone a git repository to /workspace" >&2
    if [ -n "$GIT_URL" ]; then
        echo "Git repository URL (optional, leave empty to skip): $GIT_URL [from GIT_CLONE_URL]" >&2
    else
        echo "Git repository URL (optional, leave empty to skip): [skip, from GIT_CLONE_URL]" >&2
    fi
fi

# Output hardening level and git URL (one per line)
echo "$HARDENING_LEVEL"
echo "$GIT_URL"
