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
 * Build the shell session URL.
 * @param {{baseUrl: string, shellUUID: string, parentUUID: string, debug: boolean}} config - Shell URL config
 * @returns {string} Shell session URL
 */
export function buildShellUrl(config) {
    const { baseUrl, shellUUID, parentUUID, debug } = config;
    return buildSessionPageUrl(baseUrl, shellUUID, {
        assistant: 'shell',
        parent: parentUUID,
        debug,
    });
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
 * Build the port-based files URL (cross-origin, per-port).
 * @param {{protocol: string, hostname: string}} location - Location-like object
 * @param {number|null} filesProxyPort - The per-session files proxy port
 * @returns {string|null} Port-based files URL, or null if no port
 */
export function buildPortBasedFilesUrl(location, filesProxyPort) {
    if (!filesProxyPort) return null;
    return `${location.protocol}//${location.hostname}:${filesProxyPort}`;
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
 * Decide whether the current page was loaded through the reverse tunnel.
 * Subdomain proxy URLs ("{port}.{publicHostname}") are only reachable when
 * the browser is actually talking to the tunnel host. When the same swe-swe
 * is reached directly (localhost, LAN IP, Tailscale name, etc.) the subdomain
 * form is unreachable, so callers should fall back to port-based / path-based
 * URLs. This is a function of HOW the page was loaded, not of whether the
 * server happens to be in tunnel mode.
 * @param {{hostname: string}} location - Location-like object
 * @param {string} publicHostname - Tunnel public hostname (e.g. "abc-tunnel.example.com"), or "" when not in tunnel mode
 * @returns {boolean} true when location.hostname is the tunnel host or a subdomain of it
 */
export function accessedViaTunnel(location, publicHostname) {
    if (!publicHostname) return false;
    const h = location.hostname;
    return h === publicHostname || h.endsWith('.' + publicHostname);
}

/**
 * Decide the Domain attribute for the swe-swe-theme cookie. The cookie is
 * host-scoped by default (empty Domain). In tunnel mode the per-port tabs
 * (Files, Preview, Agent Chat) live at "{port}.{publicHostname}" subdomains,
 * so a host-only cookie set on the apex is NOT sent to them -- md-serve on the
 * Files subdomain would then ignore the swe-swe theme and follow the OS
 * prefers-color-scheme. Returning publicHostname scopes the cookie to the
 * parent domain so every subdomain receives it, mirroring how the server
 * scopes the auth cookie (resolveCookieDomain in auth.go). Local mode
 * (localhost, LAN IP, Tailscale name, or any host that is not the tunnel host)
 * stays host-only -- a Domain there would be invalid or pointless.
 * @param {{hostname: string}} location - Location-like object
 * @param {string} publicHostname - Tunnel public hostname, or "" when not in tunnel mode
 * @returns {string} the Domain to stamp, or "" for a host-only cookie
 */
export function themeCookieDomain(location, publicHostname) {
    return accessedViaTunnel(location, publicHostname) ? publicHostname : '';
}

/**
 * Build the subdomain-based preview URL for tunnel mode. When swe-swe runs
 * behind a reverse tunnel (SWE_PUBLIC_HOSTNAME / --public-hostname), browser
 * requests to "{port}.{publicHostname}" are demuxed by the tunnel server
 * and forwarded to the right session's target port. The raw target port is
 * the leftmost subdomain label -- proxyPortOffset does not apply.
 * @param {{protocol: string}} location - Location-like object (only protocol used)
 * @param {number|null} targetPort - The per-session preview target port
 * @param {string} publicHostname - Public hostname (e.g. "abc-tunnel.example.com")
 * @returns {string|null} Subdomain preview URL, or null if either input is missing
 */
export function buildSubdomainPreviewUrl(location, targetPort, publicHostname) {
    if (!targetPort || !publicHostname) return null;
    return `${location.protocol}//${targetPort}.${publicHostname}`;
}

/**
 * Build the subdomain-based agent chat URL for tunnel mode. Same shape as
 * buildSubdomainPreviewUrl but for the agent-chat target port.
 * @param {{protocol: string}} location - Location-like object (only protocol used)
 * @param {number|null} targetPort - The per-session agent chat target port
 * @param {string} publicHostname - Public hostname (e.g. "abc-tunnel.example.com")
 * @returns {string|null} Subdomain agent chat URL, or null if either input is missing
 */
export function buildSubdomainAgentChatUrl(location, targetPort, publicHostname) {
    if (!targetPort || !publicHostname) return null;
    return `${location.protocol}//${targetPort}.${publicHostname}`;
}

/**
 * Build the subdomain-based files URL for tunnel mode. Same shape as
 * buildSubdomainPreviewUrl but for the files target port.
 * @param {{protocol: string}} location - Location-like object (only protocol used)
 * @param {number|null} targetPort - The per-session files target port
 * @param {string} publicHostname - Public hostname (e.g. "abc-tunnel.example.com")
 * @returns {string|null} Subdomain files URL, or null if either input is missing
 */
export function buildSubdomainFilesUrl(location, targetPort, publicHostname) {
    if (!targetPort || !publicHostname) return null;
    return `${location.protocol}//${targetPort}.${publicHostname}`;
}

/**
 * Build a subdomain-based proxy URL by combining the subdomain base with a
 * target path. Mirrors buildPortBasedProxyUrl semantics.
 * @param {{protocol: string}} location - Location-like object
 * @param {number|null} targetPort - The per-session preview target port
 * @param {string} publicHostname - Public hostname
 * @param {string} targetURL - The logical target URL (optional)
 * @returns {string|null} Subdomain proxy URL for use as iframe src, or null if missing input
 */
export function buildSubdomainProxyUrl(location, targetPort, publicHostname, targetURL) {
    const base = buildSubdomainPreviewUrl(location, targetPort, publicHostname);
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
 * Build a session page URL with canonical query param ordering.
 * Mirrors Go SessionPageQuery.Encode() -- keep both in sync.
 *
 * @param {string} baseUrl - Base URL (e.g. "http://localhost:8080")
 * @param {string} sessionUUID - Session UUID for the path segment
 * @param {{assistant: string, session?: string, name?: string, branch?: string, pwd?: string, parent?: string, debug?: boolean}} params
 * @returns {string} Full session page URL
 */
export function buildSessionPageUrl(baseUrl, sessionUUID, params) {
    const p = new URLSearchParams();
    if (params.assistant) p.set('assistant', params.assistant);
    if (params.session && params.session !== 'terminal') p.set('session', params.session);
    if (params.name) p.set('name', params.name);
    if (params.branch) p.set('branch', params.branch);
    if (params.pwd && params.pwd !== '/workspace') p.set('pwd', params.pwd);
    if (params.parent) p.set('parent', params.parent);
    if (params.debug) p.set('debug', '1');
    const qs = p.toString();
    return `${baseUrl}/session/${sessionUUID}${qs ? '?' + qs : ''}`;
}

/**
 * Get the debug query string.
 * @param {boolean} debugMode - Whether debug mode is enabled
 * @returns {string} Query string ("?debug=1" or "")
 */
export function getDebugQueryString(debugMode) {
    return debugMode ? '?debug=1' : '';
}

/**
 * Preview host-demux: translate a logical vhost host (as the user's compose
 * serves it) into a flat reachable DNS label. "app1.lvh.me:5000" under suffix
 * "lvh.me" becomes "app1-5000"; without a port it is just "app1". Only a single
 * label in front of the suffix is supported in v1 (nested "a.b.lvh.me" is
 * rejected). Returns null when the host is not under the given suffix.
 * @param {string} logicalHost - e.g. "app1.lvh.me" or "app1.lvh.me:5000"
 * @param {string} suffix - the logical vhost suffix, e.g. "lvh.me"
 * @returns {string|null} the reachable label, or null
 */
export function logicalToVhostLabel(logicalHost, suffix) {
    if (!logicalHost || !suffix) return null;
    let host = logicalHost;
    let port = '';
    const colon = host.lastIndexOf(':');
    if (colon >= 0) {
        port = host.slice(colon + 1);
        host = host.slice(0, colon);
    }
    const dotSuffix = '.' + suffix;
    if (!host.endsWith(dotSuffix)) return null;
    const name = host.slice(0, host.length - dotSuffix.length);
    // Flat labels only: the name must be a single DNS label (no dots).
    if (name === '' || name.includes('.')) return null;
    return port ? `${name}-${port}` : name;
}

/**
 * Build the browser-facing reachable origin for a vhost label. The per-session
 * listener at "{label}.{reach}:{proxyPort}" demuxes the leftmost label.
 * @param {string} label - reachable label, e.g. "app1-5000"
 * @param {string} reach - reach domain, e.g. "x.sslip.io" or "lvh.me"
 * @param {number} proxyPort - the preview proxy port, e.g. 23000
 * @param {string} protocol - location protocol, e.g. "http:" or "https:"
 * @returns {string|null} the origin, or null on missing input
 */
export function buildVhostPreviewUrl(label, reach, proxyPort, protocol) {
    if (!label || !reach || !proxyPort) return null;
    return `${protocol}//${label}.${reach}:${proxyPort}`;
}

/**
 * Parse a user-typed logical URL bar value into structured parts. Accepts an
 * optional scheme, a "{name}.{suffix}[:{port}]" host, and a trailing
 * path?query#hash. Returns null when the host is not under the suffix or is not
 * a single label in front of it.
 * @param {string} raw - e.g. "app1.lvh.me:5000/path?q#h"
 * @param {string} suffix - the logical vhost suffix, e.g. "lvh.me"
 * @returns {{logicalHost: string, port: number|null, label: string, pathSuffix: string}|null}
 */
export function parseLogicalInput(raw, suffix) {
    if (!raw || !suffix) return null;
    let rest = raw.trim();
    // Strip an optional scheme.
    const schemeIdx = rest.indexOf('://');
    if (schemeIdx >= 0) rest = rest.slice(schemeIdx + 3);
    // Split host[:port] from the path suffix at the first slash.
    let hostPort = rest;
    let pathSuffix = '/';
    const slash = rest.indexOf('/');
    if (slash >= 0) {
        hostPort = rest.slice(0, slash);
        pathSuffix = rest.slice(slash);
    }
    let host = hostPort;
    let port = null;
    const colon = hostPort.lastIndexOf(':');
    if (colon >= 0) {
        const portStr = hostPort.slice(colon + 1);
        host = hostPort.slice(0, colon);
        const n = parseInt(portStr, 10);
        if (Number.isNaN(n)) return null;
        port = n;
    }
    const label = logicalToVhostLabel(hostPort, suffix);
    if (label === null) return null;
    return { logicalHost: host, port, label, pathSuffix };
}
