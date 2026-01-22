/**
 * Pure upload queue state reducer functions.
 * Uses immutable state pattern for file upload queue management.
 * @module upload-queue
 */

/**
 * Create initial queue state.
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} Initial state
 */
export function createQueue() {
    return {
        files: [],
        isUploading: false,
        uploadStartTime: null
    };
}

/**
 * Add a file to the queue.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @param {File} file - File to enqueue
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} New state
 */
export function enqueue(state, file) {
    return {
        ...state,
        files: [...state.files, file]
    };
}

/**
 * Remove the first file from the queue.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} New state
 */
export function dequeue(state) {
    return {
        ...state,
        files: state.files.slice(1)
    };
}

/**
 * Get the first file in the queue without removing it.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {File|null} First file or null if empty
 */
export function peek(state) {
    return state.files.length > 0 ? state.files[0] : null;
}

/**
 * Check if the queue is empty.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {boolean} True if empty
 */
export function isEmpty(state) {
    return state.files.length === 0;
}

/**
 * Get queue count.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {number} Number of files in queue
 */
export function getQueueCount(state) {
    return state.files.length;
}

/**
 * Get display info for the current queue state.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {{current: File|null, remaining: number}} Info for UI display
 */
export function getQueueInfo(state) {
    return {
        current: peek(state),
        remaining: Math.max(0, state.files.length - 1)
    };
}

/**
 * Mark upload as in progress.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @param {number} now - Current timestamp (Date.now())
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} New state
 */
export function startUploading(state, now = Date.now()) {
    return {
        ...state,
        isUploading: true,
        uploadStartTime: now
    };
}

/**
 * Mark upload as complete.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} New state
 */
export function stopUploading(state) {
    return {
        ...state,
        isUploading: false,
        uploadStartTime: null
    };
}

/**
 * Clear all files from the queue.
 * @param {{files: Array, isUploading: boolean, uploadStartTime: number|null}} state - Current state
 * @returns {{files: Array, isUploading: boolean, uploadStartTime: number|null}} New state
 */
export function clearQueue(state) {
    return {
        ...state,
        files: []
    };
}
