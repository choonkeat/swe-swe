/**
 * Unit tests for url-builder.js
 * Run with: node --test url-builder.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { getBaseUrl, buildVSCodeUrl, buildShellUrl, buildPreviewUrl, getDebugQueryString } from './url-builder.js';

// getBaseUrl tests
test('getBaseUrl with port returns protocol://hostname:port', () => {
    assert.strictEqual(
        getBaseUrl({ protocol: 'http:', hostname: 'localhost', port: '8080' }),
        'http://localhost:8080'
    );
});

test('getBaseUrl without port returns protocol://hostname', () => {
    assert.strictEqual(
        getBaseUrl({ protocol: 'https:', hostname: 'example.com', port: '' }),
        'https://example.com'
    );
});

test('getBaseUrl handles https with custom port', () => {
    assert.strictEqual(
        getBaseUrl({ protocol: 'https:', hostname: 'secure.example.com', port: '443' }),
        'https://secure.example.com:443'
    );
});

test('getBaseUrl handles localhost without port', () => {
    assert.strictEqual(
        getBaseUrl({ protocol: 'http:', hostname: 'localhost', port: '' }),
        'http://localhost'
    );
});

// buildVSCodeUrl tests
test('buildVSCodeUrl with workDir encodes folder parameter', () => {
    assert.strictEqual(
        buildVSCodeUrl('http://localhost:8080', '/workspace'),
        'http://localhost:8080/vscode/?folder=%2Fworkspace'
    );
});

test('buildVSCodeUrl without workDir returns base vscode path', () => {
    assert.strictEqual(
        buildVSCodeUrl('http://localhost:8080', ''),
        'http://localhost:8080/vscode/'
    );
});

test('buildVSCodeUrl with null workDir returns base vscode path', () => {
    assert.strictEqual(
        buildVSCodeUrl('http://localhost:8080', null),
        'http://localhost:8080/vscode/'
    );
});

test('buildVSCodeUrl encodes special characters in workDir', () => {
    assert.strictEqual(
        buildVSCodeUrl('http://localhost:8080', '/path with spaces'),
        'http://localhost:8080/vscode/?folder=%2Fpath%20with%20spaces'
    );
});

test('buildVSCodeUrl handles https base URL', () => {
    assert.strictEqual(
        buildVSCodeUrl('https://example.com', '/home/user/project'),
        'https://example.com/vscode/?folder=%2Fhome%2Fuser%2Fproject'
    );
});

// buildShellUrl tests
test('buildShellUrl without debug mode', () => {
    assert.strictEqual(
        buildShellUrl({
            baseUrl: 'http://localhost:8080',
            shellUUID: 'abc-123',
            parentUUID: 'parent-456',
            debug: false
        }),
        'http://localhost:8080/session/abc-123?assistant=shell&parent=parent-456'
    );
});

test('buildShellUrl with debug mode', () => {
    assert.strictEqual(
        buildShellUrl({
            baseUrl: 'http://localhost:8080',
            shellUUID: 'abc-123',
            parentUUID: 'parent-456',
            debug: true
        }),
        'http://localhost:8080/session/abc-123?assistant=shell&parent=parent-456&debug=1'
    );
});

test('buildShellUrl encodes parentUUID with special characters', () => {
    assert.strictEqual(
        buildShellUrl({
            baseUrl: 'http://localhost:8080',
            shellUUID: 'shell-uuid',
            parentUUID: 'parent/with+special&chars',
            debug: false
        }),
        'http://localhost:8080/session/shell-uuid?assistant=shell&parent=parent%2Fwith%2Bspecial%26chars'
    );
});

test('buildShellUrl handles https base URL', () => {
    assert.strictEqual(
        buildShellUrl({
            baseUrl: 'https://secure.example.com',
            shellUUID: 'secure-shell',
            parentUUID: 'secure-parent',
            debug: true
        }),
        'https://secure.example.com/session/secure-shell?assistant=shell&parent=secure-parent&debug=1'
    );
});

// buildPreviewUrl tests
test('buildPreviewUrl uses explicit preview port when provided', () => {
    assert.strictEqual(
        buildPreviewUrl({ protocol: 'https:', hostname: 'example.com', port: '8080' }, 53007),
        'https://example.com:53007'
    );
});

test('buildPreviewUrl prefixes port with 1', () => {
    assert.strictEqual(
        buildPreviewUrl({ protocol: 'https:', hostname: 'example.com', port: '8080' }),
        'https://example.com:18080'
    );
});

test('buildPreviewUrl defaults to port 80 when empty', () => {
    assert.strictEqual(
        buildPreviewUrl({ protocol: 'http:', hostname: 'localhost', port: '' }),
        'http://localhost:180'
    );
});

test('buildPreviewUrl handles 443 port', () => {
    assert.strictEqual(
        buildPreviewUrl({ protocol: 'https:', hostname: 'secure.com', port: '443' }),
        'https://secure.com:1443'
    );
});

test('buildPreviewUrl handles localhost with custom port', () => {
    assert.strictEqual(
        buildPreviewUrl({ protocol: 'http:', hostname: 'localhost', port: '9770' }),
        'http://localhost:19770'
    );
});

// getDebugQueryString tests
test('getDebugQueryString returns ?debug=1 when true', () => {
    assert.strictEqual(getDebugQueryString(true), '?debug=1');
});

test('getDebugQueryString returns empty string when false', () => {
    assert.strictEqual(getDebugQueryString(false), '');
});

test('getDebugQueryString returns empty string for falsy values', () => {
    assert.strictEqual(getDebugQueryString(null), '');
    assert.strictEqual(getDebugQueryString(undefined), '');
    assert.strictEqual(getDebugQueryString(0), '');
});

test('getDebugQueryString returns ?debug=1 for truthy values', () => {
    assert.strictEqual(getDebugQueryString(1), '?debug=1');
    assert.strictEqual(getDebugQueryString('yes'), '?debug=1');
});
