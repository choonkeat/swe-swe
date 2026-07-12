/**
 * Unit tests for the preview host-demux helpers in url-builder.js.
 * Run with: node --test url-builder.vhost.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { logicalToVhostLabel, buildVhostPreviewUrl, parseLogicalInput } from './url-builder.js';

// logicalToVhostLabel: logical "app1.lvh.me:5000" -> reachable label "app1-5000"
test('logicalToVhostLabel with port -> name-port', () => {
    assert.strictEqual(logicalToVhostLabel('app1.lvh.me:5000', 'lvh.me'), 'app1-5000');
});

test('logicalToVhostLabel without port -> bare name', () => {
    assert.strictEqual(logicalToVhostLabel('app1.lvh.me', 'lvh.me'), 'app1');
});

test('logicalToVhostLabel rejects nested labels (flat only, v1)', () => {
    assert.strictEqual(logicalToVhostLabel('a.b.lvh.me', 'lvh.me'), null);
});

test('logicalToVhostLabel returns null for non-suffix host', () => {
    assert.strictEqual(logicalToVhostLabel('app1.example.com', 'lvh.me'), null);
});

test('logicalToVhostLabel returns null for the bare suffix itself', () => {
    assert.strictEqual(logicalToVhostLabel('lvh.me', 'lvh.me'), null);
});

// buildVhostPreviewUrl: (label, reach, proxyPort, protocol) -> reachable origin
test('buildVhostPreviewUrl builds label.reach:port', () => {
    assert.strictEqual(
        buildVhostPreviewUrl('app1-5000', 'x.sslip.io', 23000, 'http:'),
        'http://app1-5000.x.sslip.io:23000'
    );
});

test('buildVhostPreviewUrl honors https', () => {
    assert.strictEqual(
        buildVhostPreviewUrl('app1', 'lvh.me', 23000, 'https:'),
        'https://app1.lvh.me:23000'
    );
});

test('buildVhostPreviewUrl returns null on missing input', () => {
    assert.strictEqual(buildVhostPreviewUrl('', 'x.sslip.io', 23000, 'http:'), null);
    assert.strictEqual(buildVhostPreviewUrl('app1-5000', '', 23000, 'http:'), null);
    assert.strictEqual(buildVhostPreviewUrl('app1-5000', 'x.sslip.io', 0, 'http:'), null);
});

// parseLogicalInput: user-typed "app1.lvh.me:5000/path?q#h" -> structured parts
test('parseLogicalInput parses host, port, label and path suffix', () => {
    const got = parseLogicalInput('app1.lvh.me:5000/path?q#h', 'lvh.me');
    assert.deepStrictEqual(got, {
        logicalHost: 'app1.lvh.me',
        port: 5000,
        label: 'app1-5000',
        pathSuffix: '/path?q#h',
    });
});

test('parseLogicalInput tolerates an explicit scheme and no path', () => {
    const got = parseLogicalInput('http://app1.lvh.me:5000', 'lvh.me');
    assert.deepStrictEqual(got, {
        logicalHost: 'app1.lvh.me',
        port: 5000,
        label: 'app1-5000',
        pathSuffix: '/',
    });
});

test('parseLogicalInput without port', () => {
    const got = parseLogicalInput('app1.lvh.me/x', 'lvh.me');
    assert.deepStrictEqual(got, {
        logicalHost: 'app1.lvh.me',
        port: null,
        label: 'app1',
        pathSuffix: '/x',
    });
});

test('parseLogicalInput returns null for non-suffix host', () => {
    assert.strictEqual(parseLogicalInput('app1.example.com:5000', 'lvh.me'), null);
});
