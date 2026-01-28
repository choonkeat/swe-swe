/**
 * Color utility functions for theming
 * Generates a complete color palette from a single primary color
 */

/**
 * Convert hex color to RGB object
 * @param {string} hex - Hex color (e.g., "#7c3aed" or "7c3aed")
 * @returns {{r: number, g: number, b: number}|null}
 */
export function hexToRgb(hex) {
    const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
    return result ? {
        r: parseInt(result[1], 16),
        g: parseInt(result[2], 16),
        b: parseInt(result[3], 16)
    } : null;
}

/**
 * Convert RGB to hex color
 * @param {number} r - Red (0-255)
 * @param {number} g - Green (0-255)
 * @param {number} b - Blue (0-255)
 * @returns {string} Hex color with # prefix
 */
export function rgbToHex(r, g, b) {
    const toHex = (c) => Math.round(Math.min(255, Math.max(0, c))).toString(16).padStart(2, '0');
    return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
}

/**
 * Calculate relative luminance per WCAG 2.1
 * @param {number} r - Red (0-255)
 * @param {number} g - Green (0-255)
 * @param {number} b - Blue (0-255)
 * @returns {number} Luminance (0-1)
 */
export function getLuminance(r, g, b) {
    const [rs, gs, bs] = [r, g, b].map(c => {
        c = c / 255;
        return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * rs + 0.7152 * gs + 0.0722 * bs;
}

/**
 * Calculate contrast ratio between two colors (WCAG)
 * @param {string} hex1 - First hex color
 * @param {string} hex2 - Second hex color
 * @returns {number} Contrast ratio (1-21)
 */
export function getContrastRatio(hex1, hex2) {
    const rgb1 = hexToRgb(hex1);
    const rgb2 = hexToRgb(hex2);
    const l1 = getLuminance(rgb1.r, rgb1.g, rgb1.b);
    const l2 = getLuminance(rgb2.r, rgb2.g, rgb2.b);
    const lighter = Math.max(l1, l2);
    const darker = Math.min(l1, l2);
    return (lighter + 0.05) / (darker + 0.05);
}

/**
 * Get contrasting text color (white or black) for a background
 * Uses WCAG luminance threshold for AA compliance (4.5:1 ratio)
 * @param {string} bgHex - Background hex color
 * @returns {string} "#ffffff" or "#000000"
 */
export function getContrastingTextColor(bgHex) {
    const rgb = hexToRgb(bgHex);
    if (!rgb) return '#ffffff';
    const luminance = getLuminance(rgb.r, rgb.g, rgb.b);
    // Threshold of 0.179 ensures 4.5:1 contrast with white or black
    return luminance > 0.179 ? '#000000' : '#ffffff';
}

/**
 * Lighten or darken a color by a percentage
 * @param {string} hex - Hex color
 * @param {number} percent - Positive to lighten, negative to darken (-100 to 100)
 * @returns {string} Adjusted hex color
 */
export function adjustColor(hex, percent) {
    const rgb = hexToRgb(hex);
    if (!rgb) return hex;

    const adjust = (c) => {
        if (percent > 0) {
            // Lighten: move toward 255
            return c + (255 - c) * (percent / 100);
        } else {
            // Darken: move toward 0
            return c * (1 + percent / 100);
        }
    };

    return rgbToHex(adjust(rgb.r), adjust(rgb.g), adjust(rgb.b));
}

/**
 * Convert hex to rgba string
 * @param {string} hex - Hex color
 * @param {number} alpha - Alpha value (0-1)
 * @returns {string} rgba() string
 */
export function hexToRgba(hex, alpha) {
    const rgb = hexToRgb(hex);
    if (!rgb) return `rgba(0, 0, 0, ${alpha})`;
    return `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, ${alpha})`;
}

/**
 * Generate a complete theme palette from a single primary color
 * @param {string} primaryHex - Primary accent color
 * @returns {Object} Complete color palette
 */
export function generatePalette(primaryHex) {
    return {
        // Primary variants
        primary: primaryHex,
        primaryHover: adjustColor(primaryHex, 15),      // Lighter for hover
        primaryDark: adjustColor(primaryHex, -15),      // Darker for gradients
        primaryLight: adjustColor(primaryHex, 30),      // Much lighter

        // Transparent variants for backgrounds
        primary10: hexToRgba(primaryHex, 0.1),
        primary20: hexToRgba(primaryHex, 0.2),
        primary30: hexToRgba(primaryHex, 0.3),

        // Text color for use on primary background
        primaryText: getContrastingTextColor(primaryHex),

        // For gradients (used in buttons)
        gradientStart: primaryHex,
        gradientEnd: adjustColor(primaryHex, -15),
        gradientHoverStart: adjustColor(primaryHex, 15),
        gradientHoverEnd: primaryHex,
    };
}

/**
 * Apply theme colors to document CSS variables
 * @param {string} primaryHex - Primary accent color
 */
export function applyTheme(primaryHex) {
    const palette = generatePalette(primaryHex);
    const root = document.documentElement;

    // Set CSS custom properties
    root.style.setProperty('--accent-primary', palette.primary);
    root.style.setProperty('--accent-hover', palette.primaryHover);
    root.style.setProperty('--accent-dark', palette.primaryDark);
    root.style.setProperty('--accent-light', palette.primaryLight);
    root.style.setProperty('--accent-10', palette.primary10);
    root.style.setProperty('--accent-20', palette.primary20);
    root.style.setProperty('--accent-30', palette.primary30);
    root.style.setProperty('--accent-text', palette.primaryText);
    root.style.setProperty('--accent-gradient', `linear-gradient(135deg, ${palette.gradientStart} 0%, ${palette.gradientEnd} 100%)`);
    root.style.setProperty('--accent-gradient-hover', `linear-gradient(135deg, ${palette.gradientHoverStart} 0%, ${palette.gradientHoverEnd} 100%)`);
}

/**
 * Storage keys for color preferences
 */
export const COLOR_STORAGE_KEYS = {
    SERVER_DEFAULT: 'swe-swe-primary-color',
    REPO_TYPE_PREFIX: 'swe-swe-color-repo-',
    SESSION_PREFIX: 'swe-swe-color-session-',
};

/**
 * Get the effective primary color for a context
 * Priority: session > repoType > server default > fallback
 * @param {Object} options
 * @param {string} options.sessionId - Session UUID (optional)
 * @param {string} options.repoType - Repository type (optional)
 * @param {string} options.urlParam - Color from URL param (optional)
 * @param {string} options.fallback - Fallback color (default: #7c3aed)
 * @returns {string} Hex color to use
 */
export function getEffectivePrimaryColor({ sessionId, repoType, urlParam, fallback = '#7c3aed' } = {}) {
    // URL param has highest priority (for sharing)
    if (urlParam && /^#?[0-9a-fA-F]{6}$/.test(urlParam)) {
        return urlParam.startsWith('#') ? urlParam : `#${urlParam}`;
    }

    // Session-specific color
    if (sessionId) {
        const sessionColor = localStorage.getItem(COLOR_STORAGE_KEYS.SESSION_PREFIX + sessionId);
        if (sessionColor) return sessionColor;
    }

    // Repository type color
    if (repoType) {
        const repoColor = localStorage.getItem(COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + repoType);
        if (repoColor) return repoColor;
    }

    // Server default
    const serverDefault = localStorage.getItem(COLOR_STORAGE_KEYS.SERVER_DEFAULT);
    if (serverDefault) return serverDefault;

    // Fallback
    return fallback;
}

/**
 * Save a color preference
 * @param {string} key - Full storage key
 * @param {string} color - Hex color
 */
export function saveColorPreference(key, color) {
    localStorage.setItem(key, color);
}

/**
 * Clear a color preference
 * @param {string} key - Full storage key
 */
export function clearColorPreference(key) {
    localStorage.removeItem(key);
}
