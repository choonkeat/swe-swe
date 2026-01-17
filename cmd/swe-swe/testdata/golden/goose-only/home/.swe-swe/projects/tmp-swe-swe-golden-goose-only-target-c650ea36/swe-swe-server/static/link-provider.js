/**
 * Link providers for xterm.js
 * Makes file paths, URLs, and CSS colors clickable.
 * Links require modifier keys (Cmd/Ctrl or Shift) to activate.
 */

/**
 * Check if a link-activating modifier key is pressed.
 * Returns true if Cmd (macOS), Ctrl (other platforms), or Shift is pressed.
 * @param {MouseEvent} event - The mouse event
 * @returns {boolean} - Whether a valid modifier key is pressed
 */
function hasLinkModifier(event) {
    // Shift works on all platforms
    if (event.shiftKey) {
        return true;
    }
    // On macOS, use Cmd (metaKey); on other platforms, use Ctrl
    const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
    return isMac ? event.metaKey : event.ctrlKey;
}

/**
 * Get hint text for the modifier key needed to activate links.
 * @returns {string} - Hint text like "Cmd+Click" or "Ctrl+Click"
 */
function getLinkModifierHint() {
    const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
    return isMac ? 'Cmd+Click or Shift+Click' : 'Ctrl+Click or Shift+Click';
}

/**
 * Register a CSS color link provider with the terminal.
 * Makes CSS colors clickable to set the status bar color.
 * @param {Terminal} terminal - xterm.js Terminal instance
 * @param {Object} options - Configuration options
 * @param {Function} options.onColorClick - Callback when a color is clicked (receives color string)
 * @param {Function} [options.onHint] - Optional callback to show hint when clicked without modifier
 */
function registerColorLinkProvider(terminal, options) {
    // Match CSS colors:
    // - Hex: #rgb, #rrggbb (with or without alpha)
    // - Functional: rgb(), rgba(), hsl(), hsla(), oklch()
    const hexColorRegex = /#(?:[0-9a-fA-F]{3}){1,2}(?:[0-9a-fA-F]{2})?\b/g;
    const functionalColorRegex = /(?:rgb|rgba|hsl|hsla|oklch)\([^)]+\)/gi;

    function findColors(lineText) {
        const colors = [];

        // Find hex colors
        let match;
        hexColorRegex.lastIndex = 0;
        while ((match = hexColorRegex.exec(lineText)) !== null) {
            colors.push({
                text: match[0],
                startIndex: match.index
            });
        }

        // Find functional colors
        functionalColorRegex.lastIndex = 0;
        while ((match = functionalColorRegex.exec(lineText)) !== null) {
            colors.push({
                text: match[0],
                startIndex: match.index
            });
        }

        return colors;
    }

    terminal.registerLinkProvider({
        provideLinks: (bufferLineNumber, callback) => {
            const line = terminal.buffer.active.getLine(bufferLineNumber - 1);
            if (!line) {
                callback(undefined);
                return;
            }

            const lineText = line.translateToString(true);
            const colors = findColors(lineText);

            if (colors.length === 0) {
                callback(undefined);
                return;
            }

            const links = colors.map(color => ({
                text: color.text,
                range: {
                    start: { x: color.startIndex + 1, y: bufferLineNumber },
                    end: { x: color.startIndex + color.text.length + 1, y: bufferLineNumber }
                },
                activate: (event, text) => {
                    if (!hasLinkModifier(event)) {
                        if (options.onHint) {
                            options.onHint(getLinkModifierHint() + ' to set status bar color');
                        }
                        return;
                    }
                    if (options.onColorClick) {
                        options.onColorClick(text);
                    }
                },
                decorations: {
                    pointerCursor: true,
                    underline: true
                }
            }));

            callback(links);
        }
    });
}

/**
 * Register a file link provider with the terminal.
 * @param {Terminal} terminal - xterm.js Terminal instance
 * @param {Object} options - Configuration options
 * @param {Function} options.getVSCodeUrl - Function returning the VS Code URL
 * @param {Function} [options.onLinkClick] - Optional callback when a link is clicked
 * @param {Function} [options.onCopy] - Optional callback when path is copied (receives path string)
 * @param {Function} [options.onHint] - Optional callback to show hint when clicked without modifier
 */
