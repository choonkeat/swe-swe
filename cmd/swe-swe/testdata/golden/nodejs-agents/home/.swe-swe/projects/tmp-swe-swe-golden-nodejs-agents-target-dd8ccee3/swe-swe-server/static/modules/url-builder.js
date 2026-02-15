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
 * @param {number|string} previewPort - Explicit preview port (optional)
 * @returns {string} Preview URL
 */
export function buildPreviewUrl(location, previewPort) {
    const { protocol, hostname, port } = location;
    if (previewPort) {
        return `${protocol}//${hostname}:${previewPort}`;
    }
    const fallbackPort = '2' + (port || '80');
    return `${protocol}//${hostname}:${fallbackPort}`;
}

/**
 * Build a proxy URL by combining the preview base with the path from a target URL.
 * @param {{protocol: string, hostname: string, port: string}} location - Location-like object
 * @param {number|string} previewPort - Explicit preview port (optional)
 * @param {string} targetURL - The logical target URL (optional)
 * @returns {string} Proxy URL for use as iframe src
 */
export function buildProxyUrl(location, previewPort, targetURL) {
    const base = buildPreviewUrl(location, previewPort);
    if (!targetURL) return base + '/';
    try {
        const parsed = new URL(targetURL);
        return base + parsed.pathname + parsed.search + parsed.hash;
    } catch {
        return base + (targetURL.startsWith('/') ? targetURL : '/' + targetURL);
    }
}

/**
 * Build the agent chat URL.
 * @param {{protocol: string, hostname: string}} location - Location-like object
 * @param {number|string} agentChatPort - Agent chat app port
 * @returns {string|null} Agent chat proxy URL, or null if no port
 */
export function buildAgentChatUrl(location, agentChatPort) {
    const { protocol, hostname } = location;
    if (agentChatPort) {
        return `${protocol}//${hostname}:${'2' + agentChatPort}`;
    }
    return null;
}

/**
 * Get the debug query string.
 * @param {boolean} debugMode - Whether debug mode is enabled
 * @returns {string} Query string ("?debug=1" or "")
 */
export function getDebugQueryString(debugMode) {
    return debugMode ? '?debug=1' : '';
}
