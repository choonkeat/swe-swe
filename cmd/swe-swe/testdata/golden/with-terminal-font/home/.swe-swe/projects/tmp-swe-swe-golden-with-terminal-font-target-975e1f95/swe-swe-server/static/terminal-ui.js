import { formatDuration, formatFileSize, escapeHtml, escapeFilename } from './modules/util.js';
import { validateUsername, validateSessionName } from './modules/validation.js';
import { deriveShellUUID } from './modules/uuid.js';
import { getBaseUrl, buildVSCodeUrl, buildShellUrl, buildPreviewUrl, buildProxyUrl, buildAgentChatUrl, getDebugQueryString, PROXY_PORT_OFFSET } from './modules/url-builder.js';
import { OPCODE_CHUNK, encodeResize, encodeFileUpload, isChunkMessage, decodeChunkHeader, parseServerMessage } from './modules/messages.js';
import { createReconnectState, getDelay, nextAttempt, resetAttempts, formatCountdown, probeUntilReady } from './modules/reconnect.js';
import { createQueue, enqueue, dequeue, peek, isEmpty as isQueueEmpty, getQueueCount, getQueueInfo, startUploading, stopUploading, clearQueue } from './modules/upload-queue.js';
import { createAssembler, addChunk, isComplete, getReceivedCount, assemble, reset as resetAssembler, getProgress } from './modules/chunk-assembler.js';
import { getStatusBarClasses, renderStatusInfo, renderServiceLinks, renderCustomLinks, renderAssistantLink } from './modules/status-renderer.js';
import { DARK_XTERM_THEME, LIGHT_XTERM_THEME } from './theme-mode.js';

class TerminalUI extends HTMLElement {
    constructor() {
        super();
        this.ws = null;
        this.term = null;
        this.fitAddon = null;
        this.connectedAt = null;
        this.reconnectState = createReconnectState();
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
        this.previewPort = null;
        this.previewBaseUrl = null;
        this.agentChatPort = null;
        // Chat feature
        this.currentUserName = null;
        this.chatMessages = [];
        this.chatInputOpen = false;
        this.unreadChatCount = 0;
        this.chatMessageTimeouts = [];
        // File upload queue (managed via upload-queue.js pure functions)
        this.uploadQueueState = createQueue();
        // Chunked snapshot reassembly (managed via chunk-assembler.js pure functions)
        this.chunkAssemblerState = createAssembler();
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
        // Left panel tab state
        this.leftPanelTab = 'terminal'; // 'terminal' | 'chat'
        // Split-pane constants for desktop detection
        this.MIN_PANEL_WIDTH = 360; // minimum width for terminal or iframe pane
        this.RESIZER_WIDTH = 8;     // width of the resizer handle
        // Mobile view state
        this.mobileActiveView = 'terminal'; // 'terminal' | 'workspace'
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

            // Preview mode: skip terminal/WebSocket for UI iteration
            const urlParams = new URLSearchParams(window.location.search);
            this.previewMode = urlParams.has('preview');

            if (!this.previewMode) {
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
            } else {
                console.log('[TerminalUI] Preview mode: skipping terminal/WebSocket init');
                // In preview mode, enable yolo toggle for UI testing
                this.yoloSupported = true;
                this.yoloMode = urlParams.has('yolo');
                // In preview mode, manually trigger status update to show UI elements like chat button
                // Use setTimeout to ensure all initialization is complete
                setTimeout(() => this.updateStatusInfo(), 100);
            }
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
            <div class="terminal-ui xterm-focused">
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
                                    <input type="text" id="settings-session" class="settings-panel__input" placeholder="Enter session name" maxlength="256">
                                </div>
                                <div class="settings-panel__field">
                                    <label class="settings-panel__label">Appearance</label>
                                    <div class="settings-panel__theme-toggle" id="settings-theme-toggle">
                                        <button class="settings-panel__theme-btn" data-mode="light">Light</button>
                                        <button class="settings-panel__theme-btn" data-mode="dark">Dark</button>
                                        <button class="settings-panel__theme-btn selected" data-mode="system">System</button>
                                    </div>
                                </div>
                                <div class="settings-panel__field">
                                    <label class="settings-panel__label">Theme Color</label>
                                    <div class="settings-panel__color-picker">
                                        <div class="settings-panel__color-presets" id="settings-color-presets">
                                            <!-- Populated by JS -->
                                        </div>
                                        <div class="settings-panel__color-custom">
                                            <input type="color" class="settings-panel__color-input" id="settings-color-input" value="#7c3aed">
                                            <input type="text" class="settings-panel__color-hex" id="settings-color-hex" value="#7c3aed" placeholder="#7c3aed">
                                            <button class="settings-panel__color-reset" id="settings-color-reset">Reset</button>
                                        </div>
                                    </div>
                                </div>
                            </section>
                        </div>
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
                <!-- Header -->
                <header class="terminal-ui__header">
                    <div class="terminal-ui__header-left">
                        <a href="/" class="terminal-ui__back-btn" title="Back to sessions">←</a>
                        <div class="terminal-ui__session-info">
                            <span class="terminal-ui__session-name" title="Click to rename session">Session</span>
                            <span class="terminal-ui__branch-badge desktop-only"></span>
                        </div>
                        <span class="terminal-ui__connection-status">
                            <span class="terminal-ui__status-dot"></span>
                            <span class="terminal-ui__status-text desktop-only">Connecting...</span>
                            <span class="terminal-ui__uptime desktop-only"></span>
                        </span>
                    </div>
                    <div class="terminal-ui__header-right">
                        <span class="terminal-ui__viewers desktop-only"></span>
                        <button class="terminal-ui__chat-btn desktop-only" title="Chat with viewers" style="display: none;">
                            <span class="terminal-ui__chat-icon">💬</span>
                            <span class="terminal-ui__chat-badge" style="display: none;">0</span>
                        </button>
                        <button class="terminal-ui__settings-btn" title="Settings">⚙</button>
                    </div>
                </header>

                <!-- Mobile Terminal Bar (mobile only) -->
                <div class="terminal-ui__terminal-bar mobile-only">
                    <select class="terminal-ui__mobile-nav-select">
                        <option value="agent-terminal">Agent Terminal</option>
                        <option value="agent-chat" style="display: none;">Agent Chat</option>
                        <option value="preview">App Preview</option>
                        <option value="vscode">Code</option>
                        <option value="shell">Terminal</option>
                        <option value="browser">Agent View</option>
                    </select>
                    <span class="terminal-ui__assistant-badge">CLAUDE</span>
                </div>

                <!-- Main Content -->
                <div class="terminal-ui__main-content">
                    <div class="terminal-ui__split-pane">
                        <div class="terminal-ui__terminal-wrapper">
                            <!-- Left Panel Header (desktop) -->
                            <div class="terminal-ui__panel-header desktop-only">
                                <div class="terminal-ui__left-panel-tabs">
                                    <button data-left-tab="terminal" class="active">
                                        <span class="terminal-ui__terminal-icon">>_</span> Agent Terminal
                                    </button>
                                    <button data-left-tab="chat" style="display: none;">
                                        <span class="terminal-ui__chat-tab-icon">◯</span> Agent Chat
                                    </button>
                                </div>
                                <span class="terminal-ui__assistant-badge">CLAUDE</span>
                            </div>
                            <div class="terminal-ui__terminal"></div>
                            <div class="terminal-ui__agent-chat" style="display: none;">
                                <div class="terminal-ui__iframe-placeholder terminal-ui__agent-chat-placeholder">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Connecting to chat...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__agent-chat-iframe"
                                        src="about:blank"
                                        sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads">
                                </iframe>
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
                            <div class="touch-scroll-proxy">
                                <div class="scroll-spacer"></div>
                            </div>
                        </div>
                        <div class="terminal-ui__resizer"></div>
                        <div class="terminal-ui__iframe-pane">
                            <!-- Right Panel Header (desktop) -->
                            <div class="terminal-ui__panel-header desktop-only">
                                <div class="terminal-ui__panel-tabs">
                                    <button data-tab="preview" class="active">Preview</button>
                                    <button data-tab="vscode">Code</button>
                                    <button data-tab="shell">Terminal</button>
                                    <button data-tab="browser">Agent View</button>
                                </div>
                            </div>
                            <div class="terminal-ui__iframe-location">
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-home" title="Home">⌂</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-back" title="Back" disabled>◀</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-forward" title="Forward" disabled>▶</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-refresh" title="Refresh">↻</button>
                                <input type="text" class="terminal-ui__iframe-url-input" placeholder="Enter URL..." title="Current URL" />
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-go" title="Go">→</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-open-external" title="Open in new window">↗</button>
                            </div>
                            <div class="terminal-ui__iframe-container">
                                <div class="terminal-ui__iframe-placeholder">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Connecting to preview...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
                            </div>
                        </div>
                    </div>
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


                <!-- Status bar (legacy, will be removed) -->
                <div class="terminal-ui__status-bar" style="display: none;">
                    <div class="terminal-ui__status-left">
                        <div class="terminal-ui__status-icon"></div>
                        <span class="terminal-ui__status-text-legacy">Connecting...</span>
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

        const resolvedTheme = document.documentElement.getAttribute('data-theme');
        const xtermTheme = resolvedTheme === 'light' ? LIGHT_XTERM_THEME : DARK_XTERM_THEME;

        this.term = new Terminal({
            cursorBlink: true,
            fontSize: 16,
            fontFamily: 'JetBrains Mono',
            scrollback: 5000,
            theme: xtermTheme
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
                getVSCodeUrl: () => buildVSCodeUrl(getBaseUrl(window.location), this.workDir),
                onCopy: (path) => this.showStatusNotification('Copied: ' + path),
                onHint
            });
        }

