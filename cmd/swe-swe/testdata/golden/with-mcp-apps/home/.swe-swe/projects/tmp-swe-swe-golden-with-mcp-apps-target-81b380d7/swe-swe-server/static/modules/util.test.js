/**
 * Unit tests for util.js
 * Run with: node --test util.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { formatDuration, formatFileSize, escapeHtml, escapeFilename, parseLinks } from './util.js';

// formatDuration tests
test('formatDuration returns seconds only for short durations', () => {
    assert.strictEqual(formatDuration(0), '0s');
    assert.strictEqual(formatDuration(5000), '5s');
    assert.strictEqual(formatDuration(59000), '59s');
});

test('formatDuration includes minutes when >= 60 seconds', () => {
    assert.strictEqual(formatDuration(60000), '1m 0s');
    assert.strictEqual(formatDuration(65000), '1m 5s');
    assert.strictEqual(formatDuration(125000), '2m 5s');
});

test('formatDuration includes hours when >= 60 minutes', () => {
    assert.strictEqual(formatDuration(3600000), '1h 0m 0s');
    assert.strictEqual(formatDuration(3665000), '1h 1m 5s');
    assert.strictEqual(formatDuration(7325000), '2h 2m 5s');
});

// formatFileSize tests
test('formatFileSize returns bytes for small files', () => {
    assert.strictEqual(formatFileSize(0), '0 B');
    assert.strictEqual(formatFileSize(500), '500 B');
    assert.strictEqual(formatFileSize(1023), '1023 B');
});

test('formatFileSize returns KB for files >= 1KB', () => {
    assert.strictEqual(formatFileSize(1024), '1.0 KB');
    assert.strictEqual(formatFileSize(1536), '1.5 KB');
    assert.strictEqual(formatFileSize(10240), '10.0 KB');
});

test('formatFileSize returns MB for files >= 1MB', () => {
    assert.strictEqual(formatFileSize(1024 * 1024), '1.0 MB');
    assert.strictEqual(formatFileSize(1572864), '1.5 MB');
    assert.strictEqual(formatFileSize(10485760), '10.0 MB');
});

// escapeHtml tests
test('escapeHtml escapes angle brackets', () => {
    assert.strictEqual(escapeHtml('<script>'), '&lt;script&gt;');
    assert.strictEqual(escapeHtml('<div>test</div>'), '&lt;div&gt;test&lt;/div&gt;');
});

test('escapeHtml escapes ampersands', () => {
    assert.strictEqual(escapeHtml('foo & bar'), 'foo &amp; bar');
    assert.strictEqual(escapeHtml('&'), '&amp;');
});

test('escapeHtml escapes double quotes', () => {
    assert.strictEqual(escapeHtml('"foo"'), '&quot;foo&quot;');
    assert.strictEqual(escapeHtml('say "hello"'), 'say &quot;hello&quot;');
});

test('escapeHtml handles mixed special characters', () => {
    assert.strictEqual(escapeHtml('<a href="test">'), '&lt;a href=&quot;test&quot;&gt;');
    assert.strictEqual(escapeHtml('a & b < c > d "e"'), 'a &amp; b &lt; c &gt; d &quot;e&quot;');
});

test('escapeHtml passes through normal text unchanged', () => {
    assert.strictEqual(escapeHtml('hello world'), 'hello world');
    assert.strictEqual(escapeHtml(''), '');
});

test('escapeHtml returns empty string for null and undefined', () => {
    assert.strictEqual(escapeHtml(null), '');
    assert.strictEqual(escapeHtml(undefined), '');
});

// escapeFilename tests
test('escapeFilename escapes spaces', () => {
    assert.strictEqual(escapeFilename('foo bar'), 'foo\\ bar');
    assert.strictEqual(escapeFilename('my file.txt'), 'my\\ file.txt');
});

test('escapeFilename escapes single quotes', () => {
    assert.strictEqual(escapeFilename("it's"), "it\\'s");
});

test('escapeFilename escapes double quotes', () => {
    assert.strictEqual(escapeFilename('say "hi"'), 'say\\ \\"hi\\"');
});

test('escapeFilename escapes backslashes', () => {
    assert.strictEqual(escapeFilename('a\\b'), 'a\\\\b');
});

test('escapeFilename escapes dollar signs', () => {
    assert.strictEqual(escapeFilename('$HOME'), '\\$HOME');
});

test('escapeFilename escapes backticks', () => {
    assert.strictEqual(escapeFilename('`cmd`'), '\\`cmd\\`');
});

test('escapeFilename escapes exclamation marks', () => {
    assert.strictEqual(escapeFilename('hello!'), 'hello\\!');
});

test('escapeFilename passes through normal filenames unchanged', () => {
    assert.strictEqual(escapeFilename('file.txt'), 'file.txt');
    assert.strictEqual(escapeFilename('my-file_v2.0.tar.gz'), 'my-file_v2.0.tar.gz');
});

// parseLinks tests
test('parseLinks returns empty array for null/undefined/empty input', () => {
    assert.deepStrictEqual(parseLinks(null), []);
    assert.deepStrictEqual(parseLinks(undefined), []);
    assert.deepStrictEqual(parseLinks(''), []);
});

test('parseLinks parses single markdown link', () => {
    const result = parseLinks('[Google](https://google.com)');
    assert.deepStrictEqual(result, [{ text: 'Google', url: 'https://google.com' }]);
});

test('parseLinks parses multiple markdown links', () => {
    const result = parseLinks('[One](http://one.com) [Two](http://two.com)');
    assert.deepStrictEqual(result, [
        { text: 'One', url: 'http://one.com' },
        { text: 'Two', url: 'http://two.com' }
    ]);
});

test('parseLinks handles links with spaces in text', () => {
    const result = parseLinks('[My Website](https://example.com)');
    assert.deepStrictEqual(result, [{ text: 'My Website', url: 'https://example.com' }]);
});

test('parseLinks returns empty array for malformed links', () => {
    assert.deepStrictEqual(parseLinks('[no url]'), []);
    assert.deepStrictEqual(parseLinks('(no text)'), []);
    assert.deepStrictEqual(parseLinks('just plain text'), []);
});
