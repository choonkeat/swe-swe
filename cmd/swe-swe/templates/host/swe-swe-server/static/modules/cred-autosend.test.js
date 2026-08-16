/**
 * Unit tests for cred-autosend.js
 * Run with: node --test cred-autosend.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    REASON,
    isAutoSendSafe,
    orderedCredHosts,
    pickPaneHost,
    planAutoSend,
    autoRestoreHint,
} from './cred-autosend.js';

const DAY = 24 * 60 * 60 * 1000;
const TTL = 90 * DAY;
const NOW = 1_700_000_000_000;

function base(over) {
    return Object.assign({
        initSha: 'cf749ff21507eb13d7949b557925d4cf1fdfb7a1',
        protocol: 'https:',
        hostname: 'swe.example.com',
        trust: { fingerprint: '', savedAt: NOW - DAY },
        now: NOW,
        ttlMs: TTL,
        resolvedHost: '',
        creds: {},
        keyByFingerprint: {},
        envRaw: '',
    }, over || {});
}

// --- isAutoSendSafe ------------------------------------------------------

test('isAutoSendSafe: https is safe on any hostname', () => {
    assert.strictEqual(isAutoSendSafe('https:', 'swe.example.com'), true);
});

test('isAutoSendSafe: plain http on loopback names is safe', () => {
    for (const h of ['localhost', '127.0.0.1', '[::1]', '::1', 'host.docker.internal']) {
        assert.strictEqual(isAutoSendSafe('http:', h), true, h);
    }
});

test('isAutoSendSafe: plain http on a LAN name or IP is not safe', () => {
    assert.strictEqual(isAutoSendSafe('http:', '192.168.1.20'), false);
    assert.strictEqual(isAutoSendSafe('http:', 'swe.lan'), false);
});

// --- orderedCredHosts ----------------------------------------------------

test('orderedCredHosts: the repo origin host comes first when we hold it', () => {
    assert.deepStrictEqual(
        orderedCredHosts('gitlab.example.com', ['github.com', 'gitlab.example.com']),
        ['gitlab.example.com', 'github.com']
    );
});

test('orderedCredHosts: every other stored host still rides along, sorted', () => {
    assert.deepStrictEqual(
        orderedCredHosts('gitlab.example.com', ['github.com', 'bitbucket.org', 'gitlab.example.com']),
        ['gitlab.example.com', 'bitbucket.org', 'github.com']
    );
});

test('orderedCredHosts: origin host we hold nothing for does not appear', () => {
    assert.deepStrictEqual(
        orderedCredHosts('gitlab.example.com', ['github.com']),
        ['github.com']
    );
});

test('orderedCredHosts: no origin host falls back to sorted stored hosts', () => {
    assert.deepStrictEqual(
        orderedCredHosts('', ['github.com', 'bitbucket.org']),
        ['bitbucket.org', 'github.com']
    );
});

test('orderedCredHosts: nothing stored yields an empty list', () => {
    assert.deepStrictEqual(orderedCredHosts('github.com', []), []);
});

// --- pickPaneHost --------------------------------------------------------

test('pickPaneHost: prefers the repo origin host when a token is stored for it', () => {
    assert.strictEqual(pickPaneHost('gitlab.example.com', ['gitlab.example.com', 'github.com']), 'gitlab.example.com');
});

test('pickPaneHost: falls back to a host we DO hold a token for (cause 1 symptom)', () => {
    // The workspace origin is gitlab, but the only token this browser holds is
    // for github. Prefilling gitlab left the pane blank and made the user
    // retype the host by hand.
    assert.strictEqual(pickPaneHost('gitlab.example.com', ['github.com']), 'github.com');
});

test('pickPaneHost: with nothing stored, shows the repo origin host', () => {
    assert.strictEqual(pickPaneHost('gitlab.example.com', []), 'gitlab.example.com');
});

test('pickPaneHost: with neither, defaults to github.com', () => {
    assert.strictEqual(pickPaneHost('', []), 'github.com');
});

// --- planAutoSend: gates -------------------------------------------------

test('planAutoSend: no repo identity means nothing is sent', () => {
    const p = planAutoSend(base({ initSha: '' }));
    assert.strictEqual(p.reason, REASON.NO_REPO);
    assert.deepStrictEqual(p.messages, []);
});

test('planAutoSend: insecure address means nothing is sent, and says so', () => {
    const p = planAutoSend(base({
        protocol: 'http:', hostname: '192.168.1.20',
        creds: { 'github.com': { username: 'x-access-token', token: 't' } },
    }));
    assert.strictEqual(p.reason, REASON.INSECURE);
    assert.deepStrictEqual(p.messages, []);
});

test('planAutoSend: no trust entry means nothing is sent', () => {
    const p = planAutoSend(base({
        trust: null,
        creds: { 'github.com': { username: 'x-access-token', token: 't' } },
    }));
    assert.strictEqual(p.reason, REASON.UNTRUSTED);
    assert.deepStrictEqual(p.messages, []);
});

test('planAutoSend: expired trust is reported and asks the caller to clear it', () => {
    const p = planAutoSend(base({
        trust: { fingerprint: '', savedAt: NOW - TTL - 1 },
        creds: { 'github.com': { username: 'x-access-token', token: 't' } },
    }));
    assert.strictEqual(p.reason, REASON.EXPIRED);
    assert.strictEqual(p.clearTrust, true);
    assert.deepStrictEqual(p.messages, []);
});

test('planAutoSend: trusted but nothing stored sends nothing', () => {
    const p = planAutoSend(base({}));
    assert.strictEqual(p.reason, REASON.NOTHING);
    assert.deepStrictEqual(p.messages, []);
});

// --- planAutoSend: cause 1 ----------------------------------------------

test('planAutoSend: sends a token stored under a host that is NOT the repo origin', () => {
    // The regression User B hit: workspace origin is gitlab, the only saved
    // token is github, and the old code sent nothing at all.
    const p = planAutoSend(base({
        resolvedHost: 'gitlab.example.com',
        creds: { 'github.com': { username: 'x-access-token', token: 'ghp_1', name: 'Ada', email: 'ada@example.com' } },
    }));
    assert.strictEqual(p.reason, REASON.OK);
    assert.strictEqual(p.messages.length, 1);
    assert.strictEqual(p.messages[0].type, 'set_credentials');
    assert.strictEqual(p.messages[0].data.host, 'github.com');
    assert.strictEqual(p.messages[0].data.token, 'ghp_1');
});

test('planAutoSend: every stored host rides in ONE message', () => {
    const p = planAutoSend(base({
        resolvedHost: 'gitlab.example.com',
        creds: {
            'gitlab.example.com': { username: 'oauth2', token: 'glpat', name: 'Ada', email: 'ada@example.com' },
            'github.com': { username: 'x-access-token', token: 'ghp_1' },
        },
    }));
    assert.strictEqual(p.messages.length, 1);
    const d = p.messages[0].data;
    assert.strictEqual(d.host, 'gitlab.example.com');
    assert.strictEqual(d.token, 'glpat');
    assert.deepStrictEqual(d.additional_hosts, [
        { host: 'github.com', username: 'x-access-token', token: 'ghp_1' },
    ]);
});

test('planAutoSend: author identity is taken from whichever bag has one', () => {
    const p = planAutoSend(base({
        resolvedHost: 'gitlab.example.com',
        creds: {
            'gitlab.example.com': { username: 'oauth2', token: 'glpat' },
            'github.com': { username: 'x', token: 'ghp_1', name: 'Ada', email: 'ada@example.com' },
        },
    }));
    assert.strictEqual(p.messages[0].data.name, 'Ada');
    assert.strictEqual(p.messages[0].data.email, 'ada@example.com');
});

test('planAutoSend: hosts with an empty token are skipped', () => {
    const p = planAutoSend(base({
        creds: { 'github.com': { username: 'x', token: '' }, 'gitlab.com': { username: 'y', token: 'g' } },
    }));
    assert.strictEqual(p.messages.length, 1);
    assert.strictEqual(p.messages[0].data.host, 'gitlab.com');
    assert.deepStrictEqual(p.messages[0].data.additional_hosts, []);
});

// --- planAutoSend: signing key + env -------------------------------------

test('planAutoSend: a bound signing key rides on the same credentials message', () => {
    const p = planAutoSend(base({
        trust: { fingerprint: 'SHA256:abc', savedAt: NOW },
        creds: { 'github.com': { username: 'x', token: 't' } },
        keyByFingerprint: { 'SHA256:abc': { pem: 'PEMDATA', label: 'laptop' } },
    }));
    assert.strictEqual(p.messages.length, 1);
    assert.strictEqual(p.messages[0].data.signing_private_key_pem, 'PEMDATA');
    assert.strictEqual(p.messages[0].data.signing_key_label, 'laptop');
});

test('planAutoSend: a signing key with no stored token goes on its own', () => {
    const p = planAutoSend(base({
        trust: { fingerprint: 'SHA256:abc', savedAt: NOW },
        keyByFingerprint: { 'SHA256:abc': { pem: 'PEMDATA', label: 'laptop' } },
    }));
    assert.strictEqual(p.reason, REASON.OK);
    assert.strictEqual(p.messages.length, 1);
    assert.strictEqual(p.messages[0].type, 'set_signing_key');
    assert.strictEqual(p.messages[0].data.signing_private_key_pem, 'PEMDATA');
});

test('planAutoSend: repo env vars ride along when present', () => {
    const p = planAutoSend(base({
        creds: { 'github.com': { username: 'x', token: 't' } },
        envRaw: 'FOO=bar\n',
    }));
    assert.strictEqual(p.messages.length, 2);
    assert.strictEqual(p.messages[1].type, 'set_env');
    assert.strictEqual(p.messages[1].data.raw, 'FOO=bar\n');
});

test('planAutoSend: whitespace-only env blob is not sent', () => {
    const p = planAutoSend(base({
        creds: { 'github.com': { username: 'x', token: 't' } },
        envRaw: '   \n  ',
    }));
    assert.strictEqual(p.messages.length, 1);
});

// --- autoRestoreHint -----------------------------------------------------

test('autoRestoreHint: silent when everything worked', () => {
    assert.strictEqual(autoRestoreHint(REASON.OK), '');
    assert.strictEqual(autoRestoreHint(REASON.NOTHING), '');
});

test('autoRestoreHint: insecure address names https and localhost', () => {
    const h = autoRestoreHint(REASON.INSECURE);
    assert.match(h, /https/);
    assert.match(h, /localhost/);
});

test('autoRestoreHint: untrusted points at the Remember control', () => {
    assert.match(autoRestoreHint(REASON.UNTRUSTED), /Remember on this device/);
});

test('autoRestoreHint: expired says so and points at the Remember control', () => {
    const h = autoRestoreHint(REASON.EXPIRED);
    assert.match(h, /expired/i);
    assert.match(h, /Remember on this device/);
});

test('autoRestoreHint: no repo identity explains why binding is impossible', () => {
    assert.match(autoRestoreHint(REASON.NO_REPO), /git repository/);
});
