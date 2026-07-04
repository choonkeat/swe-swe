/**
 * Unit tests for status-renderer.js
 * Run with: node --test status-renderer.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    getStatusBarClasses,
    renderStatusText,
    renderStatusInfo,
    renderServiceLinks,
    renderCustomLinks,
    renderAssistantLink
} from './status-renderer.js';

// getStatusBarClasses tests
test('getStatusBarClasses returns base class for empty state', () => {
    const classes = getStatusBarClasses({});
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar']);
});

test('getStatusBarClasses adds state class', () => {
    const classes = getStatusBarClasses({ state: 'connected' });
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar', 'connected']);
});

test('getStatusBarClasses adds multiuser for multiple viewers', () => {
    const classes = getStatusBarClasses({ state: 'connected', viewers: 2 });
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar', 'connected', 'multiuser']);
});

test('getStatusBarClasses does not add multiuser for single viewer', () => {
    const classes = getStatusBarClasses({ state: 'connected', viewers: 1 });
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar', 'connected']);
});

test('getStatusBarClasses adds yolo class when yoloMode true', () => {
    const classes = getStatusBarClasses({ state: 'connected', yoloMode: true });
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar', 'connected', 'yolo']);
});

test('getStatusBarClasses combines all classes', () => {
    const classes = getStatusBarClasses({ state: 'connected', viewers: 3, yoloMode: true });
    assert.deepStrictEqual(classes, ['terminal-ui__status-bar', 'connected', 'multiuser', 'yolo']);
});

// renderStatusText tests
test('renderStatusText returns message as-is', () => {
    const html = renderStatusText({ message: 'Connecting...' });
    assert.strictEqual(html, 'Connecting...');
});

test('renderStatusText returns empty string for no message', () => {
    const html = renderStatusText({});
    assert.strictEqual(html, '');
});

test('renderStatusText preserves HTML in message', () => {
    const html = renderStatusText({ message: '<a href="/">Link</a>' });
    assert.strictEqual(html, '<a href="/">Link</a>');
});

// renderStatusInfo tests
test('renderStatusInfo returns empty for disconnected', () => {
    const html = renderStatusInfo({ connected: false });
    assert.strictEqual(html, '');
});

test('renderStatusInfo shows Connected for non-YOLO mode', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        yoloMode: false,
        yoloSupported: false
    });
    assert.match(html, /Connected as/);
    assert.match(html, /Alice/);
});

test('renderStatusInfo shows YOLO for yoloMode', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        yoloMode: true,
        yoloSupported: true
    });
    assert.match(html, /YOLO<\/span> as/);
});

test('renderStatusInfo makes status word clickable when yoloSupported', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        yoloMode: false,
        yoloSupported: true
    });
    assert.match(html, /terminal-ui__status-yolo-toggle/);
    assert.match(html, /Connected<\/span>/);
});

test('renderStatusInfo shows plain status word when yolo not supported', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        yoloMode: false,
        yoloSupported: false
    });
    assert.doesNotMatch(html, /terminal-ui__status-yolo-toggle/);
});

test('renderStatusInfo shows assistant with link', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        assistantName: 'Claude',
        yoloMode: false
    });
    assert.match(html, /with.*Claude/);
    assert.match(html, /<a href/);
    assert.match(html, /terminal-ui__status-agent/);
});

test('renderStatusInfo shows others count for multiple viewers', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        viewers: 3
    });
    assert.match(html, /2 others/);
});

test('renderStatusInfo does not show others for single viewer', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        viewers: 1
    });
    assert.doesNotMatch(html, /others/);
});

test('renderStatusInfo shows session name', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        sessionName: 'my-session'
    });
    assert.match(html, /on.*my-session/);
});

test('renderStatusInfo shows unnamed session with uuidShort fallback', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        sessionName: '',
        uuidShort: 'abc123'
    });
    assert.match(html, /Unnamed session abc123/);
});

test('renderStatusInfo escapes HTML in user content', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: '<script>alert(1)</script>',
        sessionName: '<evil>'
    });
    assert.match(html, /&lt;script&gt;/);
    assert.match(html, /&lt;evil&gt;/);
    assert.doesNotMatch(html, /<script>/);
});

test('renderStatusInfo includes debug query string', () => {
    const html = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        assistantName: 'Claude',
        debugMode: true
    });
    assert.match(html, /\?debug=1/);
});

// renderServiceLinks tests
test('renderServiceLinks returns empty for no services', () => {
    const html = renderServiceLinks({ services: [] });
    assert.strictEqual(html, '');
});

test('renderServiceLinks returns empty for undefined services', () => {
    const html = renderServiceLinks({});
    assert.strictEqual(html, '');
});

test('renderServiceLinks renders single service', () => {
    const html = renderServiceLinks({
        services: [{ name: 'vscode', label: 'VSCode', url: '/vscode/' }]
    });
    assert.match(html, /terminal-ui__status-service-links/);
    assert.match(html, /href="\/vscode\/"/);
    assert.match(html, /target="swe-swe-vscode"/);
    assert.match(html, /data-tab="vscode"/);
    assert.match(html, />VSCode</);
});

test('renderServiceLinks renders multiple services with separators', () => {
    const html = renderServiceLinks({
        services: [
            { name: 'shell', label: 'Shell', url: '/shell/' },
            { name: 'vscode', label: 'VSCode', url: '/vscode/' },
            { name: 'preview', label: 'Preview', url: '/preview/' }
        ]
    });
    assert.match(html, />Shell</);
    assert.match(html, />VSCode</);
    assert.match(html, />Preview</);
    // Check for separators between (but not after last)
    const separatorMatches = html.match(/terminal-ui__status-link-sep/g);
    assert.strictEqual(separatorMatches.length, 2);
});

test('renderServiceLinks escapes HTML in service data', () => {
    const html = renderServiceLinks({
        services: [{ name: 'test', label: '<script>', url: '/test?a=1&b=2' }]
    });
    assert.match(html, /&lt;script&gt;/);
    assert.match(html, /&amp;b=2/);
});

// renderCustomLinks tests
test('renderCustomLinks returns empty for empty string', () => {
    const html = renderCustomLinks('');
    assert.strictEqual(html, '');
});

test('renderCustomLinks renders single link', () => {
    const html = renderCustomLinks('[Docs](https://example.com)');
    assert.match(html, /terminal-ui__status-links/);
    assert.match(html, /href="https:\/\/example.com"/);
    assert.match(html, /target="_blank"/);
    assert.match(html, /rel="noopener noreferrer"/);
    assert.match(html, />Docs</);
});

test('renderCustomLinks renders multiple links with separators', () => {
    const html = renderCustomLinks('[One](http://one.com) [Two](http://two.com)');
    assert.match(html, />One</);
    assert.match(html, />Two</);
    const separatorMatches = html.match(/terminal-ui__status-link-sep/g);
    assert.strictEqual(separatorMatches.length, 1);
});

test('renderCustomLinks escapes HTML in link text', () => {
    const html = renderCustomLinks('[<script>](http://evil.com)');
    assert.match(html, /&lt;script&gt;/);
    assert.doesNotMatch(html, /<script>/);
});

// renderAssistantLink tests
test('renderAssistantLink uses assistantName if present', () => {
    const html = renderAssistantLink({
        assistantName: 'Claude',
        assistant: 'claude-opus',
        debugMode: false
    });
    assert.match(html, />Claude</);
    assert.doesNotMatch(html, /claude-opus/);
});

test('renderAssistantLink falls back to assistant', () => {
    const html = renderAssistantLink({
        assistant: 'claude-opus',
        debugMode: false
    });
    assert.match(html, />claude-opus</);
});

test('renderAssistantLink includes debug query string', () => {
    const html = renderAssistantLink({
        assistantName: 'Claude',
        debugMode: true
    });
    assert.match(html, /\?debug=1/);
});

test('renderAssistantLink escapes HTML in name', () => {
    const html = renderAssistantLink({
        assistantName: '<script>',
        debugMode: false
    });
    assert.match(html, /&lt;script&gt;/);
    assert.doesNotMatch(html, /<script>/);
});

test('renderAssistantLink sets correct target', () => {
    const html = renderAssistantLink({
        assistantName: 'Claude',
        debugMode: false
    });
    assert.match(html, /target="swe-swe-model-selector"/);
});

// Integration test - complete rendering workflow
test('integration: render full connected status', () => {
    // Get classes
    const classes = getStatusBarClasses({
        state: 'connected',
        viewers: 2,
        yoloMode: true
    });
    assert.ok(classes.includes('connected'));
    assert.ok(classes.includes('multiuser'));
    assert.ok(classes.includes('yolo'));

    // Get status info
    const statusInfo = renderStatusInfo({
        connected: true,
        userName: 'Alice',
        assistantName: 'Claude',
        sessionName: 'my-project',
        viewers: 2,
        yoloMode: true,
        yoloSupported: true,
        debugMode: false
    });
    assert.match(statusInfo, /YOLO<\/span> as/);
    assert.match(statusInfo, /Alice/);
    assert.match(statusInfo, /Claude/);
    assert.match(statusInfo, /1 others/);
    assert.match(statusInfo, /my-project/);

    // Get service links
    const serviceLinks = renderServiceLinks({
        services: [
            { name: 'shell', label: 'Shell', url: '/shell/' },
            { name: 'vscode', label: 'VSCode', url: '/vscode/' }
        ]
    });
    assert.match(serviceLinks, /Shell/);
    assert.match(serviceLinks, /VSCode/);

    // Get custom links
    const customLinks = renderCustomLinks('[Docs](http://docs.example.com)');
    assert.match(customLinks, /Docs/);
});
