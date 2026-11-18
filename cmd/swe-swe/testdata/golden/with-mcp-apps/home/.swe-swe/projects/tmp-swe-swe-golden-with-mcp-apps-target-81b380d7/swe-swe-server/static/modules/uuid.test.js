/**
 * Unit tests for uuid.js
 * Run with: node --test uuid.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { djb2Hash, deriveShellUUID } from './uuid.js';

// djb2Hash tests
test('djb2Hash returns same value for same input (deterministic)', () => {
    assert.strictEqual(djb2Hash('hello'), djb2Hash('hello'));
    assert.strictEqual(djb2Hash('world'), djb2Hash('world'));
    assert.strictEqual(djb2Hash('test123'), djb2Hash('test123'));
});

test('djb2Hash returns different values for different inputs', () => {
    assert.notStrictEqual(djb2Hash('hello'), djb2Hash('world'));
    assert.notStrictEqual(djb2Hash('foo'), djb2Hash('bar'));
    assert.notStrictEqual(djb2Hash('abc'), djb2Hash('xyz'));
});

test('djb2Hash returns a number', () => {
    assert.strictEqual(typeof djb2Hash('test'), 'number');
    assert.strictEqual(typeof djb2Hash('another string'), 'number');
});

test('djb2Hash with different seeds produces different results', () => {
    assert.notStrictEqual(djb2Hash('hello', 5381), djb2Hash('hello', 1234));
    assert.notStrictEqual(djb2Hash('world', 100), djb2Hash('world', 200));
});

test('djb2Hash returns unsigned 32-bit integer', () => {
    const hash = djb2Hash('test string');
    assert.ok(hash >= 0, 'Hash should be non-negative');
    assert.ok(hash <= 0xFFFFFFFF, 'Hash should fit in 32 bits');
});

test('djb2Hash handles empty string', () => {
    assert.strictEqual(typeof djb2Hash(''), 'number');
    assert.strictEqual(djb2Hash(''), 5381); // Default seed unchanged
});

test('djb2Hash handles unicode characters', () => {
    const hash1 = djb2Hash('日本語');
    const hash2 = djb2Hash('中文');
    assert.strictEqual(typeof hash1, 'number');
    assert.strictEqual(typeof hash2, 'number');
    assert.notStrictEqual(hash1, hash2);
});

// deriveShellUUID tests
test('deriveShellUUID returns UUID v4 format', () => {
    const uuid = deriveShellUUID('550e8400-e29b-41d4-a716-446655440000');
    // UUID v4 pattern: xxxxxxxx-xxxx-4xxx-[89ab]xxx-xxxxxxxxxxxx
    const uuidv4Regex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    assert.match(uuid, uuidv4Regex, `UUID ${uuid} should match v4 format`);
});

test('deriveShellUUID is deterministic', () => {
    const parentUUID = 'abc123-def456-ghi789';
    assert.strictEqual(deriveShellUUID(parentUUID), deriveShellUUID(parentUUID));
});

test('deriveShellUUID produces different UUIDs for different parents', () => {
    const uuid1 = deriveShellUUID('parent-1');
    const uuid2 = deriveShellUUID('parent-2');
    const uuid3 = deriveShellUUID('parent-3');
    assert.notStrictEqual(uuid1, uuid2);
    assert.notStrictEqual(uuid2, uuid3);
    assert.notStrictEqual(uuid1, uuid3);
});

test('deriveShellUUID returns 36 character string', () => {
    const uuid = deriveShellUUID('test-parent-uuid');
    assert.strictEqual(uuid.length, 36);
});

test('deriveShellUUID has correct hyphen positions', () => {
    const uuid = deriveShellUUID('test-uuid');
    assert.strictEqual(uuid[8], '-');
    assert.strictEqual(uuid[13], '-');
    assert.strictEqual(uuid[18], '-');
    assert.strictEqual(uuid[23], '-');
});

test('deriveShellUUID version nibble is always 4', () => {
    // Check multiple inputs to ensure version nibble is always 4
    const uuids = [
        deriveShellUUID('test1'),
        deriveShellUUID('test2'),
        deriveShellUUID('another-parent'),
        deriveShellUUID('550e8400-e29b-41d4-a716-446655440000')
    ];
    for (const uuid of uuids) {
        assert.strictEqual(uuid[14], '4', `UUID ${uuid} should have version 4`);
    }
});

test('deriveShellUUID variant bits are correct (8, 9, a, or b)', () => {
    // The variant nibble (position 19) should be 8, 9, a, or b
    const uuids = [
        deriveShellUUID('test1'),
        deriveShellUUID('test2'),
        deriveShellUUID('parent-xyz'),
        deriveShellUUID('any-string-here')
    ];
    for (const uuid of uuids) {
        assert.ok(
            ['8', '9', 'a', 'b'].includes(uuid[19]),
            `UUID ${uuid} variant nibble should be 8, 9, a, or b, got ${uuid[19]}`
        );
    }
});

test('deriveShellUUID handles special characters in parent UUID', () => {
    const uuid = deriveShellUUID('test-with-special-chars!@#$%');
    const uuidv4Regex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    assert.match(uuid, uuidv4Regex);
});

test('deriveShellUUID handles empty parent UUID', () => {
    const uuid = deriveShellUUID('');
    const uuidv4Regex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    assert.match(uuid, uuidv4Regex);
});
