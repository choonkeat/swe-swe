import { formatDuration, formatFileSize, escapeHtml, escapeFilename } from './modules/util.js';
import { validateUsername, validateSessionName } from './modules/validation.js';
import { deriveShellUUID } from './modules/uuid.js';
import { getBaseUrl, buildVSCodeUrl, buildShellUrl, buildPreviewUrl, buildProxyUrl, buildAgentChatUrl, buildPortBasedPreviewUrl, buildPortBasedAgentChatUrl, buildPortBasedProxyUrl, getDebugQueryString } from './modules/url-builder.js';
import { OPCODE_CHUNK, encodeResize, encodeFileUpload, isChunkMessage, decodeChunkHeader, parseServerMessage } from './modules/messages.js';
import { createReconnectState, getDelay, nextAttempt, resetAttempts, formatCountdown, probeUntilReady } from './modules/reconnect.js';
import { createQueue, enqueue, dequeue, peek, isEmpty as isQueueEmpty, getQueueCount, getQueueInfo, startUploading, stopUploading, clearQueue } from './modules/upload-queue.js';
import { createAssembler, addChunk, isComplete, getReceivedCount, assemble, reset as resetAssembler, getProgress } from './modules/chunk-assembler.js';
import { getStatusBarClasses, renderStatusInfo, renderServiceLinks, renderCustomLinks, renderAssistantLink } from './modules/status-renderer.js';
import { DARK_XTERM_THEME, LIGHT_XTERM_THEME } from './theme-mode.js';

// Strip CSI 3J (\x1b[3J = clear scrollback buffer) from a Uint8Array.
// Claude's TUI emits this during full-screen redraws, causing viewport jumps.
// The sequence is 4 bytes: 0x1b, 0x5b, 0x33, 0x4a
function stripCSI3J(data) {
    // Fast path: scan for ESC byte first
    if (data.indexOf(0x1b) === -1) return data;
    const result = new Uint8Array(data.length);
    let j = 0;
    for (let i = 0; i < data.length; i++) {
        if (i + 3 < data.length &&
            data[i] === 0x1b && data[i+1] === 0x5b &&
            data[i+2] === 0x33 && data[i+3] === 0x4a) {
            i += 3; // skip the 4-byte sequence (loop increments i once more)
            continue;
        }
        result[j++] = data[i];
    }
    if (j === data.length) return data; // nothing stripped, return original
    return result.subarray(0, j);
}

// Preset definitions: slot ids, default pane assignments, and a small SVG
// icon describing the grid layout. The icon's rects describe cells laid out
// on a 12x12 viewBox so buttons can render a mini-diagram of the layout.
// CSS (terminal-ui.css) owns the grid-template-areas for each preset id via
// `.terminal-ui__workspace[data-preset="..."]` selectors.
// Order matters -- it's the display order in the preset picker.
const LAYOUT_PRESETS = {
    classic:      { label: 'Classic',       slots: ['a', 'b'],           defaults: { a: 'agent-terminal', b: 'preview' },                                         icon: [[0,0,1,2], [1,0,1,2]] },
    single:       { label: 'Single',        slots: ['a'],                defaults: { a: 'preview' },                                                              icon: [[0,0,2,2]] },
    'two-row':    { label: '2-row',         slots: ['a', 'b'],           defaults: { a: 'preview',        b: 'agent-terminal' },                                  icon: [[0,0,2,1], [0,1,2,1]] },
    'l-bigR':     { label: 'L + big R',     slots: ['a', 'b'],           defaults: { a: 'agent-terminal', b: 'preview' },                                         icon: [[0,0,1,2], [1,0,2,2]] },
    'stacked-R':  { label: 'Big L, stk R',  slots: ['a', 'b', 'c'],      defaults: { a: 'agent-terminal', b: 'preview',        c: 'agent-chat' },                 icon: [[0,0,2,2], [2,0,1,1], [2,1,1,1]] },
    'stacked-L':  { label: 'Stk L, big R',  slots: ['a', 'b', 'c'],      defaults: { a: 'agent-terminal', b: 'agent-chat',     c: 'preview' },                    icon: [[0,0,1,1], [0,1,1,1], [1,0,2,2]] },
    't-splitBot': { label: 'T + split bot', slots: ['a', 'b', 'c'],      defaults: { a: 'preview',        b: 'agent-terminal', c: 'agent-chat' },                 icon: [[0,0,2,1], [0,1,1,1], [1,1,1,1]] },
    quadrants:    { label: 'Quadrants',     slots: ['a', 'b', 'c', 'd'], defaults: { a: 'agent-terminal', b: 'preview',        c: 'agent-chat', d: 'shell' },     icon: [[0,0,1,1], [1,0,1,1], [0,1,1,1], [1,1,1,1]] },
};

// Build a tiny SVG showing the preset's grid cells on a 12x12 viewBox.
// rects: array of [x, y, w, h] in 1-unit-per-column form (same as mock.html).
function buildPresetIcon(rects) {
    const cells = rects.map(([x, y, w, h]) =>
        `<rect x="${x*6+1}" y="${y*6+1}" width="${w*6-2}" height="${h*6-2}" rx="1" fill="currentColor" opacity="0.75"/>`
    ).join('');
    return `<svg width="14" height="14" viewBox="0 0 13 13" aria-hidden="true">${cells}</svg>`;
}

// Display order for slot tab bars (same in every slot).
const PANES_IN_ORDER = ['agent-terminal', 'agent-chat', 'preview', 'vscode', 'shell', 'browser'];

// Human-readable labels for slot tab bar buttons.
const PANE_LABELS = {
    'agent-terminal': 'Agent Terminal',
    'agent-chat':     'Agent Chat',
    'preview':        'Preview',
    'vscode':         'Code',
    'shell':          'Terminal',
    'browser':        'Agent View',
};

// Braille spinner frames for the Agent Chat tab loading indicator.
// \u escapes to satisfy `make ascii-check`; codepoints are from the
// Braille Patterns block (U+2800-U+28FF) -- the classic 10-frame
// clockwise-rotation spinner (matches cli-spinners "dots").
const CHAT_LOADING_FRAMES = [
    '\u280B', '\u2819', '\u2839', '\u2838', '\u283C',
    '\u2834', '\u2826', '\u2827', '\u2807', '\u280F',
];

// Subset of panes that render iframes. Used by the "legacy activeTab" mirror
// so pre-preset code (setPreviewURL, refreshIframe, VNC probe) can find the
// current right-side iframe without knowing about slots.
const IFRAME_PANES_PRIORITY = ['preview', 'vscode', 'shell', 'browser'];

const LAYOUT_STATE_KEY = 'swe-swe-layout-v1';

// Build a fresh slot-state object (one tab, active) from a pane id.
function slotStateForPane(paneId) {
    return { tabs: [paneId], active: paneId };
}

// Build the activeBySlot map for a preset using its defaults.
function defaultActiveBySlot(preset) {
    const result = {};
    preset.slots.forEach(slotId => {
        result[slotId] = slotStateForPane(preset.defaults[slotId]);
    });
    return result;
}

// Normalize a slot value from localStorage into {tabs, active} shape.
// Accepts the legacy string shape and either field being missing.
function normalizeSlotState(value, fallbackPaneId) {
    if (typeof value === 'string') return slotStateForPane(value);
    if (value && Array.isArray(value.tabs) && value.tabs.length > 0) {
        const active = value.active && value.tabs.includes(value.active)
            ? value.active : value.tabs[0];
        return { tabs: [...value.tabs], active };
    }
    return slotStateForPane(fallbackPaneId);
}

// Layout state persistence. Shape:
//   { preset: string, activeBySlot: {[slot]: {tabs: string[], active: string}} }
// Legacy shape (v1 initial release) stored pane strings directly in
// activeBySlot[slot]; normalizeSlotState handles the migration on read.
// On parse failure or missing preset, fall back to the classic defaults.
function loadLayoutState() {
    try {
        const s = JSON.parse(localStorage.getItem(LAYOUT_STATE_KEY));
        if (s && s.preset && LAYOUT_PRESETS[s.preset] && s.activeBySlot) {
            const preset = LAYOUT_PRESETS[s.preset];
            const activeBySlot = {};
            preset.slots.forEach(slotId => {
                activeBySlot[slotId] = normalizeSlotState(
                    s.activeBySlot[slotId],
                    preset.defaults[slotId]
                );
            });
            return { preset: s.preset, activeBySlot };
        }
    } catch (e) { /* fall through */ }
    return { preset: 'classic', activeBySlot: defaultActiveBySlot(LAYOUT_PRESETS.classic) };
}

