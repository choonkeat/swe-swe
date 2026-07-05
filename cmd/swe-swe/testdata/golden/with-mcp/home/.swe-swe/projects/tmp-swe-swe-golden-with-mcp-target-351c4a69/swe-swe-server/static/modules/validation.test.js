/**
 * Unit tests for validation.js
 * Run with: node --test validation.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { validateUsername, validateSessionName } from './validation.js';

// validateUsername - valid cases
test('validateUsername accepts valid simple name', () => {
    assert.deepStrictEqual(validateUsername('Alice'), { valid: true, name: 'Alice' });
});

test('validateUsername trims whitespace', () => {
    assert.deepStrictEqual(validateUsername('  Bob  '), { valid: true, name: 'Bob' });
});

test('validateUsername accepts name with spaces', () => {
    assert.deepStrictEqual(validateUsername('User 123'), { valid: true, name: 'User 123' });
});

test('validateUsername accepts 16 character name', () => {
    const name = 'a'.repeat(16);
    assert.deepStrictEqual(validateUsername(name), { valid: true, name: name });
});

test('validateUsername accepts alphanumeric name', () => {
    assert.deepStrictEqual(validateUsername('User123'), { valid: true, name: 'User123' });
});

// validateUsername - invalid cases
test('validateUsername rejects empty string', () => {
    assert.deepStrictEqual(validateUsername(''), { valid: false, error: 'Name cannot be empty' });
});

test('validateUsername rejects whitespace only', () => {
    assert.deepStrictEqual(validateUsername('   '), { valid: false, error: 'Name cannot be empty' });
});

test('validateUsername rejects name over 16 chars', () => {
    const name = 'a'.repeat(17);
    assert.deepStrictEqual(validateUsername(name), { valid: false, error: 'Name must be 16 characters or less' });
});

test('validateUsername rejects name with @ symbol', () => {
    assert.deepStrictEqual(validateUsername('user@domain'), { valid: false, error: 'Name can only contain letters, numbers, and spaces' });
});

test('validateUsername rejects name with hyphen', () => {
    assert.deepStrictEqual(validateUsername('user-name'), { valid: false, error: 'Name can only contain letters, numbers, and spaces' });
});

test('validateUsername rejects name with underscore', () => {
    assert.deepStrictEqual(validateUsername('user_name'), { valid: false, error: 'Name can only contain letters, numbers, and spaces' });
});

test('validateUsername rejects name with special characters', () => {
    assert.deepStrictEqual(validateUsername('user!#$%'), { valid: false, error: 'Name can only contain letters, numbers, and spaces' });
});

// validateSessionName - valid cases
test('validateSessionName accepts empty string', () => {
    assert.deepStrictEqual(validateSessionName(''), { valid: true, name: '' });
});

test('validateSessionName accepts whitespace only as empty', () => {
    assert.deepStrictEqual(validateSessionName('   '), { valid: true, name: '' });
});

test('validateSessionName accepts simple name', () => {
    assert.deepStrictEqual(validateSessionName('my-session_01'), { valid: true, name: 'my-session_01' });
});

test('validateSessionName accepts name with spaces', () => {
    assert.deepStrictEqual(validateSessionName('My Session'), { valid: true, name: 'My Session' });
});

test('validateSessionName accepts 256 character name', () => {
    const name = 'a'.repeat(256);
    assert.deepStrictEqual(validateSessionName(name), { valid: true, name: name });
});

test('validateSessionName trims whitespace', () => {
    assert.deepStrictEqual(validateSessionName('  session  '), { valid: true, name: 'session' });
});

test('validateSessionName accepts hyphen and underscore', () => {
    assert.deepStrictEqual(validateSessionName('my-session_name'), { valid: true, name: 'my-session_name' });
});

// validateSessionName - now valid cases (@ . / are allowed)
test('validateSessionName accepts name with @ symbol', () => {
    assert.deepStrictEqual(validateSessionName('owner/repo@branch'), { valid: true, name: 'owner/repo@branch' });
});

test('validateSessionName accepts name with period', () => {
    assert.deepStrictEqual(validateSessionName('my.org/repo@main'), { valid: true, name: 'my.org/repo@main' });
});

test('validateSessionName accepts name with slash', () => {
    assert.deepStrictEqual(validateSessionName('owner/repo@feature/login'), { valid: true, name: 'owner/repo@feature/login' });
});

// validateSessionName - invalid cases
test('validateSessionName rejects name over 256 chars', () => {
    const name = 'a'.repeat(257);
    assert.deepStrictEqual(validateSessionName(name), { valid: false, error: 'Name must be 256 characters or less' });
});

test('validateSessionName rejects name with special chars', () => {
    assert.deepStrictEqual(validateSessionName('session!#$%'), { valid: false, error: 'Name can only contain letters, numbers, spaces, hyphens, underscores, slashes, dots, and @' });
});
