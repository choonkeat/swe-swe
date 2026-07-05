/**
 * Unit tests for url-builder.js
 * Run with: node --test url-builder.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { getBaseUrl, buildShellUrl, buildSessionPageUrl, buildPreviewUrl, buildProxyUrl, buildAgentChatUrl, buildPortBasedPreviewUrl, buildPortBasedAgentChatUrl, buildPortBasedFilesUrl, buildPortBasedProxyUrl, buildSubdomainPreviewUrl, buildSubdomainAgentChatUrl, buildSubdomainFilesUrl, buildSubdomainProxyUrl, accessedViaTunnel, getDebugQueryString } from './url-builder.js';

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

// buildSessionPageUrl tests
test('buildSessionPageUrl with assistant only', () => {
    assert.strictEqual(
        buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude' }),
        'http://localhost:8080/session/uuid-1?assistant=claude'
    );
});

test('buildSessionPageUrl with chat session mode', () => {
    assert.strictEqual(
        buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude', session: 'chat' }),
        'http://localhost:8080/session/uuid-1?assistant=claude&session=chat'
    );
});

test('buildSessionPageUrl omits session param for terminal mode', () => {
    assert.strictEqual(
        buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude', session: 'terminal' }),
        'http://localhost:8080/session/uuid-1?assistant=claude'
    );
});

test('buildSessionPageUrl with all optional params', () => {
    const url = buildSessionPageUrl('http://localhost:8080', 'uuid-1', {
        assistant: 'claude',
        session: 'chat',
        name: 'my-session',
        branch: 'feature/foo',
        pwd: '/repos/myproject',
        parent: 'parent-uuid',
        debug: true,
    });
    assert.strictEqual(
        url,
        'http://localhost:8080/session/uuid-1?assistant=claude&session=chat&name=my-session&branch=feature%2Ffoo&pwd=%2Frepos%2Fmyproject&parent=parent-uuid&debug=1'
    );
});

test('buildSessionPageUrl omits pwd when /workspace', () => {
    assert.strictEqual(
        buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude', pwd: '/workspace' }),
        'http://localhost:8080/session/uuid-1?assistant=claude'
    );
});

test('buildSessionPageUrl includes pwd when not /workspace', () => {
    const url = buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude', pwd: '/repos/foo' });
    assert.ok(url.includes('pwd=%2Frepos%2Ffoo'));
});

test('buildSessionPageUrl with debug flag', () => {
    const url = buildSessionPageUrl('http://localhost:8080', 'uuid-1', { assistant: 'claude', debug: true });
    assert.strictEqual(
        url,
        'http://localhost:8080/session/uuid-1?assistant=claude&debug=1'
    );
});

test('buildSessionPageUrl encodes special characters', () => {
    const url = buildSessionPageUrl('http://localhost:8080', 'uuid-1', {
        assistant: 'claude',
        name: 'my session & stuff',
    });
    assert.ok(url.includes('name=my+session+%26+stuff'));
});

// buildPreviewUrl tests
test('buildPreviewUrl returns path-based URL with sessionUUID', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:1977', 'abc-123'),
        'http://localhost:1977/proxy/abc-123/preview'
    );
});

test('buildPreviewUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:1977', null),
        null
    );
});

test('buildPreviewUrl returns null when sessionUUID is empty', () => {
    assert.strictEqual(
        buildPreviewUrl('http://localhost:1977', ''),
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
        buildProxyUrl('http://localhost:1977', 'abc-123', null),
        'http://localhost:1977/proxy/abc-123/preview/'
    );
});

test('buildProxyUrl with empty targetURL returns base with slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', 'abc-123', ''),
        'http://localhost:1977/proxy/abc-123/preview/'
    );
});

test('buildProxyUrl extracts path from full URL', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', 'abc-123', 'http://localhost:3000/api/health'),
        'http://localhost:1977/proxy/abc-123/preview/api/health'
    );
});

test('buildProxyUrl preserves query string and hash from target', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', 'abc-123', 'http://localhost:3000/page?q=1#section'),
        'http://localhost:1977/proxy/abc-123/preview/page?q=1#section'
    );
});

test('buildProxyUrl handles bare path starting with slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', 'abc-123', '/some/path'),
        'http://localhost:1977/proxy/abc-123/preview/some/path'
    );
});

test('buildProxyUrl handles bare path without leading slash', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', 'abc-123', 'some/path'),
        'http://localhost:1977/proxy/abc-123/preview/some/path'
    );
});

test('buildProxyUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildProxyUrl('http://localhost:1977', null, 'http://localhost:3000/'),
        null
    );
});

// buildAgentChatUrl tests
test('buildAgentChatUrl returns path-based URL with sessionUUID', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:1977', 'abc-123'),
        'http://localhost:1977/proxy/abc-123/agentchat'
    );
});

test('buildAgentChatUrl returns null when sessionUUID is null', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:1977', null),
        null
    );
});

test('buildAgentChatUrl returns null when sessionUUID is empty', () => {
    assert.strictEqual(
        buildAgentChatUrl('http://localhost:1977', ''),
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

// buildPortBasedFilesUrl tests
test('buildPortBasedFilesUrl returns protocol://hostname:port', () => {
    assert.strictEqual(
        buildPortBasedFilesUrl({ protocol: 'http:', hostname: 'localhost' }, 29000),
        'http://localhost:29000'
    );
});

test('buildPortBasedFilesUrl returns null when port is null', () => {
    assert.strictEqual(
        buildPortBasedFilesUrl({ protocol: 'http:', hostname: 'localhost' }, null),
        null
    );
});

test('buildPortBasedFilesUrl returns null when port is 0', () => {
    assert.strictEqual(
        buildPortBasedFilesUrl({ protocol: 'http:', hostname: 'localhost' }, 0),
        null
    );
});

test('buildPortBasedFilesUrl handles https', () => {
    assert.strictEqual(
        buildPortBasedFilesUrl({ protocol: 'https:', hostname: 'example.com' }, 29000),
        'https://example.com:29000'
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

// buildSubdomainPreviewUrl tests (tunnel mode)
test('buildSubdomainPreviewUrl returns protocol://port.publicHostname', () => {
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'https:' }, 3000, 'abc-tunnel.example.com'),
        'https://3000.abc-tunnel.example.com'
    );
});

test('buildSubdomainPreviewUrl handles http', () => {
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'http:' }, 3000, 'foo.example.com'),
        'http://3000.foo.example.com'
    );
});

test('buildSubdomainPreviewUrl uses raw target port (not proxyPortOffset)', () => {
    // 3000 maps to "3000.host", not "23000.host" -- the tunnel demuxes by
    // leftmost label and forwards to 127.0.0.1:3000.
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'https:' }, 3000, 'abc.example.com'),
        'https://3000.abc.example.com'
    );
});

test('buildSubdomainPreviewUrl returns null when targetPort is null', () => {
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'https:' }, null, 'abc.example.com'),
        null
    );
});

test('buildSubdomainPreviewUrl returns null when publicHostname is empty', () => {
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'https:' }, 3000, ''),
        null
    );
});

test('buildSubdomainPreviewUrl returns null when targetPort is 0', () => {
    assert.strictEqual(
        buildSubdomainPreviewUrl({ protocol: 'https:' }, 0, 'abc.example.com'),
        null
    );
});

// buildSubdomainAgentChatUrl tests
test('buildSubdomainAgentChatUrl returns protocol://port.publicHostname', () => {
    assert.strictEqual(
        buildSubdomainAgentChatUrl({ protocol: 'https:' }, 4000, 'abc-tunnel.example.com'),
        'https://4000.abc-tunnel.example.com'
    );
});

test('buildSubdomainAgentChatUrl returns null when publicHostname missing', () => {
    assert.strictEqual(
        buildSubdomainAgentChatUrl({ protocol: 'https:' }, 4000, ''),
        null
    );
});

test('buildSubdomainAgentChatUrl returns null when targetPort missing', () => {
    assert.strictEqual(
        buildSubdomainAgentChatUrl({ protocol: 'https:' }, null, 'abc.example.com'),
        null
    );
});

// buildSubdomainFilesUrl tests
test('buildSubdomainFilesUrl returns protocol://port.publicHostname', () => {
    assert.strictEqual(
        buildSubdomainFilesUrl({ protocol: 'https:' }, 29000, 'abc-tunnel.example.com'),
        'https://29000.abc-tunnel.example.com'
    );
});

test('buildSubdomainFilesUrl returns null when publicHostname missing', () => {
    assert.strictEqual(
        buildSubdomainFilesUrl({ protocol: 'https:' }, 29000, ''),
        null
    );
});

test('buildSubdomainFilesUrl returns null when targetPort missing', () => {
    assert.strictEqual(
        buildSubdomainFilesUrl({ protocol: 'https:' }, null, 'abc.example.com'),
        null
    );
});

// buildSubdomainProxyUrl tests
test('buildSubdomainProxyUrl with no targetURL returns base with slash', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, 'abc.example.com', null),
        'https://3000.abc.example.com/'
    );
});

test('buildSubdomainProxyUrl extracts path from full URL', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, 'abc.example.com', 'http://localhost:3000/api/health'),
        'https://3000.abc.example.com/api/health'
    );
});

test('buildSubdomainProxyUrl preserves query and hash', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, 'abc.example.com', 'http://localhost:3000/page?q=1#section'),
        'https://3000.abc.example.com/page?q=1#section'
    );
});

test('buildSubdomainProxyUrl handles bare path with leading slash', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, 'abc.example.com', '/some/path'),
        'https://3000.abc.example.com/some/path'
    );
});

test('buildSubdomainProxyUrl handles bare path without leading slash', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, 'abc.example.com', 'some/path'),
        'https://3000.abc.example.com/some/path'
    );
});

test('buildSubdomainProxyUrl returns null when publicHostname missing', () => {
    assert.strictEqual(
        buildSubdomainProxyUrl({ protocol: 'https:' }, 3000, '', 'http://localhost:3000/'),
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

// accessedViaTunnel tests
test('accessedViaTunnel false when publicHostname empty (not tunnel mode)', () => {
    assert.strictEqual(accessedViaTunnel({ hostname: 'localhost' }, ''), false);
});

test('accessedViaTunnel false on localhost even when server is in tunnel mode', () => {
    // The bug this fixes: server injects publicHostname, but the browser
    // reached the page over localhost, so subdomain URLs are unreachable.
    assert.strictEqual(accessedViaTunnel({ hostname: 'localhost' }, 'abc-tunnel.example.com'), false);
});

test('accessedViaTunnel false on LAN IP / Tailscale name in tunnel mode', () => {
    assert.strictEqual(accessedViaTunnel({ hostname: '192.168.1.5' }, 'abc-tunnel.example.com'), false);
    assert.strictEqual(accessedViaTunnel({ hostname: 'mybox.tail-scale.ts.net' }, 'abc-tunnel.example.com'), false);
});

test('accessedViaTunnel true when loaded via the {port}.{publicHostname} tunnel host', () => {
    assert.strictEqual(accessedViaTunnel({ hostname: '1977.abc-tunnel.example.com' }, 'abc-tunnel.example.com'), true);
    assert.strictEqual(accessedViaTunnel({ hostname: '23000.abc-tunnel.example.com' }, 'abc-tunnel.example.com'), true);
});

test('accessedViaTunnel true when loaded via the apex publicHostname', () => {
    assert.strictEqual(accessedViaTunnel({ hostname: 'abc-tunnel.example.com' }, 'abc-tunnel.example.com'), true);
});

test('accessedViaTunnel does not match a lookalike suffix without a dot boundary', () => {
    // "evil-abc-tunnel.example.com" must NOT be treated as the tunnel host.
    assert.strictEqual(accessedViaTunnel({ hostname: 'evilabc-tunnel.example.com' }, 'abc-tunnel.example.com'), false);
});
