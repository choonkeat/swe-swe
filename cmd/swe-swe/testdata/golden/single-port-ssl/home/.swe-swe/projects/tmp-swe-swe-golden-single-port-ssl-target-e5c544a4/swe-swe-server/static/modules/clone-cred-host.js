/**
 * Pure host derivation for the New Session clone credential flow.
 * Side-effect free; shared between the dialog and its unit tests.
 * @module clone-cred-host
 */

/**
 * Derive the credential host from a clone URL. Handles https/http/ssh/git
 * schemes and scp-like "git@host:path" SSH syntax. Userinfo and port are
 * stripped; the result is lowercased. Returns "" for empty or unparseable
 * input so callers can treat "no host" uniformly (and never key credentials
 * under a bogus host).
 * @param {string} url - Repository URL as typed by the user.
 * @returns {string} Bare lowercased host, or "" when none can be derived.
 */
export function parseCloneHost(url) {
    if (typeof url !== 'string') return '';
    const s = url.trim();
    if (!s) return '';

    // scp-like SSH syntax with no scheme: user@host:path (colon before any
    // slash). Distinguished from a scheme by the absence of "://".
    if (s.indexOf('://') === -1) {
        const at = s.indexOf('@');
        const colon = s.indexOf(':');
        if (colon > -1 && (at === -1 || at < colon)) {
            const host = s.slice(at + 1, colon).trim();
            return host ? host.toLowerCase() : '';
        }
        return '';
    }

    // Scheme present: parse with URL, stripping userinfo/port via .hostname.
    try {
        const u = new URL(s);
        return (u.hostname || '').toLowerCase();
    } catch (e) {
        return '';
    }
}
