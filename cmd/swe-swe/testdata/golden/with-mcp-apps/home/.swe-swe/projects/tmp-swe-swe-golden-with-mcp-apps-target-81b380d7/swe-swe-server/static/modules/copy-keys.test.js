/**
 * Unit tests for copy-keys.js
 * Run with: node --test copy-keys.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { isApplePlatform, copyHint, isCopyShortcut } from './copy-keys.js';

const key = (o) => Object.assign({ type: 'keydown', ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, code: 'KeyC', key: 'C' }, o);

test('isApplePlatform: Mac, iPhone and iPad only', () => {
    assert.strictEqual(isApplePlatform('MacIntel'), true);
    assert.strictEqual(isApplePlatform('iPhone'), true);
    assert.strictEqual(isApplePlatform('Win32'), false);
    assert.strictEqual(isApplePlatform('Linux x86_64'), false);
    assert.strictEqual(isApplePlatform(undefined), false);
});

test('copyHint: names the keys for this keyboard', () => {
    assert.strictEqual(copyHint(true), 'Press \u2318C to copy');
    assert.strictEqual(copyHint(false), 'Press Ctrl+Shift+C to copy');
});

test('isCopyShortcut: Ctrl+Shift+C off Apple', () => {
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, shiftKey: true }), false), true);
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, shiftKey: true, code: 'KeyC', key: 'c' }), false), true);
});

test('isCopyShortcut: Ctrl+C alone stays the interrupt', () => {
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, key: 'c' }), false), false);
});

test('isCopyShortcut: never on Apple, never on keyup, never with other modifiers', () => {
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, shiftKey: true }), true), false);
    assert.strictEqual(isCopyShortcut(key({ type: 'keyup', ctrlKey: true, shiftKey: true }), false), false);
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, shiftKey: true, altKey: true }), false), false);
    assert.strictEqual(isCopyShortcut(key({ ctrlKey: true, shiftKey: true, code: 'KeyV', key: 'V' }), false), false);
});
