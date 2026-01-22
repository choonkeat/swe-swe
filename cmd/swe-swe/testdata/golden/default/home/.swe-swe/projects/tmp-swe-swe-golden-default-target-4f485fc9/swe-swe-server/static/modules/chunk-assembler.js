/**
 * Pure chunk assembly state reducer functions.
 * Used for reassembling large terminal snapshots sent in chunks.
 * @module chunk-assembler
 */

/**
 * Create initial assembler state.
 * @returns {{chunks: Array, expectedCount: number}} Initial state
 */
export function createAssembler() {
    return {
        chunks: [],
        expectedCount: 0
    };
}

/**
 * Add a chunk to the assembler.
 * Handles out-of-order arrival by using sparse array.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @param {number} index - Chunk index (0-based)
 * @param {number} total - Total expected chunks
 * @param {Uint8Array} payload - Chunk data
 * @returns {{chunks: Array, expectedCount: number}} New state
 */
export function addChunk(state, index, total, payload) {
    // Start new sequence if total changed
    if (state.expectedCount !== total) {
        const chunks = new Array(total);
        chunks[index] = payload;
        return {
            chunks,
            expectedCount: total
        };
    }

    // Add to existing sequence
    const chunks = [...state.chunks];
    chunks[index] = payload;
    return {
        ...state,
        chunks
    };
}

/**
 * Check if all chunks have been received.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @returns {boolean} True if all chunks received
 */
export function isComplete(state) {
    if (state.expectedCount === 0) return false;
    return getReceivedCount(state) === state.expectedCount;
}

/**
 * Get the count of received chunks.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @returns {number} Number of chunks received
 */
export function getReceivedCount(state) {
    return state.chunks.filter(c => c !== undefined).length;
}

/**
 * Assemble all chunks into a single Uint8Array.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @returns {Uint8Array} Combined data from all chunks
 */
export function assemble(state) {
    const totalSize = state.chunks.reduce((sum, c) => sum + (c ? c.length : 0), 0);
    const result = new Uint8Array(totalSize);
    let offset = 0;
    for (const chunk of state.chunks) {
        if (chunk) {
            result.set(chunk, offset);
            offset += chunk.length;
        }
    }
    return result;
}

/**
 * Reset the assembler for a new sequence.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @returns {{chunks: Array, expectedCount: number}} New state with cleared chunks
 */
export function reset(state) {
    return {
        chunks: [],
        expectedCount: 0
    };
}

/**
 * Get progress information for UI display.
 * @param {{chunks: Array, expectedCount: number}} state - Current state
 * @returns {{received: number, total: number}} Progress info
 */
export function getProgress(state) {
    return {
        received: getReceivedCount(state),
        total: state.expectedCount
    };
}
