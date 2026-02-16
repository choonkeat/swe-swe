/**
 * Unit tests for reconnect.js
 * Run with: node --test reconnect.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    RECONNECT_BASE_DELAY,
    RECONNECT_MAX_DELAY,
    createReconnectState,
    getDelay,
    nextAttempt,
    resetAttempts,
    formatCountdown,
    probeUntilReady
} from './reconnect.js';

// Constants tests
test('RECONNECT_BASE_DELAY is 1000ms', () => {
    assert.strictEqual(RECONNECT_BASE_DELAY, 1000);
});

test('RECONNECT_MAX_DELAY is 60000ms', () => {
    assert.strictEqual(RECONNECT_MAX_DELAY, 60000);
});

// createReconnectState tests
test('createReconnectState returns initial state with 0 attempts', () => {
    const state = createReconnectState();
    assert.strictEqual(state.attempts, 0);
});

test('createReconnectState uses default baseDelay', () => {
    const state = createReconnectState();
    assert.strictEqual(state.baseDelay, 1000);
});

test('createReconnectState uses default maxDelay', () => {
    const state = createReconnectState();
    assert.strictEqual(state.maxDelay, 60000);
});

test('createReconnectState accepts custom baseDelay', () => {
    const state = createReconnectState({ baseDelay: 500 });
    assert.strictEqual(state.baseDelay, 500);
    assert.strictEqual(state.maxDelay, 60000); // default preserved
});

test('createReconnectState accepts custom maxDelay', () => {
    const state = createReconnectState({ maxDelay: 30000 });
    assert.strictEqual(state.baseDelay, 1000); // default preserved
    assert.strictEqual(state.maxDelay, 30000);
});

test('createReconnectState accepts both custom delays', () => {
    const state = createReconnectState({ baseDelay: 500, maxDelay: 10000 });
    assert.strictEqual(state.baseDelay, 500);
    assert.strictEqual(state.maxDelay, 10000);
});

// getDelay tests - exponential backoff
test('getDelay returns baseDelay for attempt 0', () => {
    const state = createReconnectState();
    assert.strictEqual(getDelay(state), 1000);
});

test('getDelay returns 2x baseDelay for attempt 1', () => {
    const state = { ...createReconnectState(), attempts: 1 };
    assert.strictEqual(getDelay(state), 2000);
});

test('getDelay returns 4x baseDelay for attempt 2', () => {
    const state = { ...createReconnectState(), attempts: 2 };
    assert.strictEqual(getDelay(state), 4000);
});

test('getDelay returns 8x baseDelay for attempt 3', () => {
    const state = { ...createReconnectState(), attempts: 3 };
    assert.strictEqual(getDelay(state), 8000);
});

test('getDelay returns 16x baseDelay for attempt 4', () => {
    const state = { ...createReconnectState(), attempts: 4 };
    assert.strictEqual(getDelay(state), 16000);
});

test('getDelay returns 32x baseDelay for attempt 5', () => {
    const state = { ...createReconnectState(), attempts: 5 };
    assert.strictEqual(getDelay(state), 32000);
});

test('getDelay caps at maxDelay for attempt 6 (would be 64s)', () => {
    const state = { ...createReconnectState(), attempts: 6 };
    assert.strictEqual(getDelay(state), 60000);
});

test('getDelay caps at maxDelay for high attempt count', () => {
    const state = { ...createReconnectState(), attempts: 10 };
    assert.strictEqual(getDelay(state), 60000);
});

test('getDelay respects custom baseDelay', () => {
    const state = createReconnectState({ baseDelay: 500 });
    assert.strictEqual(getDelay(state), 500);
    assert.strictEqual(getDelay({ ...state, attempts: 1 }), 1000);
});

test('getDelay respects custom maxDelay', () => {
    const state = createReconnectState({ maxDelay: 5000 });
    assert.strictEqual(getDelay({ ...state, attempts: 3 }), 5000); // 8000 capped to 5000
});

// nextAttempt tests - immutability
test('nextAttempt increments attempts', () => {
    const state1 = createReconnectState();
    const state2 = nextAttempt(state1);
    assert.strictEqual(state2.attempts, 1);
});

test('nextAttempt does not mutate original state', () => {
    const state1 = createReconnectState();
    nextAttempt(state1);
    assert.strictEqual(state1.attempts, 0);
});

test('nextAttempt preserves baseDelay', () => {
    const state1 = createReconnectState({ baseDelay: 500 });
    const state2 = nextAttempt(state1);
    assert.strictEqual(state2.baseDelay, 500);
});

test('nextAttempt preserves maxDelay', () => {
    const state1 = createReconnectState({ maxDelay: 30000 });
    const state2 = nextAttempt(state1);
    assert.strictEqual(state2.maxDelay, 30000);
});

test('nextAttempt can be chained', () => {
    const state1 = createReconnectState();
    const state2 = nextAttempt(state1);
    const state3 = nextAttempt(state2);
    const state4 = nextAttempt(state3);
    assert.strictEqual(state4.attempts, 3);
});

// resetAttempts tests
test('resetAttempts sets attempts to 0', () => {
    const state1 = { ...createReconnectState(), attempts: 5 };
    const state2 = resetAttempts(state1);
    assert.strictEqual(state2.attempts, 0);
});

test('resetAttempts does not mutate original state', () => {
    const state1 = { ...createReconnectState(), attempts: 5 };
    resetAttempts(state1);
    assert.strictEqual(state1.attempts, 5);
});

test('resetAttempts preserves baseDelay', () => {
    const state1 = { ...createReconnectState({ baseDelay: 500 }), attempts: 5 };
    const state2 = resetAttempts(state1);
    assert.strictEqual(state2.baseDelay, 500);
});

test('resetAttempts preserves maxDelay', () => {
    const state1 = { ...createReconnectState({ maxDelay: 30000 }), attempts: 5 };
    const state2 = resetAttempts(state1);
    assert.strictEqual(state2.maxDelay, 30000);
});

test('resetAttempts on already-zero state returns new object', () => {
    const state1 = createReconnectState();
    const state2 = resetAttempts(state1);
    assert.notStrictEqual(state1, state2);
    assert.strictEqual(state2.attempts, 0);
});

// formatCountdown tests
test('formatCountdown rounds up milliseconds to seconds', () => {
    assert.strictEqual(formatCountdown(1000), 1);
    assert.strictEqual(formatCountdown(1500), 2);
    assert.strictEqual(formatCountdown(2000), 2);
    assert.strictEqual(formatCountdown(2001), 3);
});

test('formatCountdown handles 0', () => {
    assert.strictEqual(formatCountdown(0), 0);
});

test('formatCountdown handles sub-second delays', () => {
    assert.strictEqual(formatCountdown(1), 1);
    assert.strictEqual(formatCountdown(500), 1);
    assert.strictEqual(formatCountdown(999), 1);
});

test('formatCountdown handles large delays', () => {
    assert.strictEqual(formatCountdown(60000), 60);
    assert.strictEqual(formatCountdown(120000), 120);
});

// Integration test - complete workflow
test('integration: reconnect state workflow', () => {
    // Initial state
    let state = createReconnectState();
    assert.strictEqual(state.attempts, 0);
    assert.strictEqual(getDelay(state), 1000);

    // First reconnect attempt
    state = nextAttempt(state);
    assert.strictEqual(state.attempts, 1);
    assert.strictEqual(getDelay(state), 2000);

    // Second attempt
    state = nextAttempt(state);
    assert.strictEqual(state.attempts, 2);
    assert.strictEqual(getDelay(state), 4000);

    // Connection success - reset
    state = resetAttempts(state);
    assert.strictEqual(state.attempts, 0);
    assert.strictEqual(getDelay(state), 1000);
});

// probeUntilReady tests
// Helper: mock globalThis.fetch for probeUntilReady tests
function mockFetch(responses) {
    let callIndex = 0;
    const calls = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (url, opts) => {
        calls.push({ url, opts });
        const response = responses[callIndex++];
        if (response instanceof Error) throw response;
        return response;
    };
    return {
        calls,
        restore() { globalThis.fetch = originalFetch; }
    };
}

test('probeUntilReady resolves on first success', async () => {
    const mock = mockFetch([{ ok: true }]);
    try {
        await probeUntilReady('http://test/health', { maxAttempts: 3, baseDelay: 10, maxDelay: 100 });
        assert.strictEqual(mock.calls.length, 1);
        assert.strictEqual(mock.calls[0].url, 'http://test/health');
        assert.strictEqual(mock.calls[0].opts.method, 'HEAD');
        assert.strictEqual(mock.calls[0].opts.credentials, 'include');
    } finally {
        mock.restore();
    }
});

test('probeUntilReady retries non-ok then resolves', async () => {
    const mock = mockFetch([
        { ok: false, status: 502 },
        { ok: false, status: 502 },
        { ok: true }
    ]);
    try {
        await probeUntilReady('http://test/health', { maxAttempts: 5, baseDelay: 10, maxDelay: 100 });
        assert.strictEqual(mock.calls.length, 3);
    } finally {
        mock.restore();
    }
});

test('probeUntilReady retries fetch errors then resolves', async () => {
    const mock = mockFetch([
        new TypeError('fetch failed'),
        new TypeError('fetch failed'),
        { ok: true }
    ]);
    try {
        await probeUntilReady('http://test/health', { maxAttempts: 5, baseDelay: 10, maxDelay: 100 });
        assert.strictEqual(mock.calls.length, 3);
    } finally {
        mock.restore();
    }
});

test('probeUntilReady rejects after maxAttempts exhausted', async () => {
    const mock = mockFetch([
        { ok: false, status: 502 },
        { ok: false, status: 502 },
        { ok: false, status: 502 },
    ]);
    try {
        await assert.rejects(
            probeUntilReady('http://test/health', { maxAttempts: 3, baseDelay: 10, maxDelay: 100 }),
            { message: 'probeUntilReady: 3 attempts exhausted' }
        );
        assert.strictEqual(mock.calls.length, 3);
    } finally {
        mock.restore();
    }
});

test('probeUntilReady rejects on AbortSignal (pre-aborted)', async () => {
    const mock = mockFetch([]);
    const ac = new AbortController();
    ac.abort();
    try {
        await assert.rejects(
            probeUntilReady('http://test/health', { maxAttempts: 3, baseDelay: 10, maxDelay: 100, signal: ac.signal }),
            (err) => err instanceof DOMException && err.name === 'AbortError'
        );
        assert.strictEqual(mock.calls.length, 0);
    } finally {
        mock.restore();
    }
});

test('probeUntilReady rejects on AbortSignal (mid-probe)', async () => {
    const ac = new AbortController();
    // First call returns non-ok, then we abort during the delay before second attempt
    const originalFetch = globalThis.fetch;
    let fetchCount = 0;
    globalThis.fetch = async () => {
        fetchCount++;
        // After first fetch, abort during the retry delay
        setTimeout(() => ac.abort(), 5);
        return { ok: false, status: 502 };
    };
    try {
        await assert.rejects(
            probeUntilReady('http://test/health', { maxAttempts: 10, baseDelay: 500, maxDelay: 5000, signal: ac.signal }),
            (err) => err instanceof DOMException && err.name === 'AbortError'
        );
        assert.strictEqual(fetchCount, 1);
    } finally {
        globalThis.fetch = originalFetch;
    }
});
