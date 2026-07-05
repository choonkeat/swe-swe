/**
 * Pure validation functions for terminal-ui.
 * All functions are side-effect free and have no dependencies.
 * @module validation
 */

/**
 * Validate a username.
 * @param {string} name - The username to validate
 * @returns {{valid: boolean, name?: string, error?: string}} Validation result
 */
export function validateUsername(name) {
    name = name.trim();

    if (name.length === 0) {
        return { valid: false, error: 'Name cannot be empty' };
    }

    if (name.length > 16) {
        return { valid: false, error: 'Name must be 16 characters or less' };
    }

    if (!/^[a-zA-Z0-9 ]+$/.test(name)) {
        return { valid: false, error: 'Name can only contain letters, numbers, and spaces' };
    }

    return { valid: true, name: name };
}

/**
 * Validate a session name.
 * @param {string} name - The session name to validate
 * @returns {{valid: boolean, name?: string, error?: string}} Validation result
 */
export function validateSessionName(name) {
    name = name.trim();

    // Empty name is valid (clears the session name)
    if (name.length === 0) {
        return { valid: true, name: '' };
    }

    if (name.length > 256) {
        return { valid: false, error: 'Name must be 256 characters or less' };
    }

    if (!/^[a-zA-Z0-9 \-_/.@]+$/.test(name)) {
        return { valid: false, error: 'Name can only contain letters, numbers, spaces, hyphens, underscores, slashes, dots, and @' };
    }

    return { valid: true, name: name };
}
