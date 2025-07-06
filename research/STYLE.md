# CSS Style Guide for SWE-SWE

This document outlines the CSS variable system and color schemes used in the SWE-SWE application.

## CSS Variables

We use CSS variables to maintain consistent colors and styles throughout the application. This approach makes it easier to:
- Maintain visual consistency 
- Support theming (light/dark modes)
- Update the design system without hunting for every color instance
- Ensure permission-related UI elements match the rest of the application

## Color Palette

### Base Colors
```css
--color-white: #ffffff;
--color-black: #000000;
```

### Grayscale Palette
```css
--color-gray-50: #f5f5f5;    /* Light background */
--color-gray-100: #f0f0f0;   /* Alt background, code blocks */
--color-gray-200: #e0e0e0;   /* Borders, dividers */
--color-gray-300: #cccccc;   /* Disabled elements */
--color-gray-400: #b0b0b0;   /* Secondary text in dark mode */
--color-gray-500: #757575;   /* Disconnected status, neutral elements */
--color-gray-600: #666666;   /* Secondary text */
--color-gray-700: #555555;   /* Tertiary text */
--color-gray-800: #333333;   /* Primary text */
--color-gray-900: #1e1e1e;   /* Dark mode background */
```

### Primary Blues
```css
--color-blue-100: #e3f2fd;   /* User message background (light) */
--color-blue-500: #4a90e2;   /* Primary buttons, links */
--color-blue-600: #357abf;   /* Hover state */
--color-blue-700: #1976d2;   /* Focused state, permanent allow button */
--color-blue-800: #0e639c;   /* Dark mode primary */
--color-blue-text: #0c5991;  /* User message text */
```

### Status Colors
```css
--color-success: #4caf50;      /* Connected status, allow button */
--color-success-dark: #388e3c; /* Allow button hover */
--color-warning: #ff9800;      /* Warning notices, permission header */
--color-warning-dark: #f57c00; /* Permission header gradient */
--color-danger: #e53935;       /* Deny button, errors */
--color-danger-dark: #d32f2f;  /* Deny button hover */
```

## Semantic Variables

These variables map our color palette to specific UI purposes:

### Background Colors
```css
--bg-body: var(--color-gray-50);
--bg-surface: var(--color-white);
--bg-surface-alt: var(--color-gray-100);
--bg-code: var(--color-gray-100);
--bg-user-message: var(--color-blue-100);
```

### Text Colors
```css
--text-primary: var(--color-gray-800);
--text-secondary: var(--color-gray-700);
--text-tertiary: var(--color-gray-600);
--text-user-message: var(--color-blue-text);
```

### Border and Shadow
```css
--border-light: var(--color-gray-200);
--shadow-sm: 0 2px 4px rgba(0, 0, 0, 0.1);
--shadow-md: 0 4px 8px rgba(0, 0, 0, 0.15);
```

### Interactive Elements
```css
--color-primary: var(--color-blue-500);
--color-primary-hover: var(--color-blue-600);
```

## Permission UI Colors

```css
--permission-header-bg: linear-gradient(135deg, #f57c00 0%, #ff6f00 100%);
--permission-notice-bg: #fff3e0;
--permission-allow-bg: #43a047;
--permission-allow-hover: #388e3c;
--permission-deny-bg: #e53935;
--permission-deny-hover: #d32f2f;
--permission-permanent-bg: #1976d2;
--permission-permanent-hover: #1565c0;
--permission-skip-bg: #757575;
--permission-skip-hover: #616161;
```

## Dark Theme Variables

In dark mode, we override certain variables:

```css
--bg-body: var(--color-gray-900);
--bg-surface: #2d2d2d;
--bg-surface-alt: #383838;
--bg-code: #3a3a3a;
  
--text-primary: #d4d4d4;
--text-secondary: #b0b0b0;
--text-tertiary: #999999;
  
--border-light: #444444;
  
--color-primary: var(--color-blue-800);
--color-primary-hover: #1177bb;
  
--permission-header-bg: linear-gradient(135deg, #268bd2 0%, #2aa198 100%);
--permission-notice-bg: rgba(255, 152, 0, 0.1);
```

## Usage

Apply these variables in the CSS instead of direct color values:

```css
body {
  background-color: var(--bg-body);
  color: var(--text-primary);
}

.header {
  background-color: var(--bg-surface);
  border-bottom: 1px solid var(--border-light);
}

.connection-status.connected {
  background-color: var(--color-success);
}

.send-button {
  background-color: var(--color-primary);
  color: var(--color-white);
}

.send-button:hover {
  background-color: var(--color-primary-hover);
}
```

## Theme Switching

Theme switching is handled by changing the variable values:

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg-body: var(--color-gray-900);
    --bg-surface: #2d2d2d;
    /* other overrides */
  }
}
```

Or for class-based theme switching:

```css
body.dark-theme {
  --bg-body: var(--color-gray-900);
  --bg-surface: #2d2d2d;
  /* other overrides */
}
```