        // Register color link provider for clickable CSS colors
        if (typeof registerColorLinkProvider === 'function') {
            registerColorLinkProvider(this.term, {
                onColorClick: (color) => this.setStatusBarColor(color),
                onCopy: (color) => this.showStatusNotification('Copied: ' + color),
                onHint
            });
        }

        // Register URL link provider for clickable http/https URLs
        if (typeof registerUrlLinkProvider === 'function') {
            registerUrlLinkProvider(this.term, {
                onCopy: (url) => this.showStatusNotification('Copied: ' + url),
                onHint
            });
        }

    }

    renderLinks() {
        const statusRight = this.querySelector('.terminal-ui__status-right');
        if (!statusRight) return;

        // Remove any existing link container
        const existingContainer = statusRight.querySelector('.terminal-ui__status-links');
        if (existingContainer) {
            existingContainer.remove();
        }

        // Use pure render function for custom links HTML
        const html = renderCustomLinks(this.links);
        if (!html) return;

        // Insert HTML string by creating a wrapper
        const temp = document.createElement('div');
        temp.innerHTML = html;
        const container = temp.firstChild;
        statusRight.insertBefore(container, statusRight.firstChild);
    }

    renderServiceLinks() {
        const statusRight = this.querySelector('.terminal-ui__status-right');
        if (!statusRight) return;

        // Remove any existing service links container
        const existingContainer = statusRight.querySelector('.terminal-ui__status-service-links');
        if (existingContainer) {
            existingContainer.remove();
        }

        // Build services list
        const baseUrl = getBaseUrl(window.location);
        const services = [
            { name: 'vscode', label: 'VSCode', url: buildVSCodeUrl(baseUrl, this.workDir) },
            { name: 'preview', label: 'App Preview', url: this.getPreviewBaseUrl() },
            { name: 'browser', label: 'Agent View', url: `${baseUrl}/chrome/` }
        ];

        // Add shell link if not already in a shell session
        if (this.assistant !== 'shell') {
            const shellUUID = deriveShellUUID(this.uuid);
            const shellUrl = buildShellUrl({ baseUrl, shellUUID, parentUUID: this.uuid, debug: this.debugMode });
            services.unshift({ name: 'shell', label: 'Shell', url: shellUrl });
        }

        // Use pure render function for service links HTML
        const html = renderServiceLinks({ services });
        if (!html) return;

        // Insert HTML string by creating a wrapper
        const temp = document.createElement('div');
        temp.innerHTML = html;
        const container = temp.firstChild;

        // Attach click handlers after insertion (event handlers can't be in pure HTML)
        services.forEach((service) => {
            const link = container.querySelector(`[data-tab="${service.name}"]`);
            if (link) {
                link.addEventListener('click', (e) => this.handleTabClick(e, service.name, service.url));
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
        // Update new header elements
        const statusDot = this.querySelector('.terminal-ui__status-dot');
        const statusText = this.querySelector('.terminal-ui__status-text');
        const terminalEl = this.querySelector('.terminal-ui__terminal');

        // Update status dot class
        if (statusDot) {
            statusDot.className = 'terminal-ui__status-dot';
            if (state) {
                statusDot.classList.add(state);
            }
        }

        // Update status text (only in header, not the legacy status bar)
        if (statusText) {
            // For non-connected states, show the message
            // For connected state, updateStatusInfo will handle the display
            if (state !== 'connected') {
                statusText.textContent = message;
            } else {
                statusText.textContent = 'Connected';
            }
        }

        // Update terminal disconnected state
        terminalEl.classList.toggle('disconnected', state !== 'connected' && state !== '');

        // Legacy: also update status bar if present (for backwards compatibility)
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        if (statusBar) {
            const isMultiuser = statusBar.classList.contains('multiuser');
            statusBar.className = 'terminal-ui__status-bar ' + state;
            if (isMultiuser) {
                statusBar.classList.add('multiuser');
            }
            const legacyStatusText = statusBar.querySelector('.terminal-ui__status-text-legacy');
            if (legacyStatusText) {
                legacyStatusText.innerHTML = message;
            }
        }

        // Clear status info when not connected
        if (state !== 'connected') {
            const infoEl = this.querySelector('.terminal-ui__status-info');
            if (infoEl) infoEl.textContent = '';
        }
    }

    startUptimeTimer() {
        if (this.uptimeInterval) clearInterval(this.uptimeInterval);
        this.connectedAt = Date.now();
        // Update new header uptime element
        const uptimeEl = this.querySelector('.terminal-ui__uptime');
        // Also update legacy timer element
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        this.uptimeInterval = setInterval(() => {
            const duration = formatDuration(Date.now() - this.connectedAt);
            if (uptimeEl) uptimeEl.textContent = '• ' + duration;
            if (timerEl) timerEl.textContent = duration;
        }, 1000);
        if (uptimeEl) uptimeEl.textContent = '• 0s';
        if (timerEl) timerEl.textContent = '0s';
    }

    stopUptimeTimer() {
        if (this.uptimeInterval) {
            clearInterval(this.uptimeInterval);
            this.uptimeInterval = null;
        }
        const uptimeEl = this.querySelector('.terminal-ui__uptime');
        if (uptimeEl) uptimeEl.textContent = '';
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        if (timerEl) timerEl.textContent = '';
    }

    getAssistantLink() {
        return renderAssistantLink({
            assistantName: this.assistantName,
            assistant: this.assistant,
            debugMode: this.debugMode
        });
    }

    // Plain text assistant name for status messages (no HTML)
    getAssistantName() {
        return this.assistantName || this.assistant || '';
    }

    scheduleReconnect() {
        const delay = getDelay(this.reconnectState);
        this.reconnectState = nextAttempt(this.reconnectState);

        let remaining = formatCountdown(delay);
        this.updateStatus('reconnecting', `Reconnecting to ${this.getAssistantName()} in ${remaining}s...`);

        this.countdownInterval = setInterval(() => {
            remaining--;
            if (remaining > 0) {
                this.updateStatus('reconnecting', `Reconnecting to ${this.getAssistantName()} in ${remaining}s...`);
            }
        }, 1000);

        this.reconnectTimeout = setTimeout(() => {
            clearInterval(this.countdownInterval);
            this.connect();
        }, delay);
    }

    sendResize() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(encodeResize(this.term.rows, this.term.cols));
        }
    }

    // Fit terminal and preserve scroll position, unless user is near bottom
    // (within half screen height), in which case scroll to bottom.
    fitAndPreserveScroll() {
        if (!this.term || !this.fitAddon) return;

        const buffer = this.term.buffer.active;
        const maxLine = buffer.length - this.term.rows;
        const scrolledUp = maxLine - buffer.viewportY;
        const nearBottom = scrolledUp < this.term.rows / 2;

        this.fitAddon.fit();
        this.sendResize();

        if (nearBottom) {
            this.term.scrollToBottom();
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

        this.updateStatus('connecting', `Connecting to ${this.getAssistantName()}...`);
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        if (timerEl) timerEl.textContent = '';

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        let url = protocol + '//' + window.location.host + '/ws/' + this.uuid + '?assistant=' + encodeURIComponent(this.assistant);
        // Forward name param from page URL to WebSocket URL (for session naming)
        const nameParam = new URLSearchParams(location.search).get('name');
        if (nameParam) {
            url += '&name=' + encodeURIComponent(nameParam);
        }
        // Forward branch param from page URL to WebSocket URL (for worktree creation)
        const branchParam = new URLSearchParams(location.search).get('branch');
        if (branchParam) {
            url += '&branch=' + encodeURIComponent(branchParam);
        }
        // Forward pwd param from page URL to WebSocket URL (for external repo base path)
        const pwdParam = new URLSearchParams(location.search).get('pwd');
        if (pwdParam) {
            url += '&pwd=' + encodeURIComponent(pwdParam);
        }
        // Forward parent param from page URL to WebSocket URL (for shell session workDir inheritance)
        const parentParam = new URLSearchParams(location.search).get('parent');
        if (parentParam) {
            url += '&parent=' + encodeURIComponent(parentParam);
        }
        // Forward session mode param from page URL to WebSocket URL (chat vs terminal)
        const sessionParam = new URLSearchParams(location.search).get('session');
        if (sessionParam) {
            url += '&session=' + encodeURIComponent(sessionParam);
        }
        // Forward current theme to server so shell env (COLORFGBG) matches
        const currentTheme = document.documentElement.getAttribute('data-theme') || 'dark';
        url += '&theme=' + encodeURIComponent(currentTheme);

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
            this.reconnectState = resetAttempts(this.reconnectState);
            this.updateStatus('connected', 'Connected');
            this.startUptimeTimer();
            this.sendResize();
            this.startHeartbeat();
        };

        this.ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                const data = new Uint8Array(event.data);
                if (isChunkMessage(data)) {
                    this.handleChunk(data);
                } else {
                    this.onTerminalData(data);
                }
            } else if (typeof event.data === 'string') {
                const msg = parseServerMessage(event.data);
                if (msg !== null) {
                    this.handleJSONMessage(msg);
                } else {
                    console.error('Invalid JSON from server:', event.data);
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
                // xterm.js 5.5.0 handles clear-screen sequences without
                // viewport jumping — no scroll correction needed.
                const buffer = this.term.buffer.active;
                const maxLine = buffer.length - this.term.rows;
                const scrolledUp = maxLine - buffer.viewportY;
                const wasNearBottom = scrolledUp < this.term.rows / 2;
                this.term.write(combined);
                if (wasNearBottom) {
                    console.log(`[scroll-debug] would have called scrollToBottom (scrolledUp=${scrolledUp}, rows=${this.term.rows})`);
                }
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
    handleChunk(data) {
        const { chunkIndex, totalChunks, payload } = decodeChunkHeader(data);

        console.log(`CHUNK ${chunkIndex + 1}/${totalChunks} (${payload.length} bytes)`);

        // Add chunk to assembler
        this.chunkAssemblerState = addChunk(this.chunkAssemblerState, chunkIndex, totalChunks, payload);

        // Show chunk progress in status bar for debugging
        if (totalChunks > 1) {
            const progress = getProgress(this.chunkAssemblerState);
            this.showStatusNotification(`Receiving snapshot: ${progress.received}/${progress.total}`, 2000);
        }

        if (isComplete(this.chunkAssemblerState)) {
            this.reassembleChunks();
        }
    }

    // Reassemble chunks and decompress
    async reassembleChunks() {
        // Combine all chunks
        const compressed = assemble(this.chunkAssemblerState);
        const chunkCount = getReceivedCount(this.chunkAssemblerState);

        console.log(`REASSEMBLED: ${chunkCount} chunks, ${compressed.length} bytes compressed`);

        // Reset chunk state
        this.chunkAssemblerState = resetAssembler(this.chunkAssemblerState);

        // Decompress and write to terminal
        try {
            const decompressed = await this.decompressSnapshot(compressed);
            console.log(`DECOMPRESSED: ${compressed.length} -> ${decompressed.length} bytes`);
            this.showStatusNotification(`Snapshot loaded: ${decompressed.length} bytes`, 2000);
            this.term.write(decompressed);
            console.log(`[scroll-debug] snapshot: would have called scrollToBottom`);
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
                const prevPreviewBaseUrl = this.previewBaseUrl;
                this.previewPort = msg.previewPort || null;
                this.agentChatPort = msg.agentChatPort || null;
                // Probe agent chat proxy — wait for our proxy to be up, then load iframe.
                // agentChatPort is only sent for session=chat, so terminal sessions skip this.
                if (this.agentChatPort && !this._agentChatAvailable && !this._agentChatProbing) {
                    this._agentChatProbing = true;
                    const acUrl = buildAgentChatUrl(window.location, this.agentChatPort);
                    if (acUrl) {
                        this._agentChatProbeController = new AbortController();
                        probeUntilReady(acUrl + '/', {
                            maxAttempts: 10, baseDelay: 2000, maxDelay: 30000,
                            isReady: (resp) => resp.headers.has('X-Agent-Reverse-Proxy'),
                            signal: this._agentChatProbeController.signal,
                        }).then(() => {
                            this._agentChatAvailable = true;
                            this._agentChatProbing = false;
                            this.setAgentChatTabVisible(true);
                            // Load agent chat into iframe and dismiss placeholder on load
                            const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
                            if (chatIframe) {
                                chatIframe.src = acUrl + '/';
                                chatIframe.onload = () => {
                                    const ph = this.querySelector('.terminal-ui__agent-chat-placeholder');
                                    if (ph) ph.classList.add('hidden');
                                };
                            }
                            // Auto-activate chat tab in chat session mode
                            if (new URLSearchParams(location.search).get('session') === 'chat') {
                                this.switchLeftPanelTab('chat');
                                this.switchMobileNav('agent-chat');
                            }
                        }).catch(() => {
                            this._agentChatProbing = false;
                        });
                    }
                }
                this.updatePreviewBaseUrl();
                if (this.workDir !== prevWorkDir) {
                    this.renderServiceLinks();
                }
                if (this.previewBaseUrl !== prevPreviewBaseUrl && this.workDir === prevWorkDir) {
                    this.renderServiceLinks();
                }
                // Connect debug WebSocket when preview port becomes available or changes
                if (this.previewPort && this.previewBaseUrl !== prevPreviewBaseUrl) {
                    this.connectDebugWebSocket();
                }
                // Open preview for first time once we have previewPort
                if (this._wantsPreviewOnConnect && this.previewPort) {
                    this._wantsPreviewOnConnect = false;
                    setTimeout(() => this.openIframePane('preview', null), 100);
                }
                // Refresh iframe if port changed while preview is active
                else if (this.previewBaseUrl !== prevPreviewBaseUrl && this.activeTab === 'preview') {
                    const currentTarget = this.querySelector('.terminal-ui__iframe-url-input')?.value?.trim() || null;
                    this.setPreviewURL(currentTarget);
                }
                // YOLO mode state
                this.yoloMode = msg.yoloMode || false;
                this.yoloSupported = msg.yoloSupported || false;
                this.updateStatusInfo();
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
            window.location.href = '/' + getDebugQueryString(this.debugMode);
        }
    }

    updateStatusInfo() {
        const isConnected = this.ws && this.ws.readyState === WebSocket.OPEN;

        // Update new header elements
        const sessionNameEl = this.querySelector('.terminal-ui__session-name');
        const viewersEl = this.querySelector('.terminal-ui__viewers');

        if (sessionNameEl) {
            sessionNameEl.textContent = this.sessionName || this.uuidShort || 'Session';
        }

        if (viewersEl && isConnected) {
            if (this.viewers > 1) {
                // Show viewer count
                viewersEl.innerHTML = `<span class="terminal-ui__viewer-count">${this.viewers} viewers</span>`;
            } else {
                viewersEl.innerHTML = '';
            }
        }

        // Show/hide chat button based on viewer count (always show in preview mode for testing)
        const chatBtn = this.querySelector('.terminal-ui__chat-btn');
        if (chatBtn) {
            chatBtn.style.display = (this.previewMode || this.viewers > 1) ? 'inline-flex' : 'none';
        }

        // Update all assistant badges (mobile + desktop) with name and mode toggle
        const assistantBadges = this.querySelectorAll('.terminal-ui__assistant-badge');
        assistantBadges.forEach(badge => {
            const name = (this.assistantName || this.assistant || 'CLAUDE').toUpperCase();
            if (this.yoloSupported) {
                // Show toggle with mode label inside: "NAME [● normal]" or "NAME [yolo ●]"
                const toggleClass = this.yoloMode ? 'active' : '';
                const modeLabel = this.yoloMode ? 'yolo' : 'normal';
                badge.innerHTML = `${name} <span class="badge-toggle ${toggleClass}">${modeLabel}</span>`;
                badge.style.cursor = 'pointer';
                badge.classList.toggle('yolo', this.yoloMode);
            } else {
                badge.textContent = name;
                badge.style.cursor = 'default';
                badge.classList.remove('yolo');
            }
        });

        // Legacy: Update old status bar elements for backwards compatibility
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const statusText = this.querySelector('.terminal-ui__status-text-legacy') || this.querySelector('.terminal-ui__status-bar .terminal-ui__status-text');
        const dimsEl = this.querySelector('.terminal-ui__status-dims');

        if (statusBar) {
            // Toggle multiuser class based on viewer count
            statusBar.classList.toggle('multiuser', this.viewers > 1);
            // Toggle yolo class based on YOLO mode
            statusBar.classList.toggle('yolo', this.yoloMode);
        }

        if (isConnected && statusText) {
            // Use pure render function for status info HTML
            const html = renderStatusInfo({
                connected: true,
                userName: this.currentUserName,
                assistantName: this.assistantName,
                sessionName: this.sessionName,
                uuidShort: this.uuidShort,
                viewers: this.viewers,
                yoloMode: this.yoloMode,
                yoloSupported: this.yoloSupported,
                debugMode: this.debugMode
            });
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

            const validation = validateUsername(name);
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

            const validation = validateUsername(newName);
            if (validation.valid) {
                this.setUsername(validation.name);
                return;
            } else {
                alert('Invalid name: ' + validation.error + '\nPlease try again.');
            }
        }
    }

    promptRenameSession() {
        while (true) {
            const newName = window.prompt('Enter session name:', this.sessionName);

            // User clicked Cancel
            if (newName === null) {
                return;
            }

            const validation = validateSessionName(newName);
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
                const validation = validateUsername(e.target.value);
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
                const validation = validateSessionName(e.target.value);
                if (validation.valid) {
                    this.setSessionName(validation.name);
                } else {
                    // Restore previous value
                    e.target.value = this.sessionName || '';
                }
            });
        }

        // Theme mode toggle (light/dark/system)
        this.setupThemeToggle();

        // Theme color picker
        this.setupColorPicker();

    }

    // Setup event listeners for the new header and navigation UI
    setupHeaderEventListeners() {
        // Session name click → rename session
        const sessionName = this.querySelector('.terminal-ui__session-name');
        if (sessionName) {
            sessionName.addEventListener('click', () => {
                this.promptRenameSession();
            });
        }

        // Settings button → open settings panel
        const settingsBtn = this.querySelector('.terminal-ui__settings-btn');
        if (settingsBtn) {
            settingsBtn.addEventListener('click', () => {
                this.openSettingsPanel();
            });
        }

        // Assistant badges toggle YOLO mode when clicked
        const assistantBadges = this.querySelectorAll('.terminal-ui__assistant-badge');
        assistantBadges.forEach(badge => {
            badge.addEventListener('click', () => {
                if (this.yoloSupported) {
                    this.toggleYoloMode();
                }
            });
        });

        // Chat button in header
        const chatBtn = this.querySelector('.terminal-ui__chat-btn');
        if (chatBtn) {
            chatBtn.addEventListener('click', () => {
                this.toggleChatInput();
                // Reset unread count when opening chat
                this.unreadChatCount = 0;
                this.updateChatBadge();
            });
        }

        // Panel tabs in iframe pane
        const panelTabs = this.querySelectorAll('.terminal-ui__panel-tabs button');
        panelTabs.forEach(btn => {
            btn.addEventListener('click', () => {
                const tab = btn.dataset.tab;
                this.switchPanelTab(tab);
            });
        });

        // Left panel tabs (terminal / chat toggle)
        const leftPanelTabs = this.querySelectorAll('.terminal-ui__left-panel-tabs button');
        leftPanelTabs.forEach(btn => {
            btn.addEventListener('click', () => {
                this.switchLeftPanelTab(btn.dataset.leftTab);
            });
        });

        // Mobile navigation dropdown (unified view switcher)
        const mobileNavSelect = this.querySelector('.terminal-ui__mobile-nav-select');
        if (mobileNavSelect) {
            mobileNavSelect.addEventListener('change', (e) => {
                this.switchMobileNav(e.target.value);
            });
        }
    }

    // Switch to a different panel tab (changes iframe content)
    switchPanelTab(tab) {
        if (tab === this.activeTab) return;

        // Show/hide toolbar based on tab
        const iframePane = this.querySelector('.terminal-ui__iframe-pane');
        if (iframePane) {
            if (tab === 'preview') {
                iframePane.classList.add('show-toolbar');
            } else {
                iframePane.classList.remove('show-toolbar');
            }
        }

        this.activeTab = tab;

        if (tab === 'preview') {
            if (this._pendingPreviewIframeSrc) {
                // Preview probe already succeeded while on another tab — apply stashed URL
                const pendingSrc = this._pendingPreviewIframeSrc;
                this._pendingPreviewIframeSrc = null;
                const iframe = this.querySelector('.terminal-ui__iframe');
                if (iframe) {
                    iframe.src = pendingSrc;
                    iframe.onload = () => {
                        if (this._previewWaiting) this._onPreviewReady();
                    };
                }
            } else {
                this.setPreviewURL(null);
            }
        } else {
            const baseUrl = getBaseUrl(window.location);
            let url;
            switch (tab) {
                case 'vscode':
                    url = buildVSCodeUrl(baseUrl, this.workDir);
                    break;
                case 'shell':
                    const shellUUID = deriveShellUUID(this.uuid);
                    url = buildShellUrl({ baseUrl, shellUUID, parentUUID: this.uuid, debug: this.debugMode });
                    break;
                case 'browser':
                    url = `${baseUrl}/chrome/`;
                    break;
                default:
                    return;
            }
            this.setIframeUrl(url);
        }

        this.updateActiveTabIndicator();

        // Update mobile dropdown to match
        const panelDropdown = this.querySelector('.terminal-ui__panel-select');
        if (panelDropdown) {
            panelDropdown.value = tab;
        }
    }

    // Switch left panel between terminal and chat
    switchLeftPanelTab(tab) {
        if (tab === this.leftPanelTab) return;
        this.leftPanelTab = tab;

        const terminalUi = this.querySelector('.terminal-ui');
        const terminalEl = this.querySelector('.terminal-ui__terminal');
        const chatEl = this.querySelector('.terminal-ui__agent-chat');
        const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
        if (!terminalEl || !chatEl) return;

        // xterm-focused: gate mobile keyboard + touch-scroll-proxy (desktop tab switch)
        if (tab === 'terminal') {
            terminalUi.classList.add('xterm-focused');
        } else {
            terminalUi.classList.remove('xterm-focused');
        }

        // Update left panel tab buttons
        const buttons = this.querySelectorAll('.terminal-ui__left-panel-tabs button');
        buttons.forEach(btn => {
            btn.classList.toggle('active', btn.dataset.leftTab === tab);
        });

        if (tab === 'chat') {
            // Use visibility instead of display to preserve xterm scroll position
            terminalEl.style.visibility = 'hidden';
            terminalEl.style.position = 'absolute';
            chatEl.style.display = 'flex';
            // iframe src is set by the agent chat probe callback, not here
        } else {
            chatEl.style.display = 'none';
            terminalEl.style.visibility = '';
            terminalEl.style.position = '';
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        }

        // Sync mobile dropdown
        const mobileSelect = this.querySelector('.terminal-ui__mobile-nav-select');
        if (mobileSelect) {
            mobileSelect.value = tab === 'chat' ? 'agent-chat' : 'agent-terminal';
        }
    }

    // Single source of truth for showing/hiding the Agent Chat tab in both
    // desktop button and mobile dropdown — prevents them from desyncing.
    setAgentChatTabVisible(visible) {
        const display = visible ? '' : 'none';
        const desktopBtn = this.querySelector('button[data-left-tab="chat"]');
        if (desktopBtn) desktopBtn.style.display = display;
        const mobileOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="agent-chat"]');
        if (mobileOpt) mobileOpt.style.display = display;
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

        // Theme mode toggle
        this.populateThemeToggle();

        // Theme color picker
        this.populateColorPicker();

    }

    // Populate theme toggle with current mode
    populateThemeToggle() {
        const toggle = this.querySelector('#settings-theme-toggle');
        if (!toggle) return;
        const currentMode = window.sweSweTheme?.getStoredMode?.() || 'system';
        toggle.querySelectorAll('.settings-panel__theme-btn').forEach(btn => {
            btn.classList.toggle('selected', btn.dataset.mode === currentMode);
        });
    }

    // Setup theme toggle click handler
    setupThemeToggle() {
        const toggle = this.querySelector('#settings-theme-toggle');
        if (!toggle) return;

        toggle.addEventListener('click', (e) => {
            const btn = e.target.closest('.settings-panel__theme-btn');
            if (!btn || !btn.dataset.mode) return;
            const mode = btn.dataset.mode;

            // Update selection UI
            toggle.querySelectorAll('.settings-panel__theme-btn').forEach(b => {
                b.classList.toggle('selected', b === btn);
            });

            // Apply mode
            if (window.sweSweTheme?.setThemeMode) {
                window.sweSweTheme.setThemeMode(mode);
            }
        });

        // Listen for theme mode changes to update xterm
        window.addEventListener('theme-mode-changed', (e) => {
            if (this.term) {
                const resolved = e.detail?.resolved;
                this.term.options.theme = resolved === 'light' ? LIGHT_XTERM_THEME : DARK_XTERM_THEME;
            }
        });
    }

    // Color picker preset colors
    static PRESET_COLORS = [
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

    // Populate color picker presets and current value
    populateColorPicker() {
        const presetsContainer = this.querySelector('#settings-color-presets');
        const colorInput = this.querySelector('#settings-color-input');
        const colorHex = this.querySelector('#settings-color-hex');
        if (!presetsContainer || !colorInput || !colorHex) return;

        // Get current color from theme system
        const currentColor = window.sweSweTheme?.getCurrentColor() || '#7c3aed';

        // Update inputs
        colorInput.value = currentColor;
        colorHex.value = currentColor;

        // Populate presets if not already done
        if (presetsContainer.children.length === 0) {
            TerminalUI.PRESET_COLORS.forEach(color => {
                const btn = document.createElement('button');
                btn.className = 'settings-panel__color-preset';
                btn.style.backgroundColor = color;
                btn.dataset.color = color;
                btn.title = color;
                presetsContainer.appendChild(btn);
            });
        }

        // Update selected state
        presetsContainer.querySelectorAll('.settings-panel__color-preset').forEach(btn => {
            btn.classList.toggle('selected', btn.dataset.color === currentColor);
        });
    }

    // Setup color picker event listeners
    setupColorPicker() {
        const presetsContainer = this.querySelector('#settings-color-presets');
        const colorInput = this.querySelector('#settings-color-input');
        const colorHex = this.querySelector('#settings-color-hex');
        const colorReset = this.querySelector('#settings-color-reset');
        if (!presetsContainer || !colorInput || !colorHex) return;

        // Preset click
        presetsContainer.addEventListener('click', (e) => {
            const btn = e.target.closest('.settings-panel__color-preset');
            if (btn && btn.dataset.color) {
                this.selectColor(btn.dataset.color);
            }
        });

        // Color input change
        colorInput.addEventListener('input', () => {
            this.selectColor(colorInput.value);
        });

        // Hex input change
        colorHex.addEventListener('change', () => {
            let val = colorHex.value.trim();
            if (!val.startsWith('#')) val = '#' + val;
            if (/^#[0-9a-fA-F]{6}$/.test(val)) {
                this.selectColor(val);
            }
        });

        // Reset button
        if (colorReset) {
            colorReset.addEventListener('click', () => {
                // Clear session-specific color and use default
                if (window.sweSweTheme) {
                    const sessionKey = window.sweSweTheme.COLOR_STORAGE_KEYS.SESSION_PREFIX + this.uuid;
                    localStorage.removeItem(sessionKey);
                }
                this.selectColor('#7c3aed');
            });
        }
    }

    // Apply and save selected color
    selectColor(color) {
        const presetsContainer = this.querySelector('#settings-color-presets');
        const colorInput = this.querySelector('#settings-color-input');
        const colorHex = this.querySelector('#settings-color-hex');

        // Update inputs
        if (colorInput) colorInput.value = color;
        if (colorHex) colorHex.value = color;

        // Update preset selection
        if (presetsContainer) {
            presetsContainer.querySelectorAll('.settings-panel__color-preset').forEach(btn => {
                btn.classList.toggle('selected', btn.dataset.color === color);
            });
        }

        // Apply theme
        if (window.sweSweTheme?.applyTheme) {
            window.sweSweTheme.applyTheme(color);
        }

        // Save for this session
        if (window.sweSweTheme?.saveColorPreference && this.uuid) {
            const sessionKey = window.sweSweTheme.COLOR_STORAGE_KEYS.SESSION_PREFIX + this.uuid;
            window.sweSweTheme.saveColorPreference(sessionKey, color);
        }

        // Update URL to reflect color (for sharing)
        this.updateUrlColor(color);
    }

    // Update URL query parameter with new color
    updateUrlColor(color) {
        const url = new URL(window.location.href);
        url.searchParams.set('color', color.replace('#', ''));
        window.history.replaceState({}, '', url.toString());
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
        msgEl.innerHTML = `<span class="terminal-ui__chat-message-username">${escapeHtml(userName)}:</span> ${escapeHtml(text)}`;

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
        // Also update header chat badge
        this.updateChatBadge();
    }

    updateChatBadge() {
        const badge = this.querySelector('.terminal-ui__chat-badge');
        if (!badge) return;

        if (this.unreadChatCount > 0) {
            badge.textContent = this.unreadChatCount > 99 ? '99+' : this.unreadChatCount;
            badge.style.display = 'inline-flex';
        } else {
            badge.style.display = 'none';
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
            const buf = this.term.buffer.active;
            if (buf.viewportY >= buf.baseY - this.term.rows) this.term.scrollToBottom();
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
            const buf = this.term.buffer.active;
            if (buf.viewportY >= buf.baseY - this.term.rows) this.term.scrollToBottom();
        });
    }

    blurMobileKeyboard() {
        // Blur the text input to dismiss the mobile keyboard
        const textInput = this.querySelector('.mobile-keyboard__text');
        if (textInput) {
            textInput.blur();
        }
        // Also blur any other focused element
        if (document.activeElement && document.activeElement !== document.body) {
            document.activeElement.blur();
        }
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

        // Target the bottom-most fixed element for margin adjustment
        // Priority: mobile nav (new UI) -> mobile keyboard (fallback)
        const mobileNav = this.querySelector('.terminal-ui__mobile-nav');
        const mobileKeyboard = this.querySelector('.mobile-keyboard');
        const target = mobileNav || mobileKeyboard;

        if (keyboardVisible) {
            // Keyboard is showing - adjust layout
            // Apply margin to the bottom-most element so there's no gap
            if (target) {
                target.style.marginBottom = `${keyboardHeight}px`;
            }
        } else {
            // Keyboard hidden - reset layout
            if (target) {
                target.style.marginBottom = '0';
            }
        }

        // Refit terminal immediately (no setTimeout - use rAF)
        requestAnimationFrame(() => {
            this.fitAddon.fit();
            this.sendResize();
            const buf = this.term.buffer.active;
            if (buf.viewportY >= buf.baseY - this.term.rows) this.term.scrollToBottom();
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
                // Blur to dismiss mobile keyboard
                this.blurMobileKeyboard();
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
                // Blur to dismiss mobile keyboard
                this.blurMobileKeyboard();
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
                    // Blur to dismiss mobile keyboard
                    this.blurMobileKeyboard();
                }, 300);
            } else {
                // Send just Enter
                this.sendKey('\r');
                // Blur to dismiss mobile keyboard
                this.blurMobileKeyboard();
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
            const wasEmpty = isQueueEmpty(this.uploadQueueState);
            for (const file of fileInput.files) {
                this.addFileToQueue(file);
            }
            if (wasEmpty && !isQueueEmpty(this.uploadQueueState)) {
                this.processUploadQueue();
            }
            fileInput.value = '';
        });

        // Enter key allows newlines in textarea (no auto-submit)
        // User must tap Send/Enter button to submit
    }

    setupEventListeners() {
        // Skip terminal-related event listeners in preview mode
        if (this.term) {
            // Terminal data handler - send as binary to distinguish from JSON control messages
            this.term.onData(data => {
                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                    const encoder = new TextEncoder();
                    this.ws.send(encoder.encode(data));
                }
            });

            // Window resize
            this._resizeHandler = () => {
                this.fitAndPreserveScroll();
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
        }

        // Settings panel setup
        this.setupSettingsPanel();

        // Header and navigation event handlers
        this.setupHeaderEventListeners();

        // Listen for messages from iframes
        window.addEventListener('message', (e) => {
            // When shell in right panel exits, show Preview instead of closing the panel
            if (e.data && e.data.type === 'swe-swe-session-ended') {
                this.switchPanelTab('preview');
            }
            // When user sends first message in Agent Chat, bootstrap the agent
            // by injecting a prompt into the PTY so the agent knows to check_messages
            if (e.data && e.data.type === 'agent-chat-first-user-message') {
                if (!this._chatBootstrapped && this.ws && this.ws.readyState === WebSocket.OPEN) {
                    this._chatBootstrapped = true;
                    // Send text first, then Enter after delay to ensure text is processed
                    // (same pattern as mobile keyboard sendKey)
                    this.sendKey(e.data.text || 'check_messages; i sent u a chat message');
                    setTimeout(() => this.sendKey('\r'), 300);
                }
            }
            // When user clicks Allow/Deny in Agent Chat, forward keystroke to PTY
            if (e.data && e.data.type === 'agent-chat-permission-response') {
                this.sendKey(e.data.keystroke);
                this.sendKey('\r');
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
                this.reconnectState = resetAttempts(this.reconnectState);
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

        // Clipboard copy - trim trailing whitespace from terminal lines
        this.setupClipboardCopy();
    }

    setupClipboardCopy() {
        // Helper: clean terminal selection (trim fixed-width padding) and copy.
        const copySelection = (selection) => {
            const cleaned = selection.split('\n').map(line => line.trimEnd()).join('\n');
            if (navigator.clipboard) {
                navigator.clipboard.writeText(cleaned).catch(err => {
                    console.warn('Failed to copy selection:', err);
                });
            }
            return cleaned;
        };

        // Intercept copy events to trim trailing whitespace from each line.
        // Terminal lines are fixed-width, so selections include padding spaces.
        document.addEventListener('copy', (e) => {
            if (!this.term) return;
            const selection = this.term.getSelection();
            if (selection) {
                e.clipboardData.setData('text/plain', copySelection(selection));
                e.preventDefault();
            }
        });

        // Copy-on-select: automatically copy to clipboard when mouse selection ends.
        const termEl = this.querySelector('.terminal-ui__terminal');
        if (termEl) {
            termEl.addEventListener('mouseup', (e) => {
                if (e.button !== 0) return; // left-click only
                if (!this.term || !this.term.hasSelection()) return;
                copySelection(this.term.getSelection());
                this.showStatusNotification('Copied to clipboard', 1500);
            });
        }
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
        const container = this.querySelector('.terminal-ui__terminal-wrapper');
        const overlay = this.querySelector('.terminal-ui__drop-overlay');
        let dragCounter = 0;

        const hideOverlay = () => {
            dragCounter = 0;
            overlay.classList.remove('visible');
        };

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
            if (dragCounter <= 0) {
                hideOverlay();
            }
        });

        container.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.stopPropagation();
        });

        container.addEventListener('drop', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            hideOverlay();

            const files = e.dataTransfer.files;
            const wasEmpty = isQueueEmpty(this.uploadQueueState);

            // Add all dropped files to queue
            for (const file of files) {
                this.addFileToQueue(file);
            }

            // If queue was empty, start processing immediately
            if (wasEmpty && !isQueueEmpty(this.uploadQueueState)) {
                this.processUploadQueue();
            }

            this.term.focus();
        });

        // Safety valve: click overlay to dismiss if drag gets stuck
        overlay.addEventListener('click', () => {
            hideOverlay();
            this.term.focus();
        });

        // Safety valve: Escape key dismisses the overlay
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && overlay.classList.contains('visible')) {
                hideOverlay();
                this.term.focus();
            }
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
            this.showStatusNotification(`Pasted: ${file.name} (${formatFileSize(text.length)})`);
        } else {
            // Binary file upload
            const fileData = await this.readFileAsBinary(file);
            if (fileData === null) {
                this.showStatusNotification(`Error reading: ${file.name}`, 5000);
                return;
            }
            this.ws.send(encodeFileUpload(file.name, fileData));
            this.showStatusNotification(`Uploaded: ${file.name} (${formatFileSize(file.size)}, temporary)`);
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

    readFileAsBinary(file) {
        return new Promise((resolve) => {
            const reader = new FileReader();
            reader.onload = () => resolve(new Uint8Array(reader.result));
            reader.onerror = () => resolve(null);
            reader.readAsArrayBuffer(file);
        });
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
        this.uploadQueueState = enqueue(this.uploadQueueState, file);
        this.updateUploadOverlay();
    }

    removeFileFromQueue() {
        this.uploadQueueState = dequeue(this.uploadQueueState);
        this.updateUploadOverlay();
    }

    clearUploadQueue() {
        this.uploadQueueState = clearQueue(this.uploadQueueState);
        this.updateUploadOverlay();
    }

    startUpload() {
        this.uploadQueueState = startUploading(this.uploadQueueState);
        this.updateUploadOverlay();
    }

    endUpload() {
        this.uploadQueueState = stopUploading(this.uploadQueueState);
    }

    updateUploadOverlay() {
        const overlay = this.querySelector('.terminal-ui__upload-overlay');
        if (!overlay) return;

        const filenameEl = overlay.querySelector('.terminal-ui__upload-filename');
        const queueEl = overlay.querySelector('.terminal-ui__upload-queue');

        const info = getQueueInfo(this.uploadQueueState);
        if (info.current) {
            filenameEl.textContent = `Uploading: ${info.current.name}`;
            queueEl.textContent = info.remaining > 0 ? `${info.remaining} file(s) queued` : '';
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
        if (this.uploadQueueState.isUploading || isQueueEmpty(this.uploadQueueState)) {
            return;
        }

        this.startUpload();

        // Delay overlay display by 1 second to avoid flashing for quick uploads
        const overlayTimeout = setTimeout(() => {
            this.showUploadOverlay();
        }, 1000);

        // Process files recursively with 300ms delay between each
        // This ensures paths are sent to terminal at least 300ms apart
        const processNext = () => {
            if (isQueueEmpty(this.uploadQueueState)) {
                this.endUpload();
                clearTimeout(overlayTimeout);
                if (this.querySelector('.terminal-ui__upload-overlay').classList.contains('visible')) {
                    this.hideUploadOverlay();
                }
                return;
            }

            const file = peek(this.uploadQueueState);
            this.removeFileFromQueue();

            this.handleFile(file)
                .then(() => new Promise(resolve => setTimeout(resolve, 300)))
                .then(processNext);
        };

        processNext();
    }

    // === Split-Pane UI Methods ===
    getPreviewBaseUrl() {
        const previewPort = this.previewPort ? PROXY_PORT_OFFSET + Number(this.previewPort) : null;
        return buildPreviewUrl(window.location, previewPort);
    }

    updatePreviewBaseUrl() {
        this.previewBaseUrl = this.getPreviewBaseUrl();
    }

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

        this.updatePreviewBaseUrl();

        // Setup iframe navigation buttons
        const homeBtn = this.querySelector('.terminal-ui__iframe-home');
        const refreshBtn = this.querySelector('.terminal-ui__iframe-refresh');

        if (homeBtn) {
            homeBtn.addEventListener('click', () => {
                if (this._debugWs?.readyState === WebSocket.OPEN) {
                    this._debugWs.send(JSON.stringify({ t: 'navigate', url: '/' }));
                } else {
                    this.setPreviewURL(null);
                }
            });
        }
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.refreshIframe());
        }

        // Setup Back/Forward buttons — send navigate commands via debug WebSocket
        // since the iframe is on a different port (cross-origin)
        const backBtn = this.querySelector('.terminal-ui__iframe-back');
        const forwardBtn = this.querySelector('.terminal-ui__iframe-forward');
        if (backBtn) {
            backBtn.addEventListener('click', () => {
                if (this._debugWs?.readyState === WebSocket.OPEN) {
                    this._debugWs.send(JSON.stringify({ t: 'navigate', action: 'back' }));
                }
            });
        }
        if (forwardBtn) {
            forwardBtn.addEventListener('click', () => {
                if (this._debugWs?.readyState === WebSocket.OPEN) {
                    this._debugWs.send(JSON.stringify({ t: 'navigate', action: 'forward' }));
                }
            });
        }

        // Setup URL input for external URL debugging
        const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
        const goBtn = this.querySelector('.terminal-ui__iframe-go');

        const navigateToUrl = () => {
            const targetUrl = urlInput.value.trim();
            if (!targetUrl) return;

            // Check if this is an external URL
            let navUrl;
            try {
                const parsed = new URL(targetUrl);
                const host = parsed.hostname;
                if (host !== 'localhost' && host !== '127.0.0.1') {
                    // External URLs can't load in iframe — open in new tab
                    if (confirm('Open in new tab?\n\n' + targetUrl)) {
                        window.open(targetUrl, '_blank');
                    }
                    // Restore URL bar to previous value
                    const defaultTarget = this.previewPort ? `http://localhost:${this.previewPort}` : '';
                    urlInput.value = this._lastUrlChangeUrl ? this.reverseMapProxyUrl(this._lastUrlChangeUrl) : defaultTarget;
                    return;
                }
                navUrl = parsed.pathname + parsed.search + parsed.hash;
            } catch {
                // Bare path like "/foo" — treat as localhost path
                navUrl = targetUrl.startsWith('/') ? targetUrl : '/' + targetUrl;
            }

            if (this._debugWs?.readyState === WebSocket.OPEN) {
                this._debugWs.send(JSON.stringify({ t: 'navigate', url: navUrl }));
            }
        };

        if (goBtn) {
            goBtn.addEventListener('click', navigateToUrl);
        }
        if (urlInput) {
            urlInput.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') navigateToUrl();
            });
        }

        // Open in new window button
        const openExternalBtn = this.querySelector('.terminal-ui__iframe-open-external');
        if (openExternalBtn) {
            openExternalBtn.addEventListener('click', () => {
                const url = this._lastUrlChangeUrl || this.getPreviewBaseUrl() + '/';
                if (url) window.open(url, '_blank');
            });
        }

        // Open preview tab by default on desktop (if wide enough for split view)
        // Skip when embedded in iframe (right panel) - avoid nested iframes
        if (this.canShowSplitPane() && !this.classList.contains('embedded-in-iframe')) {
            // Defer until first BroadcastStatus delivers previewPort
            this._wantsPreviewOnConnect = true;
        }

        // session=chat: show Agent Chat tab immediately (before probe succeeds)
        if (new URLSearchParams(location.search).get('session') === 'chat') {
            this.setAgentChatTabVisible(true);
            this.switchLeftPanelTab('chat');
            this.switchMobileNav('agent-chat');
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
        this.activeTab = tab;
        if (tab === 'preview') {
            this.setPreviewURL(url); // url is targetURL or null
        } else {
            this.setIframeUrl(url);
        }
        this.updateActiveTabIndicator();

        // Re-fit terminal after layout change
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    }

    // Close iframe pane and return to 100% terminal
    closeIframePane() {
        const terminalUi = this.querySelector('.terminal-ui');
        const iframe = this.querySelector('.terminal-ui__iframe');
        if (!terminalUi) return;

        // Stop iframe content to save memory
        if (iframe) {
            iframe.onload = null;
            iframe.src = 'about:blank';
        }

        // Remove class to hide iframe pane
        terminalUi.classList.remove('iframe-visible');

        // Clear active tab state
        this.activeTab = null;
        this.updateActiveTabIndicator();

        // Re-fit terminal to full width
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    }

    // Update visual indicator for active tab (status bar, panel tabs, and settings panel)
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

        // Update panel tabs in iframe pane
        const panelTabs = this.querySelectorAll('.terminal-ui__panel-tabs button');
        panelTabs.forEach(tab => {
            if (tab.dataset.tab === this.activeTab) {
                tab.classList.add('active');
            } else {
                tab.classList.remove('active');
            }
        });

    }

    // Switch mobile navigation (unified dropdown: agent-terminal + workspace panels)
    switchMobileNav(value) {
        const terminalUi = this.querySelector('.terminal-ui');

        // xterm-focused: only when agent-terminal is the active panel
        // Gates mobile keyboard and touch-scroll-proxy to avoid blocking other panels
        if (value === 'agent-terminal') {
            terminalUi.classList.add('xterm-focused');
        } else {
            terminalUi.classList.remove('xterm-focused');
        }

        if (value === 'agent-chat') {
            // Show left pane (chat), hide iframe
            terminalUi.classList.remove('mobile-view-workspace');
            terminalUi.classList.add('mobile-view-terminal');
            this.mobileActiveView = 'terminal';
            this.switchLeftPanelTab('chat');
            return;
        }
        if (value === 'agent-terminal') {
            // Show terminal, hide iframe
            terminalUi.classList.remove('mobile-view-workspace');
            terminalUi.classList.add('mobile-view-terminal');
            this.mobileActiveView = 'terminal';
            this.switchLeftPanelTab('terminal');
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        } else {
            // Show iframe, hide terminal
            terminalUi.classList.remove('mobile-view-terminal');
            terminalUi.classList.add('mobile-view-workspace');
            this.mobileActiveView = 'workspace';
            this.switchPanelTab(value); // Reuse existing logic for panel switching
        }
    }

    // Switch between terminal and workspace views on mobile (legacy method, kept for compatibility)
    switchMobileView(view) {
        if (view === this.mobileActiveView) return;

        this.mobileActiveView = view;
        const terminalUi = this.querySelector('.terminal-ui');

        // Update container class for CSS-based view switching
        terminalUi.classList.remove('mobile-view-terminal', 'mobile-view-workspace');
        terminalUi.classList.add(`mobile-view-${view}`);

        // If switching to terminal, refit and scroll to bottom
        if (view === 'terminal') {
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        }

        // If switching to workspace, ensure iframe pane is initialized
        if (view === 'workspace' && !this.activeTab) {
            // Default to preview tab
            this.activeTab = 'preview';
            this.setPreviewURL(null);
            this.updateActiveTabIndicator();
        }
    }

    setupResizer() {
        const resizer = this.querySelector('.terminal-ui__resizer');
        const terminalWrapper = this.querySelector('.terminal-ui__terminal-wrapper');
        const splitPane = this.querySelector('.terminal-ui__split-pane');
        const iframePane = this.querySelector('.terminal-ui__iframe-pane');
        if (!resizer || !terminalWrapper || !splitPane) return;

        let isDragging = false;
        let startX = 0;
        let startWidth = 0;
        let fitPending = false;

        // Snap configuration
        const snapPoints = [20, 50, 80]; // left-pane percentage snap targets
        const snapThreshold = 2; // snap within 2% of a snap point

        // Create tooltip elements for left and right pane size indicators
        const leftTooltip = document.createElement('div');
        leftTooltip.className = 'terminal-ui__resize-tooltip terminal-ui__resize-tooltip--left';
        const rightTooltip = document.createElement('div');
        rightTooltip.className = 'terminal-ui__resize-tooltip terminal-ui__resize-tooltip--right';
        resizer.appendChild(leftTooltip);
        resizer.appendChild(rightTooltip);

        const updateTooltips = (leftPct, containerWidth, resizerWidth) => {
            const leftPx = Math.round(containerWidth * leftPct / 100);
            const rightPx = Math.round(containerWidth - leftPx - resizerWidth);
            const rightPct = 100 - leftPct;
            leftTooltip.textContent = `${leftPx}px (${Math.round(leftPct)}%)`;
            rightTooltip.textContent = `${rightPx}px (${Math.round(rightPct)}%)`;
        };

        const showTooltips = () => {
            leftTooltip.classList.add('visible');
            rightTooltip.classList.add('visible');
        };

        const hideTooltips = () => {
            leftTooltip.classList.remove('visible');
            rightTooltip.classList.remove('visible');
        };

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
            startWidth = terminalWrapper.offsetWidth;
            resizer.classList.add('dragging');
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
            // Disable iframe pointer events during drag to prevent mouse capture
            if (iframePane) iframePane.style.pointerEvents = 'none';
            // Show tooltips
            const containerWidth = splitPane.offsetWidth;
            const resizerWidth = resizer.offsetWidth;
            const leftPct = 100 - this.iframePaneWidth;
            updateTooltips(leftPct, containerWidth, resizerWidth);
            showTooltips();
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

            // Convert to left-pane percentage
            let leftPct = newWidth / containerWidth * 100;

            // Snap to nearby snap points
            for (const snap of snapPoints) {
                if (Math.abs(leftPct - snap) < snapThreshold) {
                    leftPct = snap;
                    break;
                }
            }

            this.iframePaneWidth = 100 - leftPct;
            this.applyPaneWidth();
            updateTooltips(leftPct, containerWidth, resizerWidth);
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
            hideTooltips();
            this.savePaneWidth();
            // Trigger terminal resize and notify backend
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        };

        // Double-click to reset to 50/50
        resizer.addEventListener('dblclick', () => {
            this.iframePaneWidth = 50;
            this.applyPaneWidth();
            this.savePaneWidth();
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        });

        resizer.addEventListener('mousedown', onMouseDown);
        resizer.addEventListener('touchstart', onMouseDown, { passive: false });
        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('touchmove', onMouseMove, { passive: false });
        document.addEventListener('mouseup', onMouseUp);
        document.addEventListener('touchend', onMouseUp);
    }

    applyPaneWidth() {
        const terminalWrapper = this.querySelector('.terminal-ui__terminal-wrapper');
        if (terminalWrapper) {
            terminalWrapper.style.width = (100 - this.iframePaneWidth) + '%';
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

    /**
     * Hide the preview placeholder (called when debug WS confirms the page loaded).
     */
    _onPreviewReady() {
        this._previewWaiting = false;
        const placeholder = this.querySelector('.terminal-ui__iframe-placeholder');
        if (placeholder) placeholder.classList.add('hidden');
    }

    /**
     * Reload the preview iframe (used when proxy comes up after being down).
     */
    _reloadPreviewIframe() {
        const iframe = this.querySelector('.terminal-ui__iframe');
        if (iframe && iframe.src) iframe.src = iframe.src;
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
        const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
        const placeholder = this.querySelector('.terminal-ui__iframe-placeholder');

        if (urlInput) {
            // Show full URL in input field
            urlInput.value = url;
            urlInput.title = url;
        }

        if (iframe) {
            // Show placeholder while loading
            if (placeholder) {
                placeholder.classList.remove('hidden');
            }

            iframe.onload = () => {
                if (placeholder) placeholder.classList.add('hidden');
            };

            iframe.src = url;
        }
    }

    /**
     * Set the preview URL bar and iframe src separately.
     * @param {string|null} targetURL - Logical target URL shown in bar (null = home)
     * @param {string|null} iframePath - Override iframe path instead of extracting from targetURL
     */
    setPreviewURL(targetURL, iframePath = null) {
        const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
        const iframe = this.querySelector('.terminal-ui__iframe');
        const placeholder = this.querySelector('.terminal-ui__iframe-placeholder');

        // Determine if targetURL is external (non-localhost)
        let isExternal = false;
        if (targetURL) {
            try {
                const parsed = new URL(targetURL);
                const host = parsed.hostname;
                isExternal = host !== 'localhost' && host !== '127.0.0.1';
            } catch {
                // Bare path like "/foo" — not external
            }
        }

        if (isExternal) {
            // External URLs can't load in iframe (mixed content, X-Frame-Options)
            // Open in new browser tab with confirmation
            if (confirm('Open in new tab?\n\n' + targetURL)) {
                window.open(targetURL, '_blank');
            }
            return;
        }

        // Localhost URLs: route through the proxy shell page
        const previewPort = this.previewPort ? PROXY_PORT_OFFSET + Number(this.previewPort) : null;
        const base = buildPreviewUrl(window.location, previewPort);
        let path;
        if (iframePath !== null) {
            path = iframePath;
        } else if (targetURL) {
            try {
                const parsed = new URL(targetURL);
                path = parsed.pathname + parsed.search + parsed.hash;
            } catch {
                path = targetURL.startsWith('/') ? targetURL : '/' + targetURL;
            }
        } else {
            path = '/';
        }
        const iframeSrc = base + '/__agent-reverse-proxy-debug__/shell?path=' + encodeURIComponent(path);
        this._lastUrlChangeUrl = base + path;

        // URL bar = logical target if provided, else default localhost:{PORT}
        if (urlInput) {
            const defaultTarget = this.previewPort ? `http://localhost:${this.previewPort}` : '';
            const displayUrl = targetURL || defaultTarget;
            urlInput.value = displayUrl;
            urlInput.title = displayUrl;
        }

        if (iframe) {
            // Cancel any in-progress preview probe
            if (this._previewProbeController) {
                this._previewProbeController.abort();
                this._previewProbeController = null;
            }

            // Show placeholder while we probe the proxy
            if (placeholder) placeholder.classList.remove('hidden');
            this._previewWaiting = true;

            // Probe the reverse proxy until it responds with its identity
            // header. The agent-reverse-proxy sets X-Agent-Reverse-Proxy on
            // every response (even 502 when the upstream app isn't ready).
            // Traefik's own 502 (proxy container not started) won't have it,
            // so the probe retries until our proxy is actually serving.
            this._previewProbeController = new AbortController();
            probeUntilReady(base + '/', {
                maxAttempts: 10, baseDelay: 2000, maxDelay: 30000,
                isReady: (resp) => resp.headers.has('X-Agent-Reverse-Proxy'),
                signal: this._previewProbeController.signal,
            }).then(() => {
                if (this.activeTab === 'preview') {
                    iframe.src = iframeSrc;
                } else {
                    // Defer: only load when user switches to Preview tab
                    this._pendingPreviewIframeSrc = iframeSrc;
                    return; // skip onload handler — will be set when user switches to Preview
                }
                // Fallback: dismiss placeholder on iframe load in case the
                // debug WebSocket hasn't connected yet (race condition where
                // the shell page sends urlchange before the UI observer WS
                // joins the hub).
                iframe.onload = () => {
                    if (this._previewWaiting) {
                        this._onPreviewReady();
                    }
                };
            }).catch(() => {
                // Exhausted or aborted — leave placeholder visible
            });
        }
    }

    refreshIframe() {
        if (this._debugWs?.readyState === WebSocket.OPEN) {
            this._debugWs.send(JSON.stringify({ t: 'reload' }));
        } else {
            // Fallback: force reload by resetting src
            const iframe = this.querySelector('.terminal-ui__iframe');
            if (iframe && iframe.src) iframe.src = iframe.src;
        }
    }

    /**
     * Reverse-map a proxy URL to the logical localhost:PORT URL.
     * e.g., https://host:23007/dashboard?tab=1#s → http://localhost:3007/dashboard?tab=1#s
     */
    reverseMapProxyUrl(proxyUrl) {
        if (!this.previewPort) return proxyUrl;
        try {
            const parsed = new URL(proxyUrl);
            return `http://localhost:${this.previewPort}${parsed.pathname}${parsed.search}${parsed.hash}`;
        } catch {
            return proxyUrl;
        }
    }

    /**
     * Connect to the debug WebSocket as a UI observer to receive URL change events.
     */
    connectDebugWebSocket() {
        if (this._debugWs) {
            this._debugWs.close();
            this._debugWs = null;
        }
        if (this._debugWsReconnectTimer) {
            clearTimeout(this._debugWsReconnectTimer);
            this._debugWsReconnectTimer = null;
        }

        const baseUrl = this.getPreviewBaseUrl();
        if (!baseUrl) return;

        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const previewPort = this.previewPort ? PROXY_PORT_OFFSET + Number(this.previewPort) : null;
        const wsUrl = `${wsProtocol}//${window.location.hostname}:${previewPort}/__agent-reverse-proxy-debug__/ui`;

        let ws;
        try {
            ws = new WebSocket(wsUrl);
        } catch {
            this._scheduleDebugWsReconnect();
            return;
        }
        this._debugWs = ws;

        ws.onopen = () => {
            this._debugWsAttempts = 0;
            if (this._debugWsEverConnected) {
                // Reconnect: reload the iframe if we were waiting (may still
                // show a stale page).
                if (this._previewWaiting && this.activeTab === 'preview') {
                    this._reloadPreviewIframe();
                }
            }
            this._debugWsEverConnected = true;
        };

        ws.onmessage = (e) => {
            try {
                const msg = JSON.parse(e.data);
                if (msg.t === 'urlchange' || msg.t === 'init') {
                    // Proxy and shell page confirmed alive — hide placeholder
                    if (this._previewWaiting && this.activeTab === 'preview') {
                        this._onPreviewReady();
                    }
                    const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
                    if (urlInput && this.activeTab === 'preview') {
                        const displayUrl = this.reverseMapProxyUrl(msg.url);
                        urlInput.value = displayUrl;
                        urlInput.title = displayUrl;
                    }
                    // Store for "open in new window"
                    if (msg.url) this._lastUrlChangeUrl = msg.url;
                }
                if (msg.t === 'open' && msg.url) {
                    // xdg-open shim: activate preview tab and navigate via URL bar
                    this.openIframePane('preview', msg.url);
                }
                if (msg.t === 'navstate') {
                    const backBtn = this.querySelector('.terminal-ui__iframe-back');
                    const forwardBtn = this.querySelector('.terminal-ui__iframe-forward');
                    if (backBtn) backBtn.disabled = !msg.canGoBack;
                    if (forwardBtn) forwardBtn.disabled = !msg.canGoForward;
                }
            } catch {
                // Ignore parse errors
            }
        };

        ws.onclose = () => {
            this._debugWs = null;
            // Disable nav buttons when WS disconnects
            const backBtn = this.querySelector('.terminal-ui__iframe-back');
            const forwardBtn = this.querySelector('.terminal-ui__iframe-forward');
            if (backBtn) backBtn.disabled = true;
            if (forwardBtn) forwardBtn.disabled = true;
            // Auto-reconnect — the proxy may not be up yet or may have restarted
            this._scheduleDebugWsReconnect();
        };
    }

    _scheduleDebugWsReconnect() {
        if (!this.previewPort) return;
        this._debugWsAttempts = (this._debugWsAttempts || 0) + 1;
        // Backoff: 1s, 2s, 4s, ... up to 10s
        const delay = Math.min(1000 * Math.pow(2, this._debugWsAttempts - 1), 10000);
        this._debugWsReconnectTimer = setTimeout(() => {
            this._debugWsReconnectTimer = null;
            this.connectDebugWebSocket();
        }, delay);
    }
}

customElements.define('terminal-ui', TerminalUI);
