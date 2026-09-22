/**
 * Load-supervisor for iframe panes.
 *
 * An <iframe> does not auto-retry a dropped navigation: if the first GET is
 * lost (cold tunnel re-establishment, a backgrounded/reaped tab, a weak-signal
 * timeout), the `load` event never fires, the "Connecting..." overlay never
 * clears, and nothing re-attempts -- only a manual page reload recovers. This
 * is exactly the intermittent "Files tab stuck on Connecting" failure on
 * mobile.
 *
 * The supervisor wraps an iframe with:
 *   1. a load watchdog -- if `load` doesn't fire within WATCHDOG_TIMEOUT, it
 *      re-assigns a cache-busted src and retries with capped exponential
 *      backoff (never gives up; the backend is usually fine and comes back);
 *   2. an `error`-event retry;
 *   3. `kick()` -- an immediate, backoff-reset retry to wire to focus /
 *      visibilitychange / pane-activate so a returning user waits ~0s.
 *
 * Backoff state reuses reconnect.js. Timers are injectable so the behaviour is
 * unit-testable with a virtual clock (no Date.now / Math.random -- cache-bust
 * tokens come from a monotonic counter, per repo convention).
 *
 * @module iframe-load-supervisor
 */

import { createReconnectState, getDelay, nextAttempt, resetAttempts } from './reconnect.js';

/** How long to wait for the iframe `load` event before retrying (ms). */
export const WATCHDOG_TIMEOUT = 4000;
/** Backoff floor: first retry waits this long (ms). */
export const SUPERVISOR_BASE_DELAY = 1000;
/** Backoff cap: never wait longer than this between retries (ms). */
export const SUPERVISOR_MAX_DELAY = 15000;

/**
 * Append a cache-busting query param so the browser truly re-requests the URL
 * (re-assigning the same src is a no-op and would not reload).
 * @param {string} url
 * @param {number|string} token - monotonic value; NOT time/random
 * @returns {string}
 */
export function withCacheBust(url, token) {
    const sep = url.includes('?') ? '&' : '?';
    return `${url}${sep}_r=${token}`;
}

/**
 * Overlay state signalled to the host via onOverlay(state):
 *   'connecting'   -- first load in progress
 *   'reconnecting' -- a retry is in progress (initial load was dropped)
 *   'loaded'       -- the iframe fired `load`; hide the overlay
 */
export class IframeLoadSupervisor {
    /**
     * @param {object} opts
     * @param {HTMLIFrameElement|object} opts.iframe - target iframe (assigning
     *   .src navigates it; must support addEventListener/removeEventListener)
     * @param {string} opts.url - URL to load (cache-bust appended on retries)
     * @param {(state: string) => void} [opts.onOverlay] - overlay state sink
     * @param {{setTimeout: Function, clearTimeout: Function}} [opts.timers]
     * @param {number} [opts.watchdog] - load watchdog in ms
     * @param {number} [opts.baseDelay] - backoff floor in ms
     * @param {number} [opts.maxDelay] - backoff cap in ms
     */
    constructor({ iframe, url, onOverlay, timers, watchdog, baseDelay, maxDelay } = {}) {
        this.iframe = iframe;
        this.url = url;
        this.onOverlay = onOverlay || (() => {});
        // Wrap the global timers in arrows rather than passing bare references:
        // in the browser, window.setTimeout/clearTimeout are WebIDL methods that
        // throw "Illegal invocation" if invoked with a receiver other than
        // window -- and `this.timers.setTimeout(...)` sets the receiver to the
        // timers object. Calling the globals directly inside the arrow keeps the
        // correct binding. (Node has no such check, so injected-clock tests can't
        // surface this -- it only shows up in a real browser.)
        this.timers = timers || {
            setTimeout: (fn, ms) => setTimeout(fn, ms),
            clearTimeout: (id) => clearTimeout(id),
        };
        this.watchdog = watchdog ?? WATCHDOG_TIMEOUT;
        this.backoff = createReconnectState({
            baseDelay: baseDelay ?? SUPERVISOR_BASE_DELAY,
            maxDelay: maxDelay ?? SUPERVISOR_MAX_DELAY,
        });
        this.loaded = false;
        this._timer = null;
        this._token = 0;
        this._everAttempted = false;
        this._attached = false;
        // A retry has occurred since the last successful load -- drives the
        // 'reconnecting' vs 'connecting' overlay text.
        this._reconnecting = false;
        this._onLoad = () => this._handleLoad();
        this._onError = () => this._handleError();
    }

    /** Attach listeners (idempotent) and kick off the first load. */
    start() {
        if (!this._attached) {
            this.iframe.addEventListener('load', this._onLoad);
            this.iframe.addEventListener('error', this._onError);
            this._attached = true;
        }
        this._attempt();
    }

    /** Detach listeners and cancel any pending watchdog/backoff timer. */
    stop() {
        this.timers.clearTimeout(this._timer);
        this._timer = null;
        if (this._attached) {
            this.iframe.removeEventListener('load', this._onLoad);
            this.iframe.removeEventListener('error', this._onError);
            this._attached = false;
        }
    }

    /**
     * Immediate retry with the backoff reset to the floor. Wire to focus /
     * visibilitychange / pane-activate so a returning user re-loads at ~0s
     * instead of waiting out the current backoff. No-op once loaded.
     */
    kick() {
        if (this.loaded) return;
        this.backoff = resetAttempts(this.backoff);
        this._reconnecting = true;
        this._attempt();
    }

    // --- internals ----------------------------------------------------------

    _nextToken() {
        this._token += 1;
        return this._token;
    }

    // Assign the iframe src and arm the load watchdog. The first attempt uses
    // the clean URL; every subsequent attempt is cache-busted so the browser
    // actually re-requests.
    _attempt() {
        this.timers.clearTimeout(this._timer);
        const src = this._everAttempted ? withCacheBust(this.url, this._nextToken()) : this.url;
        this._everAttempted = true;
        this.iframe.src = src;
        this.onOverlay(this._reconnecting ? 'reconnecting' : 'connecting');
        this._timer = this.timers.setTimeout(() => this._retry(), this.watchdog);
    }

    // Schedule the next attempt after the current backoff delay, then grow the
    // backoff (compute-then-increment => 1s, 2s, 4s, 8s, ... capped).
    _retry() {
        if (this.loaded) return;
        this._reconnecting = true;
        const delay = getDelay(this.backoff);
        this.backoff = nextAttempt(this.backoff);
        this._timer = this.timers.setTimeout(() => this._attempt(), delay);
    }

    _handleLoad() {
        this.loaded = true;
        this._reconnecting = false;
        this.backoff = resetAttempts(this.backoff);
        this.timers.clearTimeout(this._timer);
        this._timer = null;
        this.onOverlay('loaded');
    }

    _handleError() {
        this.loaded = false;
        this._retry();
    }
}
