/**
 * Unit tests for chunk-assembler.js
 * Run with: node --test chunk-assembler.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    createAssembler,
    addChunk,
    isComplete,
    getReceivedCount,
    assemble,
    reset,
    getProgress
} from './chunk-assembler.js';

// createAssembler tests
test('createAssembler returns empty chunks array', () => {
    const state = createAssembler();
    assert.deepStrictEqual(state.chunks, []);
});

test('createAssembler returns expectedCount 0', () => {
    const state = createAssembler();
    assert.strictEqual(state.expectedCount, 0);
});

// addChunk tests
test('addChunk initializes chunks array on first chunk', () => {
    const initial = createAssembler();
    const payload = new Uint8Array([1, 2, 3]);
    const state = addChunk(initial, 0, 3, payload);
    assert.strictEqual(state.chunks.length, 3);
    assert.strictEqual(state.expectedCount, 3);
});

test('addChunk does not mutate original state', () => {
    const initial = createAssembler();
    const payload = new Uint8Array([1, 2, 3]);
    addChunk(initial, 0, 3, payload);
    assert.deepStrictEqual(initial.chunks, []);
});

test('addChunk stores payload at correct index', () => {
    const initial = createAssembler();
    const payload = new Uint8Array([1, 2, 3]);
    const state = addChunk(initial, 1, 3, payload);
    assert.strictEqual(state.chunks[1], payload);
    assert.strictEqual(state.chunks[0], undefined);
    assert.strictEqual(state.chunks[2], undefined);
});

test('addChunk handles out-of-order chunks', () => {
    const initial = createAssembler();
    const chunk0 = new Uint8Array([1, 2]);
    const chunk2 = new Uint8Array([5, 6]);
    const chunk1 = new Uint8Array([3, 4]);

    let state = addChunk(initial, 2, 3, chunk2);
    state = addChunk(state, 0, 3, chunk0);
    state = addChunk(state, 1, 3, chunk1);

    assert.strictEqual(state.chunks[0], chunk0);
    assert.strictEqual(state.chunks[1], chunk1);
    assert.strictEqual(state.chunks[2], chunk2);
});

test('addChunk resets on different total', () => {
    const initial = createAssembler();
    const chunk1 = new Uint8Array([1]);
    const chunk2 = new Uint8Array([2]);

    let state = addChunk(initial, 0, 3, chunk1);
    assert.strictEqual(state.expectedCount, 3);

    // New sequence with different total
    state = addChunk(state, 0, 5, chunk2);
    assert.strictEqual(state.expectedCount, 5);
    assert.strictEqual(state.chunks.length, 5);
});

// isComplete tests
test('isComplete returns false for empty assembler', () => {
    const state = createAssembler();
    assert.strictEqual(isComplete(state), false);
});

test('isComplete returns false when chunks missing', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    let state = addChunk(initial, 0, 3, chunk);
    state = addChunk(state, 2, 3, chunk);
    // Missing chunk 1
    assert.strictEqual(isComplete(state), false);
});

test('isComplete returns true when all chunks received', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    let state = addChunk(initial, 0, 3, chunk);
    state = addChunk(state, 1, 3, chunk);
    state = addChunk(state, 2, 3, chunk);
    assert.strictEqual(isComplete(state), true);
});

test('isComplete returns true for single chunk', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    const state = addChunk(initial, 0, 1, chunk);
    assert.strictEqual(isComplete(state), true);
});

// getReceivedCount tests
test('getReceivedCount returns 0 for empty assembler', () => {
    const state = createAssembler();
    assert.strictEqual(getReceivedCount(state), 0);
});

test('getReceivedCount counts received chunks', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1]);
    let state = addChunk(initial, 0, 5, chunk);
    assert.strictEqual(getReceivedCount(state), 1);

    state = addChunk(state, 2, 5, chunk);
    assert.strictEqual(getReceivedCount(state), 2);

    state = addChunk(state, 4, 5, chunk);
    assert.strictEqual(getReceivedCount(state), 3);
});

// assemble tests
test('assemble combines chunks in order', () => {
    const initial = createAssembler();
    const chunk0 = new Uint8Array([1, 2, 3]);
    const chunk1 = new Uint8Array([4, 5, 6]);
    const chunk2 = new Uint8Array([7, 8, 9]);

    let state = addChunk(initial, 0, 3, chunk0);
    state = addChunk(state, 1, 3, chunk1);
    state = addChunk(state, 2, 3, chunk2);

    const result = assemble(state);
    assert.deepStrictEqual(Array.from(result), [1, 2, 3, 4, 5, 6, 7, 8, 9]);
});

test('assemble handles chunks added out of order', () => {
    const initial = createAssembler();
    const chunk0 = new Uint8Array([1, 2]);
    const chunk1 = new Uint8Array([3, 4]);
    const chunk2 = new Uint8Array([5, 6]);

    let state = addChunk(initial, 2, 3, chunk2);
    state = addChunk(state, 0, 3, chunk0);
    state = addChunk(state, 1, 3, chunk1);

    const result = assemble(state);
    assert.deepStrictEqual(Array.from(result), [1, 2, 3, 4, 5, 6]);
});

test('assemble returns empty for empty assembler', () => {
    const state = createAssembler();
    const result = assemble(state);
    assert.strictEqual(result.length, 0);
});

test('assemble handles single chunk', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3, 4, 5]);
    const state = addChunk(initial, 0, 1, chunk);
    const result = assemble(state);
    assert.deepStrictEqual(Array.from(result), [1, 2, 3, 4, 5]);
});

test('assemble handles different chunk sizes', () => {
    const initial = createAssembler();
    const chunk0 = new Uint8Array([1]);
    const chunk1 = new Uint8Array([2, 3, 4]);
    const chunk2 = new Uint8Array([5, 6]);

    let state = addChunk(initial, 0, 3, chunk0);
    state = addChunk(state, 1, 3, chunk1);
    state = addChunk(state, 2, 3, chunk2);

    const result = assemble(state);
    assert.deepStrictEqual(Array.from(result), [1, 2, 3, 4, 5, 6]);
});

// reset tests
test('reset clears chunks array', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    let state = addChunk(initial, 0, 3, chunk);
    state = reset(state);
    assert.deepStrictEqual(state.chunks, []);
});

test('reset clears expectedCount', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    let state = addChunk(initial, 0, 3, chunk);
    state = reset(state);
    assert.strictEqual(state.expectedCount, 0);
});

test('reset does not mutate original state', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1, 2, 3]);
    const state1 = addChunk(initial, 0, 3, chunk);
    reset(state1);
    assert.strictEqual(state1.expectedCount, 3);
});

// getProgress tests
test('getProgress returns zeros for empty assembler', () => {
    const state = createAssembler();
    const progress = getProgress(state);
    assert.strictEqual(progress.received, 0);
    assert.strictEqual(progress.total, 0);
});

test('getProgress returns correct values', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1]);
    let state = addChunk(initial, 0, 5, chunk);
    state = addChunk(state, 2, 5, chunk);

    const progress = getProgress(state);
    assert.strictEqual(progress.received, 2);
    assert.strictEqual(progress.total, 5);
});

test('getProgress shows complete when all received', () => {
    const initial = createAssembler();
    const chunk = new Uint8Array([1]);
    let state = addChunk(initial, 0, 2, chunk);
    state = addChunk(state, 1, 2, chunk);

    const progress = getProgress(state);
    assert.strictEqual(progress.received, 2);
    assert.strictEqual(progress.total, 2);
});

// Integration test - complete workflow
test('integration: chunk assembly workflow', () => {
    // Create assembler
    let state = createAssembler();
    assert.strictEqual(isComplete(state), false);

    // Receive chunks out of order
    const chunk0 = new Uint8Array([1, 2, 3]);
    const chunk2 = new Uint8Array([7, 8, 9]);
    const chunk1 = new Uint8Array([4, 5, 6]);

    state = addChunk(state, 0, 3, chunk0);
    assert.strictEqual(getReceivedCount(state), 1);
    assert.strictEqual(isComplete(state), false);

    state = addChunk(state, 2, 3, chunk2);
    assert.strictEqual(getReceivedCount(state), 2);
    assert.strictEqual(isComplete(state), false);

    // Check progress
    let progress = getProgress(state);
    assert.strictEqual(progress.received, 2);
    assert.strictEqual(progress.total, 3);

    state = addChunk(state, 1, 3, chunk1);
    assert.strictEqual(getReceivedCount(state), 3);
    assert.strictEqual(isComplete(state), true);

    // Assemble
    const assembled = assemble(state);
    assert.deepStrictEqual(Array.from(assembled), [1, 2, 3, 4, 5, 6, 7, 8, 9]);

    // Reset for next sequence
    state = reset(state);
    assert.strictEqual(isComplete(state), false);
    assert.strictEqual(getReceivedCount(state), 0);
});
