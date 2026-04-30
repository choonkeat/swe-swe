// Session page theme initialization
import { applyTheme, getEffectivePrimaryColor, COLOR_STORAGE_KEYS, saveColorPreference } from './color-utils.js';
import { initThemeMode, setThemeMode, getStoredMode, THEME_MODES } from './theme-mode.js';

// Apply theme mode (light/dark/system) first
initThemeMode();

// Get session ID and repo whereKey from the terminal-ui element
const terminalUI = document.querySelector('terminal-ui');
const sessionId = terminalUI ? terminalUI.getAttribute('uuid') : '';
const whereKey = terminalUI ? (terminalUI.getAttribute('data-where-key') || '') : '';

// Get color from URL param
const urlParams = new URLSearchParams(window.location.search);
const urlColor = urlParams.get('color');

const primaryColor = getEffectivePrimaryColor({
    sessionId: sessionId,
    repoType: whereKey || undefined,
    urlParam: urlColor,
    fallback: '#7c3aed'
});

applyTheme(primaryColor);

// If color came from URL, save it for this session
if (urlColor && sessionId) {
    saveColorPreference(COLOR_STORAGE_KEYS.SESSION_PREFIX + sessionId, primaryColor);
}

// Expose for settings panel
window.sweSweTheme = {
    applyTheme,
    getEffectivePrimaryColor,
    COLOR_STORAGE_KEYS,
    saveColorPreference,
    sessionId,
    whereKey,
    getCurrentColor: () => getEffectivePrimaryColor({ sessionId, repoType: whereKey || undefined, fallback: '#7c3aed' }),
    initThemeMode,
    setThemeMode,
    getStoredMode,
    THEME_MODES
};
