/**
 * Copying from the terminal. Selecting text no longer copies it; the user
 * presses the copy keys, and a hint names them after each selection.
 * Ctrl+C alone is the terminal's interrupt, so Windows/Linux copy with
 * Ctrl+Shift+C, as other terminals do.
 */

/** Whether this platform string (navigator.platform) is an Apple one. */
export function isApplePlatform(platform) {
    return /Mac|iPhone|iPad/.test(platform || '');
}

/** The hint shown after a selection, naming this keyboard's copy keys. */
export function copyHint(apple) {
    return apple ? 'Press \u2318C to copy' : 'Press Ctrl+Shift+C to copy';
}

/**
 * Whether a keydown is our Ctrl+Shift+C copy. Not on Apple, where Cmd+C
 * already fires the browser's copy event.
 */
export function isCopyShortcut(e, apple) {
    if (apple || !e || e.type !== 'keydown') return false;
    return e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey && (e.code === 'KeyC' || e.key === 'C' || e.key === 'c');
}
