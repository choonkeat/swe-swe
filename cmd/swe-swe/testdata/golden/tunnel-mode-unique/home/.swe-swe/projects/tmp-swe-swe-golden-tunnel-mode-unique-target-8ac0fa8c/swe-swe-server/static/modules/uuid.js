/**
 * Pure UUID derivation functions for terminal-ui.
 * All functions are side-effect free and have no dependencies.
 * @module uuid
 */

/**
 * djb2 hash function - fast and produces good distribution.
 * @param {string} str - String to hash
 * @param {number} [seed=5381] - Initial hash seed
 * @returns {number} 32-bit unsigned hash value
 */
export function djb2Hash(str, seed = 5381) {
    let hash = seed;
    for (let i = 0; i < str.length; i++) {
        hash = ((hash << 5) + hash) + str.charCodeAt(i);
        hash = hash >>> 0; // Convert to unsigned 32-bit integer
    }
    return hash;
}

/**
 * Derive a deterministic shell UUID from parent session UUID.
 * Uses djb2 hash algorithm (works in both HTTP and HTTPS contexts).
 * @param {string} parentUUID - The parent session's UUID
 * @returns {string} A deterministic UUID v4-formatted string
 */
export function deriveShellUUID(parentUUID) {
    const input = 'shell:' + parentUUID;

    // Generate enough hash values to fill a UUID (128 bits = 4 x 32-bit)
    const h1 = djb2Hash(input);
    const h2 = djb2Hash(input, h1);
    const h3 = djb2Hash(input, h2);
    const h4 = djb2Hash(input, h3);

    // Format as UUID: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
    const hex = (h1.toString(16).padStart(8, '0') +
                 h2.toString(16).padStart(8, '0') +
                 h3.toString(16).padStart(8, '0') +
                 h4.toString(16).padStart(8, '0'));

    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-${((parseInt(hex.slice(16, 18), 16) & 0x3f) | 0x80).toString(16)}${hex.slice(18, 20)}-${hex.slice(20, 32)}`;
}
