/**
 * Pure URL construction functions for terminal-ui.
 * All functions are side-effect free and take explicit parameters.
 * @module url-builder
 */

/**
 * Get the base URL from location object.
 * @param {{protocol: string, hostname: string, port: string}} location - Location-like object
 * @returns {string} Base URL (e.g., "https://example.com" or "http://localhost:8080")
 */
export function getBaseUrl(location) {
    const { protocol, hostname, port } = location;
    return port ? `${protocol}//${hostname}:${port}` : `${protocol}//${hostname}`;
}

/**
 * Build the VSCode URL.
 * @param {string} baseUrl - The base URL
 * @param {string} workDir - The workspace directory (optional)
 * @returns {string} VSCode URL with optional folder parameter
 */
export function buildVSCodeUrl(baseUrl, workDir) {
    if (workDir) {
        return `${baseUrl}/vscode/?folder=${encodeURIComponent(workDir)}`;
    }
    return `${baseUrl}/vscode/`;
}

/**
 * Build the shell session URL.
 * @param {{baseUrl: string, shellUUID: string, parentUUID: string, debug: boolean}} config - Shell URL config
 * @returns {string} Shell session URL
 */
export function buildShellUrl(config) {
    const { baseUrl, shellUUID, parentUUID, debug } = config;
    const debugQS = debug ? '&debug=1' : '';
    return `${baseUrl}/session/${shellUUID}?assistant=shell&parent=${encodeURIComponent(parentUUID)}${debugQS}`;
}

/**
 * Build the app preview URL.
 * @param {{protocol: string, hostname: string, port: string}} location - Location-like object
 * @returns {string} Preview URL with port prefixed by '1'
 */
export function buildPreviewUrl(location) {
    const { protocol, hostname, port } = location;
    const previewPort = '1' + (port || '80');
    return `${protocol}//${hostname}:${previewPort}`;
}

/**
 * Get the debug query string.
 * @param {boolean} debugMode - Whether debug mode is enabled
 * @returns {string} Query string ("?debug=1" or "")
 */
export function getDebugQueryString(debugMode) {
    return debugMode ? '?debug=1' : '';
}
