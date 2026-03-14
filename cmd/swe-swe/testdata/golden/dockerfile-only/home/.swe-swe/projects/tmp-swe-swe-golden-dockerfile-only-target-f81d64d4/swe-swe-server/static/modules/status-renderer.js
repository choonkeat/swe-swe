/**
 * Pure HTML rendering functions for status bar.
 * Given state, return HTML string — no DOM manipulation.
 * @module status-renderer
 */

import { escapeHtml, parseLinks } from './util.js';
import { getDebugQueryString } from './url-builder.js';

/**
 * Get CSS classes for status bar based on state.
 * @param {{state: string, viewers: number, yoloMode: boolean}} status - Status state
 * @returns {string[]} Array of CSS class names
 */
export function getStatusBarClasses(status) {
    const classes = ['terminal-ui__status-bar'];

    if (status.state) {
        classes.push(status.state);
    }

    if (status.viewers > 1) {
        classes.push('multiuser');
    }

    if (status.yoloMode) {
        classes.push('yolo');
    }

    return classes;
}

/**
 * Render main status text (connecting, reconnecting, error states).
 * @param {{state: string, message: string}} status - Status state
 * @returns {string} HTML string
 */
export function renderStatusText(status) {
    // Message may contain HTML (like links), pass through as-is
    return status.message || '';
}

/**
 * Render the "Connected as X with Y" status info line.
 * @param {{
 *   connected: boolean,
 *   userName: string,
 *   assistantName: string,
 *   sessionName: string,
 *   uuidShort: string,
 *   viewers: number,
 *   yoloMode: boolean,
 *   yoloSupported: boolean,
 *   debugMode: boolean
 * }} state - Connection state
 * @returns {string} HTML string
 */
export function renderStatusInfo(state) {
    if (!state.connected) {
        return '';
    }

    const debugQS = getDebugQueryString(state.debugMode);
    const escapedUserName = escapeHtml(state.userName || '');
    const escapedAssistantName = escapeHtml(state.assistantName || '');

    // Show "YOLO" or "Connected" based on mode, make clickable if YOLO is supported
    const statusWord = state.yoloMode ? 'YOLO' : 'Connected';
    let html;

    if (state.yoloSupported) {
        html = `<span class="terminal-ui__status-link terminal-ui__status-yolo-toggle">${statusWord}</span> as <span class="terminal-ui__status-link terminal-ui__status-name">${escapedUserName}</span>`;
    } else {
        html = `${statusWord} as <span class="terminal-ui__status-link terminal-ui__status-name">${escapedUserName}</span>`;
    }

    if (escapedAssistantName) {
        html += ` with <a href="/${debugQS}" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${escapedAssistantName}</a>`;
    }

    // Add viewer suffix if more than 1 viewer
    if (state.viewers > 1) {
        html += ` and <span class="terminal-ui__status-link terminal-ui__status-others">${state.viewers - 1} others</span>`;
    }

    // Add session name display
    const sessionDisplay = state.sessionName || `Unnamed session ${state.uuidShort || ''}`;
    html += ` on <span class="terminal-ui__status-link terminal-ui__status-session">${escapeHtml(sessionDisplay.trim())}</span>`;

    return html;
}

/**
 * Render service links (Shell, VSCode, Preview, Browser).
 * @param {{
 *   services: Array<{name: string, label: string, url: string}>
 * }} config - Service configuration
 * @returns {string} HTML string for service links container
 */
export function renderServiceLinks(config) {
    if (!config.services || config.services.length === 0) {
        return '';
    }

    const parts = [];

    config.services.forEach((service, index) => {
        const escapedLabel = escapeHtml(service.label);
        const escapedUrl = escapeHtml(service.url);
        const escapedName = escapeHtml(service.name);

        parts.push(
            `<a href="${escapedUrl}" target="swe-swe-${escapedName}" ` +
            `class="terminal-ui__status-link terminal-ui__status-tab" ` +
            `data-tab="${escapedName}">${escapedLabel}</a>`
        );

        if (index < config.services.length - 1) {
            parts.push('<span class="terminal-ui__status-link-sep"> | </span>');
        }
    });

    return `<div class="terminal-ui__status-service-links">${parts.join('')}</div>`;
}

/**
 * Render custom user-defined links from markdown-style link string.
 * @param {string} linksStr - Markdown links like "[text](url) [text2](url2)"
 * @returns {string} HTML string for links container
 */
export function renderCustomLinks(linksStr) {
    const links = parseLinks(linksStr);

    if (links.length === 0) {
        return '';
    }

    const parts = [];

    links.forEach((link, index) => {
        const escapedText = escapeHtml(link.text);
        const escapedUrl = escapeHtml(link.url);

        parts.push(
            `<a href="${escapedUrl}" target="_blank" rel="noopener noreferrer" ` +
            `class="terminal-ui__status-link">${escapedText}</a>`
        );

        if (index < links.length - 1) {
            parts.push('<span class="terminal-ui__status-link-sep"> | </span>');
        }
    });

    return `<div class="terminal-ui__status-links">${parts.join('')}</div>`;
}

/**
 * Render an assistant link for status messages.
 * @param {{assistantName: string, assistant: string, debugMode: boolean}} config - Config
 * @returns {string} HTML anchor tag
 */
export function renderAssistantLink(config) {
    const name = config.assistantName || config.assistant || '';
    const debugQS = getDebugQueryString(config.debugMode);
    const escapedName = escapeHtml(name);
    return `<a href="/${debugQS}" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${escapedName}</a>`;
}
