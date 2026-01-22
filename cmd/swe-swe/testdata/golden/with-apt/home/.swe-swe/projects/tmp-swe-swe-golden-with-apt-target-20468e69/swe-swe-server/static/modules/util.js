/**
 * Pure utility functions for terminal-ui.
 * All functions are side-effect free and have no dependencies.
 * @module util
 */

/**
 * Format a duration in milliseconds to human-readable string.
 * @param {number} ms - Duration in milliseconds
 * @returns {string} Formatted duration (e.g., "1h 2m 3s", "5m 30s", "45s")
 */
export function formatDuration(ms) {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    if (hours > 0) {
        return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
    } else if (minutes > 0) {
        return `${minutes}m ${seconds % 60}s`;
    }
    return `${seconds}s`;
}

/**
 * Format a file size in bytes to human-readable string.
 * @param {number} bytes - Size in bytes
 * @returns {string} Formatted size (e.g., "500 B", "1.5 KB", "2.3 MB")
 */
export function formatFileSize(bytes) {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Escape HTML special characters to prevent XSS.
 * Pure implementation without DOM dependency.
 * @param {string} text - Text to escape
 * @returns {string} HTML-escaped text
 */
export function escapeHtml(text) {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

/**
 * Escape special shell characters in a filename.
 * @param {string} name - Filename to escape
 * @returns {string} Shell-escaped filename
 */
export function escapeFilename(name) {
    // Escape special shell characters
    return name.replace(/(['"\\$`!])/g, '\\$1').replace(/ /g, '\\ ');
}

/**
 * Parse markdown-style links from a string.
 * @param {string} linksStr - String containing markdown links like "[text](url)"
 * @returns {Array<{text: string, url: string}>} Array of parsed link objects
 */
export function parseLinks(linksStr) {
    if (!linksStr) return [];
    // Parse markdown-style links: [text](url)
    // Pattern handles escaped brackets if needed
    const regex = /\[([^\]]+)\]\(([^)]+)\)/g;
    const links = [];
    let match;
    while ((match = regex.exec(linksStr)) !== null) {
        links.push({ text: match[1], url: match[2] });
    }
    return links;
}
