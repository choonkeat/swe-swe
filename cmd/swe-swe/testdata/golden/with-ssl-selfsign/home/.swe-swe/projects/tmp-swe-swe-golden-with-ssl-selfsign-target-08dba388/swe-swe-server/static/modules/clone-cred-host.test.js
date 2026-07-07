/**
 * Unit tests for clone-cred-host.js
 * Run with: node --test clone-cred-host.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { parseCloneHost } from './clone-cred-host.js';

test('https URL returns the host', () => {
    assert.strictEqual(parseCloneHost('https://github.com/acme/private.git'), 'github.com');
});

test('https URL with userinfo strips the user', () => {
    assert.strictEqual(parseCloneHost('https://x-access-token@github.com/acme/private.git'), 'github.com');
});

test('https URL with port strips the port', () => {
    assert.strictEqual(parseCloneHost('https://gitlab.example.com:8443/acme/private.git'), 'gitlab.example.com');
});

test('scp-like SSH URL returns the host', () => {
    assert.strictEqual(parseCloneHost('git@github.com:acme/private.git'), 'github.com');
});

test('ssh:// URL returns the host', () => {
    assert.strictEqual(parseCloneHost('ssh://git@gitlab.example.com/acme/private.git'), 'gitlab.example.com');
});

test('host is lowercased', () => {
    assert.strictEqual(parseCloneHost('https://GitHub.COM/acme/x.git'), 'github.com');
});

test('surrounding whitespace is trimmed', () => {
    assert.strictEqual(parseCloneHost('  https://github.com/acme/x.git  '), 'github.com');
});

test('empty or garbage returns empty string', () => {
    assert.strictEqual(parseCloneHost(''), '');
    assert.strictEqual(parseCloneHost('   '), '');
    assert.strictEqual(parseCloneHost('not a url'), '');
    assert.strictEqual(parseCloneHost(null), '');
    assert.strictEqual(parseCloneHost(undefined), '');
});
