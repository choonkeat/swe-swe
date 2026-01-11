#!/bin/bash
# Prompt user for password with non-echoing input and hardening level
# Validates password matches confirmation
# Returns password and hardening level separated by newline

set -e

# Phase 1: Password prompt
while true; do
    read -sp "Set new swe-swe password: " PASSWORD
    echo "" >&2

    if [ -z "$PASSWORD" ]; then
        echo "ERROR: Password cannot be empty" >&2
        echo ""
        continue
    fi

    echo "" >&2
    read -sp "Confirm new password: " PASSWORD_CONFIRM
    echo "" >&2

    if [ -z "$PASSWORD_CONFIRM" ]; then
        echo "ERROR: Confirmation password cannot be empty" >&2
        echo ""
        continue
    fi

    if [ "$PASSWORD" != "$PASSWORD_CONFIRM" ]; then
        echo "ERROR: Passwords do not match" >&2
        echo ""
        continue
    fi

    # Passwords match, break out of loop
    break
done

# Phase 2: OS hardening prompt
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

# Output password and hardening level (one per line)
echo "$PASSWORD"
echo "$HARDENING_LEVEL"
