/**
 * Pure reconnection state reducer functions.
 * Uses immutable state pattern for predictable reconnection behavior.
 * @module reconnect
 */

/**
 * Default reconnection configuration.
 */
export const RECONNECT_BASE_DELAY = 1000;   // 1 second
export const RECONNECT_MAX_DELAY = 60000;   // 60 seconds

/**
 * Create initial reconnection state.
 * @param {object} config - Optional configuration overrides
 * @param {number} config.baseDelay - Base delay in ms (default: 1000)
 * @param {number} config.maxDelay - Max delay in ms (default: 60000)
 * @returns {{attempts: number, baseDelay: number, maxDelay: number}} Initial state
 */
export function createReconnectState(config = {}) {
    return {
        attempts: 0,
        baseDelay: config.baseDelay ?? RECONNECT_BASE_DELAY,
        maxDelay: config.maxDelay ?? RECONNECT_MAX_DELAY
    };
}

/**
 * Calculate reconnection delay with exponential backoff.
 * Formula: min(baseDelay * 2^attempts, maxDelay)
 * @param {{attempts: number, baseDelay: number, maxDelay: number}} state - Current state
 * @returns {number} Delay in milliseconds
 */
export function getDelay(state) {
    const { attempts, baseDelay, maxDelay } = state;
    return Math.min(baseDelay * Math.pow(2, attempts), maxDelay);
}

/**
 * Create new state with incremented attempt count.
 * @param {{attempts: number, baseDelay: number, maxDelay: number}} state - Current state
 * @returns {{attempts: number, baseDelay: number, maxDelay: number}} New state with attempts+1
 */
export function nextAttempt(state) {
    return {
        ...state,
        attempts: state.attempts + 1
    };
}

/**
 * Create new state with reset attempt count.
 * @param {{attempts: number, baseDelay: number, maxDelay: number}} state - Current state
 * @returns {{attempts: number, baseDelay: number, maxDelay: number}} New state with attempts=0
 */
export function resetAttempts(state) {
    return {
        ...state,
        attempts: 0
    };
}

/**
 * Format delay for countdown display.
 * @param {number} delayMs - Delay in milliseconds
 * @returns {number} Delay in whole seconds (rounded up)
 */
export function formatCountdown(delayMs) {
    return Math.ceil(delayMs / 1000);
}
