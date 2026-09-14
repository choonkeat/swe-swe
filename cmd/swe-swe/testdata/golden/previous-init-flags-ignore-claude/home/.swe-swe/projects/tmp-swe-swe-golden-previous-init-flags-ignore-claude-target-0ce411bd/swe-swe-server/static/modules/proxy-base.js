/**
 * Shared port-vs-path resolution for the cross-origin panes.
 *
 * Preview, Agent Chat and Files can each be reached three ways:
 *
 *   subdomain  "{proxyPort}.{publicHostname}"  -- tunnel / wildcard box
 *   port       "{hostname}:{proxyPort}"        -- every port reachable
 *   path       "/proxy/{uuid}/{pane}"          -- same origin, always reachable
 *
 * The first two are faster and keep the pane in its own origin, but they are
 * only reachable on some deployments. The path form always works because it
 * rides the very connection that served the page -- so it is the fallback, and
 * it is never probed.
 *
 * Each pane used to make this decision inline, and Files never made it at all:
 * it loaded its port unconditionally, so a box with a single reachable port
 * showed the browser's own "refused to connect" page in the pane forever (the
 * load supervisor kept re-requesting the same unreachable port, because a
 * browser error page still fires `load`).
 *
 * The probe is a GET of "{base}/__probe__", which swe-swe-server's corsWrapper
 * answers with the X-Agent-Reverse-Proxy marker header before any auth or
 * forwarding. A missing header, a non-CORS response, a refused connection and
 * a firewall that silently drops the packets all resolve to "not reachable".
 *
 * @module proxy-base
 */

/** Path that answers a reachability probe on every proxy listener. */
export const PROBE_PATH = '/__probe__';

/** Marker header swe-swe-server stamps on proxy responses. */
export const PROBE_HEADER = 'X-Agent-Reverse-Proxy';

/**
 * How long to wait for a probe before calling the candidate unreachable.
 * A refused connection fails in milliseconds, but a firewall that DROPs
 * instead of rejecting would otherwise leave the pane on its placeholder
 * until the browser's own (minutes-long) connect timeout.
 */
export const PROBE_TIMEOUT = 5000;

/**
 * Build the default probe: a CORS GET that resolves true only when the marker
 * header comes back. Timers are injectable so tests drive a virtual clock.
 * @param {object} [opts]
 * @param {typeof fetch} [opts.fetchImpl]
 * @param {number} [opts.timeoutMs]
 * @param {{setTimeout: Function, clearTimeout: Function}} [opts.timers]
 * @returns {(base: string) => Promise<boolean>}
 */
export function makeProbe({ fetchImpl, timeoutMs, timers } = {}) {
    const doFetch = fetchImpl || ((...a) => fetch(...a));
    const ms = timeoutMs ?? PROBE_TIMEOUT;
    // Wrap the globals in arrows rather than passing bare references: in the
    // browser setTimeout/clearTimeout throw "Illegal invocation" when called
    // with a receiver other than window (see iframe-load-supervisor.js).
    const t = timers || {
        setTimeout: (fn, delay) => setTimeout(fn, delay),
        clearTimeout: (id) => clearTimeout(id),
    };
    return function probe(base) {
        let timer = null;
        // AbortController is absent in some test doubles; the timeout is a
        // belt-and-braces guard, so skip it rather than fail the probe.
        const ctl = typeof AbortController === 'function' ? new AbortController() : null;
        if (ctl) timer = t.setTimeout(() => ctl.abort(), ms);
        return Promise.resolve()
            .then(() => doFetch(base + PROBE_PATH, {
                method: 'GET',
                mode: 'cors',
                signal: ctl ? ctl.signal : undefined,
            }))
            .then((resp) => !!(resp && resp.headers && resp.headers.has(PROBE_HEADER)))
            .catch(() => false)
            .then((ok) => {
                if (timer !== null) t.clearTimeout(timer);
                return ok;
            });
    };
}

/**
 * Pick the first reachable candidate.
 *
 * Candidates are tried in order, so put the preferred form first. A candidate
 * marked `trusted` is returned without probing and ends the search -- that is
 * the same-origin path form, which cannot be unreachable if the page itself
 * loaded. Candidates with no `base` are skipped, so callers can list a form
 * whose port has not arrived yet without guarding each one.
 *
 * @param {Array<{mode: string, base: string|null, trusted?: boolean}>} candidates
 * @param {(base: string) => Promise<boolean>} probe
 * @returns {Promise<{mode: string, base: string}|null>} the winner, or null
 *   when every candidate was absent or unreachable
 */
export async function resolveProxyBase(candidates, probe) {
    for (const c of candidates || []) {
        if (!c || !c.base) continue;
        // Ordering decides: a trusted candidate is reachable by definition, so
        // reaching it means every preferred form above it has already failed.
        if (c.trusted) return { mode: c.mode, base: c.base };
        if (await probe(c.base)) return { mode: c.mode, base: c.base };
    }
    return null;
}

/**
 * The candidate list every pane uses, in the one order that is correct:
 * subdomain (when the page itself came through the tunnel), then the port,
 * then the same-origin path as the guaranteed fallback.
 *
 * Passing a null for any of the first two simply drops it from the list.
 *
 * @param {object} opts
 * @param {string|null} opts.subdomainBase
 * @param {string|null} opts.portBase
 * @param {string|null} opts.pathBase - same-origin; the trusted fallback
 * @returns {Array<{mode: string, base: string|null, trusted?: boolean}>}
 */
export function proxyCandidates({ subdomainBase, portBase, pathBase }) {
    return [
        { mode: 'subdomain', base: subdomainBase || null },
        { mode: 'port', base: portBase || null },
        { mode: 'path', base: pathBase || null, trusted: true },
    ];
}
