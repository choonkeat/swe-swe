// Homepage theme initialization
import { applyTheme, getEffectivePrimaryColor, COLOR_STORAGE_KEYS, saveColorPreference } from './color-utils.js';

const DEFAULT_COLOR = '#7c3aed';
const PRESET_COLORS = [
    '#7c3aed', // Purple (default)
    '#2563eb', // Blue
    '#0891b2', // Cyan
    '#059669', // Emerald
    '#16a34a', // Green
    '#ca8a04', // Yellow
    '#ea580c', // Orange
    '#dc2626', // Red
    '#db2777', // Pink
    '#9333ea', // Violet
];

// Apply theme on page load
const primaryColor = getEffectivePrimaryColor({ fallback: DEFAULT_COLOR });
applyTheme(primaryColor);

// Settings dialog functionality
const settingsOverlay = document.getElementById('settings-dialog-overlay');
const settingsBtn = document.getElementById('settings-btn');
const settingsClose = document.getElementById('settings-close');
const colorInput = document.getElementById('color-input');
const colorHex = document.getElementById('color-hex');
const colorReset = document.getElementById('color-reset');
const colorPresets = document.getElementById('color-presets');

// Populate preset colors
PRESET_COLORS.forEach(color => {
    const btn = document.createElement('button');
    btn.className = 'color-picker__preset';
    btn.style.backgroundColor = color;
    btn.dataset.color = color;
    btn.title = color;
    if (color === primaryColor) {
        btn.classList.add('selected');
    }
    btn.onclick = () => selectColor(color);
    colorPresets.appendChild(btn);
});

function selectColor(color) {
    // Update UI
    colorInput.value = color;
    colorHex.value = color;

    // Update preset selection
    colorPresets.querySelectorAll('.color-picker__preset').forEach(btn => {
        btn.classList.toggle('selected', btn.dataset.color === color);
    });

    // Apply and save
    applyTheme(color);
    saveColorPreference(COLOR_STORAGE_KEYS.SERVER_DEFAULT, color);
}

function openSettings() {
    const currentColor = getEffectivePrimaryColor({ fallback: DEFAULT_COLOR });
    colorInput.value = currentColor;
    colorHex.value = currentColor;
    colorPresets.querySelectorAll('.color-picker__preset').forEach(btn => {
        btn.classList.toggle('selected', btn.dataset.color === currentColor);
    });
    settingsOverlay.style.display = 'flex';
}

function closeSettings() {
    settingsOverlay.style.display = 'none';
}

settingsBtn.onclick = openSettings;
settingsClose.onclick = closeSettings;
settingsOverlay.onclick = (e) => {
    if (e.target === settingsOverlay) closeSettings();
};

colorInput.oninput = () => selectColor(colorInput.value);
colorHex.onchange = () => {
    let val = colorHex.value.trim();
    if (!val.startsWith('#')) val = '#' + val;
    if (/^#[0-9a-fA-F]{6}$/.test(val)) {
        selectColor(val);
    }
};

colorReset.onclick = () => {
    localStorage.removeItem(COLOR_STORAGE_KEYS.SERVER_DEFAULT);
    selectColor(DEFAULT_COLOR);
};

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && settingsOverlay.style.display === 'flex') {
        closeSettings();
    }
});

// Expose for other scripts
window.sweSweTheme = {
    applyTheme,
    getEffectivePrimaryColor,
    COLOR_STORAGE_KEYS,
    saveColorPreference,
    getCurrentColor: () => getEffectivePrimaryColor({ fallback: DEFAULT_COLOR }),
    DEFAULT_COLOR,
    PRESET_COLORS
};
