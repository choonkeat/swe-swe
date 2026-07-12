/**
 * Unit tests for iframe-load-supervisor.js
 * Run with: node --test iframe-load-supervisor.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    WATCHDOG_TIMEOUT,
    SUPERVISOR_BASE_DELAY,
    SUPERVISOR_MAX_DELAY,
    withCacheBust,
    IframeLoadSupervisor,
} from './iframe-load-supervisor.js';

// --- Test doubles -----------------------------------------------------------

// Deterministic controllable clock. No Date.now / real timers.
function makeClock() {
    let now = 0;
    let seq = 0;
    const tasks = new Map();
    return {
        setTimeout(fn, delay) {
            const id = ++seq;
            tasks.set(id, { fn, at: now + (delay || 0) });
            return id;
        },
        clearTimeout(id) {
            tasks.delete(id);
        },
        // Advance virtual time by ms, firing due tasks in chronological order.
        tick(ms) {
            const target = now + ms;
            while (true) {
                let pick = null;
                for (const [id, task] of tasks) {
                    if (pick === null || task.at < pick.task.at) pick = { id, task };
                }
                if (!pick || pick.task.at > target) break;
                now = pick.task.at;
                tasks.delete(pick.id);
                pick.task.fn();
            }
            now = target;
        },
        pending() {
            return tasks.size;
        },
    };
}

// Minimal iframe stub tracking src assignments + event listeners.
function makeIframe() {
    const listeners = {};
    const iframe = {
        srcHistory: [],
        _src: '',
        get src() { return this._src; },
        set src(v) { this._src = v; this.srcHistory.push(v); },
        addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
        removeEventListener(type, fn) {
            listeners[type] = (listeners[type] || []).filter((f) => f !== fn);
        },
        fire(type) { (listeners[type] || []).slice().forEach((fn) => fn()); },
        listenerCount(type) { return (listeners[type] || []).length; },
    };
    return iframe;
}

function makeSup(overrides = {}) {
    const clock = makeClock();
    const iframe = makeIframe();
    const overlay = [];
    const sup = new IframeLoadSupervisor({
        iframe,
        url: 'https://files.example/',
        onOverlay: (state) => overlay.push(state),
        timers: clock,
        ...overrides,
    });
    return { sup, iframe, clock, overlay };
}

// --- Constants --------------------------------------------------------------

test('WATCHDOG_TIMEOUT is 4000ms', () => {
    assert.strictEqual(WATCHDOG_TIMEOUT, 4000);
});

test('backoff floor 1s, cap 15s', () => {
    assert.strictEqual(SUPERVISOR_BASE_DELAY, 1000);
    assert.strictEqual(SUPERVISOR_MAX_DELAY, 15000);
});

// --- withCacheBust ----------------------------------------------------------

test('withCacheBust appends ?_r= when no query', () => {
    assert.strictEqual(withCacheBust('https://x/', 3), 'https://x/?_r=3');
});

test('withCacheBust appends &_r= when query present', () => {
    assert.strictEqual(withCacheBust('https://x/?a=1', 7), 'https://x/?a=1&_r=7');
});

// --- start / happy path -----------------------------------------------------

test('start() loads the clean url (no cache-bust) and shows connecting overlay', () => {
    const { sup, iframe, overlay } = makeSup();
    sup.start();
    assert.strictEqual(iframe.srcHistory.length, 1);
    assert.strictEqual(iframe.srcHistory[0], 'https://files.example/');
    assert.strictEqual(overlay[0], 'connecting');
});

test('load event hides overlay, marks loaded, clears watchdog (no further reloads)', () => {
    const { sup, iframe, clock, overlay } = makeSup();
    sup.start();
    iframe.fire('load');
    assert.strictEqual(overlay[overlay.length - 1], 'loaded');
    assert.strictEqual(sup.loaded, true);
    // Watchdog must be cancelled: advancing well past it triggers no reload.
    clock.tick(60000);
    assert.strictEqual(iframe.srcHistory.length, 1);
});

// --- watchdog -> retry ------------------------------------------------------

test('no load within watchdog -> retry with cache-busted src + reconnecting overlay', () => {
    const { sup, iframe, clock, overlay } = makeSup();
    sup.start();
    // Nothing fires. Watchdog (4s) elapses, then first backoff delay (1s).
    clock.tick(WATCHDOG_TIMEOUT);
    clock.tick(SUPERVISOR_BASE_DELAY);
    assert.strictEqual(iframe.srcHistory.length, 2);
    assert.match(iframe.srcHistory[1], /https:\/\/files\.example\/\?_r=\d+/);
    assert.strictEqual(overlay[overlay.length - 1], 'reconnecting');
});

test('backoff sequence is 1s,2s,4s,8s then capped at 15s', () => {
    const { sup, iframe, clock } = makeSup();
    sup.start(); // attempt #1 (clean)
    const expected = [1000, 2000, 4000, 8000, 15000, 15000];
    for (let i = 0; i < expected.length; i++) {
        clock.tick(WATCHDOG_TIMEOUT);        // watchdog on current attempt fires
        // Just before the backoff delay elapses, no new attempt yet.
        clock.tick(expected[i] - 1);
        assert.strictEqual(iframe.srcHistory.length, i + 1, `no reload before ${expected[i]}ms (i=${i})`);
        clock.tick(1);                        // backoff delay completes -> new attempt
        assert.strictEqual(iframe.srcHistory.length, i + 2, `reload at ${expected[i]}ms (i=${i})`);
    }
});

test('error event triggers an immediate retry (scheduled by backoff)', () => {
    const { sup, iframe, clock } = makeSup();
    sup.start();
    iframe.fire('error');
    clock.tick(SUPERVISOR_BASE_DELAY);
    assert.strictEqual(iframe.srcHistory.length, 2);
    assert.match(iframe.srcHistory[1], /_r=\d+/);
});

test('successful load after retries resets backoff to the floor', () => {
    const { sup, iframe, clock } = makeSup();
    sup.start();
    // Two failed rounds -> backoff would otherwise be at 4s next.
    clock.tick(WATCHDOG_TIMEOUT + 1000);  // retry 1 (1s)
    clock.tick(WATCHDOG_TIMEOUT + 2000);  // retry 2 (2s)
    assert.strictEqual(iframe.srcHistory.length, 3);
    iframe.fire('load');                  // success -> backoff reset to floor
    assert.strictEqual(sup.loaded, true);
    // A subsequent drop is re-detected via the error event; the retry must use
    // the 1s floor again, not the grown 4s delay.
    iframe.fire('error');
    clock.tick(999);
    assert.strictEqual(iframe.srcHistory.length, 3, 'no reload before the 1s floor');
    clock.tick(1);
    assert.strictEqual(iframe.srcHistory.length, 4, 'reload at the 1s floor');
});

// --- kick (focus / visibility) ----------------------------------------------

test('kick() while not loaded triggers an immediate attempt with backoff reset', () => {
    const { sup, iframe, clock, overlay } = makeSup();
    sup.start();
    // Let it fail a couple rounds so backoff has grown.
    clock.tick(WATCHDOG_TIMEOUT + 1000);
    clock.tick(WATCHDOG_TIMEOUT + 2000);
    const before = iframe.srcHistory.length;
    sup.kick();
    // Immediate -- no need to advance the clock.
    assert.strictEqual(iframe.srcHistory.length, before + 1);
    assert.match(iframe.srcHistory[iframe.srcHistory.length - 1], /_r=\d+/);
    assert.strictEqual(overlay[overlay.length - 1], 'reconnecting');
    // And backoff was reset: next watchdog->retry uses the 1s floor.
    const after = iframe.srcHistory.length;
    clock.tick(WATCHDOG_TIMEOUT);
    clock.tick(1000);
    assert.strictEqual(iframe.srcHistory.length, after + 1);
});

test('kick() while loaded is a no-op', () => {
    const { sup, iframe } = makeSup();
    sup.start();
    iframe.fire('load');
    const before = iframe.srcHistory.length;
    sup.kick();
    assert.strictEqual(iframe.srcHistory.length, before);
});

// --- stop -------------------------------------------------------------------

test('stop() clears the watchdog and detaches listeners', () => {
    const { sup, iframe, clock } = makeSup();
    sup.start();
    assert.strictEqual(iframe.listenerCount('load'), 1);
    assert.strictEqual(iframe.listenerCount('error'), 1);
    sup.stop();
    assert.strictEqual(iframe.listenerCount('load'), 0);
    assert.strictEqual(iframe.listenerCount('error'), 0);
    clock.tick(60000);
    assert.strictEqual(iframe.srcHistory.length, 1); // no retry after stop
});

test('start() is idempotent for listeners (no double-attach on re-start)', () => {
    const { sup, iframe } = makeSup();
    sup.start();
    sup.start();
    assert.strictEqual(iframe.listenerCount('load'), 1);
    assert.strictEqual(iframe.listenerCount('error'), 1);
});
