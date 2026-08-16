/**
 * Pure decision logic for re-sending saved git secrets when a session starts.
 * Side-effect free (no localStorage, no window, no WebSocket) so the whole
 * gate chain is unit-testable; terminal-ui.js supplies the browser state and
 * posts whatever messages come back.
 *
 * Three defects this module exists to close, all of which showed up as
 * "swe-swe wipes my credentials every time I end a session":
 *
 *   1. Only the workspace's origin-remote host was ever looked up. A token
 *      saved under a different host was held by the browser and never sent,
 *      leaving the Settings pane blank until the user retyped the host by
 *      hand. orderedCredHosts/pickPaneHost fix that.
 *   2. A dismissed (or browser-suppressed) trust prompt left auto-send off
 *      forever with nothing on screen to say so.
 *   3. On a plain-http LAN address auto-send is deliberately off, and that
 *      was equally silent.
 *
 * (2) and (3) are why planAutoSend returns a REASON even when it sends
 * nothing: the caller renders autoRestoreHint(reason) in the Settings pane
 * so the user can see why, and fix it.
 *
 * @module cred-autosend
 */

/** Why an auto-send did or did not happen. */
export const REASON = {
    /** Secrets were handed to the caller to send. */
    OK: 'ok',
    /** Workdir is not a git repo with commits, so no repo identity to bind to. */
    NO_REPO: 'no-repo',
    /** Page is not on a TLS-protected or loopback address. */
    INSECURE: 'insecure',
    /** The user has not ticked "Remember on this device" for this repo. */
    UNTRUSTED: 'untrusted',
    /** The trust entry aged out; caller should drop it. */
    EXPIRED: 'expired',
    /** Trusted, but this browser holds no secrets for this repo. */
    NOTHING: 'nothing',
};

/** Hostnames treated as a secure context even over plain http. */
const LOOPBACK_HOSTS = ['localhost', '127.0.0.1', '[::1]', '::1', 'host.docker.internal'];

/**
 * How many extra hosts one auto-send message may carry. A browser that has
 * accumulated tokens for dozens of forges should not turn every session start
 * into an oversized frame.
 */
export const MAX_ADDITIONAL_HOSTS = 16;

/**
 * Whether secrets may leave the browser automatically on this address.
 * On plain ws:// an on-path attacker would read the PEM in the clear, so
 * auto-send is refused and the user must press Save. Loopback names have no
 * network for an attacker to sit on -- the same carve-out browsers make for
 * "secure context" capabilities.
 * @param {string} protocol - window.location.protocol, e.g. "https:".
 * @param {string} hostname - window.location.hostname.
 * @returns {boolean}
 */
export function isAutoSendSafe(protocol, hostname) {
    if (protocol === 'https:') return true;
    return LOOPBACK_HOSTS.indexOf(hostname) !== -1;
}

/**
 * The hosts to send credentials for, in order. The repo's origin-remote host
 * leads when we hold a token for it (it is the one git is most likely to
 * reach for); every other stored host follows, sorted so the order is stable
 * across sessions.
 * @param {string} resolvedHost - Host of the workdir's origin remote, or "".
 * @param {string[]} storedHosts - Hosts this browser holds a token for.
 * @returns {string[]}
 */
export function orderedCredHosts(resolvedHost, storedHosts) {
    const rest = (storedHosts || []).filter((h) => h && h !== resolvedHost).sort();
    const lead = (storedHosts || []).indexOf(resolvedHost) !== -1 ? [resolvedHost] : [];
    return lead.concat(rest);
}

/**
 * The host to prefill in the Settings > Git HTTPS pane. Prefers the repo's
 * origin-remote host, but only when a token is actually stored for it --
 * otherwise the pane rendered empty and the user had to retype the host to
 * make their own saved token reappear.
 * @param {string} resolvedHost - Host of the workdir's origin remote, or "".
 * @param {string[]} storedHosts - Hosts this browser holds a token for.
 * @returns {string} Never empty; falls back to "github.com".
 */
export function pickPaneHost(resolvedHost, storedHosts) {
    const ordered = orderedCredHosts(resolvedHost, storedHosts);
    if (ordered.length > 0) return ordered[0];
    return resolvedHost || 'github.com';
}

/**
 * @typedef {Object} AutoSendPlan
 * @property {string} reason - One of REASON.
 * @property {boolean} clearTrust - Caller should delete the trust entry.
 * @property {Array<{type: string, data: Object}>} messages - WS messages to send, in order.
 */