function registerFileLinkProvider(terminal, options) {
    // Match file paths with optional line:col suffixes
    // Examples: /workspace/foo.go, ./src/bar.ts:42, src/baz.js:10:5
    const filePathRegex = /(?:^|[\s'"`(\[{])((\.{0,2}\/)?[a-zA-Z0-9_.-]+(?:\/[a-zA-Z0-9_.-]+)*\.[a-zA-Z0-9]+(?::\d+(?::\d+)?)?)/g;

    // Patterns to exclude (URLs, common false positives)
    const excludePatterns = [
        /^https?:\/\//i,
        /^ftp:\/\//i,
        /^file:\/\//i,
        /^git@/i,
        /^ssh:\/\//i,
        /^\d+\.\d+\.\d+/, // Version numbers like 1.2.3
    ];

    // Common file extensions to include
    const validExtensions = new Set([
        // Code
        'go', 'js', 'ts', 'jsx', 'tsx', 'py', 'rb', 'rs', 'java', 'kt', 'scala',
        'c', 'cpp', 'cc', 'h', 'hpp', 'cs', 'swift', 'php', 'pl', 'pm',
        // Config/Data
        'json', 'yaml', 'yml', 'toml', 'xml', 'csv', 'sql',
        // Web
        'html', 'htm', 'css', 'scss', 'sass', 'less', 'vue', 'svelte',
        // Shell/Scripts
        'sh', 'bash', 'zsh', 'fish', 'ps1', 'bat', 'cmd',
        // Docs
        'md', 'txt', 'rst', 'adoc',
        // Other
        'proto', 'graphql', 'gql', 'tf', 'tfvars', 'mod', 'sum',
        'env', 'lock', 'dockerfile', 'makefile', 'gitignore',
    ]);

    function isValidFilePath(text) {
        // Skip excluded patterns
        for (const pattern of excludePatterns) {
            if (pattern.test(text)) {
                return false;
            }
        }

        // Extract base filename and extension
        const pathPart = text.split(':')[0]; // Remove line:col suffix
        const parts = pathPart.split('/');
        const filename = parts[parts.length - 1];
        const extMatch = filename.match(/\.([a-zA-Z0-9]+)$/);

        if (!extMatch) {
            return false;
        }

        const ext = extMatch[1].toLowerCase();
        return validExtensions.has(ext);
    }

    terminal.registerLinkProvider({
        provideLinks: (bufferLineNumber, callback) => {
            const line = terminal.buffer.active.getLine(bufferLineNumber - 1);
            if (!line) {
                callback(undefined);
                return;
            }

            const lineText = line.translateToString(true);
            const links = [];

            let match;
            filePathRegex.lastIndex = 0;
            while ((match = filePathRegex.exec(lineText)) !== null) {
                const fullMatch = match[0];
                const filePath = match[1];

                if (!isValidFilePath(filePath)) {
                    continue;
                }

                // Calculate start index accounting for leading whitespace/delimiter
                const startIndex = match.index + (fullMatch.length - filePath.length);

                links.push({
                    text: filePath,
                    range: {
                        start: { x: startIndex + 1, y: bufferLineNumber },
                        end: { x: startIndex + filePath.length + 1, y: bufferLineNumber }
                    },
                    activate: (event, text) => {
                        if (!hasLinkModifier(event)) {
                            if (options.onHint) {
                                options.onHint(getLinkModifierHint() + ' to open file');
                            }
                            return;
                        }

                        // Copy path to clipboard (requires secure context)
                        if (navigator.clipboard) {
                            navigator.clipboard.writeText(text).then(() => {
                                if (options.onCopy) {
                                    options.onCopy(text);
                                }
                            }).catch(err => {
                                console.warn('Failed to copy to clipboard:', err);
                            });
                        }

                        // Open VS Code
                        if (options.getVSCodeUrl) {
                            window.open(options.getVSCodeUrl(), 'swe-swe-vscode');
                        }

                        // Call optional callback
                        if (options.onLinkClick) {
                            options.onLinkClick(text);
                        }
                    },
                    decorations: {
                        pointerCursor: true,
                        underline: true
                    }
                });
            }

            callback(links.length > 0 ? links : undefined);
        }
    });
}

/**
 * Register a URL link provider with the terminal.
 * Makes http/https URLs clickable.
 * @param {Terminal} terminal - xterm.js Terminal instance
 * @param {Object} [options] - Configuration options
 * @param {Function} [options.onLinkClick] - Optional callback when a link is clicked
 * @param {Function} [options.onHint] - Optional callback to show hint when clicked without modifier
 */
function registerUrlLinkProvider(terminal, options = {}) {
    // Match http/https URLs
    // Based on a simplified but practical URL regex
    // Allows parentheses for Wikipedia-style links; cleanUrl() handles trailing punctuation
    const urlRegex = /https?:\/\/[^\s<>\[\]"'`]+/gi;

    function cleanUrl(url) {
        // Remove trailing punctuation that's likely not part of the URL
        // But preserve if it looks like part of the path (e.g., URLs ending in parentheses for wiki links)
        let cleaned = url;

        // Balance parentheses - if there are more closing than opening, trim trailing )
        const openParens = (cleaned.match(/\(/g) || []).length;
        const closeParens = (cleaned.match(/\)/g) || []).length;
        if (closeParens > openParens) {
            const excess = closeParens - openParens;
            for (let i = 0; i < excess; i++) {
                if (cleaned.endsWith(')')) {
                    cleaned = cleaned.slice(0, -1);
                }
            }
        }

        // Remove common trailing punctuation
        cleaned = cleaned.replace(/[.,;:!?]+$/, '');

        return cleaned;
    }

    terminal.registerLinkProvider({
        provideLinks: (bufferLineNumber, callback) => {
            const line = terminal.buffer.active.getLine(bufferLineNumber - 1);
            if (!line) {
                callback(undefined);
                return;
            }

            const lineText = line.translateToString(true);
            const links = [];

            let match;
            urlRegex.lastIndex = 0;
            while ((match = urlRegex.exec(lineText)) !== null) {
                const rawUrl = match[0];
                const url = cleanUrl(rawUrl);
                const startIndex = match.index;

                links.push({
                    text: url,
                    range: {
                        start: { x: startIndex + 1, y: bufferLineNumber },
                        end: { x: startIndex + url.length + 1, y: bufferLineNumber }
                    },
                    activate: (event, text) => {
                        if (!hasLinkModifier(event)) {
                            if (options.onHint) {
                                options.onHint(getLinkModifierHint() + ' to open link');
                            }
                            return;
                        }

                        // Open URL in new tab
                        window.open(text, '_blank', 'noopener,noreferrer');

                        // Call optional callback
                        if (options.onLinkClick) {
                            options.onLinkClick(text);
                        }
                    },
                    decorations: {
                        pointerCursor: true,
                        underline: true
                    }
                });
            }

            callback(links.length > 0 ? links : undefined);
        }
    });
}

