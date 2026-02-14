// Theme mode management: light / dark / system
// Shared module used by both homepage and session page.

export const THEME_MODES = { LIGHT: 'light', DARK: 'dark', SYSTEM: 'system' };
export const THEME_STORAGE_KEY = 'swe-swe-theme-mode';

const DARK_MEDIA = window.matchMedia('(prefers-color-scheme: dark)');

// --- xterm theme objects ---
export const DARK_XTERM_THEME  = { background: '#1e1e1e', foreground: '#d4d4d4' };
export const LIGHT_XTERM_THEME = {
    background:  '#ffffff',
    foreground:  '#333333',
    cursor:      '#333333',
    black:          '#000000',
    red:            '#cd3131',
    green:          '#107C10',
    yellow:         '#866b00',
    blue:           '#0451a5',
    magenta:        '#bc05bc',
    cyan:           '#0b7285',
    white:          '#555555',
    brightBlack:    '#666666',
    brightRed:      '#cd3131',
    brightGreen:    '#14CE14',
    brightYellow:   '#866b00',
    brightBlue:     '#0451a5',
    brightMagenta:  '#bc05bc',
    brightCyan:     '#0b7285',
    brightWhite:    '#a5a5a5',
    selectionBackground: '#add6ff',
};

/** Read stored preference; defaults to 'system'. */
export function getStoredMode() {
    const v = localStorage.getItem(THEME_STORAGE_KEY);
    return v && Object.values(THEME_MODES).includes(v) ? v : THEME_MODES.SYSTEM;
}

/** Resolve 'system' to 'light' or 'dark' based on OS preference. */
export function getResolvedMode(mode) {
    if (mode === THEME_MODES.SYSTEM) {
        return DARK_MEDIA.matches ? THEME_MODES.DARK : THEME_MODES.LIGHT;
    }
    return mode;
}

/** Set the theme cookie so inline scripts on other pages can avoid FOUC. */
function setThemeCookie(resolved) {
    document.cookie = `swe-swe-theme=${resolved};path=/;max-age=31536000;SameSite=Lax`;
}

/** Apply resolved mode: set data-theme attribute (CSS handles variable overrides). */
export function applyMode(mode) {
    const resolved = getResolvedMode(mode);
    document.documentElement.setAttribute('data-theme', resolved);
    setThemeCookie(resolved);

    window.dispatchEvent(new CustomEvent('theme-mode-changed', {
        detail: { mode, resolved }
    }));
}

/** Persist preference and apply. */
export function setThemeMode(mode) {
    localStorage.setItem(THEME_STORAGE_KEY, mode);
    applyMode(mode);
}

/** Initialise theme: apply stored pref and listen for OS changes. */
export function initThemeMode() {
    const mode = getStoredMode();
    applyMode(mode);

    DARK_MEDIA.addEventListener('change', () => {
        if (getStoredMode() === THEME_MODES.SYSTEM) {
            applyMode(THEME_MODES.SYSTEM);
        }
    });
}
