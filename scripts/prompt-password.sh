#!/bin/bash
# Prompt user for password with non-echoing input and hardening level
# Validates password matches confirmation
# Returns password and hardening level separated by newline

set -e

# Phase 1: Password prompt
while true; do
    read -sp "Enter swe-swe password: " PASSWORD
    echo ""
    echo ""

    if [ -z "$PASSWORD" ]; then
        echo "ERROR: Password cannot be empty" >&2
        echo ""
        continue
    fi

    read -sp "Confirm password: " PASSWORD_CONFIRM
    echo ""
    echo ""

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

# Phase 2: Hardening level prompt
echo ""
echo "Choose OS hardening level:"
echo "  (1) None"
echo "  (2) Moderate (default) - UFW, Fail2ban, auto-updates, SSH hardening"
echo "  (3) Comprehensive - All moderate + auditd, AIDE, rkhunter, kernel hardening"
echo ""

while true; do
    read -p "Hardening level (1-3, default 2): " HARDENING_CHOICE
    HARDENING_CHOICE=${HARDENING_CHOICE:-2}

    case "$HARDENING_CHOICE" in
        1)
            HARDENING_LEVEL="none"
            break
            ;;
        2)
            HARDENING_LEVEL="moderate"
            break
            ;;
        3)
            HARDENING_LEVEL="comprehensive"
            break
            ;;
        *)
            echo "ERROR: Invalid choice. Please enter 1, 2, or 3."
            echo ""
            continue
            ;;
    esac
done

# Output password and hardening level (one per line)
echo "$PASSWORD"
echo "$HARDENING_LEVEL"
