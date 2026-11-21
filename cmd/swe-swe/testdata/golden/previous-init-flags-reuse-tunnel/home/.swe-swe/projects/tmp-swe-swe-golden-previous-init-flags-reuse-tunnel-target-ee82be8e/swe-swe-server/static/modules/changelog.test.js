/**
 * Unit tests for changelog.js
 * Run with: node --test changelog.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync } from 'node:fs';
import { parseChangelog, inlineToHtml, isUnread } from './changelog.js';

const SAMPLE = [
    '# CHANGELOG',
    '',
    '## Unreleased',
    '',
    '- not shipped yet',
    '',
    '## v2.43.2 - Locked Doors',
    '',
    '### Fixes',
    '',
    '- **Login redirect** is safer.',
    '- Session names',
    '  are escaped.',
    '',
    '## v2.43.0 - Env Vars Up Front',
    '',
    '### Features',
    '',
    '- Edit env vars.',
    '',
    '### Fixes',
    '',
    '- Certs.',
    '',
    '## v2.42.0',
    '',
    '- loose bullet',
].join('\n');

test('parseChangelog reads releases in file order and skips non-version headings', () => {
    const r = parseChangelog(SAMPLE);
    assert.deepStrictEqual(r.map((x) => x.version), ['2.43.2', '2.43.0', '2.42.0']);
    assert.strictEqual(r[0].title, 'Locked Doors');
    assert.strictEqual(r[2].title, '');
});

test('parseChangelog groups bullets under their ### section and joins continuation lines', () => {
    const r = parseChangelog(SAMPLE);
    assert.deepStrictEqual(r[0].sections, [
        { heading: 'Fixes', items: ['**Login redirect** is safer.', 'Session names are escaped.'] },
    ]);
    assert.deepStrictEqual(r[1].sections.map((s) => s.heading), ['Features', 'Fixes']);
    assert.deepStrictEqual(r[2].sections, [{ heading: '', items: ['loose bullet'] }]);
});

test('parseChangelog honours the limit', () => {
    assert.deepStrictEqual(parseChangelog(SAMPLE, 1).map((x) => x.version), ['2.43.2']);
    assert.deepStrictEqual(parseChangelog(SAMPLE, 2).map((x) => x.version), ['2.43.2', '2.43.0']);
    assert.strictEqual(parseChangelog(SAMPLE, 10).length, 3);
});

test('parseChangelog tolerates empty or garbage input', () => {
    assert.deepStrictEqual(parseChangelog(''), []);
    assert.deepStrictEqual(parseChangelog(null), []);
    assert.deepStrictEqual(parseChangelog('<html>404</html>'), []);
});

test('parseChangelog parses the real CHANGELOG.md', () => {
    const text = readFileSync(new URL('../../../../../../../CHANGELOG.md', import.meta.url), 'utf8');
    const r = parseChangelog(text, 3);
    assert.strictEqual(r.length, 3);
    for (const rel of r) {
        assert.match(rel.version, /^\d+\.\d+\.\d+/);
        assert.ok(rel.sections.length > 0, rel.version + ' has sections');
        assert.ok(rel.sections.every((s) => s.items.length > 0), rel.version + ' sections have items');
    }
});

const BASE = 'https://github.com/o/r/blob/main/';

test('inlineToHtml renders bold, code and links', () => {
    assert.strictEqual(inlineToHtml('**a** and `b`', BASE), '<strong>a</strong> and <code>b</code>');
    assert.strictEqual(
        inlineToHtml('See [ADR](docs/adr/1.md).', BASE),
        'See <a href="https://github.com/o/r/blob/main/docs/adr/1.md" target="_blank" rel="noopener">ADR</a>.'
    );
});

test('inlineToHtml escapes markup, including inside code spans', () => {
    assert.strictEqual(inlineToHtml('<img src=x onerror=alert(1)>', BASE), '&lt;img src=x onerror=alert(1)&gt;');
    assert.strictEqual(inlineToHtml('`<b>` and `**x**`', BASE), '<code>&lt;b&gt;</code> and <code>**x**</code>');
});

test('inlineToHtml drops non-http link targets', () => {
    assert.strictEqual(inlineToHtml('[x](javascript:void0)', BASE), 'x');
});

test('isUnread: never opened is read, newer release is unread, same or older is read', () => {
    assert.strictEqual(isUnread('2.43.2', null), false);
    assert.strictEqual(isUnread('2.43.2', ''), false);
    assert.strictEqual(isUnread('2.43.2', '2.43.1'), true);
    assert.strictEqual(isUnread('2.43.2', '2.43.2'), false);
    assert.strictEqual(isUnread('2.43.2', '2.44.0'), false);
    assert.strictEqual(isUnread(null, null), false);
});
