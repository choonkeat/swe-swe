/**
 * Unit tests for url-builder.js
 * Run with: node --test url-builder.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { getBaseUrl, buildVSCodeUrl, buildShellUrl, buildPreviewUrl, buildProxyUrl, buildAgentChatUrl, buildPortBasedPreviewUrl, buildPortBasedAgentChatUrl, buildPortBasedProxyUrl, getDebugQueryString } from './url-builder.js';

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
test('buildPreviewUrl returns path-based URL with sessionUUID', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:9898', 'abc-123'),
        'http://localhost:9898/proxy/abc-123/preview'
    );
});

test('buildPreviewUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:9898', null),
        null
    );
});

test('buildPreviewUrl returns null when sessionUUID is empty', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:9898', ''),
        null
    );
});

test('buildPreviewUrl handles https base URL', () => {
    assert.strictEqual(
        buildPreviewUrl('https://example.com', 'uuid-456'),
        'https://example.com/proxy/uuid-456/preview'
    );
});

// buildProxyUrl tests
test('buildProxyUrl with no targetURL returns base with slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', null),
        'http://localhost:9898/proxy/abc-123/preview/'
    );
});

test('buildProxyUrl with empty targetURL returns base with slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', ''),
        'http://localhost:9898/proxy/abc-123/preview/'
    );
});

test('buildProxyUrl extracts path from full URL', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', 'http://localhost:3000/api/health'),
        'http://localhost:9898/proxy/abc-123/preview/api/health'
    );
});

test('buildProxyUrl preserves query string and hash from target', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', 'http://localhost:3000/page?q=1#section'),
        'http://localhost:9898/proxy/abc-123/preview/page?q=1#section'
    );
});

test('buildProxyUrl handles bare path starting with slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', '/some/path'),
        'http://localhost:9898/proxy/abc-123/preview/some/path'
    );
});

test('buildProxyUrl handles bare path without leading slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', 'abc-123', 'some/path'),
        'http://localhost:9898/proxy/abc-123/preview/some/path'
    );
});

test('buildProxyUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:9898', null, 'http://localhost:3000/'),
        null
    );
});

// buildAgentChatUrl tests
test('buildAgentChatUrl returns path-based URL with sessionUUID', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:9898', 'abc-123'),
        'http://localhost:9898/proxy/abc-123/agentchat'
    );
});

test('buildAgentChatUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:9898', null),
        null
    );
});

test('buildAgentChatUrl returns null when sessionUUID is empty', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:9898', ''),
        null
    );
});

test('buildAgentChatUrl handles https base URL', () => {
    assert.strictEqual(
        buildAgentChatUrl('https://example.com', 'uuid-456'),
        'https://example.com/proxy/uuid-456/agentchat'
    );
});

// buildPortBasedPreviewUrl tests
test('buildPortBasedPreviewUrl returns protocol://hostname:port', () => {
    assert.strictEqual(
        buildPortBasedPreviewUrl({ protocol: 'http:', hostname: 'localhost' }, 23000),
        'http://localhost:23000'
    );
});

test('buildPortBasedPreviewUrl returns null when port is null', () => {
    assert.strictEqual(
        buildPortBasedPreviewUrl({ protocol: 'http:', hostname: 'localhost' }, null),
        null
    );
});

test('buildPortBasedPreviewUrl returns null when port is 0', () => {
    assert.strictEqual(
        buildPortBasedPreviewUrl({ protocol: 'http:', hostname: 'localhost' }, 0),
        null
    );
});

test('buildPortBasedPreviewUrl handles https', () => {
    assert.strictEqual(
        buildPortBasedPreviewUrl({ protocol: 'https:', hostname: 'example.com' }, 23000),
        'https://example.com:23000'
    );
});

// buildPortBasedAgentChatUrl tests
test('buildPortBasedAgentChatUrl returns protocol://hostname:port', () => {
    assert.strictEqual(
        buildPortBasedAgentChatUrl({ protocol: 'http:', hostname: 'localhost' }, 24000),
        'http://localhost:24000'
    );
});

test('buildPortBasedAgentChatUrl returns null when port is null', () => {
    assert.strictEqual(
        buildPortBasedAgentChatUrl({ protocol: 'http:', hostname: 'localhost' }, null),
        null
    );
});

test('buildPortBasedAgentChatUrl returns null when port is 0', () => {
    assert.strictEqual(
        buildPortBasedAgentChatUrl({ protocol: 'http:', hostname: 'localhost' }, 0),
        null
    );
});

// buildPortBasedProxyUrl tests
test('buildPortBasedProxyUrl with no targetURL returns base with slash', () => {
    assert.strictEqual(
        buildPortBasedProxyUrl({ protocol: 'http:', hostname: 'localhost' }, 23000, null),
        'http://localhost:23000/'
    );
});

test('buildPortBasedProxyUrl extracts path from full URL', () => {
    assert.strictEqual(
        buildPortBasedProxyUrl({ protocol: 'http:', hostname: 'localhost' }, 23000, 'http://localhost:3000/api/health'),
        'http://localhost:23000/api/health'
    );
});

test('buildPortBasedProxyUrl preserves query and hash', () => {
    assert.strictEqual(
        buildPortBasedProxyUrl({ protocol: 'http:', hostname: 'localhost' }, 23000, 'http://localhost:3000/page?q=1#section'),
        'http://localhost:23000/page?q=1#section'
    );
});

test('buildPortBasedProxyUrl handles bare path', () => {
    assert.strictEqual(
        buildPortBasedProxyUrl({ protocol: 'http:', hostname: 'localhost' }, 23000, '/some/path'),
        'http://localhost:23000/some/path'
    );
});

test('buildPortBasedProxyUrl returns null when port is null', () => {
    assert.strictEqual(
        buildPortBasedProxyUrl({ protocol: 'http:', hostname: 'localhost' }, null, 'http://localhost:3000/'),
        null
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
