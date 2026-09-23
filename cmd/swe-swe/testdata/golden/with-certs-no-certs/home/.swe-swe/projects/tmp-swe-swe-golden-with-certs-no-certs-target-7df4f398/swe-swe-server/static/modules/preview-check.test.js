/**
 * Unit tests for preview-check.js
 * Run with: node --test preview-check.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { collectPreviewCheck } from './preview-check.js';

// The Preview tab stayed white on an iPad behind Cloudflare with no developer
// console to look inside it. The Check button's report has to say, in one
// screenshot, how far the two-level page got and what the network did to
// swe-swe's replies.

function fakeResponse(status, headers) {
    const h = new Map(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
    return { status, headers: { get: (k) => (h.has(k.toLowerCase()) ? h.get(k.toLowerCase()) : null) } };
}

function frameWith({ url, innerSrc, innerDoc, innerError }) {
    const inner = {
        getAttribute: () => innerSrc,
        get contentWindow() {
            if (innerError) {
                return { get location() { throw new Error(innerError); }, get document() { throw new Error(innerError); } };
            }
            return { location: { href: innerDoc.url }, document: { body: { innerText: innerDoc.text } } };
        },
    };
    return {
        getAttribute: () => url,
        contentWindow: {
            location: { href: url },
            document: { getElementById: (id) => (id === 'inner' ? inner : null) },
        },
    };
}

test('reports each level of the Preview tab and each reply as the browser saw it', async () => {
    const pane = frameWith({
        url: 'https://box/proxy/u1/preview/__agent-reverse-proxy-debug__/shell?path=%2F',
        innerSrc: '/proxy/u1/preview/',
        innerDoc: { url: 'https://box/proxy/u1/preview/', text: 'Hello, World!' },
    });
    const seen = [];
    const fetchImpl = async (url) => {
        seen.push(url);
        return fakeResponse(200, { 'X-Frame-Options': 'SAMEORIGIN', 'Content-Security-Policy': "frame-ancestors 'self'" });
    };
    const lines = await collectPreviewCheck({
        pane, ui: { _proxyMode: 'path', _previewProbeGaveUp: false, _previewWaiting: false, _previewAppUp: true },
        fetchImpl, userAgent: 'UA-test',
    });
    const text = lines.join('\n');
    assert.match(text, /mode: path/);
    assert.match(text, /app running: true/);
    assert.match(text, /pane: https:\/\/box\/proxy\/u1\/preview\/__agent-reverse-proxy-debug__\/shell/);
    assert.match(text, /inner src: \/proxy\/u1\/preview\//);
    assert.match(text, /inner shows: Hello, World!/);
    assert.match(text, /X-Frame-Options: SAMEORIGIN/);
    assert.match(text, /frame-ancestors 'self'/);
    assert.match(text, /marker: missing/, 'a reply without X-Agent-Reverse-Proxy says so');
    assert.match(text, /UA-test/);
    assert.deepStrictEqual(seen, [
        'https://box/proxy/u1/preview/__agent-reverse-proxy-debug__/shell?path=%2F',
        'https://box/proxy/u1/preview/',
    ], 'checks the outer page and the app page');
});

test('an inner page the browser will not let us read is reported, not thrown', async () => {
    const pane = frameWith({
        url: 'https://box/proxy/u1/preview/__agent-reverse-proxy-debug__/shell?path=%2F',
        innerSrc: '/proxy/u1/preview/',
        innerError: 'Blocked a frame',
    });
    const lines = await collectPreviewCheck({
        pane, ui: {}, fetchImpl: async () => fakeResponse(502, {}), userAgent: 'UA',
    });
    const text = lines.join('\n');
    assert.match(text, /inner: cannot read \(Blocked a frame\)/);
    assert.match(text, /status 502/);
});

test('a pane that never got a page says so', async () => {
    const lines = await collectPreviewCheck({
        pane: { getAttribute: () => '', contentWindow: { location: { href: 'about:blank' }, document: { getElementById: () => null } } },
        ui: { _previewProbeGaveUp: true }, fetchImpl: async () => { throw new Error('no'); }, userAgent: 'UA',
    });
    const text = lines.join('\n');
    assert.match(text, /pane: \(not loaded\)/);
    assert.match(text, /gave up waiting: true/);
});
