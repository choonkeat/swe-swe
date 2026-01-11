/**
 * Link providers for xterm.js
 * Makes file paths and CSS colors clickable.
 */

/**
 * Register a CSS color link provider with the terminal.
 * Makes CSS colors clickable to set the status bar color.
 * @param {Terminal} terminal - xterm.js Terminal instance
 * @param {Object} options - Configuration options
 * @param {Function} options.onColorClick - Callback when a color is clicked (receives color string)
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
                    if (options.onColorClick) {
                        options.onColorClick(text);
                    }
                },
                // Tooltip on hover
                decorations: {
                    pointerCursor: true
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
                        // Copy path to clipboard (requires secure context)
                        if (navigator.clipboard) {
                            navigator.clipboard.writeText(text).catch(err => {
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
                    }
                });
            }

            callback(links.length > 0 ? links : undefined);
        }
    });
}
