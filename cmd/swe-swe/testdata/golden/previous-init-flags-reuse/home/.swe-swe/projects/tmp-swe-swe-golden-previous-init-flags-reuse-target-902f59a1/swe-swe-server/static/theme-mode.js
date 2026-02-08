// Theme mode management: light / dark / system
// Shared module used by both homepage and session page.

export const THEME_MODES = { LIGHT: 'light', DARK: 'dark', SYSTEM: 'system' };
export const THEME_STORAGE_KEY = 'swe-swe-theme-mode';

const DARK_MEDIA = window.matchMedia('(prefers-color-scheme: dark)');

// --- Light structural palette (overrides `:root` dark defaults) ---
const LIGHT_VARS = {
    '--bg-primary':      '#ffffff',
    '--bg-secondary':    '#f1f5f9',
    '--bg-tertiary':     '#f8fafc',
    '--bg-elevated':     '#e2e8f0',
    '--bg-terminal':     '#ffffff',
    '--border-primary':  '#cbd5e1',
    '--border-secondary':'#94a3b8',
    '--text-primary':    '#0f172a',
    '--text-secondary':  '#475569',
    '--text-muted':      '#94a3b8',
    '--text-bright':     '#0f172a',
};

// --- xterm theme objects ---
export const DARK_XTERM_THEME  = { background: '#1e1e1e', foreground: '#d4d4d4' };
export const LIGHT_XTERM_THEME = { background: '#ffffff', foreground: '#1e293b' };

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

/** Apply resolved mode: set data-theme attribute and structural CSS vars. */
export function applyMode(mode) {
    const resolved = getResolvedMode(mode);
    const root = document.documentElement;

    root.setAttribute('data-theme', resolved);

    if (resolved === THEME_MODES.LIGHT) {
        for (const [prop, val] of Object.entries(LIGHT_VARS)) {
            root.style.setProperty(prop, val);
        }
    } else {
        // Dark mode: remove overrides so :root defaults take effect
        for (const prop of Object.keys(LIGHT_VARS)) {
            root.style.removeProperty(prop);
        }
    }

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
