/**
 * Unit tests for messages.js
 * Run with: node --test messages.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import {
    OPCODE_RESIZE,
    OPCODE_FILE_UPLOAD,
    OPCODE_CHUNK,
    encodeResize,
    encodeFileUpload,
    isChunkMessage,
    decodeChunkHeader,
    parseServerMessage
} from './messages.js';

// Constants tests
test('OPCODE_RESIZE is 0x00', () => {
    assert.strictEqual(OPCODE_RESIZE, 0x00);
});

test('OPCODE_FILE_UPLOAD is 0x01', () => {
    assert.strictEqual(OPCODE_FILE_UPLOAD, 0x01);
});

test('OPCODE_CHUNK is 0x02', () => {
    assert.strictEqual(OPCODE_CHUNK, 0x02);
});

// encodeResize tests
test('encodeResize returns 5-byte message', () => {
    const msg = encodeResize(24, 80);
    assert.strictEqual(msg.length, 5);
});

test('encodeResize has OPCODE_RESIZE as first byte', () => {
    const msg = encodeResize(24, 80);
    assert.strictEqual(msg[0], OPCODE_RESIZE);
});

test('encodeResize encodes rows in big-endian', () => {
    const msg = encodeResize(24, 80);
    const rows = (msg[1] << 8) | msg[2];
    assert.strictEqual(rows, 24);
});

test('encodeResize encodes cols in big-endian', () => {
    const msg = encodeResize(24, 80);
    const cols = (msg[3] << 8) | msg[4];
    assert.strictEqual(cols, 80);
});

test('encodeResize handles large values (> 255)', () => {
    const msg = encodeResize(300, 400);
    const rows = (msg[1] << 8) | msg[2];
    const cols = (msg[3] << 8) | msg[4];
    assert.strictEqual(rows, 300);
    assert.strictEqual(cols, 400);
});

test('encodeResize handles max 16-bit values', () => {
    const msg = encodeResize(65535, 65535);
    const rows = (msg[1] << 8) | msg[2];
    const cols = (msg[3] << 8) | msg[4];
    assert.strictEqual(rows, 65535);
    assert.strictEqual(cols, 65535);
});

test('encodeResize handles zero values', () => {
    const msg = encodeResize(0, 0);
    const rows = (msg[1] << 8) | msg[2];
    const cols = (msg[3] << 8) | msg[4];
    assert.strictEqual(rows, 0);
    assert.strictEqual(cols, 0);
});

// encodeFileUpload tests
test('encodeFileUpload has OPCODE_FILE_UPLOAD as first byte', () => {
    const msg = encodeFileUpload('test.txt', new Uint8Array([1, 2, 3]));
    assert.strictEqual(msg[0], OPCODE_FILE_UPLOAD);
});

test('encodeFileUpload encodes filename length in big-endian', () => {
    const msg = encodeFileUpload('test.txt', new Uint8Array([1, 2, 3]));
    const nameLen = (msg[1] << 8) | msg[2];
    assert.strictEqual(nameLen, 8); // "test.txt" is 8 bytes
});

test('encodeFileUpload includes filename bytes', () => {
    const msg = encodeFileUpload('abc', new Uint8Array([99]));
    const nameBytes = msg.slice(3, 6);
    assert.strictEqual(String.fromCharCode(...nameBytes), 'abc');
});

test('encodeFileUpload includes file data after filename', () => {
    const data = new Uint8Array([10, 20, 30]);
    const msg = encodeFileUpload('ab', data);
    // Message: [0x01, 0, 2, 'a', 'b', 10, 20, 30]
    assert.deepStrictEqual(Array.from(msg.slice(5)), [10, 20, 30]);
});

test('encodeFileUpload calculates correct total length', () => {
    const data = new Uint8Array([1, 2, 3, 4, 5]);
    const msg = encodeFileUpload('file.bin', data);
    // 1 (opcode) + 2 (name length) + 8 (filename) + 5 (data)
    assert.strictEqual(msg.length, 16);
});

test('encodeFileUpload handles empty filename', () => {
    const msg = encodeFileUpload('', new Uint8Array([1]));
    const nameLen = (msg[1] << 8) | msg[2];
    assert.strictEqual(nameLen, 0);
    assert.strictEqual(msg[3], 1); // data starts immediately
});

test('encodeFileUpload handles empty data', () => {
    const msg = encodeFileUpload('x', new Uint8Array([]));
    // 1 (opcode) + 2 (name length) + 1 (filename) + 0 (data)
    assert.strictEqual(msg.length, 4);
});

test('encodeFileUpload handles unicode filenames', () => {
    const msg = encodeFileUpload('日本語.txt', new Uint8Array([1]));
    // UTF-8: 日本語 is 9 bytes (3 bytes each) + .txt is 4 bytes = 13 bytes
    const nameLen = (msg[1] << 8) | msg[2];
    assert.strictEqual(nameLen, 13);
});

// isChunkMessage tests
test('isChunkMessage returns true for valid chunk', () => {
    const chunk = new Uint8Array([0x02, 0, 3, 1, 2, 3]);
    assert.strictEqual(isChunkMessage(chunk), true);
});

test('isChunkMessage returns false for resize message', () => {
    const resize = new Uint8Array([0x00, 0, 24, 0, 80]);
    assert.strictEqual(isChunkMessage(resize), false);
});

test('isChunkMessage returns false for file upload message', () => {
    const upload = new Uint8Array([0x01, 0, 1, 97, 1]);
    assert.strictEqual(isChunkMessage(upload), false);
});

test('isChunkMessage returns false for too-short messages', () => {
    assert.strictEqual(isChunkMessage(new Uint8Array([0x02])), false);
    assert.strictEqual(isChunkMessage(new Uint8Array([0x02, 0])), false);
});

test('isChunkMessage returns true for minimal chunk (3 bytes)', () => {
    const minChunk = new Uint8Array([0x02, 0, 1]);
    assert.strictEqual(isChunkMessage(minChunk), true);
});

test('isChunkMessage returns false for empty message', () => {
    assert.strictEqual(isChunkMessage(new Uint8Array([])), false);
});

// decodeChunkHeader tests
test('decodeChunkHeader extracts chunkIndex', () => {
    const chunk = new Uint8Array([0x02, 2, 5, 10, 20, 30]);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.chunkIndex, 2);
});

test('decodeChunkHeader extracts totalChunks', () => {
    const chunk = new Uint8Array([0x02, 2, 5, 10, 20, 30]);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.totalChunks, 5);
});

test('decodeChunkHeader extracts payload', () => {
    const chunk = new Uint8Array([0x02, 2, 5, 10, 20, 30]);
    const header = decodeChunkHeader(chunk);
    assert.deepStrictEqual(Array.from(header.payload), [10, 20, 30]);
});

test('decodeChunkHeader handles first chunk (index 0)', () => {
    const chunk = new Uint8Array([0x02, 0, 10, 1, 2]);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.chunkIndex, 0);
});

test('decodeChunkHeader handles last chunk (index == total - 1)', () => {
    const chunk = new Uint8Array([0x02, 9, 10, 1, 2]);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.chunkIndex, 9);
    assert.strictEqual(header.totalChunks, 10);
});

test('decodeChunkHeader handles empty payload', () => {
    const chunk = new Uint8Array([0x02, 0, 1]);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.payload.length, 0);
});

test('decodeChunkHeader handles large payload', () => {
    const payload = new Uint8Array(1000).fill(42);
    const chunk = new Uint8Array(3 + payload.length);
    chunk[0] = 0x02;
    chunk[1] = 0;
    chunk[2] = 1;
    chunk.set(payload, 3);
    const header = decodeChunkHeader(chunk);
    assert.strictEqual(header.payload.length, 1000);
});

// parseServerMessage tests
test('parseServerMessage parses valid JSON', () => {
    const result = parseServerMessage('{"type":"pong","data":{"ts":123}}');
    assert.deepStrictEqual(result, { type: 'pong', data: { ts: 123 } });
});

test('parseServerMessage returns null for invalid JSON', () => {
    assert.strictEqual(parseServerMessage('not json'), null);
});

test('parseServerMessage returns null for empty string', () => {
    assert.strictEqual(parseServerMessage(''), null);
});

test('parseServerMessage handles arrays', () => {
    const result = parseServerMessage('[1, 2, 3]');
    assert.deepStrictEqual(result, [1, 2, 3]);
});

test('parseServerMessage handles primitives', () => {
    assert.strictEqual(parseServerMessage('42'), 42);
    assert.strictEqual(parseServerMessage('"hello"'), 'hello');
    assert.strictEqual(parseServerMessage('true'), true);
    assert.strictEqual(parseServerMessage('null'), null);
});

test('parseServerMessage handles nested objects', () => {
    const result = parseServerMessage('{"a":{"b":{"c":1}}}');
    assert.deepStrictEqual(result, { a: { b: { c: 1 } } });
});

test('parseServerMessage returns null for truncated JSON', () => {
    assert.strictEqual(parseServerMessage('{"type":'), null);
});

test('parseServerMessage handles unicode', () => {
    const result = parseServerMessage('{"name":"日本語"}');
    assert.deepStrictEqual(result, { name: '日本語' });
});