function saveLayoutState(state) {
    try { localStorage.setItem(LAYOUT_STATE_KEY, JSON.stringify(state)); }
    catch (e) { /* out of quota / disabled -- layout is session-only */ }
}

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
        this.sessionUUID = null;
        // Port-based proxy mode state
        this._proxyMode = null; // null = undecided, 'port' = per-port, 'path' = path-based
        this.previewProxyPort = null;
        this.agentChatProxyPort = null;
        this.publicPort = null;
        this.cdpPort = null;
        this.vncPort = null;
        this.vncProxyPort = null;
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
        // Preset-driven workspace state. `preset` is one of the LAYOUT_PRESETS ids
        // (classic, single, two-row, l-bigR, stacked-R, stacked-L, t-splitBot,
        // quadrants). `activeBySlot` maps slot id (a/b/c/d) -> pane id
        // (agent-terminal / agent-chat / preview / vscode / shell / browser).
        // Loaded from localStorage; falls back to classic defaults on missing/
        // invalid state. See terminal-ui.css for the grid-template-areas per preset.
        const _layoutState = loadLayoutState();
        this.preset = _layoutState.preset;
        this.activeBySlot = _layoutState.activeBySlot;
        // Legacy mirror of the right-slot assignment -- some pre-preset call
        // sites (setPreviewURL, refreshIframe, browser VNC probe) still reference
        // it. Reflects whichever pane is active in slot b (or the first iframe
        // pane in any other slot in more complex presets).
        this.activeTab = 'preview';
        // Tracks which right-pane iframes have had their src set at least once.
        // Used to skip reloads when switching back to an already-initialized pane.
        this._paneLoaded = new Set();
        // Preset state legacy mirror for compat with existing mobile logic.
        this.leftPanelTab = 'terminal'; // 'terminal' | 'chat' -- mobile only
        // Split-pane constants for desktop detection
        this.MIN_PANEL_WIDTH = 360; // minimum width for a slot
        this.RESIZER_WIDTH = 8;     // historical; no longer used with the grid
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

            // Shell sessions render inside the Terminal pane iframe, so the URL
            // is /session/<shellUUID>?assistant=shell -- which reloads the full
            // terminal-ui custom element. Without an override, the embedded view
            // rehydrates whatever preset + slot state the user's localStorage
            // holds, which includes tab bars for Preview / Agent View / Terminal
            // -- and clicking that inner Terminal recurses, producing the
            // infinite-nesting view. Force single-slot agent-terminal in shell
            // mode and do NOT persist (so localStorage, which belongs to the
            // outer session, is untouched).
            if (this.assistant === 'shell') {
                this.preset = 'single';
                this.activeBySlot = { a: { tabs: ['agent-terminal'], active: 'agent-terminal' } };
            }

            this.render();

            // For chat sessions, drop Agent Chat into its preset home slot
            // immediately (marked as "known" so the tab shows the (Loading)
            // label via _isPaneKnown while the probe runs). activate:false so
            // Agent Terminal keeps initial focus -- probe success handler
            // below flips the active tab to chat once the iframe is usable.
            // _agentChatPending gates _isPaneKnown / (Loading) label;
            // _agentChatProbing is reserved for the in-flight probe so the
            // WS-init probe at line 1231 can run.
            if (new URLSearchParams(location.search).get('session') === 'chat') {
                this._agentChatPending = true;
                this.autoAddPaneToHome('agent-chat', { activate: false });
                this.startChatLoadingAnimation();
            }

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
        if (this._chatLoadingTimer) {
            clearInterval(this._chatLoadingTimer);
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
                            <hr class="settings-panel__divider">
                            <button class="settings-panel__end-session" id="settings-end-session">End Session</button>
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
                        <!-- Labeled group so the preset bunch reads as one
                             thing and doesn't visually bleed into the gear. -->
                        <div class="terminal-ui__preset-picker-group desktop-only">
                            <span class="terminal-ui__preset-picker-label">Layout</span>
                            <div class="terminal-ui__preset-picker"></div>
                        </div>
                        <div class="terminal-ui__header-sep desktop-only" aria-hidden="true"></div>
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
                        <option value="agent-chat" hidden disabled>Agent Chat</option>
                        <option value="preview">App Preview</option>
                        <option value="vscode" hidden disabled>Code</option>
                        <option value="shell">Terminal</option>
                        <option value="browser" hidden disabled>Agent View</option>
                    </select>
                    <span class="terminal-ui__assistant-badge">CLAUDE</span>
                </div>

                <!-- Main Content: preset-driven workspace grid.
                     All 6 panes mount into stable pane-hosts at startup and never
                     reparent; their grid-area is reassigned when the preset or
                     slot assignments change. Slot frames (tab bars) overlay the
                     pane-hosts in the same grid cell via z-index + padding. -->
                <div class="terminal-ui__main-content">
                    <div class="terminal-ui__workspace" data-preset="classic">
                        <!-- Slot frames are rendered dynamically by JS. -->

                        <!-- Agent Terminal pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="agent-terminal">
                            <div class="terminal-ui__terminal">
                                <div class="terminal-ui__drop-overlay">
                                    <div class="terminal-ui__drop-icon">+</div>
                                    <div>Drop file to paste contents</div>
                                </div>
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

                        <!-- Agent Chat pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="agent-chat" hidden>
                            <div class="terminal-ui__agent-chat">
                                <div class="terminal-ui__iframe-placeholder terminal-ui__agent-chat-placeholder">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Connecting to chat...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__agent-chat-iframe"
                                        src="about:blank"
                                        sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"
                                        allow="microphone; autoplay; speech-synthesis">
                                </iframe>
                            </div>
                        </div>

                        <!-- App Preview pane-host (includes preview-only URL bar) -->
                        <div class="terminal-ui__pane-host terminal-ui__pane-host--preview" data-pane="preview">
                            <div class="terminal-ui__iframe-location">
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-home" title="Home">⌂</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-back" title="Back" disabled>◀</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-forward" title="Forward" disabled>▶</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-refresh" title="Refresh">↻</button>
                                <div class="terminal-ui__iframe-url-bar" title="Current URL">
                                    <span class="terminal-ui__iframe-url-prefix"></span>
                                    <input type="text" class="terminal-ui__iframe-url-input" placeholder="/" />
                                </div>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-go" title="Go">→</button>
                                <button class="terminal-ui__iframe-nav-btn terminal-ui__iframe-open-external" title="Open in new window">↗</button>
                            </div>
                            <div class="terminal-ui__iframe-slot" data-pane="preview">
                                <div class="terminal-ui__iframe-placeholder">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Connecting to preview...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" data-pane="preview" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
                            </div>
                        </div>

                        <!-- Code (code-server) pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="vscode" hidden>
                            <div class="terminal-ui__iframe-slot" data-pane="vscode">
                                <div class="terminal-ui__iframe-placeholder hidden">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Loading Code...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" data-pane="vscode" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
                            </div>
                        </div>

                        <!-- Terminal (interactive shell) pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="shell" hidden>
                            <div class="terminal-ui__iframe-slot" data-pane="shell">
                                <div class="terminal-ui__iframe-placeholder hidden">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Loading terminal...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" data-pane="shell" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
                            </div>
                        </div>

                        <!-- Agent View (VNC) pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="browser" hidden>
                            <div class="terminal-ui__iframe-slot" data-pane="browser">
                                <div class="terminal-ui__iframe-placeholder hidden">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Starting browser...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" data-pane="browser" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
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

        // Build services list (filter out entries with null URLs -- e.g. preview before sessionUUID arrives)
        const baseUrl = getBaseUrl(window.location);
        const serviceEntries = [
            
            { name: 'preview', label: 'App Preview', url: this.getPreviewBaseUrl() },
        ];
        if (this.browserStarted) {
            serviceEntries.push({ name: 'browser', label: 'Agent View', url: this.getBrowserViewUrl() });
        }
        const services = serviceEntries.filter(s => s.url != null);

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
        // Forward extra_args param from page URL to WebSocket URL (extra CLI flags appended to agent command)
        const extraArgsParam = new URLSearchParams(location.search).get('extra_args');
        if (extraArgsParam) {
            url += '&extra_args=' + encodeURIComponent(extraArgsParam);
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

            // Parent session not found (e.g., after server reboot) -- retry so
            // the parent tab has a chance to reconnect and recreate first.
            if (event.code === 4001) {
                this.updateStatus('connecting', 'Waiting for parent session...');
                setTimeout(() => this.scheduleReconnect(), 1000);
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
                let combined = new Uint8Array(total);
                let offset = 0;
                for (const arr of this.pendingWrites) {
                    combined.set(arr, offset);
                    offset += arr.length;
                }
                // Fix 5 (Mar 21): Strip CSI 3J (clear scrollback) to prevent
                // viewport jump. Claude's TUI emits \x1b[3J during full-screen
                // redraws, which clears the scrollback buffer and can cause the
                // viewport to jump to the top. Stripping it preserves scrollback
                // (bounded by scrollback limit) and lets xterm.js manage viewport
                // position natively. See research/2026-02-05-xterm-scroll-flicker.md
                combined = stripCSI3J(combined);
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
    handleChunk(data) {
        const { chunkIndex, totalChunks, payload } = decodeChunkHeader(data);

        console.log(`CHUNK ${chunkIndex + 1}/${totalChunks} (${payload.length} bytes)`);

        // Add chunk to assembler
        this.chunkAssemblerState = addChunk(this.chunkAssemblerState, chunkIndex, totalChunks, payload);

        // Show chunk progress in status bar for debugging
        if (totalChunks > 1) {
            const progress = getProgress(this.chunkAssemblerState);
            this.debugLog(`Receiving snapshot: ${progress.received}/${progress.total}`, 2000);
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
            this.debugLog(`Snapshot loaded: ${decompressed.length} bytes`, 2000);
            this.term.write(decompressed, () => {
                this.term.scrollToBottom();
            });
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
                this.updateUrlBarPrefix();
                this.agentChatPort = msg.agentChatPort || null;
                this.sessionUUID = msg.sessionUUID || null;
                this.previewProxyPort = msg.previewProxyPort || null;
                this.agentChatProxyPort = msg.agentChatProxyPort || null;
                this.publicPort = msg.publicPort || null;
                this.cdpPort = msg.cdpPort || null;
                this.vncPort = msg.vncPort || null;
                this.vncProxyPort = msg.vncProxyPort || null;
                // Browser (CDP chrome) just came online -- show the Agent View
                // tab and auto-add it to its preset-defined home slot (same
                // spirit as the old "open right panel" auto-switch).
                if (msg.browserStarted && !this.browserStarted) {
                    this.browserStarted = true;
                    this.setAgentViewTabVisible(true);
                    this.autoAddPaneToHome('browser');
                } else if (!this.browserStarted) {
                    this.browserStarted = false;
                    this._browserViewReady = false;
                }
                // Probe agent chat proxy -- two-phase: path-based first, then try port-based.
                // agentChatPort is only sent for session=chat, so terminal sessions skip this.
                if (this.sessionUUID && this.agentChatPort && !this._agentChatAvailable && !this._agentChatProbing) {
                    this._agentChatProbing = true;
                    this._rerenderSlotTabs();
                    const acPathUrl = buildAgentChatUrl(getBaseUrl(window.location), this.sessionUUID);
                    const acPortUrl = buildPortBasedAgentChatUrl(window.location, this.agentChatProxyPort);
                    if (acPathUrl) {
                        this._agentChatProbeController = new AbortController();
                        // Phase 1: probe path-based URL to wait for proxy handler to be up
                        probeUntilReady(acPathUrl + '/', {
                            method: 'GET',
                            maxAttempts: 10, baseDelay: 2000, maxDelay: 30000,
                            isReady: (resp) => resp.ok && resp.headers.has('X-Agent-Reverse-Proxy'),
                            signal: this._agentChatProbeController.signal,
                        }).then(() => {
                            // Phase 2: quick probe port-based URL to determine mode.
                            // Uses /__probe__ which bypasses ForwardAuth in Traefik -- avoids
                            // Safari's stricter cross-port CORS+credentials blocking.
                            let chosenUrl = acPathUrl;
                            if (acPortUrl) {
                                return fetch(acPortUrl + '/__probe__', { method: 'GET', mode: 'cors' })
                                    .then(resp => {
                                        if (resp.headers.has('X-Agent-Reverse-Proxy')) {
                                            chosenUrl = acPortUrl;
                                            this._acProxyMode = 'port';
                                        } else {
                                            this._acProxyMode = 'path';
                                        }
                                    })
                                    .catch(() => { this._acProxyMode = 'path'; })
                                    .then(() => chosenUrl);
                            }
                            this._acProxyMode = 'path';
                            return chosenUrl;
                        }).then((chosenUrl) => {
                            this._agentChatAvailable = true;
                            this._agentChatProbing = false;
                            this._agentChatPending = false;
                            this.setAgentChatTabVisible(true);
                            const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
                            if (chatIframe) {
                                chatIframe.src = (chosenUrl || acPathUrl) + '/';
                                chatIframe.onload = () => {
                                    const ph = this.querySelector('.terminal-ui__agent-chat-placeholder');
                                    if (ph) ph.classList.add('hidden');
                                    // Loading complete -- remove loading indicator and activate tab
                                    this.stopChatLoadingAnimation();
                                    if (new URLSearchParams(location.search).get('session') === 'chat') {
                                        // Slot-model activation: page-load auto-add used
                                        // activate:false to keep Agent Terminal focused
                                        // while the probe ran; now the iframe is usable,
                                        // flip the slot's active tab to Agent Chat.
                                        // persist:false -- treat this flip as ephemeral
                                        // session intent so it doesn't leave agent-chat
                                        // baked into localStorage for the next visit.
                                        const chatSlot = this._slotForPane('agent-chat');
                                        if (chatSlot) this.setActiveInSlot(chatSlot, 'agent-chat', { persist: false });
                                        this.switchLeftPanelTab('chat');
                                        this.switchMobileNav('agent-chat');
                                    }
                                };
                            }
                        }).catch(() => {
                            this._agentChatProbing = false;
                            this._agentChatPending = false;
                            this._rerenderSlotTabs();
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
                // Load preview once we have sessionUUID + previewPort. applyPreset()
                // runs at page load and calls _loadPaneIfNeeded('preview') before the
                // WS init arrives; setPreviewURL(null) silently bails out then because
                // sessionUUID is null. Retry here with the now-known session so the
                // iframe src actually gets set and the "Connecting to preview..."
                // placeholder clears.
                if (this.previewPort && this.sessionUUID && this._slotForPane('preview')) {
                    const previewIframe = this._iframeFor('preview');
                    const previewSrc = previewIframe ? previewIframe.getAttribute('src') : null;
                    if (!previewSrc) {
                        this._wantsPreviewOnConnect = false;
                        this._paneLoaded.add('preview');
                        setTimeout(() => this.setPreviewURL(null), 100);
                    } else if (this.previewBaseUrl !== prevPreviewBaseUrl && this.activeTab === 'preview') {
                        // Port changed while preview is active -- refresh with current target.
                        const currentTarget = this.querySelector('.terminal-ui__iframe-url-input')?.value?.trim() || null;
                        this.setPreviewURL(currentTarget);
                    }
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
                // Show toggle with mode label inside: "NAME [* normal]" or "NAME [yolo *]"
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

        // Close on X button click
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

        // End Session button
        const endSessionBtn = panel.querySelector('#settings-end-session');
        if (endSessionBtn) {
            endSessionBtn.addEventListener('click', () => {
                this.closeSettingsPanel();
                const uuid = this.sessionUUID;
                if (!uuid) {
                    window.location.href = '/';
                    return;
                }
                checkPublicPortAndEndSession({
                    uuid: uuid,
                    publicPort: this.publicPort,
                    onSuccess: function() {
                        window.location.href = '/';
                    }
                });
            });
        }

    }

    // Setup event listeners for the new header and navigation UI
    setupHeaderEventListeners() {
        // Session name click -> rename session
        const sessionName = this.querySelector('.terminal-ui__session-name');
        if (sessionName) {
            sessionName.addEventListener('click', () => {
                this.promptRenameSession();
            });
        }

        // Settings button -> open settings panel
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

        this.activeTab = tab;
        this._showPaneSlot(tab);

        if (tab === 'preview') {
            if (this._pendingPreviewIframeSrc) {
                // Preview probe already succeeded while on another tab -- apply stashed URL
                const pendingSrc = this._pendingPreviewIframeSrc;
                this._pendingPreviewIframeSrc = null;
                const iframe = this._iframeFor('preview');
                if (iframe) {
                    iframe.src = pendingSrc;
                    iframe.onload = () => {
                        if (this._previewWaiting) this._onPreviewReady();
                    };
                }
                this._paneLoaded.add('preview');
            } else if (!this._paneLoaded.has('preview')) {
                this._paneLoaded.add('preview');
                this.setPreviewURL(null);
            }
            // else: already loaded -- state is preserved, just showing the slot
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
                    url = this.getBrowserViewUrl();
                    if (url && !this._browserViewReady) {
                        // Probe VNC readiness via same-origin endpoint to get real status codes.
                        // Direct cross-origin probes to the VNC port return opaque responses
                        // (mode: 'no-cors'), making 502 Bad Gateway indistinguishable from 200.
                        const placeholder = this._placeholderFor('browser');
                        const placeholderText = placeholder ? placeholder.querySelector('.terminal-ui__iframe-placeholder-text') : null;
                        if (placeholder) placeholder.classList.remove('hidden');
                        if (placeholderText) placeholderText.textContent = 'Starting browser...';
                        if (!this._browserViewProbing) {
                            this._browserViewProbing = true;
                            const probeUrl = `${baseUrl}/api/session/${this.uuid}/vnc-ready`;
                            probeUntilReady(probeUrl, {
                                method: 'GET',
                                maxAttempts: 30,
                                baseDelay: 1000,
                                maxDelay: 5000,
                            }).then(() => {
                                this._browserViewReady = true;
                                this._browserViewProbing = false;
                                if (this.activeTab === 'browser') {
                                    this.setIframeUrl(this.getBrowserViewUrl(), 'browser');
                                }
                            }).catch(() => {
                                this._browserViewProbing = false;
                            });
                        }
                        this.updateActiveTabIndicator();
                        const panelDropdown = this.querySelector('.terminal-ui__panel-select');
                        if (panelDropdown) panelDropdown.value = tab;
                        return;
                    }
                    break;
                default:
                    return;
            }
            this.setIframeUrl(url, tab);
        }

        this.updateActiveTabIndicator();

        // Update mobile dropdown to match
        const panelDropdown = this.querySelector('.terminal-ui__panel-select');
        if (panelDropdown) {
            panelDropdown.value = tab;
        }

        // Re-fit terminal after layout change
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    }

    // Switch left panel between terminal and chat
    switchLeftPanelTab(tab) {
        if (tab !== this.leftPanelTab) {
            this.leftPanelTab = tab;

            const terminalUi = this.querySelector('.terminal-ui');
            const terminalEl = this.querySelector('.terminal-ui__terminal');
            const chatEl = this.querySelector('.terminal-ui__agent-chat');
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

            // Visibility is handled by the preset-grid pane-host `hidden`
            // attribute (see applyPreset) -- this function used to also set
            // inline `visibility:hidden` / `position:absolute` on the inner
            // .terminal-ui__terminal, but those styles survived slot-tab
            // switches (which go through setActiveInSlot, not this method),
            // leaving Agent Terminal blank after the first chat activation.
            if (tab === 'terminal') setTimeout(() => this.fitAndPreserveScroll(), 50);

            // Sync mobile dropdown
            const mobileSelect = this.querySelector('.terminal-ui__mobile-nav-select');
            if (mobileSelect) {
                mobileSelect.value = tab === 'chat' ? 'agent-chat' : 'agent-terminal';
            }
        }

        // Focus always transfers -- whether tab switched via click or programmatically
        if (tab === 'chat') {
            const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
            if (chatIframe && chatIframe.contentWindow) {
                chatIframe.contentWindow.focus();
                try {
                    const activeEl = chatIframe.contentWindow.document.activeElement;
                    if (activeEl && activeEl !== chatIframe.contentWindow.document.body) {
                        activeEl.focus();
                    }
                } catch (e) { /* cross-origin: ignore */ }
            }
        } else {
            if (this.term) this.term.focus();
        }
    }

    // Single source of truth for the Agent Chat tab availability. Flips the
    // internal flag, re-renders every slot tab bar so chat appears as a valid
    // replace-option, and syncs the mobile dropdown.
    setAgentChatTabVisible(visible) {
        this._agentChatAvailable = !!visible;
        this._rerenderSlotTabs();
        // iOS Safari ignores display:none on <option> in native pickers;
        // use hidden+disabled attributes which Safari does respect.
        const mobileOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="agent-chat"]');
        if (mobileOpt) {
            mobileOpt.hidden = !visible;
            mobileOpt.disabled = !visible;
        }
    }

    // Show/hide the Agent View tab (slot tab bars + mobile dropdown option).
    setAgentViewTabVisible(visible) {
        this._rerenderSlotTabs();
        const mobileOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="browser"]');
        if (mobileOpt) {
            mobileOpt.hidden = !visible;
            mobileOpt.disabled = !visible;
        }
    }

    // Advance the braille spinner on every Agent Chat tab label + the mobile
    // nav option. Cheaper than _rerenderSlotTabs() each tick: we only mutate
    // the label span's textContent, not the whole tab bar DOM.
    startChatLoadingAnimation() {
        this._chatLoadingFrame = 0;
        const tick = () => {
            this._chatLoadingFrame = (this._chatLoadingFrame + 1) % CHAT_LOADING_FRAMES.length;
            const label = this._paneTabLabel('agent-chat');
            this.querySelectorAll('.terminal-ui__slot-tab[data-pane="agent-chat"] .terminal-ui__slot-tab-label').forEach(el => {
                el.textContent = label;
            });
            const mobileOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="agent-chat"]');
            if (mobileOpt) mobileOpt.textContent = label;
        };
        this._chatLoadingTimer = setInterval(tick, 100);
    }

    stopChatLoadingAnimation() {
        if (this._chatLoadingTimer) {
            clearInterval(this._chatLoadingTimer);
            this._chatLoadingTimer = null;
        }
        this._chatLoadingFrame = 0;
        // Now that _agentChatAvailable is true (or _agentChat{Probing,Pending}
        // are both false on probe failure), _paneTabLabel drops the spinner
        // automatically -- rerender so any tab bar instances catch up.
        this._rerenderSlotTabs();
        const mobileOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="agent-chat"]');
        if (mobileOpt) mobileOpt.textContent = 'Agent Chat';
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

        // Main row key buttons (Esc, Tab, Shift+Tab)
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
            // When user says Stop/Cancel in Agent Chat, send Esc Esc to abort
            // the current tool, then type the supplied text (e.g. a nudge or
            // a slash-command like /clear). Falls back to the default nudge
            // if the sender did not supply text (legacy agent-chat clients).
            if (e.data && e.data.type === 'agent-chat-interrupt') {
                this.sendKey('\x1b');
                this.sendKey('\x1b');
                setTimeout(() => {
                    this.sendKey(e.data.text || 'check_messages; i sent u a chat message');
                    setTimeout(() => this.sendKey('\r'), 300);
                }, 300);
            }
            // When user confirms "clear context" in Agent Chat, run /clear
            if (e.data && e.data.type === 'agent-chat-clear') {
                this.sendKey('/clear');
                setTimeout(() => this.sendKey('\r'), 300);
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
        // Scope drag/drop to the xterm pane only, so drops on Agent Chat
        // (which has its own scoped drop zone) don't get captured here.
        const container = this.querySelector('.terminal-ui__terminal');
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

    // === Preset grid methods ===

    // Apply the current preset + slot assignments to the DOM. Safe to call
    // repeatedly; idempotent when state is unchanged.
    applyPreset() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;

        const preset = LAYOUT_PRESETS[this.preset] || LAYOUT_PRESETS.classic;
        workspace.dataset.preset = this.preset;

        // Header preset picker (one button per preset; active highlighted).
        this._renderPresetPicker();

        // Render slot frames (tab bars) for the active preset.
        this._renderSlotFrames(preset);

        // Assign each pane-host to its slot grid-area if this pane is the
        // active tab for some slot. Panes that are in a slot's tabs list but
        // not active stay mounted but hidden.
        const activePaneBySlot = {};
        for (const [slotId, state] of Object.entries(this.activeBySlot)) {
            if (state && state.active) activePaneBySlot[state.active] = slotId;
        }
        const paneHosts = this.querySelectorAll('.terminal-ui__pane-host');
        paneHosts.forEach(host => {
            const paneId = host.dataset.pane;
            const slotId = activePaneBySlot[paneId];
            if (slotId) {
                host.style.gridArea = slotId;
                host.hidden = false;
                host.dataset.slot = slotId;
            } else {
                host.style.gridArea = '';
                host.hidden = true;
                delete host.dataset.slot;
            }
        });

        // Clear legacy inline styles on the inner left-panel elements that
        // pre-preset switchLeftPanelTab used to write. Stale styles would
        // leave Agent Terminal blank (visibility:hidden) after the first
        // chat activation because slot-tab switches bypass that function.
        const termInner = this.querySelector('.terminal-ui__terminal');
        if (termInner) {
            termInner.style.visibility = '';
            termInner.style.position = '';
        }
        const chatInner = this.querySelector('.terminal-ui__agent-chat');
        if (chatInner) {
            chatInner.style.display = '';
        }

        // Keep legacy activeTab in sync for pre-preset code paths.
        this._syncLegacyActiveTab();

        // Trigger initial iframe load for each active pane in a slot. Non-active
        // tabs stay lazy until their tab is activated.
        for (const state of Object.values(this.activeBySlot)) {
            if (state && state.active) this._loadPaneIfNeeded(state.active);
        }

        // Refit xterm if agent-terminal is now visible in a slot.
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    }

    // Which slot (if any) currently has this pane in its tab list.
    _slotForPane(paneId) {
        for (const [slotId, state] of Object.entries(this.activeBySlot)) {
            if (state && state.tabs && state.tabs.includes(paneId)) return slotId;
        }
        return null;
    }

    // Activate a tab that's already in a slot's tab list. Live -- no reload.
    // Just re-applies the preset so the now-active pane swaps into the slot's
    // grid cell and the previously-active one is hidden.
    //
    // `persist` (default true) controls whether the change is written to
    // localStorage. Pass {persist:false} for ephemeral auto-activations (e.g.
    // the ?session=chat probe-success flip) so they don't pollute the user's
    // saved layout -- otherwise the next visit re-opens with agent-chat
    // already active, defeating the "Agent Terminal stays active during
    // probe" behavior.
    setActiveInSlot(slotId, paneId, { persist = true } = {}) {
        const state = this.activeBySlot[slotId];
        if (!state || !state.tabs.includes(paneId)) return;
        if (state.active === paneId) return;
        state.active = paneId;
        if (persist) {
            saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot });
        }
        this.applyPreset();
        this._loadPaneIfNeeded(paneId);
    }

    // Add a pane as a new tab in a slot. If the pane already lives in another
    // slot, it's removed from there first (a pane lives in exactly one slot's
    // tab list). `activate` (default true) controls whether the newly-added
    // pane becomes the active tab; pass false for page-load auto-adds that
    // shouldn't steal focus from the slot's preset default.
    // `persist` (default true) controls whether the result is written to
    // localStorage. Pass {persist:false} for ephemeral auto-open paths (e.g.
    // browserStarted auto-adding Agent View, or session=chat pre-probe) so
    // the user's next visit isn't primed with an auto-opened pane they never
    // actually asked for.
    addPaneToSlot(slotId, paneId, { activate = true, persist = true } = {}) {
        const target = this.activeBySlot[slotId];
        if (!target) return;
        // Remove from any other slot to keep the "one pane, one slot" rule.
        for (const [sid, st] of Object.entries(this.activeBySlot)) {
            if (sid === slotId || !st || !st.tabs) continue;
            const idx = st.tabs.indexOf(paneId);
            if (idx < 0) continue;
            st.tabs.splice(idx, 1);
            if (st.active === paneId) {
                st.active = st.tabs[Math.min(idx, st.tabs.length - 1)] || null;
            }
        }
        if (!target.tabs.includes(paneId)) target.tabs.push(paneId);
        if (activate || !target.active) target.active = paneId;
        if (persist) {
            saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot });
        }
        this.applyPreset();
        if (target.active === paneId) this._loadPaneIfNeeded(paneId);
    }

    // Remove a tab from a slot (x button on the tab). Pane becomes unassigned
    // (reappears in other slots' "+" popovers). Live -- no reload.
    removeTabFromSlot(slotId, paneId) {
        const state = this.activeBySlot[slotId];
        if (!state || !state.tabs) return;
        const idx = state.tabs.indexOf(paneId);
        if (idx < 0) return;
        state.tabs.splice(idx, 1);
        if (state.active === paneId) {
            state.active = state.tabs[Math.min(idx, state.tabs.length - 1)] || null;
        }
        saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot });
        this.applyPreset();
    }

    // Back-compat adapter: some older call sites still say "assignPaneToSlot".
    // Now that means "add + activate".
    assignPaneToSlot(paneId, slotId) {
        this.addPaneToSlot(slotId, paneId);
    }

    // Return the slot id where a pane should "live" in the current preset.
    // Priority:
    //   1. Preset's defaults declares the pane explicitly -> use that slot.
    //   2. Chat-like panes (agent-terminal/agent-chat) share the slot that
    //      holds agent-terminal in defaults ("old left panel").
    //   3. Iframe-like panes (preview/vscode/shell/browser) share the slot
    //      that holds preview in defaults ("old right panel").
    //   4. Fall back to the preset's first slot.
    _paneHome(paneId) {
        const preset = LAYOUT_PRESETS[this.preset] || LAYOUT_PRESETS.classic;
        for (const [slotId, defaultPane] of Object.entries(preset.defaults)) {
            if (defaultPane === paneId) return slotId;
        }
        const isChatLike = (paneId === 'agent-terminal' || paneId === 'agent-chat');
        const companion = isChatLike ? 'agent-terminal' : 'preview';
        for (const [slotId, defaultPane] of Object.entries(preset.defaults)) {
            if (defaultPane === companion) return slotId;
        }
        return preset.slots[0];
    }

    // Add a pane to its preset-defined home slot. Used for auto-open paths:
    // `?session=chat` (Agent Chat, activate=false so Agent Terminal keeps
    // initial focus while chat probe runs) and browserStarted (Agent View
    // from playwright CDP lazy-load, activate=true since it fires on user
    // action and should take focus).
    //
    // persist:false by default -- auto-opens are session-driven, not user
    // intent; we don't want to leave Agent View (or any auto-opened pane)
    // baked into localStorage so the NEXT session re-opens it even if the
    // browser/chat proxy isn't running.
    autoAddPaneToHome(paneId, { activate = true, persist = false } = {}) {
        const slotId = this._paneHome(paneId);
        if (!slotId) return;
        this.addPaneToSlot(slotId, paneId, { activate, persist });
    }

    // Lazy-load an iframe pane the first time it becomes active. Agent-terminal
    // and agent-chat are already mounted and don't need URL kicks.
    _loadPaneIfNeeded(paneId) {
        if (this._paneLoaded.has(paneId)) return;
        const baseUrl = getBaseUrl(window.location);
        switch (paneId) {
            case 'preview':
                // Defer if sessionUUID hasn't arrived yet -- the WS init handler
                // will call us again once it does. Marking as loaded prematurely
                // would silently drop the load (setPreviewURL bails on null base).
                if (!this.sessionUUID) return;
                this._paneLoaded.add('preview');
                this.setPreviewURL(null);
                break;
            case 'vscode':
                this._paneLoaded.add('vscode');
                this.setIframeUrl(buildVSCodeUrl(baseUrl, this.workDir), 'vscode');
                break;
            case 'shell': {
                this._paneLoaded.add('shell');
                const shellUUID = deriveShellUUID(this.uuid);
                const url = buildShellUrl({ baseUrl, shellUUID, parentUUID: this.uuid, debug: this.debugMode });
                this.setIframeUrl(url, 'shell');
                break;
            }
            case 'browser': {
                const url = this.getBrowserViewUrl();
                if (!url) return;
                if (this._browserViewReady) {
                    this._paneLoaded.add('browser');
                    this.setIframeUrl(url, 'browser');
                    return;
                }
                // VNC readiness probe -- see the original browser path in
                // switchPanelTab for rationale (cross-origin probes return
                // opaque responses).
                const placeholder = this._placeholderFor('browser');
                const placeholderText = placeholder ? placeholder.querySelector('.terminal-ui__iframe-placeholder-text') : null;
                if (placeholder) placeholder.classList.remove('hidden');
                if (placeholderText) placeholderText.textContent = 'Starting browser...';
                if (!this._browserViewProbing) {
                    this._browserViewProbing = true;
                    const probeUrl = `${baseUrl}/api/session/${this.uuid}/vnc-ready`;
                    probeUntilReady(probeUrl, {
                        method: 'GET',
                        maxAttempts: 30,
                        baseDelay: 1000,
                        maxDelay: 5000,
                    }).then(() => {
                        this._browserViewReady = true;
                        this._browserViewProbing = false;
                        if (this._slotForPane('browser')) {
                            this._paneLoaded.add('browser');
                            this.setIframeUrl(this.getBrowserViewUrl(), 'browser');
                        }
                    }).catch(() => {
                        this._browserViewProbing = false;
                    });
                }
                break;
            }
            default:
                // agent-terminal, agent-chat -- pre-mounted; no URL kick needed.
                break;
        }
    }

    // Change preset: merge overlapping slot assignments (per TASK.md:
    // "When preset changes, carry over slot assignments where slot IDs
    // overlap; fill new slots with the preset's defaults"), persist,
    // reload. Slot tab lists carry over intact; new slots get a single tab
    // from the preset's default.
    changePreset(newPresetId) {
        const newPreset = LAYOUT_PRESETS[newPresetId];
        if (!newPreset) return;
        if (newPresetId === this.preset) return;
        const newActiveBySlot = {};
        newPreset.slots.forEach(slotId => {
            newActiveBySlot[slotId] = this.activeBySlot[slotId]
                || slotStateForPane(newPreset.defaults[slotId]);
        });
        saveLayoutState({ preset: newPresetId, activeBySlot: newActiveBySlot });
        window.location.reload();
    }

    // Render the preset picker icon row in the header.
    _renderPresetPicker() {
        const container = this.querySelector('.terminal-ui__preset-picker');
        if (!container) return;
        container.innerHTML = '';
        Object.entries(LAYOUT_PRESETS).forEach(([presetId, spec]) => {
            const btn = document.createElement('button');
            btn.className = 'terminal-ui__preset-btn';
            btn.type = 'button';
            btn.dataset.preset = presetId;
            btn.title = spec.label;
            if (presetId === this.preset) btn.classList.add('active');
            btn.innerHTML = buildPresetIcon(spec.icon);
            btn.addEventListener('click', () => this.changePreset(presetId));
            container.appendChild(btn);
        });
    }

    // Render slot frames (tab bars) for the active preset. Each slot-frame
    // overlays its pane-host in the same grid cell (z-index + pointer-events
    // scoping keep the pane-host clickable beneath the tab buttons).
    _renderSlotFrames(preset) {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        // Remove any stale slot frames.
        workspace.querySelectorAll('.terminal-ui__slot-frame').forEach(el => el.remove());
        // Build one per slot in the active preset.
        preset.slots.forEach(slotId => {
            const frame = document.createElement('div');
            frame.className = 'terminal-ui__slot-frame';
            frame.dataset.slot = slotId;
            frame.style.gridArea = slotId;
            frame.appendChild(this._buildSlotTabBar(slotId));
            workspace.appendChild(frame);
        });
    }

    // Build one slot's tab bar. Each pane in the slot's `tabs` list renders
    // as a tab (active one highlighted), with an "x" to close it. A "+" at
    // the end opens a popover of panes not in any slot (dumping ground);
    // clicking adds a new tab to this slot and activates it.
    _buildSlotTabBar(slotId) {
        const bar = document.createElement('div');
        bar.className = 'terminal-ui__slot-tab-bar';
        bar.dataset.slot = slotId;
        const state = this.activeBySlot[slotId] || { tabs: [], active: null };

        (state.tabs || []).forEach(paneId => {
            const btn = document.createElement('button');
            btn.className = 'terminal-ui__slot-tab';
            btn.dataset.pane = paneId;
            btn.type = 'button';
            if (paneId === state.active) btn.classList.add('active');
            if (!this._isPaneKnown(paneId)) btn.classList.add('unavailable');

            const label = document.createElement('span');
            label.className = 'terminal-ui__slot-tab-label';
            label.textContent = this._paneTabLabel(paneId);
            btn.appendChild(label);

            const close = document.createElement('span');
            close.className = 'terminal-ui__slot-tab-close';
            close.textContent = '\u00D7';
            close.title = 'Remove tab';
            close.addEventListener('click', (e) => {
                e.stopPropagation();
                this.removeTabFromSlot(slotId, paneId);
            });
            btn.appendChild(close);

            btn.addEventListener('click', () => this.setActiveInSlot(slotId, paneId));
            bar.appendChild(btn);
        });

        // "+" opens a popover of panes not in any slot's tab list.
        const panesInAnySlot = new Set();
        for (const st of Object.values(this.activeBySlot)) {
            if (st && st.tabs) for (const p of st.tabs) panesInAnySlot.add(p);
        }
        const unassigned = PANES_IN_ORDER.filter(p => !panesInAnySlot.has(p) && this._isPaneKnown(p));
        if (unassigned.length > 0) {
            const addBtn = document.createElement('button');
            addBtn.className = 'terminal-ui__slot-add';
            addBtn.type = 'button';
            addBtn.title = 'Add tab';
            addBtn.textContent = '+';
            addBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this._showSlotReplaceMenu(slotId, unassigned, addBtn);
            });
            bar.appendChild(addBtn);
        }

        return bar;
    }

    // Returns the display label for a pane tab. For agent-chat during its
    // probe, appends a braille spinner frame (driven by
    // startChatLoadingAnimation ticking _chatLoadingFrame) so the user sees
    // the tab "working" without the static "(Loading)" noise.
    _paneTabLabel(paneId) {
        let label = PANE_LABELS[paneId] || paneId;
        if (paneId === 'agent-chat' && !this._agentChatAvailable && (this._agentChatProbing || this._agentChatPending)) {
            const frame = CHAT_LOADING_FRAMES[(this._chatLoadingFrame || 0) % CHAT_LOADING_FRAMES.length];
            label += ' ' + frame;
        }
        return label;
    }

    // A pane is "known" and can appear in tab bars if it's available or being
    // probed. agent-terminal / preview / shell are always known.
    _isPaneKnown(paneId) {
        if (paneId === 'agent-chat') return this._agentChatAvailable || !!this._agentChatProbing || !!this._agentChatPending;
        if (paneId === 'browser') return !!this.vncProxyPort;
        if (paneId === 'vscode') return this._vscodeEnabled();
        return true;
    }

    // Show a small popover anchored to the slot's "+" button listing each
    // unassigned pane. Clicking one replaces this slot's current pane.
    _showSlotReplaceMenu(slotId, unassigned, anchor) {
        const existing = document.querySelector('.terminal-ui__slot-replace-menu');
        if (existing) existing.remove();

        const menu = document.createElement('div');
        menu.className = 'terminal-ui__slot-replace-menu';
        const rect = anchor.getBoundingClientRect();
        menu.style.position = 'fixed';
        menu.style.top = `${Math.round(rect.bottom + 2)}px`;
        menu.style.left = `${Math.round(rect.left)}px`;

        unassigned.forEach(paneId => {
            const item = document.createElement('button');
            item.className = 'terminal-ui__slot-replace-item';
            item.type = 'button';
            item.textContent = this._paneTabLabel(paneId);
            item.addEventListener('click', () => {
                menu.remove();
                this.assignPaneToSlot(paneId, slotId);
            });
            menu.appendChild(item);
        });

        document.body.appendChild(menu);
        // Dismiss on any outside click.
        const dismiss = (e) => {
            if (!menu.contains(e.target)) {
                menu.remove();
                document.removeEventListener('mousedown', dismiss, true);
            }
        };
        setTimeout(() => document.addEventListener('mousedown', dismiss, true), 0);
    }

    // Re-render slot frames + header picker. Useful when probe state changes
    // (chat/vnc/vscode becoming available mid-session).
    _rerenderSlotTabs() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        const preset = LAYOUT_PRESETS[this.preset] || LAYOUT_PRESETS.classic;
        this._renderSlotFrames(preset);
    }

    // vscode (code-server) is only shown when the init variant asks for it.
    // The server-rendered mobile <option> uses a Go-template placeholder that
    // collapses to empty-string (enabled) or `hidden disabled` (disabled), so
    // we mirror that signal here.
    _vscodeEnabled() {
        const opt = this.querySelector('.terminal-ui__mobile-nav-select option[value="vscode"]');
        if (!opt) return false;
        return !(opt.hidden || opt.disabled);
    }

    // Mirror the first iframe-capable pane into this.activeTab so pre-preset
    // call sites (setPreviewURL, refreshIframe, browser probe) keep working.
    _syncLegacyActiveTab() {
        for (const pane of IFRAME_PANES_PRIORITY) {
            if (this._slotForPane(pane)) { this.activeTab = pane; return; }
        }
        this.activeTab = null;
    }

    // === Split-Pane UI Methods ===

    // Per-pane iframe lookups: each pane ("preview", "vscode", "shell", "browser")
    // has its own <iframe> and placeholder, all mounted at once and toggled via
    // the [hidden] attribute on the slot wrapper. This preserves state across
    // tab switches -- src is set once per pane and not reset.
    _iframeFor(pane) {
        if (!pane) return null;
        return this.querySelector(`.terminal-ui__iframe[data-pane="${pane}"]`);
    }

    _slotFor(pane) {
        if (!pane) return null;
        return this.querySelector(`.terminal-ui__iframe-slot[data-pane="${pane}"]`);
    }

    _placeholderFor(pane) {
        const slot = this._slotFor(pane);
        return slot ? slot.querySelector('.terminal-ui__iframe-placeholder') : null;
    }

    _showPaneSlot(pane) {
        this.querySelectorAll('.terminal-ui__iframe-slot').forEach(el => {
            el.hidden = (el.dataset.pane !== pane);
        });
    }

    _hideAllPaneSlots() {
        this.querySelectorAll('.terminal-ui__iframe-slot').forEach(el => {
            el.hidden = true;
        });
    }

    getBrowserViewUrl() {
        if (!this.vncProxyPort) return null;
        const loc = window.location;
        // noVNC's vnc_lite.html with query params to configure WebSocket connection
        const v = new URL(import.meta.url).searchParams.get('v') || '';
        return `${loc.protocol}//${loc.hostname}:${this.vncProxyPort}/vnc_lite.html?host=${loc.hostname}&port=${this.vncProxyPort}&reconnect=true&resize=scale&autoconnect=true${v ? '&v=' + v : ''}`;
    }

    getPreviewBaseUrl() {
        if (this._proxyMode === 'port' && this.previewProxyPort) {
            return buildPortBasedPreviewUrl(window.location, this.previewProxyPort);
        }
        return buildPreviewUrl(getBaseUrl(window.location), this.sessionUUID);
    }

    updatePreviewBaseUrl() {
        this.previewBaseUrl = this.getPreviewBaseUrl();
    }

    initSplitPaneUi() {
        // Initialize split-pane UI infrastructure
        // The iframe pane starts hidden (terminal at 100% width)
        // Phase 2 will add toggle behavior via service link clicks

        // Probe for the preview iframe; its absence means the split-pane
        // structure isn't mounted in this variant.
        if (!this._iframeFor('preview')) {
            return;
        }

        // Render the preset grid (tab bars + pane-host slot assignments).
        this.applyPreset();

        // Initialize mobile-nav state so the `data-mobile-active` pane-host
        // attribute is set from page load. Without this the mobile viewport
        // shows all pane-hosts display:none until the user changes the
        // dropdown. Harmless on desktop (the attribute doesn't participate in
        // desktop grid layout).
        const mobileSel = this.querySelector('.terminal-ui__mobile-nav-select');
        if (mobileSel) {
            const initialMobilePane = new URLSearchParams(location.search).get('session') === 'chat'
                ? 'agent-chat'
                : (mobileSel.value || 'agent-terminal');
            this.switchMobileNav(initialMobilePane);
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

        // Setup Back/Forward buttons -- send navigate commands via debug WebSocket
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
                    // External URLs can't load in iframe -- open in new tab
                    if (confirm('Open in new tab?\n\n' + targetUrl)) {
                        window.open(targetUrl, '_blank');
                    }
                    // Restore URL bar to previous path
                    urlInput.value = this._lastUrlChangeUrl ? this.pathFromProxyUrl(this._lastUrlChangeUrl) : '/';
                    return;
                }
                navUrl = parsed.pathname + parsed.search + parsed.hash;
            } catch {
                // Bare path like "/foo" -- treat as localhost path
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
        const iframePane = this.querySelector('.terminal-ui__iframe-pane');
        if (!terminalUi || !iframePane) return;

        // Add class to show iframe pane
        terminalUi.classList.add('iframe-visible');

        // Show/hide toolbar based on tab (only preview gets toolbar)
        if (tab === 'preview') {
            iframePane.classList.add('show-toolbar');
        } else {
            iframePane.classList.remove('show-toolbar');
        }

        // Apply saved pane width
        this.applyPaneWidth();

        // Update active tab state and reveal its slot
        this.activeTab = tab;
        this._showPaneSlot(tab);

        // Load or navigate the pane's iframe
        if (tab === 'preview') {
            this._paneLoaded.add('preview');
            this.setPreviewURL(url); // url is targetURL or null
        } else {
            this._paneLoaded.add(tab);
            this.setIframeUrl(url, tab);
        }
        this.updateActiveTabIndicator();

        // Re-fit terminal after layout change
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    }

    // Close iframe pane and return to 100% terminal.
    // Panes stay mounted (not reset to about:blank) so state survives re-opening.
    closeIframePane() {
        const terminalUi = this.querySelector('.terminal-ui');
        if (!terminalUi) return;

        // Hide all pane slots; leave their iframes mounted so state (scroll,
        // form inputs, code-server session, xterm-over-iframe buffer) persists.
        this._hideAllPaneSlots();

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

    // Switch mobile navigation (unified dropdown). The mobile CSS in
    // terminal-ui.css:2094 hides all pane-hosts by default and reveals the
    // one carrying the `data-mobile-active` attribute; mirror that here.
    // Slot state (activeBySlot) is intentionally ignored on mobile -- the
    // dropdown is the source of truth for which single pane is visible.
    switchMobileNav(value) {
        const terminalUi = this.querySelector('.terminal-ui');

        // Mobile visibility: set data-mobile-active on the matching pane-host,
        // strip it from every other pane-host. Lazy-load the iframe if this is
        // the first time this pane is being shown.
        this.querySelectorAll('.terminal-ui__pane-host').forEach(host => {
            if (host.dataset.pane === value) {
                host.dataset.mobileActive = '';
            } else {
                delete host.dataset.mobileActive;
            }
        });
        this._loadPaneIfNeeded(value);

        // xterm-focused: only when agent-terminal is the active panel
        // Gates mobile keyboard and touch-scroll-proxy to avoid blocking other panels
        if (value === 'agent-terminal') {
            terminalUi.classList.add('xterm-focused');
        } else {
            terminalUi.classList.remove('xterm-focused');
        }

        if (value === 'agent-terminal') {
            this.mobileActiveView = 'terminal';
            setTimeout(() => this.fitAndPreserveScroll(), 50);
        } else if (value === 'agent-chat') {
            this.mobileActiveView = 'terminal';
        } else {
            this.mobileActiveView = 'workspace';
        }
        // Keep the legacy class in sync for any css that still reads it.
        terminalUi.classList.toggle('mobile-view-terminal', this.mobileActiveView === 'terminal');
        terminalUi.classList.toggle('mobile-view-workspace', this.mobileActiveView === 'workspace');

        // Sync the dropdown in case this was called programmatically (e.g. the
        // WS-init probe-success handler calls switchMobileNav('agent-chat')).
        const sel = this.querySelector('.terminal-ui__mobile-nav-select');
        if (sel && sel.value !== value) sel.value = value;
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
        const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
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
            // Disable iframe pointer events during drag so mouse movement
            // that strays over either iframe doesn't get captured and stall the drag
            if (iframePane) iframePane.style.pointerEvents = 'none';
            if (chatIframe) chatIframe.style.pointerEvents = 'none';
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
            if (chatIframe) chatIframe.style.pointerEvents = '';
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
        const placeholder = this._placeholderFor('preview');
        if (placeholder) placeholder.classList.add('hidden');
    }

    /**
     * Reload the preview iframe (used when proxy comes up after being down).
     */
    _reloadPreviewIframe() {
        const iframe = this._iframeFor('preview');
        if (iframe && iframe.src) iframe.src = iframe.src;
    }

    /**
     * Set iframe src for a specific pane (shell / vscode / browser).
     * Idempotent: does nothing if the iframe is already at the requested URL,
     * which preserves pane state across tab switches.
     *
     * Does NOT touch the URL bar -- that's owned by setPreviewURL, since the
     * toolbar is only visible for the preview pane.
     */
    setIframeUrl(url, pane = this.activeTab) {
        // Validate URL
        try {
            new URL(url);
        } catch (e) {
            console.warn('[TerminalUI] Invalid iframe URL:', url);
            return;
        }

        const iframe = this._iframeFor(pane);
        const placeholder = this._placeholderFor(pane);
        if (!iframe) return;

        // State-preservation: skip reload if iframe already at this exact URL.
        if (iframe.getAttribute('src') === url) return;

        if (placeholder) {
            placeholder.classList.remove('hidden');
        }

        iframe.onload = () => {
            if (placeholder) placeholder.classList.add('hidden');
        };

        iframe.src = url;
    }

    /**
     * Set the preview URL bar and iframe src separately.
     * @param {string|null} targetURL - Logical target URL shown in bar (null = home)
     * @param {string|null} iframePath - Override iframe path instead of extracting from targetURL
     */
    setPreviewURL(targetURL, iframePath = null) {
        const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
        const iframe = this._iframeFor('preview');
        const placeholder = this._placeholderFor('preview');

        // Determine if targetURL is external (non-localhost)
        let isExternal = false;
        if (targetURL) {
            try {
                const parsed = new URL(targetURL);
                const host = parsed.hostname;
                isExternal = host !== 'localhost' && host !== '127.0.0.1';
            } catch {
                // Bare path like "/foo" -- not external
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
        const base = buildPreviewUrl(getBaseUrl(window.location), this.sessionUUID);
        if (!base) return;
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

        // URL bar shows only the path -- the fixed prefix handles host:port.
        if (urlInput) {
            urlInput.value = path;
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

            // Two-phase probe:
            // Phase 1: Probe path-based URL to wait for proxy handler to be up.
            // Phase 2: Quick probe port-based URL to determine preferred mode.
            this._previewProbeController = new AbortController();
            const portBasedBase = buildPortBasedPreviewUrl(window.location, this.previewProxyPort);
            probeUntilReady(base + '/', {
                method: 'GET',
                maxAttempts: 10, baseDelay: 2000, maxDelay: 30000,
                isReady: (resp) => resp.headers.has('X-Agent-Reverse-Proxy'),
                signal: this._previewProbeController.signal,
            }).then(() => {
                // Phase 2: try port-based if available.
                // Uses /__probe__ which bypasses ForwardAuth in Traefik -- avoids
                // Safari's stricter cross-port CORS+credentials blocking.
                if (portBasedBase && this._proxyMode !== 'path') {
                    return fetch(portBasedBase + '/__probe__', { method: 'GET', mode: 'cors' })
                        .then(resp => {
                            if (resp.headers.has('X-Agent-Reverse-Proxy')) {
                                this._proxyMode = 'port';
                            } else {
                                this._proxyMode = 'path';
                            }
                        })
                        .catch(() => { this._proxyMode = 'path'; });
                }
                if (!this._proxyMode) this._proxyMode = 'path';
            }).then(() => {
                // Compute final iframe src based on chosen mode
                let finalBase = base;
                let finalIframeSrc = iframeSrc;
                if (this._proxyMode === 'port' && portBasedBase) {
                    finalBase = portBasedBase;
                    finalIframeSrc = finalBase + '/__agent-reverse-proxy-debug__/shell?path=' + encodeURIComponent(path);
                    this._lastUrlChangeUrl = finalBase + path;
                }

                if (this.activeTab === 'preview') {
                    iframe.src = finalIframeSrc;
                } else {
                    this._pendingPreviewIframeSrc = finalIframeSrc;
                    return;
                }
                iframe.onload = () => {
                    if (this._previewWaiting) {
                        this._onPreviewReady();
                    }
                };
            }).catch(() => {
                // Exhausted or aborted -- leave placeholder visible
            });
        }
    }

    refreshIframe() {
        if (this._debugWs?.readyState === WebSocket.OPEN) {
            this._debugWs.send(JSON.stringify({ t: 'reload' }));
        } else {
            // Fallback: force reload of the currently active pane's iframe.
            const iframe = this._iframeFor(this.activeTab);
            if (iframe && iframe.src) iframe.src = iframe.src;
        }
    }

    /**
     * Reverse-map a proxy URL to the logical localhost:PORT URL.
     * e.g., https://host/proxy/{uuid}/preview/dashboard?tab=1#s -> http://localhost:3000/dashboard?tab=1#s
     */
    reverseMapProxyUrl(proxyUrl) {
        if (!this.previewPort) return proxyUrl;
        try {
            const parsed = new URL(proxyUrl);
            // Strip the /proxy/{sessionUUID}/preview prefix from path-based routing
            const prefix = this.sessionUUID ? `/proxy/${this.sessionUUID}/preview` : '';
            let path = parsed.pathname;
            if (prefix && path.startsWith(prefix)) {
                path = path.slice(prefix.length) || '/';
            }
            return `http://localhost:${this.previewPort}${path}${parsed.search}${parsed.hash}`;
        } catch {
            return proxyUrl;
        }
    }

    /**
     * Extract just the path from a proxy URL.
     * e.g., https://host/proxy/{uuid}/preview/dashboard?tab=1#s -> /dashboard?tab=1#s
     */
    pathFromProxyUrl(proxyUrl) {
        try {
            const parsed = new URL(proxyUrl);
            const prefix = this.sessionUUID ? `/proxy/${this.sessionUUID}/preview` : '';
            let path = parsed.pathname;
            if (prefix && path.startsWith(prefix)) {
                path = path.slice(prefix.length) || '/';
            }
            return path + parsed.search + parsed.hash;
        } catch {
            return '/';
        }
    }

    /**
     * Update the non-editable host:port prefix in the URL bar.
     */
    updateUrlBarPrefix() {
        const prefix = this.querySelector('.terminal-ui__iframe-url-prefix');
        if (prefix) {
            prefix.textContent = this.previewPort ? `localhost:${this.previewPort}` : '';
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

        const previewBase = this.getPreviewBaseUrl();
        if (!previewBase) return;

        const wsUrl = previewBase.replace(/^http/, 'ws') + '/__agent-reverse-proxy-debug__/ui';

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
                    // Proxy and shell page confirmed alive -- hide placeholder
                    if (this._previewWaiting && this.activeTab === 'preview') {
                        this._onPreviewReady();
                    }
                    const urlInput = this.querySelector('.terminal-ui__iframe-url-input');
                    if (urlInput && this.activeTab === 'preview') {
                        urlInput.value = this.pathFromProxyUrl(msg.url);
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
            // Auto-reconnect -- the proxy may not be up yet or may have restarted
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
