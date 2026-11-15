/**
 * Binary message encoding/decoding for WebSocket protocol.
 * All functions are pure and side-effect free.
 * @module messages
 */

// Binary protocol opcodes
export const OPCODE_RESIZE = 0x00;
export const OPCODE_FILE_UPLOAD = 0x01;
export const OPCODE_CHUNK = 0x02;
export const OPCODE_UPLOAD_CHUNK = 0x03;

// Bytes per streamed upload chunk; the server writes each one straight to disk.
export const UPLOAD_CHUNK_SIZE = 1024 * 1024;

/**
 * Encode a terminal resize message.
 * Format: [0x00, rows_hi, rows_lo, cols_hi, cols_lo]
 * @param {number} rows - Terminal rows
 * @param {number} cols - Terminal columns
 * @returns {Uint8Array} Binary message (5 bytes)
 */
export function encodeResize(rows, cols) {
    return new Uint8Array([
        OPCODE_RESIZE,
        (rows >> 8) & 0xFF, rows & 0xFF,
        (cols >> 8) & 0xFF, cols & 0xFF
    ]);
}

/**
 * Encode a file upload message.
 * Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
 * @param {string} filename - The filename
 * @param {Uint8Array} data - The file data
 * @returns {Uint8Array} Binary message
 */
export function encodeFileUpload(filename, data) {
    const encoder = new TextEncoder();
    const nameBytes = encoder.encode(filename);
    const nameLen = nameBytes.length;

    const message = new Uint8Array(1 + 2 + nameLen + data.length);
    message[0] = OPCODE_FILE_UPLOAD;
    message[1] = (nameLen >> 8) & 0xFF;  // name length high byte
    message[2] = nameLen & 0xFF;          // name length low byte
    message.set(nameBytes, 3);
    message.set(data, 3 + nameLen);

    return message;
}

/**
 * Encode one piece of a streamed upload (announced by an upload_start JSON
 * message, finished by upload_end).
 * Format: [0x03, ...piece_bytes]
 * @param {Uint8Array} data - The piece
 * @returns {Uint8Array} Binary message
 */
export function encodeUploadChunk(data) {
    const message = new Uint8Array(1 + data.length);
    message[0] = OPCODE_UPLOAD_CHUNK;
    message.set(data, 1);
    return message;
}

/**
 * Text shown when a file is over the upload limit, or null when it fits.
 * @param {string} name - File name
 * @param {number} size - File size in bytes
 * @param {number} maxBytes - Limit in bytes (0 or missing = no limit)
 * @param {function(number): string} formatSize - Byte formatter
 * @returns {string|null}
 */
export function uploadTooLargeMessage(name, size, maxBytes, formatSize) {
    if (!maxBytes || maxBytes <= 0 || size <= maxBytes) return null;
    return `Too large: ${name} is ${formatSize(size)}; the upload limit is ${formatSize(maxBytes)}`;
}

/**
 * Check if a binary message is a chunk message.
 * @param {Uint8Array} data - Binary data
 * @returns {boolean} True if this is a chunk message (starts with 0x02 and has header)
 */
export function isChunkMessage(data) {
    return data.length >= 3 && data[0] === OPCODE_CHUNK;
}

/**
 * Decode a chunk message header.
 * Format: [0x02, chunkIndex, totalChunks, ...payload]
 * @param {Uint8Array} data - Binary chunk data
 * @returns {{chunkIndex: number, totalChunks: number, payload: Uint8Array}} Decoded header
 */
export function decodeChunkHeader(data) {
    return {
        chunkIndex: data[1],
        totalChunks: data[2],
        payload: data.slice(3)
    };
}

/**
 * Parse a JSON server message.
 * @param {string} jsonStr - JSON string from server
 * @returns {object|null} Parsed message or null if invalid JSON
 */
export function parseServerMessage(jsonStr) {
    try {
        return JSON.parse(jsonStr);
    } catch (e) {
        return null;
    }
}
