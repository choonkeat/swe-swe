#!/bin/bash
# Prompt user for password with non-echoing input
# Validates password matches confirmation
# Returns password on stdout, or empty string on error

set -e

while true; do
    read -sp "Enter swe-swe password: " PASSWORD
    echo ""

    if [ -z "$PASSWORD" ]; then
        echo "ERROR: Password cannot be empty" >&2
        echo ""
        continue
    fi

    read -sp "Confirm password: " PASSWORD_CONFIRM
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

    # Passwords match, output and exit
    echo "$PASSWORD"
    break
done
