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

/**
 * Poll a URL until it returns a successful response.
 * Uses exponential backoff via createReconnectState/getDelay/nextAttempt.
 * First attempt is immediate (no delay).
 * @param {string} url - URL to probe
 * @param {object} opts - Options
 * @param {number} opts.maxAttempts - Max probe attempts (default: 10)
 * @param {number} opts.baseDelay - Base delay in ms (default: 2000)
 * @param {number} opts.maxDelay - Max delay in ms (default: 30000)
 * @param {string} opts.method - HTTP method (default: 'HEAD')
 * @param {string} opts.credentials - Fetch credentials mode (default: 'include')
 * @param {AbortSignal} opts.signal - AbortSignal for cancellation
 * @returns {Promise<void>} Resolves when resp.ok, rejects on exhaustion or abort
 */
export function probeUntilReady(url, opts = {}) {
    const {
        maxAttempts = 10,
        baseDelay = 2000,
        maxDelay = 30000,
        method = 'HEAD',
        credentials = 'include',
        signal,
    } = opts;

    let state = createReconnectState({ baseDelay, maxDelay });
    let attempt = 0;

    return new Promise((resolve, reject) => {
        if (signal?.aborted) {
            reject(signal.reason ?? new DOMException('Aborted', 'AbortError'));
            return;
        }

        const onAbort = () => {
            reject(signal.reason ?? new DOMException('Aborted', 'AbortError'));
        };
        signal?.addEventListener('abort', onAbort, { once: true });

        const cleanup = () => {
            signal?.removeEventListener('abort', onAbort);
        };

        const probe = () => {
            if (signal?.aborted) return; // already rejected via onAbort
            attempt++;
            const fetchOpts = { method, credentials };
            if (signal) fetchOpts.signal = signal;
            fetch(url, fetchOpts).then(resp => {
                if (resp.ok) {
                    cleanup();
                    resolve();
                } else {
                    scheduleRetry();
                }
            }).catch(err => {
                if (err?.name === 'AbortError') return; // already rejected via onAbort
                scheduleRetry();
            });
        };

        const scheduleRetry = () => {
            if (attempt >= maxAttempts) {
                cleanup();
                reject(new Error(`probeUntilReady: ${maxAttempts} attempts exhausted`));
                return;
            }
            const delay = getDelay(state);
            state = nextAttempt(state);
            setTimeout(probe, delay);
        };

        // First attempt is immediate
        probe();
    });
}
