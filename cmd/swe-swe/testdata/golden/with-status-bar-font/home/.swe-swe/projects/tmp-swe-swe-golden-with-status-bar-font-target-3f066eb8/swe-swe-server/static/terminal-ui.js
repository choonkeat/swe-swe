class TerminalUI extends HTMLElement {
    constructor() {
        super();
        this.ws = null;
        this.term = null;
        this.fitAddon = null;
        this.connectedAt = null;
        this.reconnectAttempts = 0;
        this.reconnectTimeout = null;
        this.countdownInterval = null;
        this.uptimeInterval = null;
        this.heartbeatInterval = null;
        // Mobile keyboard state
        this.ctrlActive = false;
        this.navActive = false;
        // Session status from server
        this.viewers = 0;
        this.ptyRows = 0;
        this.ptyCols = 0;
        this.assistantName = '';
        this.sessionName = '';
        this.uuidShort = '';
        this.workDir = '';
        // Chat feature
        this.currentUserName = null;
        this.chatMessages = [];
        this.chatInputOpen = false;
        this.unreadChatCount = 0;
        this.chatMessageTimeouts = [];
        // File upload queue
        this.uploadQueue = [];
        this.isUploading = false;
        this.uploadStartTime = null;
        // Chunked snapshot reassembly
        this.chunks = [];
        this.expectedChunks = 0;
        // Debug mode from query string (accepts debug=true, debug=1, etc.)
        const debugParam = new URLSearchParams(location.search).get('debug');
        this.debugMode = debugParam === 'true' || debugParam === '1';
        // PTY output instrumentation for idle detection
        this.lastOutputTime = null;
        this.outputIdleTimer = null;
        this.outputIdleThreshold = 2000; // ms
        // Process exit state - prevents reconnection after session ends
        this.processExited = false;
        // Split-pane UI state
        this.iframePaneWidth = 50; // percentage
        this.activeTab = null; // null | 'shell' | 'vscode' | 'preview' | 'browser'
        // Split-pane constants for desktop detection
        this.MIN_PANEL_WIDTH = 360; // minimum width for terminal or iframe pane
        this.RESIZER_WIDTH = 8;     // width of the resizer handle
    }

    static get observedAttributes() {
        return ['uuid', 'assistant', 'links'];
    }

    get uuid() {
        return this.getAttribute('uuid') || '';
    }

    get assistant() {
        return this.getAttribute('assistant') || '';
    }

    get links() {
        return this.getAttribute('links') || '';
    }

    connectedCallback() {
        // Capture original window height BEFORE keyboard can appear
        // This is critical for visualViewport keyboard detection
        this.originalWindowHeight = window.innerHeight;
        this.lastKeyboardHeight = 0;

        try {
            // Redirect to homepage if no assistant specified
            if (!this.assistant) {
                window.location.href = '/';
                return;
            }
            // Load username from localStorage if available
            let storedName = null;
            try {
                storedName = localStorage.getItem('swe-swe-username');
            } catch (e) {
                console.warn('[TerminalUI] localStorage not available:', e);
            }
            if (storedName) {
                this.currentUserName = storedName;
            } else {
                // Auto-generate a random username and store it immediately
                this.currentUserName = this.generateRandomUsername();
                try {
                    localStorage.setItem('swe-swe-username', this.currentUserName);
                } catch (e) {
                    console.warn('[TerminalUI] Could not save username:', e);
                }
            }
            this.render();
            // Detect if running inside an iframe (panel view) and hide status bar
            if (window.self !== window.top) {
                this.classList.add('embedded-in-iframe');
            }
            this.initTerminal();
            this.debugLog('initTerminal done');
            // iOS Safari needs a brief delay before WebSocket connection
            // Without this, the connection silently fails (works with Web Inspector attached
            // because the debugger adds enough delay)
            this.debugLog('scheduling connect() in 200ms');
            setTimeout(() => {
                this.debugLog('setTimeout fired, calling connect()');
                this.connect();
            }, 200);
            this.setupEventListeners();
            this.renderLinks();
            this.renderServiceLinks();
            this.initSplitPaneUi();

            // Expose for console testing
            window.terminalUI = this;
        } catch (e) {
            console.error('[TerminalUI] connectedCallback failed:', e);
            // Show error in the UI since we might not have console
            document.body.innerHTML = '<pre style="color:red;padding:20px;">Init error: ' + e.message + '\n' + e.stack + '</pre>';
        }
    }

    disconnectedCallback() {
        this.cleanup();
    }

    attributeChangedCallback(name, oldValue, newValue) {
        if (name === 'links' && oldValue !== newValue) {
            this.renderLinks();
        }
    }

    cleanup() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
        if (this.reconnectTimeout) {
            clearTimeout(this.reconnectTimeout);
        }
        if (this.countdownInterval) {
            clearInterval(this.countdownInterval);
        }
        if (this.uptimeInterval) {
            clearInterval(this.uptimeInterval);
        }
        if (this.heartbeatInterval) {
            clearInterval(this.heartbeatInterval);
        }
        // Clean up chat message timeouts
        this.chatMessageTimeouts.forEach(timeout => clearTimeout(timeout));
        this.chatMessageTimeouts = [];
        // Clean up visualViewport listeners
        if (window.visualViewport && this._viewportHandler) {
            window.visualViewport.removeEventListener('resize', this._viewportHandler);
            window.visualViewport.removeEventListener('scroll', this._viewportHandler);
        }
        if (this.term) {
            this.term.dispose();
        }
    }

    render() {
        this.innerHTML = `
            <div class="terminal-ui">
                <div class="settings-panel" hidden aria-modal="true" role="dialog" aria-labelledby="settings-panel-title">
                    <div class="settings-panel__backdrop"></div>
                    <div class="settings-panel__content">
                        <header class="settings-panel__header">
                            <span id="settings-panel-title">Session Settings</span>
                            <button class="settings-panel__close" aria-label="Close settings">&times;</button>
                        </header>
                        <div class="settings-panel__body">
                            <section class="settings-panel__fields">
                                <div class="settings-panel__field">
                                    <label class="settings-panel__label" for="settings-username">Username</label>
                                    <input type="text" id="settings-username" class="settings-panel__input" placeholder="Enter your name" maxlength="16">
                                </div>
                                <div class="settings-panel__field">
                                    <label class="settings-panel__label" for="settings-session">Session Name</label>
                                    <input type="text" id="settings-session" class="settings-panel__input" placeholder="Enter session name" maxlength="32">
                                </div>
                                <div class="settings-panel__field">
                                    <label class="settings-panel__label">Status Bar Color</label>
                                    <div class="settings-panel__color-row">
                                        <input type="color" class="settings-panel__color-picker" value="#007acc">
                                        <input type="text" class="settings-panel__input settings-panel__color-input" placeholder="#007acc or color name">
                                    </div>
                                    <div class="settings-panel__swatches">
                                        <button class="settings-panel__swatch" style="background: #007acc" data-color="#007acc" title="Blue"></button>
                                        <button class="settings-panel__swatch" style="background: #dc2626" data-color="#dc2626" title="Red"></button>
                                        <button class="settings-panel__swatch" style="background: #16a34a" data-color="#16a34a" title="Green"></button>
                                        <button class="settings-panel__swatch" style="background: #f97316" data-color="#f97316" title="Orange"></button>
                                        <button class="settings-panel__swatch" style="background: #8b5cf6" data-color="#8b5cf6" title="Purple"></button>
                                        <button class="settings-panel__swatch" style="background: #64748b" data-color="#64748b" title="Gray"></button>
                                        <button class="settings-panel__swatch" style="background: #eab308" data-color="#eab308" title="Yellow"></button>
                                        <button class="settings-panel__swatch" style="background: #ec4899" data-color="#ec4899" title="Pink"></button>
                                    </div>
                                </div>
                                <div class="settings-panel__field settings-panel__field--yolo">
                                    <div class="settings-panel__toggle-row">
                                        <label class="settings-panel__label">YOLO Mode</label>
                                        <div class="settings-panel__toggle" id="settings-yolo-toggle" role="switch" aria-checked="false" tabindex="0"></div>
                                    </div>
                                </div>
                            </section>
                            <nav class="settings-panel__nav">
                                <a href="/" target="swe-swe-home" class="settings-panel__nav-btn">
                                    <span class="settings-panel__nav-icon">🏠</span>
                                    <span>Home</span>
                                </a>
                                <a href="" target="_blank" class="settings-panel__nav-btn settings-panel__nav-shell settings-panel__nav-tab" data-tab="shell">
                                    <span class="settings-panel__nav-icon">💻</span>
                                    <span>Shell</span>
                                </a>
                                <a href="/vscode/" target="swe-swe-vscode" class="settings-panel__nav-btn settings-panel__nav-vscode settings-panel__nav-tab" data-tab="vscode">
                                    <span class="settings-panel__nav-icon">📝</span>
                                    <span>VSCode</span>
                                </a>
                                <a href="" target="_blank" class="settings-panel__nav-btn settings-panel__nav-tab" data-tab="preview">
                                    <span class="settings-panel__nav-icon">📡</span>
                                    <span>App Preview</span>
                                </a>
                                <a href="/chrome/" target="swe-swe-browser" class="settings-panel__nav-btn settings-panel__nav-tab" data-tab="browser">
                                    <span class="settings-panel__nav-icon">🌐</span>
                                    <span>Agent View</span>
                                </a>
                            </nav>
                        </div>
                    </div>
                </div>
                <div class="terminal-ui__drop-overlay">
                    <div class="terminal-ui__drop-icon">+</div>
                    <div>Drop file to paste contents</div>
                </div>
                <div class="terminal-ui__upload-overlay">
                    <div class="terminal-ui__upload-spinner"></div>
                    <div class="terminal-ui__upload-text">
                        <div class="terminal-ui__upload-filename"></div>
                        <div class="terminal-ui__upload-queue"></div>
                    </div>
                </div>
                <div class="terminal-ui__chat-overlay"></div>
                <div class="terminal-ui__chat-input-overlay">
                    <input
                        type="text"
                        class="terminal-ui__chat-input"
                        placeholder="Enter message..."
                    >
                    <div class="terminal-ui__chat-button-group">
                        <button class="terminal-ui__chat-send-btn">Send</button>
                        <button class="terminal-ui__chat-cancel-btn">Cancel</button>
                    </div>
                </div>
                <div class="terminal-ui__paste-overlay">
                    <textarea
                        class="terminal-ui__paste-textarea"
                        placeholder="Paste content here..."
                    ></textarea>
                    <div class="terminal-ui__paste-button-group">
                        <button class="terminal-ui__paste-send-btn">Send</button>
                        <button class="terminal-ui__paste-cancel-btn">Cancel</button>
                    </div>
                </div>
                <div class="terminal-ui__split-pane">
                    <div class="terminal-ui__terminal"></div>
                    <div class="terminal-ui__resizer"></div>
                    <div class="terminal-ui__iframe-pane">
                        <div class="terminal-ui__iframe-location">
                            <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-home" title="Home">⌂</button>
                            <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-refresh" title="Refresh">↻</button>
                            <span class="terminal-ui__iframe-url"></span>
                        </div>
                        <div class="terminal-ui__iframe-container">
                            <div class="terminal-ui__iframe-placeholder">
                                Loading...
                                <div class="terminal-ui__iframe-placeholder-hint">
                                    If you have issues, check the URL or try refreshing.
                                </div>
                            </div>
                            <iframe class="terminal-ui__iframe" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals"></iframe>
                        </div>
                    </div>
                </div>
                <div class="touch-scroll-proxy">
                    <div class="scroll-spacer"></div>
                </div>
                <div class="mobile-keyboard">
                    <div class="mobile-keyboard__main">
                        <button data-key="Escape">Esc</button>
                        <button data-key="Tab">Tab</button>
                        <button data-key="ShiftTab">⇧Tab</button>
                        <button data-toggle="ctrl" class="mobile-keyboard__toggle">Ctrl</button>
                        <button data-toggle="nav" class="mobile-keyboard__toggle">Nav</button>
                    </div>
                    <div class="mobile-keyboard__ctrl">
                        <button data-ctrl="a"><span>A</span><small>begin</small></button>
                        <button data-ctrl="c"><span>C</span><small>cancel</small></button>
                        <button data-ctrl="d"><span>D</span><small>eof</small></button>
                        <button data-ctrl="e"><span>E</span><small>end</small></button>
                        <button data-ctrl="k"><span>K</span><small>kill</small></button>
                        <button data-ctrl="o"><span>O</span><small>open</small></button>
                        <button data-ctrl="w"><span>W</span><small>word</small></button>
                    </div>
                    <div class="mobile-keyboard__nav">
                        <button data-key="AltLeft">⌥←</button>
                        <button data-key="ArrowLeft">←</button>
                        <button data-key="ArrowRight">→</button>
                        <button data-key="AltRight">⌥→</button>
                        <button data-key="ArrowUp">↑</button>
                        <button data-key="ArrowDown">↓</button>
                    </div>
                    <div class="mobile-keyboard__input">
                        <button class="mobile-keyboard__attach" aria-label="Attach file" title="Uploads are temporary (in-memory). Move to /workspace to keep.">
                            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>
                            </svg>
                        </button>
                        <input type="file" class="mobile-keyboard__file-input" multiple hidden>
                        <textarea rows="1" placeholder="Type command..." class="mobile-keyboard__text" autocomplete="off"></textarea>
                        <button class="mobile-keyboard__send">Enter</button>
                    </div>
                </div>
                <div class="terminal-ui__status-bar">
                    <div class="terminal-ui__status-left">
                        <div class="terminal-ui__status-icon"></div>
                        <span class="terminal-ui__status-text">Connecting...</span>
                    </div>
                    <div class="terminal-ui__status-right">
                        <span class="terminal-ui__status-info"></span>
                        <span class="terminal-ui__status-dims"></span>
                        <span class="terminal-ui__status-timer"></span>
                    </div>
                </div>
            </div>
        `;
    }

    initTerminal() {
        const terminalEl = this.querySelector('.terminal-ui__terminal');

        this.term = new Terminal({
            cursorBlink: true,
            fontSize: 14,
            fontFamily: 'Menlo, Monaco, "Courier New", monospace',
            scrollback: 5000,
            theme: {
                background: '#1e1e1e',
                foreground: '#d4d4d4'
            }
        });

        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);
        this.term.open(terminalEl);
        this.fitAddon.fit();

        // Track focus state to dim terminal on desktop when blurred
        // Use textarea focus/blur since xterm.js onFocus/onBlur may not be publicly exposed
        const mobileKeyboard = this.querySelector('.mobile-keyboard');
        const textarea = terminalEl.querySelector('textarea');
        if (textarea) {
            textarea.addEventListener('focus', () => {
                terminalEl.classList.remove('blurred');
            });
            textarea.addEventListener('blur', () => {
                // Only dim terminal on desktop (when mobile keyboard not visible)
                if (!mobileKeyboard || !mobileKeyboard.classList.contains('visible')) {
                    terminalEl.classList.add('blurred');
                }
            });
        }
        // Start blurred since terminal doesn't have focus initially
        if (!mobileKeyboard || !mobileKeyboard.classList.contains('visible')) {
            terminalEl.classList.add('blurred');
        }

        // Common hint callback for all link providers
        const onHint = (msg) => this.showStatusNotification(msg);

        // Register file path link provider for clickable paths
        if (typeof registerFileLinkProvider === 'function') {
            registerFileLinkProvider(this.term, {
                getVSCodeUrl: () => this.getVSCodeUrl(),
                onCopy: (path) => this.showStatusNotification('Copied: ' + path),
                onHint
            });
        }

        // Register color link provider for clickable CSS colors
        if (typeof registerColorLinkProvider === 'function') {
            registerColorLinkProvider(this.term, {
                onColorClick: (color) => this.setStatusBarColor(color),
                onHint
            });
        }

        // Register URL link provider for clickable http/https URLs
        if (typeof registerUrlLinkProvider === 'function') {
            registerUrlLinkProvider(this.term, { onHint });
        }

        this.term.write('Session: ' + this.uuid + '\r\n');
    }

    formatDuration(ms) {
        const seconds = Math.floor(ms / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        if (hours > 0) {
            return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
        } else if (minutes > 0) {
            return `${minutes}m ${seconds % 60}s`;
        }
        return `${seconds}s`;
    }

    parseLinks(linksStr) {
        if (!linksStr) return [];
        // Parse markdown-style links: [text](url)
        // Pattern handles escaped brackets if needed
        const regex = /\[([^\]]+)\]\(([^)]+)\)/g;
        const links = [];
        let match;
        while ((match = regex.exec(linksStr)) !== null) {
            links.push({ text: match[1], url: match[2] });
        }
        return links;
    }

    renderLinks() {
        const statusRight = this.querySelector('.terminal-ui__status-right');
        if (!statusRight) return;

        // Remove any existing link container
        const existingContainer = statusRight.querySelector('.terminal-ui__status-links');
        if (existingContainer) {
            existingContainer.remove();
        }

        const links = this.parseLinks(this.links);
        if (links.length === 0) return;

        const container = document.createElement('div');
        container.className = 'terminal-ui__status-links';

        links.forEach((link, index) => {
            const a = document.createElement('a');
            a.href = link.url;
            a.target = '_blank';
            a.rel = 'noopener noreferrer';
            a.className = 'terminal-ui__status-link';
            a.textContent = link.text;
            container.appendChild(a);

            // Add separator between links
            if (index < links.length - 1) {
                const sep = document.createElement('span');
                sep.className = 'terminal-ui__status-link-sep';
                sep.textContent = ' | ';
                container.appendChild(sep);
            }
        });

        statusRight.insertBefore(container, statusRight.firstChild);
    }

    getBaseUrl() {
        const protocol = window.location.protocol;
        const port = window.location.port;
        return port ? `${protocol}//${window.location.hostname}:${port}` : `${protocol}//${window.location.hostname}`;
    }

    getVSCodeUrl() {
        const baseUrl = this.getBaseUrl();
        if (this.workDir) {
            return `${baseUrl}/vscode/?folder=${encodeURIComponent(this.workDir)}`;
        }
        return `${baseUrl}/vscode/`;
    }

    // Derive a deterministic shell UUID from parent session UUID
    // Uses djb2 hash algorithm (works in both HTTP and HTTPS contexts)
    deriveShellUUID(parentUUID) {
        const input = 'shell:' + parentUUID;
        // djb2 hash function - fast and produces good distribution
        const djb2 = (str, seed = 5381) => {
            let hash = seed;
            for (let i = 0; i < str.length; i++) {
                hash = ((hash << 5) + hash) + str.charCodeAt(i);
                hash = hash >>> 0; // Convert to unsigned 32-bit integer
            }
            return hash;
        };
        // Generate enough hash values to fill a UUID (128 bits = 4 x 32-bit)
        const h1 = djb2(input);
        const h2 = djb2(input, h1);
        const h3 = djb2(input, h2);
        const h4 = djb2(input, h3);
        // Format as UUID: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
        const hex = (h1.toString(16).padStart(8, '0') +
                     h2.toString(16).padStart(8, '0') +
                     h3.toString(16).padStart(8, '0') +
                     h4.toString(16).padStart(8, '0'));
        return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-${((parseInt(hex.slice(16, 18), 16) & 0x3f) | 0x80).toString(16)}${hex.slice(18, 20)}-${hex.slice(20, 32)}`;
    }

    renderServiceLinks() {
        const statusRight = this.querySelector('.terminal-ui__status-right');
        if (!statusRight) return;

        // Remove any existing service links container
        const existingContainer = statusRight.querySelector('.terminal-ui__status-service-links');
        if (existingContainer) {
            existingContainer.remove();
        }

        // All services use path-based routing
        const baseUrl = this.getBaseUrl();
        const services = [
            { name: 'vscode', label: 'VSCode', url: this.getVSCodeUrl() },
            { name: 'preview', label: 'App Preview', url: this.previewBaseUrl || `${window.location.protocol}//${window.location.hostname}:1${window.location.port || '80'}` },
            { name: 'browser', label: 'Agent View', url: `${baseUrl}/chrome/` }
        ];

        // Add shell link if not already in a shell session
        if (this.assistant !== 'shell') {
            const shellUUID = this.deriveShellUUID(this.uuid);
            const debugQS = this.debugMode ? '&debug=1' : '';
            const shellUrl = `${baseUrl}/session/${shellUUID}?assistant=shell&parent=${encodeURIComponent(this.uuid)}${debugQS}`;
            services.unshift({ name: 'shell', label: 'Shell', url: shellUrl });
        }

        const container = document.createElement('div');
        container.className = 'terminal-ui__status-service-links';

        services.forEach((service, index) => {
            const a = document.createElement('a');
            a.href = service.url;
            a.target = `swe-swe-${service.name}`;
            a.className = 'terminal-ui__status-link terminal-ui__status-tab';
            a.dataset.tab = service.name;
            a.textContent = service.label;

            // Add click handler for tab toggle behavior
            a.addEventListener('click', (e) => this.handleTabClick(e, service.name, service.url));

            container.appendChild(a);

            // Add separator between links
            if (index < services.length - 1) {
                const sep = document.createElement('span');
                sep.className = 'terminal-ui__status-link-sep';
                sep.textContent = ' | ';
                container.appendChild(sep);
            }
        });

        // Insert before status-info (first child after any custom links)
        const statusInfo = statusRight.querySelector('.terminal-ui__status-info');
        if (statusInfo) {
            statusRight.insertBefore(container, statusInfo);
        } else {
            statusRight.insertBefore(container, statusRight.firstChild);
        }
    }

    updateStatus(state, message) {
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const statusText = this.querySelector('.terminal-ui__status-text');
        const terminalEl = this.querySelector('.terminal-ui__terminal');

        // Preserve multiuser class if present
        const isMultiuser = statusBar.classList.contains('multiuser');
        statusBar.className = 'terminal-ui__status-bar ' + state;
        if (isMultiuser) {
            statusBar.classList.add('multiuser');
        }
        statusText.innerHTML = message;
        terminalEl.classList.toggle('disconnected', state !== 'connected' && state !== '');

        // Clear status info when not connected
        if (state !== 'connected') {
            const infoEl = this.querySelector('.terminal-ui__status-info');
            if (infoEl) infoEl.textContent = '';
        }
    }

    startUptimeTimer() {
        if (this.uptimeInterval) clearInterval(this.uptimeInterval);
        this.connectedAt = Date.now();
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        this.uptimeInterval = setInterval(() => {
            timerEl.textContent = this.formatDuration(Date.now() - this.connectedAt);
        }, 1000);
        timerEl.textContent = '0s';
    }

    stopUptimeTimer() {
        if (this.uptimeInterval) {
            clearInterval(this.uptimeInterval);
            this.uptimeInterval = null;
        }
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        if (timerEl) timerEl.textContent = '';
    }

    getReconnectDelay() {
        return Math.min(1000 * Math.pow(2, this.reconnectAttempts), 60000);
    }

    getDebugQueryString() {
        return this.debugMode ? '?debug=1' : '';
    }

    getAssistantLink() {
        const name = this.assistantName || this.assistant;
        const debugQS = this.getDebugQueryString();
        return `<a href="/${debugQS}" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${name}</a>`;
    }

    scheduleReconnect() {
        const delay = this.getReconnectDelay();
        this.reconnectAttempts++;

        let remaining = Math.ceil(delay / 1000);
        this.updateStatus('reconnecting', `Reconnecting to ${this.getAssistantLink()} in ${remaining}s...`);

        this.countdownInterval = setInterval(() => {
            remaining--;
            if (remaining > 0) {
                this.updateStatus('reconnecting', `Reconnecting to ${this.getAssistantLink()} in ${remaining}s...`);
            }
        }, 1000);

        this.reconnectTimeout = setTimeout(() => {
            clearInterval(this.countdownInterval);
            this.connect();
        }, delay);
    }

    sendResize() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            const msg = new Uint8Array([
                0x00,
                (this.term.rows >> 8) & 0xFF, this.term.rows & 0xFF,
                (this.term.cols >> 8) & 0xFF, this.term.cols & 0xFF
            ]);
            this.ws.send(msg);
        }
    }

    connect() {
        this.debugLog('connect() called');
        if (this.reconnectTimeout) {
            clearTimeout(this.reconnectTimeout);
            this.reconnectTimeout = null;
        }
        if (this.countdownInterval) {
            clearInterval(this.countdownInterval);
            this.countdownInterval = null;
        }

        this.updateStatus('connecting', `Connecting to ${this.getAssistantLink()}...`);
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        if (timerEl) timerEl.textContent = '';

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        let url = protocol + '//' + window.location.host + '/ws/' + this.uuid + '?assistant=' + encodeURIComponent(this.assistant);
        // Forward name param from page URL to WebSocket URL (for session naming)
        const nameParam = new URLSearchParams(location.search).get('name');
        if (nameParam) {
            url += '&name=' + encodeURIComponent(nameParam);
        }
        // Forward parent param from page URL to WebSocket URL (for shell session workDir inheritance)
        const parentParam = new URLSearchParams(location.search).get('parent');
        if (parentParam) {
            url += '&parent=' + encodeURIComponent(parentParam);
        }

        this.debugLog('Creating WebSocket to: ' + url);
        console.log('[WS] Connecting to', url);
        try {
            this.ws = new WebSocket(url);
            this.debugLog('WebSocket created, readyState=' + this.ws.readyState);
        } catch (e) {
            this.debugLog('WebSocket constructor threw: ' + e.message);
            console.error('[WS] Failed to create WebSocket:', e);
            this.updateStatus('error', 'WebSocket creation failed: ' + e.message);
            setTimeout(() => this.scheduleReconnect(), 1000);
            return;
        }
        this.ws.binaryType = 'arraybuffer';

        // iOS Safari silently fails WebSocket connections to self-signed certs
        // Detect stuck CONNECTING state and show helpful error
        const connectTimeout = setTimeout(() => {
            if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
                this.debugLog('WebSocket stuck in CONNECTING state (iOS Safari self-signed cert issue)');
                console.error('[WS] Connection timeout - stuck in CONNECTING state');
                this.updateStatus('error', 'iOS Safari: WebSocket blocked (self-signed cert). Use Let\'s Encrypt or connect Mac Safari Web Inspector.');
                this.ws.close();
            }
        }, 5000);

        this.ws.onopen = () => {
            clearTimeout(connectTimeout);
            console.log('[WS] Connected to', url);
            this.reconnectAttempts = 0;
            this.updateStatus('connected', 'Connected');
            this.startUptimeTimer();
            this.sendResize();
            this.startHeartbeat();
        };

        this.ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                const data = new Uint8Array(event.data);
                // Check for chunk message (0x02 prefix)
                if (data.length >= 3 && data[0] === 0x02) {
                    this.handleChunk(data);
                } else {
                    this.onTerminalData(data);
                }
            } else if (typeof event.data === 'string') {
                try {
                    const msg = JSON.parse(event.data);
                    this.handleJSONMessage(msg);
                } catch (e) {
                    console.error('Invalid JSON from server:', e);
                }
            }
        };

        this.ws.onclose = (event) => {
            clearTimeout(connectTimeout);
            const reason = event.reason || `code ${event.code}`;
            console.log('[WS] Closed:', event.code, reason, 'wasClean:', event.wasClean);
            this.stopUptimeTimer();
            this.stopHeartbeat();

            // Don't reconnect if process has exited - let user review terminal output
            if (this.processExited) {
                console.log('[WS] Process exited, not reconnecting');
                return;
            }

            // Show close reason in status bar for debugging
            this.updateStatus('error', `Disconnected: ${reason}`);
            // Brief delay to show the error before scheduling reconnect
            setTimeout(() => this.scheduleReconnect(), 1000);
        };

        this.ws.onerror = (event) => {
            clearTimeout(connectTimeout);
            console.error('[WS] Error:', event);
            this.updateStatus('error', 'Connection error');
            this.stopUptimeTimer();
            this.stopHeartbeat();
            // onclose will be called after onerror, so reconnect is handled there
        };
    }

    // Terminal data received
    // Batches writes within a single animation frame to reduce flicker
    onTerminalData(data) {
        // Track output timing for idle detection
        const now = Date.now();
        const timeSinceLastOutput = this.lastOutputTime ? now - this.lastOutputTime : 0;
        this.lastOutputTime = now;

        // Reset idle timer
        if (this.outputIdleTimer) {
            clearTimeout(this.outputIdleTimer);
        }
        this.outputIdleTimer = setTimeout(() => {
            this.onOutputIdle();
        }, this.outputIdleThreshold);

        if (!this.pendingWrites) {
            this.pendingWrites = [];
            requestAnimationFrame(() => {
                // Combine all pending writes into one
                const total = this.pendingWrites.reduce((sum, arr) => sum + arr.length, 0);
                const combined = new Uint8Array(total);
                let offset = 0;
                for (const arr of this.pendingWrites) {
                    combined.set(arr, offset);
                    offset += arr.length;
                }
                this.term.write(combined);
                this.pendingWrites = null;
            });
        }
        this.pendingWrites.push(data);
    }

    // Called when no output received for outputIdleThreshold ms
    onOutputIdle() {
        const idleMs = Date.now() - this.lastOutputTime;
        this.debugLog(`Output idle for ${idleMs}ms - user input needed?`, 5000);
    }

    // Handle chunked snapshot message
    // Format: [0x02, chunkIndex, totalChunks, ...payload]
    handleChunk(data) {
        const chunkIndex = data[1];
        const totalChunks = data[2];
        const payload = data.slice(3);

        console.log(`CHUNK ${chunkIndex + 1}/${totalChunks} (${payload.length} bytes)`);

        // Initialize chunks array if this is the first chunk or a new sequence
        if (this.expectedChunks !== totalChunks) {
            this.chunks = new Array(totalChunks);
            this.expectedChunks = totalChunks;
        }

        // Store chunk payload
        this.chunks[chunkIndex] = payload;

        // Check if all chunks received
        const receivedCount = this.chunks.filter(c => c !== undefined).length;

        // Show chunk progress in status bar for debugging
        if (totalChunks > 1) {
            this.showStatusNotification(`Receiving snapshot: ${receivedCount}/${totalChunks}`, 2000);
        }

        if (receivedCount === totalChunks) {
            this.reassembleChunks();
        }
    }

    // Reassemble chunks and decompress
    async reassembleChunks() {
        // Combine all chunks
        const totalSize = this.chunks.reduce((sum, c) => sum + c.length, 0);
        const compressed = new Uint8Array(totalSize);
        let offset = 0;
        for (const chunk of this.chunks) {
            compressed.set(chunk, offset);
            offset += chunk.length;
        }

        console.log(`REASSEMBLED: ${this.chunks.length} chunks, ${compressed.length} bytes compressed`);

        // Reset chunk state
        this.chunks = [];
        this.expectedChunks = 0;

        // Decompress and write to terminal
        try {
            const decompressed = await this.decompressSnapshot(compressed);
            console.log(`DECOMPRESSED: ${compressed.length} -> ${decompressed.length} bytes`);
            this.showStatusNotification(`Snapshot loaded: ${decompressed.length} bytes`, 2000);
            this.onTerminalData(decompressed);
            // Scroll to bottom after snapshot - the \x1b[2J clear screen can reset viewport
            requestAnimationFrame(() => this.term.scrollToBottom());
        } catch (e) {
            console.error('Failed to decompress snapshot:', e);
            this.showStatusNotification(`Decompress failed: ${e.message}`, 5000);
            // Try writing compressed data directly (fallback for uncompressed data)
            this.onTerminalData(compressed);
        }
    }

    // Decompress gzip data using DecompressionStream API
    async decompressSnapshot(compressed) {
        // Check for gzip magic bytes (0x1f 0x8b)
        if (compressed.length < 2 || compressed[0] !== 0x1f || compressed[1] !== 0x8b) {
            // Not gzip compressed, return as-is
            return compressed;
        }

        // Use DecompressionStream API (available in modern browsers)
        const ds = new DecompressionStream('gzip');
        const writer = ds.writable.getWriter();
        const reader = ds.readable.getReader();

        // Write compressed data
        writer.write(compressed);
        writer.close();

        // Read decompressed data
        const chunks = [];
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            chunks.push(value);
        }

        // Combine chunks
        const totalLength = chunks.reduce((sum, c) => sum + c.length, 0);
        const result = new Uint8Array(totalLength);
        let pos = 0;
        for (const chunk of chunks) {
            result.set(chunk, pos);
            pos += chunk.length;
        }

        return result;
    }

    sendJSON(obj) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(obj));
        }
    }

    handleJSONMessage(msg) {
        switch (msg.type) {
            case 'pong':
                // Heartbeat response
                if (msg.data && msg.data.ts) {
                    const latency = Date.now() - msg.data.ts;
                    console.log(`Heartbeat pong: ${latency}ms`);
                }
                break;
            case 'status':
                // Session status update
                this.viewers = msg.viewers || 0;
                this.ptyCols = msg.cols || 0;
                this.ptyRows = msg.rows || 0;
                if (msg.assistant) {
                    this.assistantName = msg.assistant;
                }
                this.sessionName = msg.sessionName || '';
                this.uuidShort = msg.uuidShort || '';
                const prevWorkDir = this.workDir;
                this.workDir = msg.workDir || '';
                if (this.workDir !== prevWorkDir) {
                    this.renderServiceLinks();
                }
                // YOLO mode state
                this.yoloMode = msg.yoloMode || false;
                this.yoloSupported = msg.yoloSupported || false;
                this.updateStatusInfo();
                // Update settings panel toggle if open
                this.updateSettingsYoloToggle();
                break;
            case 'chat':
                // Incoming chat message
                if (msg.userName && msg.text) {
                    const isOwn = msg.userName === this.currentUserName;
                    this.addChatMessage(msg.userName, msg.text, isOwn);
                }
                break;
            case 'file_upload':
                // File upload response
                if (msg.success) {
                    this.showStatusNotification(`Saved: ${msg.filename}`, 3000);
                } else {
                    this.showStatusNotification(`Upload failed: ${msg.error || 'Unknown error'}`, 5000);
                }
                break;
            case 'exit':
                // Process exited - prompt user with worktree options or return home
                this.handleProcessExit(msg.exitCode, msg.worktree);
                break;
            default:
                console.log('Unknown JSON message:', msg);
        }
    }

    handleProcessExit(exitCode, worktree) {
        // Mark process as exited to prevent WebSocket reconnection
        this.processExited = true;

        // Update status bar to show exited state
        this.updateStatus('', 'Session ended');

        // Stop uptime timer
        this.stopUptimeTimer();

        // If embedded in iframe (panel view), notify parent to close the pane
        if (window.self !== window.top) {
            window.parent.postMessage({ type: 'swe-swe-session-ended', exitCode }, '*');
            return;
        }

        // Show standard confirmation dialog (same for worktree and non-worktree sessions)
        const message = exitCode === 0
            ? 'The session has ended successfully.\n\nReturn to the home page to start a new session?'
            : `The session ended with exit code ${exitCode}.\n\nReturn to the home page to start a new session?`;

        if (confirm(message)) {
            window.location.href = '/' + this.getDebugQueryString();
        }
    }

    updateStatusInfo() {
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const statusText = this.querySelector('.terminal-ui__status-text');
        const dimsEl = this.querySelector('.terminal-ui__status-dims');

        if (!statusText) return;

        // Toggle multiuser class based on viewer count
        statusBar.classList.toggle('multiuser', this.viewers > 1);
        // Toggle yolo class based on YOLO mode
        statusBar.classList.toggle('yolo', this.yoloMode);

        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            // Build "Connected/YOLO as {name} with {agent}" message with separate clickable parts
            const userName = this.currentUserName;
            const debugQS = this.getDebugQueryString();

            // Show "YOLO" or "Connected" based on mode, make clickable if YOLO is supported
            const statusWord = this.yoloMode ? 'YOLO' : 'Connected';
            let html;
            if (this.yoloSupported) {
                html = `<span class="terminal-ui__status-link terminal-ui__status-yolo-toggle">${statusWord}</span> as <span class="terminal-ui__status-link terminal-ui__status-name">${userName}</span>`;
            } else {
                html = `${statusWord} as <span class="terminal-ui__status-link terminal-ui__status-name">${userName}</span>`;
            }
            if (this.assistantName) {
                html += ` with <a href="/${debugQS}" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${this.assistantName}</a>`;
            }

            // Add viewer suffix if more than 1 viewer
            if (this.viewers > 1) {
                html += ` and <span class="terminal-ui__status-link terminal-ui__status-others">${this.viewers - 1} others</span>`;
            }

            // Add session name display
            const sessionDisplay = this.sessionName || `Unnamed session ${this.uuidShort}`;
            html += ` on <span class="terminal-ui__status-link terminal-ui__status-session">${sessionDisplay}</span>`;

            statusText.innerHTML = html;

            // Display dimensions separately on the right
            if (dimsEl) {
                dimsEl.textContent = `${this.ptyCols}×${this.ptyRows}`;
            }
        } else {
            // For connecting/error/reconnecting states, updateStatus() handles the text
            if (dimsEl) {
                dimsEl.textContent = '';
            }
        }
    }

    generateRandomUsername() {
        const randomNum = Math.floor(Math.random() * 10000);
        return `User${randomNum}`;
    }

    validateUsername(name) {
        name = name.trim();

        if (name.length === 0) {
            return { valid: false, error: 'Name cannot be empty' };
        }

        if (name.length > 16) {
            return { valid: false, error: 'Name must be 16 characters or less' };
        }

        if (!/^[a-zA-Z0-9 ]+$/.test(name)) {
            return { valid: false, error: 'Name can only contain letters, numbers, and spaces' };
        }

        return { valid: true, name: name.trim() };
    }

    getUserName() {
        if (this.currentUserName) {
            return this.currentUserName;
        }

        // Check localStorage
        let storedName = localStorage.getItem('swe-swe-username');
        if (storedName) {
            this.currentUserName = storedName;
            this.updateUsernameDisplay();
            return storedName;
        }

        // Prompt for name
        while (true) {
            const name = window.prompt('Your name');

            // User clicked Cancel
            if (name === null) {
                return null;
            }

            const validation = this.validateUsername(name);
            if (validation.valid) {
                this.setUsername(validation.name);
                return validation.name;
            } else {
                alert('Invalid name: ' + validation.error + '\nPlease try again.');
            }
        }
    }

    updateUsernameDisplay() {
        // Update the status display after username is set or changed
        this.updateStatusInfo();
    }

    promptRenameUsername() {
        while (true) {
            const newName = window.prompt('Enter new name:', this.currentUserName);

            // User clicked Cancel
            if (newName === null) {
                return;
            }

            const validation = this.validateUsername(newName);
            if (validation.valid) {
                this.setUsername(validation.name);
                return;
            } else {
                alert('Invalid name: ' + validation.error + '\nPlease try again.');
            }
        }
    }

    validateSessionName(name) {
        name = name.trim();

        // Empty name is valid (clears the session name)
        if (name.length === 0) {
            return { valid: true, name: '' };
        }

        if (name.length > 32) {
            return { valid: false, error: 'Name must be 32 characters or less' };
        }

        if (!/^[a-zA-Z0-9 \-_]+$/.test(name)) {
            return { valid: false, error: 'Name can only contain letters, numbers, spaces, hyphens, and underscores' };
        }

        return { valid: true, name: name };
    }

    promptRenameSession() {
        while (true) {
            const newName = window.prompt('Enter session name (max 32 chars):', this.sessionName);

            // User clicked Cancel
            if (newName === null) {
                return;
            }

            const validation = this.validateSessionName(newName);
            if (validation.valid) {
                // Send rename request to server
                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                    this.ws.send(JSON.stringify({
                        type: 'rename_session',
                        name: validation.name
                    }));
                }
                return;
            } else {
                alert('Invalid name: ' + validation.error + '\nPlease try again.');
            }
        }
    }

    toggleYoloMode() {
        if (!this.yoloSupported) {
            return;
        }

        const action = this.yoloMode ? 'Disable' : 'Enable';
        if (confirm(`${action} YOLO mode? The agent will restart.`)) {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({ type: 'toggle_yolo' }));
            }
        }
    }

    toggleChatInput() {
        if (this.chatInputOpen) {
            this.closeChatInput();
        } else {
            this.openChatInput();
        }
    }

    openChatInput() {
        const overlay = this.querySelector('.terminal-ui__chat-input-overlay');
        const input = this.querySelector('.terminal-ui__chat-input');
        if (!overlay) return;

        this.chatInputOpen = true;
        overlay.classList.add('active');
        input.focus();
        input.value = '';
        this.clearChatNotification();
    }

    closeChatInput() {
        const overlay = this.querySelector('.terminal-ui__chat-input-overlay');
        if (!overlay) return;

        this.chatInputOpen = false;
        overlay.classList.remove('active');
    }

    // Settings Panel Methods
    openSettingsPanel() {
        const panel = this.querySelector('.settings-panel');
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        if (!panel) return;

        // Populate inputs with current values before showing
        this.populateSettingsPanel();

        panel.removeAttribute('hidden');
        statusBar.setAttribute('aria-expanded', 'true');

        // Focus first input
        const firstInput = panel.querySelector('input');
        if (firstInput) {
            firstInput.focus();
        }

        // Store the element that opened the panel to restore focus on close
        this._settingsPanelOpener = document.activeElement;
    }

    closeSettingsPanel() {
        const panel = this.querySelector('.settings-panel');
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        if (!panel) return;

        panel.setAttribute('hidden', '');
        statusBar.setAttribute('aria-expanded', 'false');

        // Restore focus to the element that opened the panel
        if (this._settingsPanelOpener && typeof this._settingsPanelOpener.focus === 'function') {
            this._settingsPanelOpener.focus();
        } else {
            this.term.focus();
        }
    }

    isSettingsPanelOpen() {
        const panel = this.querySelector('.settings-panel');
        return panel && !panel.hasAttribute('hidden');
    }

    setupSettingsPanel() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;

        const backdrop = panel.querySelector('.settings-panel__backdrop');
        const closeBtn = panel.querySelector('.settings-panel__close');

        // Close on backdrop click
        if (backdrop) {
            backdrop.addEventListener('click', () => this.closeSettingsPanel());
        }

        // Close on × button click
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.closeSettingsPanel());
        }

        // Close on Escape key
        panel.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                e.stopPropagation();
                this.closeSettingsPanel();
            }
        });

        // Username input
        const usernameInput = panel.querySelector('#settings-username');
        if (usernameInput) {
            usernameInput.addEventListener('change', (e) => {
                const validation = this.validateUsername(e.target.value);
                if (validation.valid) {
                    this.setUsername(validation.name);
                } else {
                    // Restore previous value
                    e.target.value = this.currentUserName || '';
                }
            });
        }

        // Session name input
        const sessionInput = panel.querySelector('#settings-session');
        if (sessionInput) {
            sessionInput.addEventListener('change', (e) => {
                const validation = this.validateSessionName(e.target.value);
                if (validation.valid) {
                    this.setSessionName(validation.name);
                } else {
                    // Restore previous value
                    e.target.value = this.sessionName || '';
                }
            });
        }

        // Color picker
        const colorPicker = panel.querySelector('.settings-panel__color-picker');
        const colorInput = panel.querySelector('.settings-panel__color-input');
        const swatches = panel.querySelectorAll('.settings-panel__swatch');

        if (colorPicker) {
            colorPicker.addEventListener('input', (e) => {
                this.setStatusBarColor(e.target.value);
                if (colorInput) colorInput.value = e.target.value;
            });
        }

        if (colorInput) {
            colorInput.addEventListener('change', (e) => {
                const color = e.target.value.trim();
                if (color) {
                    this.setStatusBarColor(color);
                    if (colorPicker) colorPicker.value = this.normalizeColorForPicker(color);
                }
            });
        }

        swatches.forEach(swatch => {
            swatch.addEventListener('click', () => {
                const color = swatch.dataset.color;
                this.setStatusBarColor(color);
                if (colorPicker) colorPicker.value = color;
                if (colorInput) colorInput.value = color;
                this.updateActiveSwatches(color);
            });
        });

        // YOLO toggle
        const yoloToggle = panel.querySelector('#settings-yolo-toggle');
        if (yoloToggle) {
            const handleYoloToggle = () => {
                this.toggleYoloMode();
            };
            yoloToggle.addEventListener('click', handleYoloToggle);
            yoloToggle.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    handleYoloToggle();
                }
            });
        }

        // Restore saved color from localStorage
        this.restoreStatusBarColor();
    }

    // Set username helper
    setUsername(name) {
        this.currentUserName = name;
        localStorage.setItem('swe-swe-username', name);
        this.updateUsernameDisplay();
    }

    // Set session name helper (sends to server)
    setSessionName(name) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({
                type: 'rename_session',
                name: name
            }));
        }
    }

    // Set status bar color with live preview and persistence
    setStatusBarColor(color) {
        document.documentElement.style.setProperty('--status-bar-color', color);
        try {
            localStorage.setItem('settings:statusBarColor', color);
        } catch (e) {
            console.warn('[TerminalUI] Could not save color:', e);
        }
        this.updateActiveSwatches(color);
    }

    // Restore status bar color from localStorage
    restoreStatusBarColor() {
        try {
            const savedColor = localStorage.getItem('settings:statusBarColor');
            if (savedColor) {
                document.documentElement.style.setProperty('--status-bar-color', savedColor);
            }
        } catch (e) {
            console.warn('[TerminalUI] Could not restore color:', e);
        }
    }

    // Normalize a CSS color to hex for the color picker input
    normalizeColorForPicker(color) {
        // Try to convert to hex using a temporary element
        const temp = document.createElement('div');
        temp.style.color = color;
        document.body.appendChild(temp);
        const computed = getComputedStyle(temp).color;
        document.body.removeChild(temp);

        // Parse rgb(r, g, b) format
        const match = computed.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/);
        if (match) {
            const r = parseInt(match[1]).toString(16).padStart(2, '0');
            const g = parseInt(match[2]).toString(16).padStart(2, '0');
            const b = parseInt(match[3]).toString(16).padStart(2, '0');
            return `#${r}${g}${b}`;
        }

        return color;
    }

    // Update active swatch highlighting
    updateActiveSwatches(activeColor) {
        const swatches = this.querySelectorAll('.settings-panel__swatch');
        swatches.forEach(swatch => {
            swatch.classList.toggle('active', swatch.dataset.color === activeColor);
        });
    }

    // Update YOLO toggle in settings panel (called on status update)
    updateSettingsYoloToggle() {
        const yoloField = this.querySelector('.settings-panel__field--yolo');
        const yoloToggle = this.querySelector('#settings-yolo-toggle');
        if (yoloField && yoloToggle) {
            yoloField.classList.toggle('supported', this.yoloSupported);
            yoloToggle.classList.toggle('active', this.yoloMode);
            yoloToggle.setAttribute('aria-checked', this.yoloMode ? 'true' : 'false');
        }
    }

    // Populate settings inputs when panel opens
    populateSettingsPanel() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;

        // Username
        const usernameInput = panel.querySelector('#settings-username');
        if (usernameInput) {
            usernameInput.value = this.currentUserName || '';
        }

        // Session name
        const sessionInput = panel.querySelector('#settings-session');
        if (sessionInput) {
            sessionInput.value = this.sessionName || '';
        }

        // Color - read from CSS variable first (set by server), then localStorage
        const colorPicker = panel.querySelector('.settings-panel__color-picker');
        const colorInput = panel.querySelector('.settings-panel__color-input');
        let currentColor = '#007acc';
        try {
            // First try to get the actual CSS variable (set by server template)
            const cssColor = getComputedStyle(document.documentElement).getPropertyValue('--status-bar-color').trim();
            if (cssColor) {
                currentColor = cssColor;
            } else {
                // Fall back to localStorage
                currentColor = localStorage.getItem('settings:statusBarColor') || '#007acc';
            }
        } catch (e) {}

        if (colorPicker) {
            colorPicker.value = this.normalizeColorForPicker(currentColor);
        }
        if (colorInput) {
            colorInput.value = currentColor;
        }
        this.updateActiveSwatches(currentColor);

        // Update navigation links with dynamic URLs and add click handlers
        const baseUrl = this.getBaseUrl();
        const vscodeLink = panel.querySelector('.settings-panel__nav-vscode');
        if (vscodeLink) {
            vscodeLink.href = this.getVSCodeUrl();
        }

        // Update nav tab links with dynamic URLs and add click handlers for iframe toggle
        const navTabs = panel.querySelectorAll('.settings-panel__nav-tab');
        navTabs.forEach(link => {
            const tab = link.dataset.tab;
            // Set dynamic hrefs
            if (tab === 'shell') {
                // Hide shell link if already in a shell session
                if (this.assistant === 'shell') {
                    link.style.display = 'none';
                    return;
                }
                const shellUUID = this.deriveShellUUID(this.uuid);
                const debugQS = this.debugMode ? '&debug=1' : '';
                link.href = `${baseUrl}/session/${shellUUID}?assistant=shell&parent=${encodeURIComponent(this.uuid)}${debugQS}`;
            } else if (tab === 'preview') {
                link.href = this.previewBaseUrl || `${window.location.protocol}//${window.location.hostname}:1${window.location.port || '80'}`;
            } else if (tab === 'browser') {
                link.href = `${baseUrl}/chrome/`;
            }
            // Add click handler for iframe toggle behavior
            link.addEventListener('click', (e) => {
                this.handleTabClick(e, tab, link.href);
                // Close settings panel after clicking a tab
                if (this.canShowSplitPane() && this.isRegularClick(e)) {
                    this.closeSettingsPanel();
                }
            });
        });

        // YOLO toggle
        const yoloField = panel.querySelector('.settings-panel__field--yolo');
        const yoloToggle = panel.querySelector('#settings-yolo-toggle');
        if (yoloField && yoloToggle) {
            // Show/hide based on agent support
            yoloField.classList.toggle('supported', this.yoloSupported);
            // Set toggle state
            yoloToggle.classList.toggle('active', this.yoloMode);
            yoloToggle.setAttribute('aria-checked', this.yoloMode ? 'true' : 'false');
        }
    }

    showPasteOverlay() {
        const overlay = this.querySelector('.terminal-ui__paste-overlay');
        const textarea = this.querySelector('.terminal-ui__paste-textarea');
        if (!overlay) return;

        overlay.classList.add('active');
        textarea.value = '';
        textarea.focus();
    }

    hidePasteOverlay() {
        const overlay = this.querySelector('.terminal-ui__paste-overlay');
        if (!overlay) return;

        overlay.classList.remove('active');
        this.term.focus();
    }

    sendPasteContent() {
        const textarea = this.querySelector('.terminal-ui__paste-textarea');
        const text = textarea ? textarea.value : '';

        if (text && this.ws && this.ws.readyState === WebSocket.OPEN) {
            const encoder = new TextEncoder();
            this.ws.send(encoder.encode(text));
        }

        this.hidePasteOverlay();
    }

    addChatMessage(userName, text, isOwn = false) {
        const overlay = this.querySelector('.terminal-ui__chat-overlay');
        if (!overlay) return;

        const msgEl = document.createElement('div');
        msgEl.className = `terminal-ui__chat-message ${isOwn ? 'own' : 'other'}`;
        msgEl.innerHTML = `<span class="terminal-ui__chat-message-username">${this.escapeHtml(userName)}:</span> ${this.escapeHtml(text)}`;

        // Click message to open chat input
        msgEl.addEventListener('click', () => {
            this.openChatInput();
        });

        overlay.appendChild(msgEl);
        this.chatMessages.push({ userName, text, isOwn, timestamp: new Date() });

        // Auto-fade out after 5 seconds
        const timeout = setTimeout(() => {
            msgEl.classList.add('fading');
            setTimeout(() => msgEl.remove(), 400);
        }, 5000);

        this.chatMessageTimeouts.push(timeout);

        // If message is from other user and chat input not open, show notification
        if (!isOwn && !this.chatInputOpen) {
            this.unreadChatCount++;
            this.showChatNotification(this.unreadChatCount);
        }
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    showStatusNotification(message, durationMs = 3000) {
        const overlay = this.querySelector('.terminal-ui__chat-overlay');
        if (!overlay) return;

        const msgEl = document.createElement('div');
        msgEl.className = 'terminal-ui__chat-message system';
        msgEl.textContent = message;

        overlay.appendChild(msgEl);

        // Auto-fade after duration
        setTimeout(() => {
            msgEl.classList.add('fading');
            setTimeout(() => msgEl.remove(), 400);
        }, durationMs);
    }

    debugLog(message, durationMs = 3000) {
        if (!this.debugMode) return;
        this.showStatusNotification(`[DEBUG] ${message}`, durationMs);
    }

    sendChatMessage() {
        const input = this.querySelector('.terminal-ui__chat-input');
        if (!input) return;

        const text = input.value.trim();
        if (!text) return;

        // If no username, prompt for one
        if (!this.currentUserName) {
            const userName = this.getUserName();
            if (!userName) return;
        }

        // Send to server
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.sendJSON({
                type: 'chat',
                userName: this.currentUserName,
                text: text
            });
            // Clear input for next message
            input.value = '';
            input.focus();
        }
    }

    showChatNotification(count) {
        const badge = this.querySelector('.terminal-ui__status-info').parentElement.querySelector('.terminal-ui__chat-notification');
        if (!badge) {
            // Create badge if it doesn't exist
            const statusRight = this.querySelector('.terminal-ui__status-right');
            const newBadge = document.createElement('span');
            newBadge.className = 'terminal-ui__chat-notification';
            newBadge.textContent = count;
            statusRight.appendChild(newBadge);
        } else {
            badge.textContent = count;
            badge.style.display = 'block';
        }
    }

    clearChatNotification() {
        const badge = this.querySelector('.terminal-ui__chat-notification');
        if (badge) {
            badge.style.display = 'none';
        }
        this.unreadChatCount = 0;
    }

    startHeartbeat() {
        this.stopHeartbeat();
        this.heartbeatInterval = setInterval(() => {
            this.sendJSON({type: 'ping', data: {ts: Date.now()}});
        }, 30000); // every 30 seconds
    }

    stopHeartbeat() {
        if (this.heartbeatInterval) {
            clearInterval(this.heartbeatInterval);
            this.heartbeatInterval = null;
        }
    }

    // Mobile keyboard methods
    sendKey(code) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            const encoder = new TextEncoder();
            this.ws.send(encoder.encode(code));
        }
    }

    toggleCtrl() {
        this.ctrlActive = !this.ctrlActive;
        const btn = this.querySelector('[data-toggle="ctrl"]');
        const row = this.querySelector('.mobile-keyboard__ctrl');
        btn.classList.toggle('active', this.ctrlActive);
        row.classList.toggle('visible', this.ctrlActive);
        // Re-fit terminal after keyboard row changes
        requestAnimationFrame(() => {
            this.fitAddon.fit();
            this.sendResize();
            this.term.scrollToBottom();
        });
    }

    toggleNav() {
        this.navActive = !this.navActive;
        const btn = this.querySelector('[data-toggle="nav"]');
        const row = this.querySelector('.mobile-keyboard__nav');
        btn.classList.toggle('active', this.navActive);
        row.classList.toggle('visible', this.navActive);
        // Re-fit terminal after keyboard row changes
        requestAnimationFrame(() => {
            this.fitAddon.fit();
            this.sendResize();
            this.term.scrollToBottom();
        });
    }

    setupKeyboardVisibility() {
        const keyboard = this.querySelector('.mobile-keyboard');
        if (!keyboard) return;

        const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
        const isNarrow = window.matchMedia('(max-width: 768px)').matches;
        const forceShow = new URLSearchParams(location.search).get('keyboard') === 'show';

        if ((hasTouch && isNarrow) || forceShow) {
            keyboard.classList.add('visible');
        }
    }

    // === Touch Scroll Proxy ===
    // Provides native iOS momentum scrolling by overlaying a scrollable div
    // that syncs scroll position to xterm.js

    setupTouchScrollProxy() {
        this.scrollProxy = this.querySelector('.touch-scroll-proxy');
        this.scrollSpacer = this.querySelector('.scroll-spacer');
        this.terminalEl = this.querySelector('.terminal-ui__terminal');

        if (!this.scrollProxy || !this.scrollSpacer || !this.terminalEl) return;

        // State for preventing sync loops
        this.syncingFromProxy = false;
        this.syncingFromTerm = false;

        // Approximate line height (xterm default ~17px)
        this.scrollLineHeight = 17;

        // Initial spacer height
        this.updateSpacerHeight();

        // Keep spacer in sync with buffer
        this.term.onWriteParsed(() => {
            this.updateSpacerHeight();
            // Auto-scroll to bottom when new content (if already at bottom)
            const maxLine = this.term.buffer.active.length - this.term.rows;
            const atBottom = this.term.buffer.active.viewportY >= maxLine - 1;
            if (atBottom) {
                this.syncTermToProxy();
            }
        });

        // Proxy scroll -> xterm scroll
        this.scrollProxy.addEventListener('scroll', () => this.syncProxyToTerm(), { passive: true });

        // xterm scroll -> proxy scroll (for programmatic scrolls)
        this.term.onScroll(() => this.syncTermToProxy());

        // Tap on proxy to focus terminal (since xterm has pointer-events: none on touch)
        this.scrollProxy.addEventListener('click', () => this.term.focus());
    }

    updateSpacerHeight() {
        if (!this.scrollSpacer || !this.scrollProxy) return;

        const bufferLines = this.term.buffer.active.length;
        // Spacer must EXCEED viewport height to be scrollable
        const height = Math.max(
            bufferLines * this.scrollLineHeight,
            this.scrollProxy.clientHeight + 100
        );
        this.scrollSpacer.style.height = `${height}px`;
    }

    syncProxyToTerm() {
        if (this.syncingFromTerm) return;
        this.syncingFromProxy = true;

        const maxScroll = this.scrollProxy.scrollHeight - this.scrollProxy.clientHeight;
        const scrollTop = this.scrollProxy.scrollTop;

        // Rubber band effect for overscroll
        if (scrollTop < 0) {
            // Top overscroll - push terminal down
            const rubberBand = Math.min(-scrollTop * 0.5, 100);
            this.terminalEl.style.transform = `translateY(${rubberBand}px)`;
        } else if (scrollTop > maxScroll) {
            // Bottom overscroll - push terminal up
            const rubberBand = Math.max((maxScroll - scrollTop) * 0.5, -100);
            this.terminalEl.style.transform = `translateY(${rubberBand}px)`;
        } else {
            // Normal scroll - reset transform
            this.terminalEl.style.transform = 'translateY(0)';
        }

        // Sync scroll position to xterm
        if (maxScroll > 0) {
            const scrollRatio = Math.max(0, Math.min(1, scrollTop / maxScroll));
            const maxLine = this.term.buffer.active.length - this.term.rows;
            const targetLine = Math.round(scrollRatio * maxLine);
            this.term.scrollToLine(targetLine);
        }

        requestAnimationFrame(() => { this.syncingFromProxy = false; });
    }

    syncTermToProxy() {
        if (this.syncingFromProxy) return;
        this.syncingFromTerm = true;

        const maxLine = this.term.buffer.active.length - this.term.rows;
        if (maxLine > 0) {
            const scrollRatio = this.term.buffer.active.viewportY / maxLine;
            const maxScroll = this.scrollProxy.scrollHeight - this.scrollProxy.clientHeight;
            this.scrollProxy.scrollTop = scrollRatio * maxScroll;
        }

        requestAnimationFrame(() => { this.syncingFromTerm = false; });
    }

    // === visualViewport Keyboard Handling ===
    // Detects virtual keyboard and adjusts layout accordingly

    setupViewportListeners() {
        if (!window.visualViewport) return;

        this._viewportHandler = () => this.updateViewport();
        window.visualViewport.addEventListener('resize', this._viewportHandler);
        window.visualViewport.addEventListener('scroll', this._viewportHandler);

        // Also handle input focus/blur to prevent iOS scroll weirdness
        const mobileInput = this.querySelector('.mobile-keyboard__text');
        if (mobileInput) {
            mobileInput.addEventListener('focus', () => {
                setTimeout(() => {
                    window.scrollTo(0, 0);
                    this.updateViewport();
                }, 100);
            });
            mobileInput.addEventListener('blur', () => {
                setTimeout(() => {
                    window.scrollTo(0, 0);
                    this.updateViewport();
                }, 100);
            });
        }
    }

    updateViewport() {
        const vv = window.visualViewport;
        if (!vv) return;

        // Calculate keyboard height using original window height as reference
        // (interactive-widget=resizes-content causes window.innerHeight to shrink)
        const keyboardHeight = Math.max(0, this.originalWindowHeight - vv.height);
        const keyboardVisible = keyboardHeight > 50; // threshold to filter noise

        // Only update layout if significant change (>20px)
        if (Math.abs(keyboardHeight - this.lastKeyboardHeight) <= 20) {
            return;
        }
        this.lastKeyboardHeight = keyboardHeight;

        const mobileKeyboard = this.querySelector('.mobile-keyboard');
        const terminalContainer = this.querySelector('.terminal-ui');

        if (keyboardVisible) {
            // Keyboard is showing - adjust layout
            if (mobileKeyboard) {
                // Move mobile keyboard above virtual keyboard
                mobileKeyboard.style.marginBottom = `${keyboardHeight}px`;
            }
        } else {
            // Keyboard hidden - reset layout
            if (mobileKeyboard) {
                mobileKeyboard.style.marginBottom = '0';
            }
        }

        // Refit terminal immediately (no setTimeout - use rAF)
        requestAnimationFrame(() => {
            this.fitAddon.fit();
            this.sendResize();
            this.term.scrollToBottom();
            this.updateSpacerHeight();
        });
    }

    setupMobileKeyboard() {
        // Determine if keyboard should be visible
        this.setupKeyboardVisibility();

        const KEY_CODES = {
            'Escape': '\x1b',
            'Tab': '\t',
            'ShiftTab': '\x1b[Z',
            'ArrowLeft': '\x1b[D',
            'ArrowRight': '\x1b[C',
            'ArrowUp': '\x1b[A',
            'ArrowDown': '\x1b[B',
            'AltLeft': '\x1bb',   // Option+Left = backward-word
            'AltRight': '\x1bf'   // Option+Right = forward-word
        };

        const CTRL_CODES = {
            'a': '\x01',
            'c': '\x03',
            'd': '\x04',
            'e': '\x05',
            'k': '\x0B',
            'w': '\x17'
        };

        // Main row key buttons (Esc, Tab, ⇧Tab)
        this.querySelectorAll('.mobile-keyboard [data-key]').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                const key = btn.dataset.key;
                if (KEY_CODES[key]) {
                    this.sendKey(KEY_CODES[key]);
                }
                this.term.focus();
            });
        });

        // Toggle buttons (Ctrl, Nav)
        this.querySelectorAll('.mobile-keyboard [data-toggle]').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                if (btn.dataset.toggle === 'ctrl') {
                    this.toggleCtrl();
                } else if (btn.dataset.toggle === 'nav') {
                    this.toggleNav();
                }
            });
        });

        // Ctrl row buttons (A, C, D, E, K, W)
        this.querySelectorAll('.mobile-keyboard [data-ctrl]').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                const key = btn.dataset.ctrl.toLowerCase();
                if (CTRL_CODES[key]) {
                    this.sendKey(CTRL_CODES[key]);
                }
                this.term.focus();
            });
        });

        // Input bar
        const textInput = this.querySelector('.mobile-keyboard__text');
        const sendBtn = this.querySelector('.mobile-keyboard__send');

        // Update button text based on input value
        textInput.addEventListener('input', () => {
            sendBtn.textContent = textInput.value ? 'Send' : 'Enter';
        });

        // Send button click
        sendBtn.addEventListener('click', (e) => {
            e.preventDefault();
            const text = textInput.value;
            if (text) {
                // Send text, then Enter with delay to ensure text is processed first
                this.sendKey(text);
                textInput.value = '';
                sendBtn.textContent = 'Enter';
                setTimeout(() => {
                    this.sendKey('\r');
                    this.term.focus();
                }, 300);
            } else {
                // Send just Enter
                this.sendKey('\r');
                this.term.focus();
            }
        });

        // File attachment button
        const attachBtn = this.querySelector('.mobile-keyboard__attach');
        const fileInput = this.querySelector('.mobile-keyboard__file-input');

        attachBtn.addEventListener('click', (e) => {
            e.preventDefault();
            fileInput.click();
        });

        fileInput.addEventListener('change', () => {
            const wasEmpty = this.uploadQueue.length === 0;
            for (const file of fileInput.files) {
                this.addFileToQueue(file);
            }
            if (wasEmpty && this.uploadQueue.length > 0) {
                this.processUploadQueue();
            }
            fileInput.value = '';
        });

        // Enter key allows newlines in textarea (no auto-submit)
        // User must tap Send/Enter button to submit
    }

    setupEventListeners() {
        // Terminal data handler - send as binary to distinguish from JSON control messages
        this.term.onData(data => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                const encoder = new TextEncoder();
                this.ws.send(encoder.encode(data));
            }
        });

        // Window resize
        this._resizeHandler = () => {
            this.fitAddon.fit();
            this.sendResize();
        };
        window.addEventListener('resize', this._resizeHandler);

        // Mobile keyboard setup
        this.setupMobileKeyboard();

        // Touch scroll proxy for iOS momentum scrolling
        this.setupTouchScrollProxy();

        // visualViewport keyboard handling for iOS
        this.setupViewportListeners();

        // Terminal click to focus
        this.querySelector('.terminal-ui__terminal').addEventListener('click', () => {
            this.term.focus();
        });

        // Settings panel setup
        this.setupSettingsPanel();

        // Listen for session-ended messages from iframe to auto-close pane
        window.addEventListener('message', (e) => {
            if (e.data && e.data.type === 'swe-swe-session-ended') {
                this.closeIframePane();
            }
        });

        // Status bar click: reconnect when disconnected, open settings when connected
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        statusBar.addEventListener('click', (e) => {
            // Don't trigger if clicking on interactive child elements
            if (e.target.tagName === 'A' || e.target.tagName === 'BUTTON') {
                return;
            }

            if (statusBar.classList.contains('connecting') || statusBar.classList.contains('error') || statusBar.classList.contains('reconnecting')) {
                // Close existing connection attempt if any
                if (this.ws) {
                    this.ws.close();
                    this.ws = null;
                }
                // Reset backoff on manual retry
                this.reconnectAttempts = 0;
                this.connect();
            } else if (statusBar.classList.contains('connected')) {
                // When connected, open settings panel
                this.openSettingsPanel();
            }
        });

        // Status text (left side) click handler for connected state
        // Delegate to handle clicks on separate hyperlinks
        const statusText = this.querySelector('.terminal-ui__status-text');
        statusText.addEventListener('click', (e) => {
            // If clicking on an anchor, let the link work but don't open settings
            if (e.target.tagName === 'A') {
                e.stopPropagation();
                return;
            }

            // Only handle specific interactive elements when WebSocket is connected
            if (!(this.ws && this.ws.readyState === WebSocket.OPEN)) {
                // Let click bubble to status bar handler for reconnect
                return;
            }

            // Check if clicked on name link - stop propagation and handle
            if (e.target.classList.contains('terminal-ui__status-name')) {
                e.stopPropagation();
                if (!this.currentUserName) {
                    this.getUserName();
                } else {
                    this.promptRenameUsername();
                }
            }
            // Check if clicked on "others" link
            else if (e.target.classList.contains('terminal-ui__status-others')) {
                e.stopPropagation();
                this.toggleChatInput();
            }
            // Check if clicked on session name link
            else if (e.target.classList.contains('terminal-ui__status-session')) {
                e.stopPropagation();
                this.promptRenameSession();
            }
            // Check if clicked on YOLO toggle
            else if (e.target.classList.contains('terminal-ui__status-yolo-toggle')) {
                e.stopPropagation();
                this.toggleYoloMode();
            }
            // Otherwise let click bubble to status bar handler to open settings panel
        });

        // Chat input handlers
        const chatInput = this.querySelector('.terminal-ui__chat-input');
        const sendBtn = this.querySelector('.terminal-ui__chat-send-btn');
        const cancelBtn = this.querySelector('.terminal-ui__chat-cancel-btn');

        if (chatInput) {
            chatInput.addEventListener('keypress', (e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    this.sendChatMessage();
                }
            });

            chatInput.addEventListener('keydown', (e) => {
                if (e.key === 'Escape') {
                    this.closeChatInput();
                }
            });
        }

        if (sendBtn) {
            sendBtn.addEventListener('click', () => {
                this.sendChatMessage();
            });
        }

        if (cancelBtn) {
            cancelBtn.addEventListener('click', () => {
                this.closeChatInput();
            });
        }

        // Paste overlay handlers
        const pasteTextarea = this.querySelector('.terminal-ui__paste-textarea');
        const pasteSendBtn = this.querySelector('.terminal-ui__paste-send-btn');
        const pasteCancelBtn = this.querySelector('.terminal-ui__paste-cancel-btn');

        if (pasteTextarea) {
            pasteTextarea.addEventListener('keydown', (e) => {
                if (e.key === 'Escape') {
                    this.hidePasteOverlay();
                }
                // Ctrl/Cmd+Enter to send
                if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                    e.preventDefault();
                    this.sendPasteContent();
                }
            });
        }

        if (pasteSendBtn) {
            pasteSendBtn.addEventListener('click', () => {
                this.sendPasteContent();
            });
        }

        if (pasteCancelBtn) {
            pasteCancelBtn.addEventListener('click', () => {
                this.hidePasteOverlay();
            });
        }

        // Upload overlay ESC key handler
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                const uploadOverlay = this.querySelector('.terminal-ui__upload-overlay');
                if (uploadOverlay && uploadOverlay.classList.contains('visible')) {
                    e.preventDefault();
                    e.stopPropagation();
                    this.hideUploadOverlay();
                }
            }
        });

        // File drag-drop support
        this.setupFileDrop();

        // Clipboard paste support for files
        this.setupClipboardPaste();
    }

    setupClipboardPaste() {
        document.addEventListener('paste', async (e) => {
            // Only handle if terminal is focused or has focus within
            if (!this.contains(document.activeElement) && document.activeElement !== this) {
                return;
            }

            const items = e.clipboardData?.items;
            if (!items) return;

            // Check for file items
            const fileItems = Array.from(items).filter(item => item.kind === 'file');
            if (fileItems.length === 0) {
                // No files - let xterm handle normal text paste
                return;
            }

            // Prevent default paste behavior
            e.preventDefault();

            // Handle each file
            for (const item of fileItems) {
                const file = item.getAsFile();
                if (file) {
                    await this.handleFile(file);
                }
            }
            this.term.focus();
        });
    }

    setupFileDrop() {
        const container = this.querySelector('.terminal-ui');
        const overlay = this.querySelector('.terminal-ui__drop-overlay');
        let dragCounter = 0;

        container.addEventListener('dragenter', (e) => {
            e.preventDefault();
            e.stopPropagation();
            dragCounter++;
            if (e.dataTransfer.types.includes('Files')) {
                overlay.classList.add('visible');
            }
        });

        container.addEventListener('dragleave', (e) => {
            e.preventDefault();
            e.stopPropagation();
            dragCounter--;
            if (dragCounter === 0) {
                overlay.classList.remove('visible');
            }
        });

        container.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.stopPropagation();
        });

        container.addEventListener('drop', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            dragCounter = 0;
            overlay.classList.remove('visible');

            const files = e.dataTransfer.files;
            const wasEmpty = this.uploadQueue.length === 0;

            // Add all dropped files to queue
            for (const file of files) {
                this.addFileToQueue(file);
            }

            // If queue was empty, start processing immediately
            if (wasEmpty && this.uploadQueue.length > 0) {
                this.processUploadQueue();
            }

            this.term.focus();
        });
    }

    async handleFile(file) {
        console.log('File dropped:', file.name, file.type, file.size);

        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            this.showStatusNotification('Not connected', 3000);
            return;
        }

        if (this.isTextFile(file)) {
            // Read and paste text directly to terminal
            const text = await this.readFileAsText(file);
            if (text === null) {
                this.showStatusNotification(`Error reading: ${file.name}`, 5000);
                return;
            }
            const encoder = new TextEncoder();
            this.ws.send(encoder.encode(text));
            this.showStatusNotification(`Pasted: ${file.name} (${this.formatFileSize(text.length)})`);
        } else {
            // Binary file: send as binary upload with 0x01 prefix
            // Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
            const fileData = await this.readFileAsBinary(file);
            if (fileData === null) {
                this.showStatusNotification(`Error reading: ${file.name}`, 5000);
                return;
            }
            const encoder = new TextEncoder();
            const nameBytes = encoder.encode(file.name);
            const nameLen = nameBytes.length;

            // Build the message: 0x01 + 2-byte name length + name + file data
            const message = new Uint8Array(1 + 2 + nameLen + fileData.length);
            message[0] = 0x01; // file upload message type
            message[1] = (nameLen >> 8) & 0xFF; // name length high byte
            message[2] = nameLen & 0xFF; // name length low byte
            message.set(nameBytes, 3);
            message.set(fileData, 3 + nameLen);

            this.ws.send(message);
            this.showStatusNotification(`Uploaded: ${file.name} (${this.formatFileSize(file.size)}, temporary)`);
        }
    }

    isTextFile(file) {
        // Check MIME type first
        if (file.type.startsWith('text/')) return true;
        if (/^application\/(json|javascript|typescript|xml|yaml|x-yaml)/.test(file.type)) return true;

        // Check extension
        const textExtensions = /\.(txt|md|js|ts|jsx|tsx|go|py|rb|rs|c|cpp|h|hpp|java|sh|bash|zsh|fish|json|yaml|yml|toml|xml|html|css|scss|sass|less|sql|graphql|proto|makefile|dockerfile|gitignore|env|conf|cfg|ini|log)$/i;
        return textExtensions.test(file.name);
    }

    escapeFilename(name) {
        // Escape special shell characters
        return name.replace(/(['"\\$`!])/g, '\\$1').replace(/ /g, '\\ ');
    }

    readFileAsBinary(file) {
        return new Promise((resolve) => {
            const reader = new FileReader();
            reader.onload = () => resolve(new Uint8Array(reader.result));
            reader.onerror = () => resolve(null);
            reader.readAsArrayBuffer(file);
        });
    }

    formatFileSize(bytes) {
        if (bytes < 1024) return `${bytes} B`;
        if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
        return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    }

    readFileAsText(file) {
        return new Promise((resolve) => {
            const reader = new FileReader();
            reader.onload = () => resolve(reader.result);
            reader.onerror = () => resolve(null);
            reader.readAsText(file);
        });
    }

    addFileToQueue(file) {
        this.uploadQueue.push(file);
        this.updateUploadOverlay();
    }

    removeFileFromQueue() {
        this.uploadQueue.shift();
        this.updateUploadOverlay();
    }

    clearUploadQueue() {
        this.uploadQueue = [];
        this.updateUploadOverlay();
    }

    getQueueCount() {
        return this.uploadQueue.length;
    }

    startUpload() {
        this.isUploading = true;
        this.uploadStartTime = Date.now();
        this.updateUploadOverlay();
    }

    endUpload() {
        this.isUploading = false;
        this.uploadStartTime = null;
    }

    updateUploadOverlay() {
        const overlay = this.querySelector('.terminal-ui__upload-overlay');
        if (!overlay) return;

        const filenameEl = overlay.querySelector('.terminal-ui__upload-filename');
        const queueEl = overlay.querySelector('.terminal-ui__upload-queue');

        if (this.uploadQueue.length > 0) {
            const currentFile = this.uploadQueue[0];
            filenameEl.textContent = `Uploading: ${currentFile.name}`;

            if (this.uploadQueue.length > 1) {
                queueEl.textContent = `${this.uploadQueue.length - 1} file(s) queued`;
            } else {
                queueEl.textContent = '';
            }
        }
    }

    showUploadOverlay() {
        const overlay = this.querySelector('.terminal-ui__upload-overlay');
        if (overlay) {
            overlay.classList.remove('hidden');
            overlay.classList.add('visible');
        }
    }

    hideUploadOverlay() {
        const overlay = this.querySelector('.terminal-ui__upload-overlay');
        if (overlay) {
            overlay.classList.add('hidden');
            overlay.classList.remove('visible');
            // Remove hidden class after animation completes
            setTimeout(() => {
                overlay.classList.remove('hidden');
            }, 200);
        }
    }

    async processUploadQueue() {
        if (this.isUploading || this.uploadQueue.length === 0) {
            return;
        }

        this.startUpload();

        // Delay overlay display by 1 second to avoid flashing for quick uploads
        const overlayTimeout = setTimeout(() => {
            this.showUploadOverlay();
        }, 1000);

        while (this.uploadQueue.length > 0) {
            const file = this.uploadQueue[0];
            await this.handleFile(file);
            this.removeFileFromQueue();
        }

        this.endUpload();

        // Clear timeout if uploads finished quickly (under 1 second)
        clearTimeout(overlayTimeout);

        // Only hide if overlay was actually shown
        if (this.querySelector('.terminal-ui__upload-overlay').classList.contains('visible')) {
            this.hideUploadOverlay();
        }
    }

    // === Split-Pane UI Methods ===

    initSplitPaneUi() {
        // Initialize split-pane UI infrastructure
        // The iframe pane starts hidden (terminal at 100% width)
        // Phase 2 will add toggle behavior via service link clicks

        const iframe = this.querySelector('.terminal-ui__iframe');
        if (!iframe) {
            return; // Split-pane structure not present
        }

        // Load saved pane width (will be applied when iframe is shown)
        this.loadSavedPaneWidth();

        // Setup resizer drag functionality
        this.setupResizer();

        // Compute preview URL: prepend "1" to port (e.g., 1977 -> 11977, 8080 -> 18080)
        // This matches docker-compose: "1${SWE_PORT:-1977}:9899"
        // Note: SWE_PORT must be ≤ 9999 for valid preview port (max 19999 < 65535)
        const currentPort = window.location.port || (window.location.protocol === 'https:' ? '443' : '80');
        const previewPort = '1' + currentPort;
        this.previewBaseUrl = `${window.location.protocol}//${window.location.hostname}:${previewPort}`;

        // Setup iframe navigation buttons
        const homeBtn = this.querySelector('.terminal-ui__iframe-home');
        const refreshBtn = this.querySelector('.terminal-ui__iframe-refresh');

        if (homeBtn) {
            homeBtn.addEventListener('click', () => {
                this.setIframeUrl(this.previewBaseUrl + '/');
            });
        }
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.refreshIframe());
        }

        // Track iframe URL changes (limited by cross-origin policy)
        if (iframe) {
            iframe.addEventListener('load', () => this.updateIframeUrlDisplay());
        }

        // Open preview tab by default on desktop (if wide enough for split view)
        // Skip when embedded in iframe (right panel) - avoid nested iframes
        if (this.canShowSplitPane() && !this.classList.contains('embedded-in-iframe')) {
            // Use setTimeout to ensure DOM is ready and terminal has been initialized
            setTimeout(() => {
                this.openIframePane('preview', this.previewBaseUrl + '/');
            }, 100);
        }
    }

    // Check if viewport is wide enough for split-pane layout
    canShowSplitPane() {
        // Need room for two panels plus resizer
        const minWidth = (this.MIN_PANEL_WIDTH * 2) + this.RESIZER_WIDTH;
        return window.innerWidth >= minWidth;
    }

    // Check if this is a regular left-click without modifier keys
    isRegularClick(e) {
        return !e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0;
    }

    // Handle tab click - toggle iframe pane or open in new tab
    handleTabClick(e, tab, url) {
        const canSplit = this.canShowSplitPane();
        const isRegular = this.isRegularClick(e);

        // On desktop with regular click: toggle iframe pane
        // Otherwise: let default link behavior open new tab
        if (canSplit && isRegular) {
            e.preventDefault();

            if (tab === this.activeTab) {
                // Clicking active tab closes the pane
                this.closeIframePane();
            } else {
                // Clicking different tab opens/switches pane
                this.openIframePane(tab, url);
            }
        }
        // else: don't preventDefault, let browser open in new tab
    }

    // Open iframe pane with specified tab content
    openIframePane(tab, url) {
        const terminalUi = this.querySelector('.terminal-ui');
        const iframe = this.querySelector('.terminal-ui__iframe');
        const iframePane = this.querySelector('.terminal-ui__iframe-pane');
        if (!terminalUi || !iframe) return;

        // Add class to show iframe pane
        terminalUi.classList.add('iframe-visible');

        // Show/hide toolbar based on tab (only preview gets toolbar)
        if (iframePane) {
            if (tab === 'preview') {
                iframePane.classList.add('show-toolbar');
            } else {
                iframePane.classList.remove('show-toolbar');
            }
        }

        // Apply saved pane width
        this.applyPaneWidth();

        // Update iframe src
        this.setIframeUrl(url);

        // Update active tab state
        this.activeTab = tab;
        this.updateActiveTabIndicator();

        // Re-fit terminal after layout change
        if (this.fitAddon) {
            setTimeout(() => {
                this.fitAddon.fit();
                this.sendResize();
            }, 50);
        }
    }

    // Close iframe pane and return to 100% terminal
    closeIframePane() {
        const terminalUi = this.querySelector('.terminal-ui');
        const iframe = this.querySelector('.terminal-ui__iframe');
        if (!terminalUi) return;

        // Stop iframe content to save memory
        if (iframe) {
            iframe.src = 'about:blank';
        }

        // Remove class to hide iframe pane
        terminalUi.classList.remove('iframe-visible');

        // Clear active tab state
        this.activeTab = null;
        this.updateActiveTabIndicator();

        // Re-fit terminal to full width
        if (this.fitAddon) {
            setTimeout(() => {
                this.fitAddon.fit();
                this.sendResize();
            }, 50);
        }
    }

    // Update visual indicator for active tab (status bar and settings panel)
    updateActiveTabIndicator() {
        // Update status bar tabs
        const statusTabs = this.querySelectorAll('.terminal-ui__status-tab');
        statusTabs.forEach(tab => {
            if (tab.dataset.tab === this.activeTab) {
                tab.classList.add('active');
            } else {
                tab.classList.remove('active');
            }
        });

        // Update settings panel nav tabs
        const settingsTabs = this.querySelectorAll('.settings-panel__nav-tab');
        settingsTabs.forEach(tab => {
            if (tab.dataset.tab === this.activeTab) {
                tab.classList.add('active');
            } else {
                tab.classList.remove('active');
            }
        });
    }

    setupResizer() {
        const resizer = this.querySelector('.terminal-ui__resizer');
        const terminal = this.querySelector('.terminal-ui__terminal');
        const splitPane = this.querySelector('.terminal-ui__split-pane');
        const iframePane = this.querySelector('.terminal-ui__iframe-pane');
        if (!resizer || !terminal || !splitPane) return;

        let isDragging = false;
        let startX = 0;
        let startWidth = 0;
        let fitPending = false;

        // Throttled fit to avoid excessive reflows during drag
        const throttledFit = () => {
            if (fitPending || !this.fitAddon) return;
            fitPending = true;
            requestAnimationFrame(() => {
                this.fitAddon.fit();
                this.sendResize(); // Notify backend of new size
                fitPending = false;
            });
        };

        const onMouseDown = (e) => {
            isDragging = true;
            startX = e.clientX || e.touches?.[0]?.clientX || 0;
            startWidth = terminal.offsetWidth;
            resizer.classList.add('dragging');
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
            // Disable iframe pointer events during drag to prevent mouse capture
            if (iframePane) iframePane.style.pointerEvents = 'none';
            e.preventDefault();
        };

        const onMouseMove = (e) => {
            if (!isDragging) return;
            const clientX = e.clientX || e.touches?.[0]?.clientX || 0;
            const delta = clientX - startX;
            const containerWidth = splitPane.offsetWidth;
            const resizerWidth = resizer.offsetWidth;
            const minWidth = 150;

            let newWidth = startWidth + delta;
            // Enforce minimum widths
            newWidth = Math.max(minWidth, newWidth);
            newWidth = Math.min(containerWidth - resizerWidth - minWidth, newWidth);

            // Convert to percentage
            this.iframePaneWidth = 100 - (newWidth / containerWidth * 100);
            this.applyPaneWidth();
            // Live-fit terminal during drag
            throttledFit();
        };

        const onMouseUp = () => {
            if (!isDragging) return;
            isDragging = false;
            resizer.classList.remove('dragging');
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
            // Re-enable iframe pointer events
            if (iframePane) iframePane.style.pointerEvents = '';
            this.savePaneWidth();
            // Trigger terminal resize and notify backend
            if (this.fitAddon) {
                setTimeout(() => {
                    this.fitAddon.fit();
                    this.sendResize();
                }, 50);
            }
        };

        // Double-click to reset to 50/50
        resizer.addEventListener('dblclick', () => {
            this.iframePaneWidth = 50;
            this.applyPaneWidth();
            this.savePaneWidth();
            if (this.fitAddon) {
                setTimeout(() => {
                    this.fitAddon.fit();
                    this.sendResize();
                }, 50);
            }
        });

        resizer.addEventListener('mousedown', onMouseDown);
        resizer.addEventListener('touchstart', onMouseDown, { passive: false });
        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('touchmove', onMouseMove, { passive: false });
        document.addEventListener('mouseup', onMouseUp);
        document.addEventListener('touchend', onMouseUp);
    }

    applyPaneWidth() {
        const terminal = this.querySelector('.terminal-ui__terminal');
        if (terminal) {
            terminal.style.width = (100 - this.iframePaneWidth) + '%';
        }
    }

    loadSavedPaneWidth() {
        try {
            const saved = localStorage.getItem('swe-swe-iframe-width');
            if (saved) {
                const width = parseFloat(saved);
                if (!isNaN(width) && width >= 10 && width <= 90) {
                    this.iframePaneWidth = width;
                }
            }
        } catch (e) {
            console.warn('[TerminalUI] Could not load pane width:', e);
        }
    }

    savePaneWidth() {
        try {
            localStorage.setItem('swe-swe-iframe-width', this.iframePaneWidth.toString());
        } catch (e) {
            console.warn('[TerminalUI] Could not save pane width:', e);
        }
    }

    setIframeUrl(url) {
        // Validate URL
        try {
            new URL(url);
        } catch (e) {
            console.warn('[TerminalUI] Invalid iframe URL:', url);
            return;
        }

        const iframe = this.querySelector('.terminal-ui__iframe');
        const urlDisplay = this.querySelector('.terminal-ui__iframe-url');
        const placeholder = this.querySelector('.terminal-ui__iframe-placeholder');

        if (urlDisplay) {
            // Show path relative to base, or full URL if different origin
            try {
                const urlObj = new URL(url);
                urlDisplay.textContent = urlObj.pathname + urlObj.search + urlObj.hash || '/';
            } catch (e) {
                urlDisplay.textContent = url;
            }
            urlDisplay.title = url;
        }

        if (iframe) {
            // Show placeholder while loading
            if (placeholder) {
                placeholder.classList.remove('hidden');
            }

            iframe.onload = () => {
                if (placeholder) {
                    placeholder.classList.add('hidden');
                }
            };

            iframe.onerror = () => {
                if (placeholder) {
                    placeholder.textContent = 'Failed to load: ' + url;
                    placeholder.classList.remove('hidden');
                }
            };

            iframe.src = url;
        }
    }

    refreshIframe() {
        const iframe = this.querySelector('.terminal-ui__iframe');
        if (iframe && iframe.src) {
            const placeholder = this.querySelector('.terminal-ui__iframe-placeholder');
            if (placeholder) {
                placeholder.textContent = 'Loading...';
                placeholder.classList.remove('hidden');
            }
            // Force reload by setting src again
            iframe.src = iframe.src;
        }
    }

    updateIframeUrlDisplay() {
        const iframe = this.querySelector('.terminal-ui__iframe');
        const urlDisplay = this.querySelector('.terminal-ui__iframe-url');
        if (!iframe || !urlDisplay) return;

        try {
            // Try to read current URL (may fail due to cross-origin policy)
            const currentUrl = iframe.contentWindow?.location?.href;
            if (currentUrl && currentUrl !== 'about:blank') {
                // Show path + query string relative to base URL
                const url = new URL(currentUrl);
                const displayUrl = url.pathname + url.search + url.hash;
                urlDisplay.textContent = displayUrl || '/';
                urlDisplay.title = currentUrl;
            }
        } catch (e) {
            // Cross-origin: can't read iframe location, show base URL
            if (this.previewBaseUrl) {
                urlDisplay.textContent = this.previewBaseUrl;
                urlDisplay.title = this.previewBaseUrl;
            }
        }
    }
}

customElements.define('terminal-ui', TerminalUI);
