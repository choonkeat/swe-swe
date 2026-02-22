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
 * Build the app preview base URL (path-based, same origin).
 * @param {string} baseUrl - The base URL of swe-swe-server
 * @param {string} sessionUUID - Session UUID
 * @returns {string|null} Preview proxy base URL, or null if no sessionUUID
 */
export function buildPreviewUrl(baseUrl, sessionUUID) {
    if (!sessionUUID) return null;
    return `${baseUrl}/proxy/${sessionUUID}/preview`;
}

/**
 * Build a proxy URL by combining the preview base with a target path.
 * @param {string} baseUrl - The base URL of swe-swe-server
 * @param {string} sessionUUID - Session UUID
 * @param {string} targetURL - The logical target URL (optional)
 * @returns {string|null} Proxy URL for use as iframe src, or null if no sessionUUID
 */
export function buildProxyUrl(baseUrl, sessionUUID, targetURL) {
    const base = buildPreviewUrl(baseUrl, sessionUUID);
    if (!base) return null;
    if (!targetURL) return base + '/';
    try {
        const parsed = new URL(targetURL);
        return base + parsed.pathname + parsed.search + parsed.hash;
    } catch {
        return base + (targetURL.startsWith('/') ? targetURL : '/' + targetURL);
    }
}

/**
 * Build the agent chat URL (path-based, same origin).
 * @param {string} baseUrl - The base URL of swe-swe-server
 * @param {string} sessionUUID - Session UUID
 * @returns {string|null} Agent chat proxy URL, or null if no sessionUUID
 */
export function buildAgentChatUrl(baseUrl, sessionUUID) {
    if (!sessionUUID) return null;
    return `${baseUrl}/proxy/${sessionUUID}/agentchat`;
}

/**
 * Build the port-based preview URL (cross-origin, per-port).
 * @param {{protocol: string, hostname: string}} location - Location-like object
 * @param {number|null} previewProxyPort - The per-session preview proxy port
 * @returns {string|null} Port-based preview URL, or null if no port
 */
export function buildPortBasedPreviewUrl(location, previewProxyPort) {
    if (!previewProxyPort) return null;
    return `${location.protocol}//${location.hostname}:${previewProxyPort}`;
}

/**
 * Build the port-based agent chat URL (cross-origin, per-port).
 * @param {{protocol: string, hostname: string}} location - Location-like object
 * @param {number|null} agentChatProxyPort - The per-session agent chat proxy port
 * @returns {string|null} Port-based agent chat URL, or null if no port
 */
export function buildPortBasedAgentChatUrl(location, agentChatProxyPort) {
    if (!agentChatProxyPort) return null;
    return `${location.protocol}//${location.hostname}:${agentChatProxyPort}`;
}

/**
 * Build a port-based proxy URL by combining the port-based base with a target path.
 * @param {{protocol: string, hostname: string}} location - Location-like object
 * @param {number|null} previewProxyPort - The per-session preview proxy port
 * @param {string} targetURL - The logical target URL (optional)
 * @returns {string|null} Port-based proxy URL for use as iframe src, or null if no port
 */
export function buildPortBasedProxyUrl(location, previewProxyPort, targetURL) {
    const base = buildPortBasedPreviewUrl(location, previewProxyPort);
    if (!base) return null;
    if (!targetURL) return base + '/';
    try {
        const parsed = new URL(targetURL);
        return base + parsed.pathname + parsed.search + parsed.hash;
    } catch {
        return base + (targetURL.startsWith('/') ? targetURL : '/' + targetURL);
    }
}

/**
 * Get the debug query string.
 * @param {boolean} debugMode - Whether debug mode is enabled
 * @returns {string} Query string ("?debug=1" or "")
 */
export function getDebugQueryString(debugMode) {
    return debugMode ? '?debug=1' : '';
}