/**
 * Decide what a freshly-opened session should be handed.
 *
 * @param {Object} input
 * @param {string} input.initSha - Init-commit SHA identifying the repo, or "".
 * @param {string} input.protocol - window.location.protocol.
 * @param {string} input.hostname - window.location.hostname.
 * @param {{fingerprint: string, savedAt: number}|null} input.trust - Trust entry for this (origin, repo).
 * @param {number} input.now - Current epoch ms.
 * @param {number} input.ttlMs - Trust lifetime in ms.
 * @param {string} input.resolvedHost - Host of the workdir's origin remote, or "".
 * @param {Object<string, {username?: string, token?: string, name?: string, email?: string}>} input.creds - Stored bags by host.
 * @param {Object<string, {pem?: string, label?: string}>} input.keyByFingerprint - Stored signing keys.
 * @param {string} input.envRaw - Stored repo env-var blob.
 * @returns {AutoSendPlan}
 */
export function planAutoSend(input) {
    const nothing = (reason, clearTrust) => ({
        reason: reason,
        clearTrust: clearTrust === true,
        messages: [],
    });

    if (!input || !input.initSha) return nothing(REASON.NO_REPO);
    if (!isAutoSendSafe(input.protocol, input.hostname)) return nothing(REASON.INSECURE);

    const trust = input.trust;
    if (!trust) return nothing(REASON.UNTRUSTED);
    if (typeof trust.savedAt !== 'number' || input.now - trust.savedAt > input.ttlMs) {
        return nothing(REASON.EXPIRED, true);
    }

    const creds = input.creds || {};
    // Only hosts we hold an actual token for are worth sending; a bag with a
    // username but no token would just overwrite a good server-side entry.
    const storedHosts = Object.keys(creds).filter((h) => creds[h] && creds[h].token);
    const hosts = orderedCredHosts(input.resolvedHost, storedHosts);

    // Encrypted keys need a passphrase, which is never persisted, so only
    // keys stored unencrypted can come back automatically.
    const key = trust.fingerprint ? input.keyByFingerprint[trust.fingerprint] : null;
    const haveKey = !!(key && key.pem);

    const messages = [];
    if (hosts.length > 0) {
        const primary = hosts[0];
        const bag = creds[primary];
        // The author identity is per session, not per host, so take it from
        // whichever bag actually carries one -- the token that matches the
        // origin remote is not necessarily the one the user typed a name into.
        const ident = hosts
            .map((h) => creds[h])
            .find((b) => (b.name || '').trim() || (b.email || '').trim()) || {};
        messages.push({
            type: 'set_credentials',
            data: {
                host: primary,
                username: bag.username || '',
                token: bag.token,
                name: ident.name || '',
                email: ident.email || '',
                // ONE message for every host: the server sets the author and
                // rewrites the per-session gitconfig once per set_credentials,
                // so a message per host would rewrite N times and (with the
                // author only on the first) blank the identity on the rest.
                additional_hosts: hosts.slice(1, 1 + MAX_ADDITIONAL_HOSTS).map((h) => ({
                    host: h,
                    username: creds[h].username || '',
                    token: creds[h].token,
                })),
                signing_private_key_pem: haveKey ? key.pem : '',
                signing_passphrase: '',
                signing_key_label: haveKey ? (key.label || '') : '',
            },
        });
    } else if (haveKey) {
        messages.push({
            type: 'set_signing_key',
            data: {
                signing_private_key_pem: key.pem,
                signing_passphrase: '',
                signing_key_label: key.label || '',
            },
        });
    }

    // Repo env vars share the same trust gate, already cleared above.
    const envRaw = input.envRaw || '';
    if (envRaw.trim()) {
        messages.push({ type: 'set_env', data: { raw: envRaw } });
    }

    if (hosts.length === 0 && !haveKey) return nothing(REASON.NOTHING);
    return { reason: REASON.OK, clearTrust: false, messages: messages };
}

/**
 * The one-line explanation the Settings pane shows when auto-restore did not
 * hand anything to the server. Empty string means "say nothing".
 * @param {string} reason - One of REASON.
 * @returns {string}
 */
export function autoRestoreHint(reason) {
    switch (reason) {
        case REASON.INSECURE:
            return 'Auto-restore is off: this page is not on a secure address. Open swe-swe over https (or on localhost) to have saved secrets sent to new sessions automatically.';
        case REASON.UNTRUSTED:
            return 'Saved in this browser only. Tick "Remember on this device" to send it to new sessions automatically.';
        case REASON.EXPIRED:
            return 'Auto-restore expired after 90 days. Tick "Remember on this device" again.';
        case REASON.NO_REPO:
            return 'Auto-restore is off: this folder is not a git repository with commits, so there is nothing to tie the saved secrets to.';
        default:
            return '';
    }
}
