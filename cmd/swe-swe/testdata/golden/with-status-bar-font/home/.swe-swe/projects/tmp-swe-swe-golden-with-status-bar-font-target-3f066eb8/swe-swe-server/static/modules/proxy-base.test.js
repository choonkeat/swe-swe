/**
 * Unit tests for proxy-base.js
 * Run with: node --test proxy-base.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    PROBE_HEADER,
    PROBE_PATH,
    makeProbe,
    proxyCandidates,
    resolveProxyBase,
} from './proxy-base.js';

// --- Response/fetch doubles ---------------------------------------------

function respWith(headerNames) {
    const lower = headerNames.map((h) => h.toLowerCase());
    return { headers: { has: (h) => lower.includes(h.toLowerCase()) } };
}

// A probe that answers from a fixed set of reachable bases, and records order.
function fakeProbe(reachable, seen) {
    return (base) => {
        if (seen) seen.push(base);
        return Promise.resolve(reachable.includes(base));
    };
}

// --- proxyCandidates -----------------------------------------------------

test('proxyCandidates orders subdomain, then port, then trusted path', () => {
    const c = proxyCandidates({
        subdomainBase: 'https://23000.example.com',
        portBase: 'http://box:23000',
        pathBase: 'http://box:1977/proxy/u/preview',
    });
    assert.deepStrictEqual(c.map((x) => x.mode), ['subdomain', 'port', 'path']);
    assert.strictEqual(c[2].trusted, true);
    assert.ok(!c[0].trusted);
    assert.ok(!c[1].trusted);
});

test('proxyCandidates turns missing forms into nulls rather than dropping slots', () => {
    const c = proxyCandidates({ subdomainBase: null, portBase: '', pathBase: '/p' });
    assert.strictEqual(c[0].base, null);
    assert.strictEqual(c[1].base, null);
    assert.strictEqual(c[2].base, '/p');
});

// --- resolveProxyBase ----------------------------------------------------

test('prefers the subdomain when it is reachable', async () => {
    const seen = [];
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: 'S', portBase: 'P', pathBase: 'PATH' }),
        fakeProbe(['S', 'P'], seen)
    );
    assert.deepStrictEqual(got, { mode: 'subdomain', base: 'S' });
    assert.deepStrictEqual(seen, ['S'], 'stops probing once one answers');
});

test('prefers the port when the subdomain is unreachable', async () => {
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: 'S', portBase: 'P', pathBase: 'PATH' }),
        fakeProbe(['P'])
    );
    assert.deepStrictEqual(got, { mode: 'port', base: 'P' });
});

test('prefers the port when there is no subdomain at all', async () => {
    const seen = [];
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: null, portBase: 'P', pathBase: 'PATH' }),
        fakeProbe(['P'], seen)
    );
    assert.deepStrictEqual(got, { mode: 'port', base: 'P' });
    assert.deepStrictEqual(seen, ['P'], 'an absent candidate is not probed');
});

// This is the single-reachable-port box: nothing but the page's own origin
// answers, and the pane must still load instead of showing the browser's
// "refused to connect" page.
test('falls back to the same-origin path when no port is reachable', async () => {
    const seen = [];
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: 'S', portBase: 'P', pathBase: 'PATH' }),
        fakeProbe([], seen)
    );
    assert.deepStrictEqual(got, { mode: 'path', base: 'PATH' });
    assert.deepStrictEqual(seen, ['S', 'P'], 'the trusted fallback is never probed');
});

test('a wildcard box whose DNS does not actually resolve still falls back', async () => {
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: 'S', portBase: null, pathBase: 'PATH' }),
        fakeProbe([])
    );
    assert.deepStrictEqual(got, { mode: 'path', base: 'PATH' });
});

test('returns null when even the path base is missing', async () => {
    const got = await resolveProxyBase(
        proxyCandidates({ subdomainBase: null, portBase: null, pathBase: null }),
        fakeProbe([])
    );
    assert.strictEqual(got, null);
});

test('tolerates an empty or absent candidate list', async () => {
    assert.strictEqual(await resolveProxyBase([], fakeProbe([])), null);
    assert.strictEqual(await resolveProxyBase(undefined, fakeProbe([])), null);
});

// --- makeProbe -----------------------------------------------------------

test('makeProbe requests {base}/__probe__ and accepts the marker header', async () => {
    const calls = [];
    const probe = makeProbe({
        fetchImpl: (url, opts) => {
            calls.push({ url, mode: opts.mode });
            return Promise.resolve(respWith([PROBE_HEADER]));
        },
    });
    assert.strictEqual(await probe('http://box:23000'), true);
    assert.deepStrictEqual(calls, [{ url: 'http://box:23000' + PROBE_PATH, mode: 'cors' }]);
});

test('makeProbe rejects a response without the marker header', async () => {
    const probe = makeProbe({ fetchImpl: () => Promise.resolve(respWith(['content-type'])) });
    assert.strictEqual(await probe('http://box:23000'), false);
});

test('makeProbe treats a refused connection as unreachable', async () => {
    const probe = makeProbe({ fetchImpl: () => Promise.reject(new TypeError('Failed to fetch')) });
    assert.strictEqual(await probe('http://box:23000'), false);
});

test('makeProbe treats a throwing fetch as unreachable', async () => {
    const probe = makeProbe({ fetchImpl: () => { throw new Error('boom'); } });
    assert.strictEqual(await probe('http://box:23000'), false);
});

test('makeProbe clears its timeout on success', async () => {
    const cleared = [];
    const probe = makeProbe({
        fetchImpl: () => Promise.resolve(respWith([PROBE_HEADER])),
        timers: { setTimeout: () => 42, clearTimeout: (id) => cleared.push(id) },
    });
    assert.strictEqual(await probe('http://box:23000'), true);
    assert.deepStrictEqual(cleared, [42]);
});

// A firewall that DROPs rather than rejects leaves fetch pending; without the
// abort the pane would sit on its placeholder until the browser's own connect
// timeout, which is minutes.
test('makeProbe gives up on a fetch that never settles', async () => {
    let fire = null;
    const probe = makeProbe({
        // Mirrors a real fetch: rejects on abort, including an abort that
        // landed before the call was made.
        fetchImpl: (_url, opts) => new Promise((_resolve, reject) => {
            if (opts.signal.aborted) return reject(new Error('aborted'));
            opts.signal.addEventListener('abort', () => reject(new Error('aborted')));
        }),
        timers: { setTimeout: (fn) => { fire = fn; return 1; }, clearTimeout: () => {} },
    });
    const p = probe('http://box:23000');
    assert.ok(fire, 'a timeout was armed');
    fire();
    assert.strictEqual(await p, false);
});
