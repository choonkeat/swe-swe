/**
 * Unit tests for upload-queue.js
 * Run with: node --test upload-queue.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    createQueue,
    enqueue,
    dequeue,
    peek,
    isEmpty,
    getQueueCount,
    getQueueInfo,
    startUploading,
    stopUploading,
    clearQueue
} from './upload-queue.js';

// Mock files
const file1 = { name: 'test1.txt', size: 100 };
const file2 = { name: 'test2.txt', size: 200 };
const file3 = { name: 'test3.txt', size: 300 };

// createQueue tests
test('createQueue returns empty files array', () => {
    const state = createQueue();
    assert.deepStrictEqual(state.files, []);
});

test('createQueue returns isUploading false', () => {
    const state = createQueue();
    assert.strictEqual(state.isUploading, false);
});

test('createQueue returns uploadStartTime null', () => {
    const state = createQueue();
    assert.strictEqual(state.uploadStartTime, null);
});

// isEmpty tests
test('isEmpty returns true for empty queue', () => {
    const state = createQueue();
    assert.strictEqual(isEmpty(state), true);
});

test('isEmpty returns false for non-empty queue', () => {
    const state = enqueue(createQueue(), file1);
    assert.strictEqual(isEmpty(state), false);
});

// enqueue tests
test('enqueue adds file to queue', () => {
    const state = enqueue(createQueue(), file1);
    assert.strictEqual(state.files.length, 1);
    assert.strictEqual(state.files[0], file1);
});

test('enqueue does not mutate original state', () => {
    const initial = createQueue();
    enqueue(initial, file1);
    assert.strictEqual(initial.files.length, 0);
});

test('enqueue preserves existing files', () => {
    const state1 = enqueue(createQueue(), file1);
    const state2 = enqueue(state1, file2);
    assert.strictEqual(state2.files.length, 2);
    assert.strictEqual(state2.files[0], file1);
    assert.strictEqual(state2.files[1], file2);
});

test('enqueue preserves isUploading', () => {
    const state1 = startUploading(createQueue(), 12345);
    const state2 = enqueue(state1, file1);
    assert.strictEqual(state2.isUploading, true);
});

// dequeue tests
test('dequeue removes first file', () => {
    const state1 = enqueue(enqueue(createQueue(), file1), file2);
    const state2 = dequeue(state1);
    assert.strictEqual(state2.files.length, 1);
    assert.strictEqual(state2.files[0], file2);
});

test('dequeue does not mutate original state', () => {
    const state1 = enqueue(enqueue(createQueue(), file1), file2);
    dequeue(state1);
    assert.strictEqual(state1.files.length, 2);
});

test('dequeue on empty queue returns empty', () => {
    const state = dequeue(createQueue());
    assert.strictEqual(state.files.length, 0);
});

test('dequeue preserves isUploading', () => {
    const state1 = startUploading(enqueue(createQueue(), file1), 12345);
    const state2 = dequeue(state1);
    assert.strictEqual(state2.isUploading, true);
});

// peek tests
test('peek returns first file', () => {
    const state = enqueue(enqueue(createQueue(), file1), file2);
    assert.strictEqual(peek(state), file1);
});

test('peek returns null for empty queue', () => {
    const state = createQueue();
    assert.strictEqual(peek(state), null);
});

test('peek does not modify queue', () => {
    const state = enqueue(createQueue(), file1);
    peek(state);
    assert.strictEqual(state.files.length, 1);
});

// getQueueCount tests
test('getQueueCount returns 0 for empty queue', () => {
    const state = createQueue();
    assert.strictEqual(getQueueCount(state), 0);
});

test('getQueueCount returns correct count', () => {
    const state = enqueue(enqueue(enqueue(createQueue(), file1), file2), file3);
    assert.strictEqual(getQueueCount(state), 3);
});

// getQueueInfo tests
test('getQueueInfo returns null current for empty queue', () => {
    const state = createQueue();
    const info = getQueueInfo(state);
    assert.strictEqual(info.current, null);
    assert.strictEqual(info.remaining, 0);
});

test('getQueueInfo returns current file and remaining count', () => {
    const state = enqueue(enqueue(createQueue(), file1), file2);
    const info = getQueueInfo(state);
    assert.strictEqual(info.current, file1);
    assert.strictEqual(info.remaining, 1);
});

test('getQueueInfo returns 0 remaining for single file', () => {
    const state = enqueue(createQueue(), file1);
    const info = getQueueInfo(state);
    assert.strictEqual(info.current, file1);
    assert.strictEqual(info.remaining, 0);
});

// startUploading tests
test('startUploading sets isUploading true', () => {
    const state = startUploading(createQueue(), 12345);
    assert.strictEqual(state.isUploading, true);
});

test('startUploading sets uploadStartTime', () => {
    const state = startUploading(createQueue(), 12345);
    assert.strictEqual(state.uploadStartTime, 12345);
});

test('startUploading does not mutate original state', () => {
    const initial = createQueue();
    startUploading(initial, 12345);
    assert.strictEqual(initial.isUploading, false);
});

test('startUploading preserves files', () => {
    const state1 = enqueue(createQueue(), file1);
    const state2 = startUploading(state1, 12345);
    assert.strictEqual(state2.files.length, 1);
    assert.strictEqual(state2.files[0], file1);
});

test('startUploading uses Date.now by default', () => {
    const before = Date.now();
    const state = startUploading(createQueue());
    const after = Date.now();
    assert.ok(state.uploadStartTime >= before && state.uploadStartTime <= after);
});

// stopUploading tests
test('stopUploading sets isUploading false', () => {
    const state1 = startUploading(createQueue(), 12345);
    const state2 = stopUploading(state1);
    assert.strictEqual(state2.isUploading, false);
});

test('stopUploading clears uploadStartTime', () => {
    const state1 = startUploading(createQueue(), 12345);
    const state2 = stopUploading(state1);
    assert.strictEqual(state2.uploadStartTime, null);
});

test('stopUploading does not mutate original state', () => {
    const state1 = startUploading(createQueue(), 12345);
    stopUploading(state1);
    assert.strictEqual(state1.isUploading, true);
});

test('stopUploading preserves files', () => {
    const state1 = startUploading(enqueue(createQueue(), file1), 12345);
    const state2 = stopUploading(state1);
    assert.strictEqual(state2.files.length, 1);
});

// clearQueue tests
test('clearQueue empties files array', () => {
    const state1 = enqueue(enqueue(createQueue(), file1), file2);
    const state2 = clearQueue(state1);
    assert.strictEqual(state2.files.length, 0);
});

test('clearQueue does not mutate original state', () => {
    const state1 = enqueue(createQueue(), file1);
    clearQueue(state1);
    assert.strictEqual(state1.files.length, 1);
});

test('clearQueue preserves isUploading', () => {
    const state1 = startUploading(enqueue(createQueue(), file1), 12345);
    const state2 = clearQueue(state1);
    assert.strictEqual(state2.isUploading, true);
});

test('clearQueue preserves uploadStartTime', () => {
    const state1 = startUploading(enqueue(createQueue(), file1), 12345);
    const state2 = clearQueue(state1);
    assert.strictEqual(state2.uploadStartTime, 12345);
});

// Integration test - complete workflow
test('integration: upload queue workflow', () => {
    // Initial state
    let state = createQueue();
    assert.strictEqual(isEmpty(state), true);

    // Add files
    state = enqueue(state, file1);
    state = enqueue(state, file2);
    assert.strictEqual(getQueueCount(state), 2);

    // Start upload
    state = startUploading(state, 100000);
    assert.strictEqual(state.isUploading, true);

    // Process first file
    const current = peek(state);
    assert.strictEqual(current, file1);
    state = dequeue(state);
    assert.strictEqual(getQueueCount(state), 1);

    // Process second file
    state = dequeue(state);
    assert.strictEqual(isEmpty(state), true);

    // End upload
    state = stopUploading(state);
    assert.strictEqual(state.isUploading, false);
});
