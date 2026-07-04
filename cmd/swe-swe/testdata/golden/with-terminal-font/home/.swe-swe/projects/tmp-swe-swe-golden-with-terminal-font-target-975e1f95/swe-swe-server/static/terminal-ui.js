import { formatDuration, formatFileSize, escapeHtml, escapeFilename } from './modules/util.js';
import { validateUsername, validateSessionName } from './modules/validation.js';
import { deriveShellUUID } from './modules/uuid.js';
import { getBaseUrl, buildShellUrl, buildPreviewUrl, buildProxyUrl, buildAgentChatUrl, buildPortBasedPreviewUrl, buildPortBasedAgentChatUrl, buildPortBasedFilesUrl, buildPortBasedProxyUrl, buildSubdomainPreviewUrl, buildSubdomainAgentChatUrl, buildSubdomainFilesUrl, accessedViaTunnel, getDebugQueryString } from './modules/url-builder.js';
import { dedupePanesAcrossSlots } from './modules/slot-state.js';
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
const PANES_IN_ORDER = ['agent-terminal', 'agent-chat', 'preview', 'files', 'shell', 'browser'];

// Human-readable labels for slot tab bar buttons.
const PANE_LABELS = {
    'agent-terminal': 'Agent Terminal',
    'agent-chat':     'Agent Chat',
    'preview':        'Preview',
    'files':          'Files',
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
const IFRAME_PANES_PRIORITY = ['preview', 'shell', 'browser'];

// Per-preset gutter spec. Each gutter sits on the boundary between two
// adjacent slots (`between`) and lets the user drag-resize that boundary.
// Position + perpendicular span are derived from slot bounding rects: by
// default the union of `between` slots, but `spanRange` overrides this
// when the gutter actually controls more cells than the two it sits
// between (e.g. quadrants col-gutter resizes both rows because cols are
// shared, so we want it visually spanning the full workspace height).
const PRESET_GUTTERS = {
    classic:      { cols: [{ between: ['a', 'b'] }] },
    single:       {},
    'two-row':    { rows: [{ between: ['a', 'b'] }] },
    'l-bigR':     { cols: [{ between: ['a', 'b'] }] },
    'stacked-R':  { cols: [{ between: ['a', 'b'] }],
                    rows: [{ between: ['b', 'c'] }] },
    'stacked-L':  { cols: [{ between: ['a', 'c'] }],
                    rows: [{ between: ['a', 'b'] }] },
    't-splitBot': { rows: [{ between: ['a', 'b'] }],
                    cols: [{ between: ['b', 'c'] }] },
    quadrants:    { cols: [{ between: ['a', 'b'], spanRange: ['a', 'c'] }],
                    rows: [{ between: ['a', 'c'], spanRange: ['a', 'b'] }] },
};

// Minimum px width/height a pane keeps during gutter drag. Matches the
// legacy MIN_PANEL_WIDTH used by the now-defunct flexbox split-pane.
const GUTTER_MIN_PX = 200;

// Gutter drag snap targets. Hold Alt while dragging to bypass.
//   - Ratio snap (50%) on both column and row gutters.
//   - Device-width snaps on column gutters only, applied to whichever
//     side (left or right pane) is closer to the device width.
const GUTTER_SNAP_RATIO_THRESHOLD_PX = 12;  // cap on the 2%-of-total threshold
const GUTTER_SNAP_RATIO_THRESHOLD_FRAC = 0.02;
const GUTTER_SNAP_DEVICE_THRESHOLD_PX = 8;
const GUTTER_DEVICE_SNAPS = [
    { px: 375, label: '375' },
    { px: 640, label: '640 (sm)' },
    { px: 768, label: '768 (md)' },
];

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

// Normalize a sizesByPreset map from localStorage. Drops entries for
// unknown presets and keeps only well-formed [num, num] arrays so a
// corrupt entry can't break grid-template later.
function normalizeSizesByPreset(value) {
    const result = {};
    if (!value || typeof value !== 'object') return result;
    for (const [presetId, sizes] of Object.entries(value)) {
        if (!LAYOUT_PRESETS[presetId] || !sizes || typeof sizes !== 'object') continue;
        const entry = {};
        if (Array.isArray(sizes.cols) && sizes.cols.length === 2
            && sizes.cols.every(n => typeof n === 'number' && isFinite(n) && n > 0)) {
            entry.cols = [sizes.cols[0], sizes.cols[1]];
        }
        if (Array.isArray(sizes.rows) && sizes.rows.length === 2
            && sizes.rows.every(n => typeof n === 'number' && isFinite(n) && n > 0)) {
            entry.rows = [sizes.rows[0], sizes.rows[1]];
        }
        if (entry.cols || entry.rows) result[presetId] = entry;
    }
    return result;
}

// Layout state persistence. Shape:
//   { preset: string,
//     activeBySlot: {[slot]: {tabs: string[], active: string}},
//     sizesByPreset: {[preset]: {cols?: [n,n], rows?: [n,n]}} }
// Legacy shape (v1 initial release) stored pane strings directly in
// activeBySlot[slot]; normalizeSlotState handles the migration on read.
// sizesByPreset was added later -- absence is normal and means "use the
// preset's default fr ratios from CSS".
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
            dedupePanesAcrossSlots(activeBySlot, preset.slots);
            return {
                preset: s.preset,
                activeBySlot,
                sizesByPreset: normalizeSizesByPreset(s.sizesByPreset),
            };
        }
    } catch (e) { /* fall through */ }
    return {
        preset: 'classic',
        activeBySlot: defaultActiveBySlot(LAYOUT_PRESETS.classic),
        sizesByPreset: {},
    };
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
        this.filesProxyPort = null;
        this.publicPort = null;
        this.cdpPort = null;
        this.vncPort = null;
        this.vncProxyPort = null;
        // Tunnel mode public hostname; non-empty when SWE_PUBLIC_HOSTNAME is
        // set on the server. When non-empty, cross-port URLs are built as
        // "{port}.{publicHostname}" instead of using proxy-port offsets.
        this.publicHostname = '';
        // Tunnel client lifecycle as observed by the server-side
        // supervisor. Shape: {state, retryAfterMs?, reason?}. Absent
        // when the supervisor is not running. Used to surface a
        // "rate-limited; retrying in 5m" hint instead of an indefinite
        // spinner during reconnect/fatal states.
        this.tunnelStatus = null;
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
        // (agent-terminal / agent-chat / preview / shell / browser).
        // Loaded from localStorage; falls back to classic defaults on missing/
        // invalid state. See terminal-ui.css for the grid-template-areas per preset.
        const _layoutState = loadLayoutState();
        this.preset = _layoutState.preset;
        this.activeBySlot = _layoutState.activeBySlot;
        // Per-preset overrides for grid track sizes (set by gutter drags).
        // Shape: { [presetId]: { cols?: [n,n], rows?: [n,n] } }. Numbers are
        // fr ratios -- e.g. cols: [45, 55] means `45fr 55fr`. Empty by
        // default; entries are added/removed as the user drags/dblclicks.
        this.sizesByPreset = _layoutState.sizesByPreset || {};
        // Whenever a slot contains agent-terminal, force it active on mount
        // regardless of what was last persisted. The agent terminal is the
        // primary surface where blocking prompts appear (e.g. the
        // swe-swe-agent-chat --dangerously-load-development-channels confirm
        // screen, auth flows, agent crashes), so it must be the default tab
        // the user sees. localStorage is untouched, so any saved preference
        // is harmless -- mid-session tab switches via setActiveInSlot still
        // work normally; this only governs the initial mount. Probes that
        // auto-open other panes (chat probe-success, browser VNC) use
        // persist:false and run after this, so they can still take focus
        // when ready without permanently overriding the mount default.
        for (const slot of Object.values(this.activeBySlot)) {
            if (slot && Array.isArray(slot.tabs) && slot.tabs.includes('agent-terminal')) {
                slot.active = 'agent-terminal';
            }
        }
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
        // Minimum width per slot used by canShowSplitPane() to decide
        // whether the desktop split layout fits in the current viewport.
        this.MIN_PANEL_WIDTH = 360;
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

    // publicHostname scoped to how the page was actually loaded. The server
    // sets this.publicHostname whenever it is in tunnel mode, but subdomain
    // proxy URLs ("{port}.{publicHostname}") are only reachable when the
    // browser reached this page through the tunnel host. When opened directly
    // (localhost / LAN / Tailscale), this returns "" so the URL builders fall
    // back to port-based / path-based forms that are actually reachable.
    get effectivePublicHostname() {
        return accessedViaTunnel(window.location, this.publicHostname) ? this.publicHostname : '';
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
                            <nav class="settings-panel__nav" role="tablist" aria-label="Settings sections">
                                <span class="settings-panel__nav-section">Workspace</span>
                                <button class="settings-panel__nav-item settings-panel__nav-item--active" role="tab" data-tab="profile" aria-selected="true">
                                    <span class="settings-panel__nav-label">Profile</span>
                                </button>
                                <button class="settings-panel__nav-item" role="tab" data-tab="appearance" aria-selected="false">
                                    <span class="settings-panel__nav-label">Appearance</span>
                                </button>
                                <span class="settings-panel__nav-section">Credentials</span>
                                <button class="settings-panel__nav-item" role="tab" data-tab="git" aria-selected="false">
                                    <span class="settings-panel__nav-label">Git HTTPS</span>
                                    <span class="settings-panel__nav-badge" id="settings-nav-badge-git" hidden></span>
                                </button>
                                <button class="settings-panel__nav-item" role="tab" data-tab="ssh" aria-selected="false">
                                    <span class="settings-panel__nav-label">SSH Signing</span>
                                    <span class="settings-panel__nav-badge" id="settings-nav-badge-ssh" hidden></span>
                                </button>
                            </nav>
                            <div class="settings-panel__pane-host">
                                <!-- PROFILE -->
                                <section class="settings-panel__pane settings-panel__pane--active" data-pane="profile" role="tabpanel">
                                    <h3 class="settings-panel__pane-title">Profile</h3>
                                    <p class="settings-panel__pane-sub">How you and this session are identified inside this workspace.</p>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-username">Username</label>
                                        <input type="text" id="settings-username" class="settings-panel__input" placeholder="Enter your name" maxlength="16">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-session">Session name</label>
                                        <input type="text" id="settings-session" class="settings-panel__input" placeholder="Enter session name" maxlength="256">
                                    </div>
                                    <div class="settings-panel__pane-footer">
                                        <span class="settings-panel__pane-status" id="settings-profile-status">Unsaved changes are discarded on close</span>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-profile-revert" type="button">Revert</button>
                                        <button class="settings-panel__btn settings-panel__btn--primary" id="settings-profile-save" type="button">Save</button>
                                    </div>
                                </section>

                                <!-- APPEARANCE -->
                                <section class="settings-panel__pane" data-pane="appearance" role="tabpanel" hidden>
                                    <h3 class="settings-panel__pane-title">Appearance</h3>
                                    <p class="settings-panel__pane-sub">Local to this browser. Live preview while you tweak &mdash; close without saving to revert.</p>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label">Theme mode</label>
                                        <div class="settings-panel__theme-toggle" id="settings-theme-toggle">
                                            <button class="settings-panel__theme-btn" data-mode="light" type="button">Light</button>
                                            <button class="settings-panel__theme-btn" data-mode="dark" type="button">Dark</button>
                                            <button class="settings-panel__theme-btn selected" data-mode="system" type="button">System</button>
                                        </div>
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label">Accent color</label>
                                        <div class="settings-panel__color-picker">
                                            <div class="settings-panel__color-presets" id="settings-color-presets">
                                                <!-- Populated by JS -->
                                            </div>
                                            <div class="settings-panel__color-custom">
                                                <input type="color" class="settings-panel__color-input" id="settings-color-input" value="#7c3aed">
                                                <input type="text" class="settings-panel__color-hex" id="settings-color-hex" value="#7c3aed" placeholder="#7c3aed">
                                                <button class="settings-panel__color-reset" id="settings-color-reset" type="button">Reset</button>
                                            </div>
                                        </div>
                                    </div>
                                    <div class="settings-panel__pane-footer">
                                        <span class="settings-panel__pane-status" id="settings-appearance-status">Live preview &mdash; not yet saved</span>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-appearance-revert" type="button">Revert</button>
                                        <button class="settings-panel__btn settings-panel__btn--primary" id="settings-appearance-save" type="button">Save</button>
                                    </div>
                                </section>

                                <!-- GIT HTTPS -->
                                <section class="settings-panel__pane" data-pane="git" role="tabpanel" hidden>
                                    <h3 class="settings-panel__pane-title">Git HTTPS credentials</h3>
                                    <p class="settings-panel__pane-sub">In-memory on the server only. Never written to disk; cleared when this session ends.</p>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-host">Host</label>
                                        <input type="text" id="settings-cred-host" class="settings-panel__input" placeholder="github.com">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-username">Username</label>
                                        <input type="text" id="settings-cred-username" class="settings-panel__input" placeholder="x-access-token (default for GitHub PATs)">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-token">Token</label>
                                        <input type="password" id="settings-cred-token" class="settings-panel__input" placeholder="ghp_..." autocomplete="off">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-name">Author name</label>
                                        <input type="text" id="settings-cred-name" class="settings-panel__input" placeholder="Your Name">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-email">Author email</label>
                                        <input type="email" id="settings-cred-email" class="settings-panel__input" placeholder="you@example.com">
                                    </div>
                                    <div class="settings-panel__pane-footer">
                                        <span class="settings-panel__pane-status settings-panel__cred-status" id="settings-cred-status"></span>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-cred-forget" type="button" hidden>Forget HTTPS on this device</button>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-cred-test" type="button">Test connection</button>
                                        <button class="settings-panel__btn settings-panel__btn--primary" id="settings-cred-save" type="button">Save credentials</button>
                                    </div>
                                </section>

                                <!-- SSH SIGNING -->
                                <section class="settings-panel__pane" data-pane="ssh" role="tabpanel" hidden>
                                    <h3 class="settings-panel__pane-title">SSH commit signing</h3>
                                    <p class="settings-panel__pane-sub">Paste an OpenSSH ed25519 private key. Held in server memory only; never on disk. Used by git commit -S via the credential broker. The matching public key must already be registered as a signing key on your forge.</p>
                                    <div class="settings-panel__field-row settings-panel__field-row--stacked">
                                        <label class="settings-panel__label" for="settings-cred-signing-key">Signing private key (ed25519)</label>
                                        <textarea id="settings-cred-signing-key" class="settings-panel__input settings-panel__input--multiline" rows="6" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----" autocomplete="off" spellcheck="false"></textarea>
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-signing-passphrase">Passphrase</label>
                                        <input type="password" id="settings-cred-signing-passphrase" class="settings-panel__input" placeholder="leave blank if key is unencrypted" autocomplete="off">
                                    </div>
                                    <div class="settings-panel__field-row">
                                        <label class="settings-panel__label" for="settings-cred-signing-label">Label</label>
                                        <input type="text" id="settings-cred-signing-label" class="settings-panel__input" placeholder="laptop@example">
                                    </div>
                                    <div class="settings-panel__pane-footer">
                                        <span class="settings-panel__pane-status settings-panel__cred-status" id="settings-cred-signing-status"></span>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-cred-signing-forget" type="button" hidden>Forget on this device</button>
                                        <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-cred-signing-verify" type="button">Verify key</button>
                                        <button class="settings-panel__btn settings-panel__btn--primary" id="settings-cred-signing-save" type="button">Save key</button>
                                    </div>
                                    <p class="settings-panel__hint settings-panel__hint--inline">Verify derives the public key, signs a test payload, and confirms the signature parses &mdash; proves the passphrase is right and the key is loadable. It does not contact your forge.</p>
                                </section>
                            </div>
                        </div>
                        <footer class="settings-panel__footer">
                            <button class="settings-panel__end-link" id="settings-end-session" type="button">End session</button>
                            <div class="settings-panel__footer-meta" id="settings-footer-meta"></div>
                            <button class="settings-panel__btn settings-panel__btn--secondary settings-panel__footer-close" type="button">Close</button>
                        </footer>
                        <div class="settings-panel__end-confirm" id="settings-end-confirm" hidden>
                            <div class="settings-panel__end-confirm-card">
                                <h4 class="settings-panel__end-confirm-title">End this session?</h4>
                                <p class="settings-panel__end-confirm-body">Closes the workspace, terminates the agent, and clears in-memory credentials. Repository changes on disk are unaffected.</p>
                                <div class="settings-panel__end-confirm-actions">
                                    <button class="settings-panel__btn settings-panel__btn--secondary" id="settings-end-cancel" type="button">Cancel</button>
                                    <button class="settings-panel__btn settings-panel__btn--danger-strong" id="settings-end-confirm-yes" type="button">Yes, end session</button>
                                </div>
                            </div>
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
                        <option value="files" hidden disabled>Files</option>
                        <option value="shell">Terminal</option>
                        <option value="browser" hidden disabled>Agent View</option>
                    </select>
                    <button class="terminal-ui__mobile-nav-popout" type="button" title="Open in new browser tab" hidden>↗</button>
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

                        <!-- Files (md-serve) pane-host -->
                        <div class="terminal-ui__pane-host" data-pane="files" hidden>
                            <div class="terminal-ui__iframe-slot" data-pane="files">
                                <div class="terminal-ui__iframe-placeholder hidden">
                                    <div class="terminal-ui__iframe-placeholder-status">
                                        <span class="terminal-ui__iframe-placeholder-dot"></span>
                                        <span class="terminal-ui__iframe-placeholder-text">Connecting to files...</span>
                                    </div>
                                </div>
                                <iframe class="terminal-ui__iframe" data-pane="files" src="" sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals allow-downloads"></iframe>
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
            // Browser-side auto-restore of the SSH signing key AND HTTPS
            // credentials. Only fires when the user has explicitly trusted
            // this (origin, repo) pair via a Save flow + first-time dialog.
            // Sends ONE combined message when both are present so the server
            // rewrites the per-session gitconfig once (no 2N-write herd on
            // many browsers reconnecting). See _maybeAutoConnectSecrets.
            this._maybeAutoConnectSecrets();
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

            // Fatal session creation error (e.g. "branch already checked out").
            // Stop reconnecting so the user sees the reason instead of an
            // endless loop that would just hit the same error. The full error
            // arrived as a session_error JSON message just before close --
            // prefer it over the truncated close-reason field.
            if (event.code === 4002) {
                this.processExited = true;
                this.updateStatus('error', this._sessionErrorMsg || reason || 'Could not create session');
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
            case 'session_error':
                // Fatal error from the server (e.g. worktree creation failed).
                // Stash the full text so the onclose 4002 handler can display it
                // -- the WS close-reason field would otherwise truncate the
                // most useful tail of git's output.
                this._sessionErrorMsg = typeof msg.message === 'string' ? msg.message : '';
                break;
            case 'credentials_stored':
                // Server acked set_credentials. Hosts list reflects what the
                // server holds for this session (write-only -- no values).
                this._credsStoredHosts = Array.isArray(msg.hosts) ? msg.hosts : [];
                this._refreshCredsStatus();
                if (typeof msg.signing_fingerprint === 'string' && msg.signing_fingerprint) {
                    this._signingFingerprint = msg.signing_fingerprint;
                    this._signingError = '';
                } else if (typeof msg.signing_error === 'string' && msg.signing_error) {
                    this._signingFingerprint = '';
                    this._signingError = msg.signing_error;
                }
                if (this._autoConnectInFlight) {
                    // Combined auto-send (creds [+ key]) landed. Extend the
                    // trust TTL on success; drop trust on a signing error so
                    // we do not loop on a bad stored key each reconnect.
                    this._autoConnectInFlight = false;
                    const initSha = this.dataset.initSha || '';
                    if (initSha) {
                        if (typeof msg.signing_error === 'string' && msg.signing_error) {
                            this._clearSigningTrust(initSha);
                        } else {
                            this._touchSigningTrust(initSha);
                        }
                    }
                } else if (this._credsManualSaveInFlight) {
                    // User-initiated save: offer to trust this browser so a
                    // future visit auto-sends without a manual Save.
                    this._credsManualSaveInFlight = false;
                    this._maybePromptCredsTrust();
                }
                this._refreshSigningStatus();
                this._refreshCredsForgetButton();
                break;
            case 'credentials_tested':
                // Server acked test_credentials. Show the result against the
                // existing credential status line on the Git HTTPS pane.
                {
                    const status = this.querySelector('#settings-cred-status');
                    if (status) {
                        const ok = !!msg.ok;
                        const text = typeof msg.message === 'string' && msg.message
                            ? msg.message
                            : (ok ? 'Connection OK' : 'Connection failed');
                        status.textContent = text;
                        status.setAttribute('data-state', ok ? 'ok' : 'err');
                    }
                }
                break;
            case 'signing_key_stored':
                // Server acked set_signing_key. Save flow OR auto-restore.
                this._signingVerified = '';
                if (typeof msg.fingerprint === 'string' && msg.fingerprint) {
                    this._signingFingerprint = msg.fingerprint;
                    this._signingError = '';
                    this._handleSigningKeyStored(msg.fingerprint);
                } else if (typeof msg.error === 'string' && msg.error) {
                    this._signingFingerprint = '';
                    this._signingError = msg.error;
                    // Auto-restore that fails (e.g. server can no longer
                    // parse the stored PEM) should clear the trust so we
                    // do not loop on every reconnect.
                    if (this._signingAutoRestoreInFlight) {
                        const initSha = this.dataset.initSha || '';
                        if (initSha) this._clearSigningTrust(initSha);
                    }
                }
                this._signingAutoRestoreInFlight = false;
                this._refreshSigningStatus();
                break;
            case 'signing_key_verified':
                // Server acked verify_signing_key. Did NOT persist; only
                // confirms parse + signing roundtrip succeeded.
                if (typeof msg.fingerprint === 'string' && msg.fingerprint) {
                    this._signingVerified = msg.fingerprint + ' (parsed and signed test payload)';
                    this._signingError = '';
                } else if (typeof msg.error === 'string' && msg.error) {
                    this._signingVerified = '';
                    this._signingError = msg.error;
                } else {
                    this._signingVerified = '';
                }
                this._refreshSigningStatus();
                break;
            case 'session_cred_state':
                // Connect-time (and on-change) snapshot of server-side
                // credential/signing state. Lets the panel show what is
                // already set up server-side without a manual Save. Carries
                // no secrets -- only host names, the signing fingerprint,
                // local override text, and the computed signing verdict.
                this._credsStoredHosts = Array.isArray(msg.hosts) ? msg.hosts : [];
                if (typeof msg.signing_fingerprint === 'string') {
                    this._signingFingerprint = msg.signing_fingerprint;
                }
                if (typeof msg.local_gpg_overrides === 'string') {
                    this.dataset.localGpgOverrides = msg.local_gpg_overrides;
                }
                this._signingActive = !!msg.signing_active;
                this._signingInactiveReason = typeof msg.signing_inactive_reason === 'string'
                    ? msg.signing_inactive_reason : '';
                this._signingStateKnown = true;
                this._refreshCredsStatus();
                this._refreshSigningStatus();
                this._refreshLocalSigningOverridesWarning();
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
                const prevPreviewProxyPort = this.previewProxyPort;
                const prevVncProxyPort = this.vncProxyPort;
                const prevFilesProxyPort = this.filesProxyPort;
                this.previewPort = msg.previewPort || null;
                this.updateUrlBarPrefix();
                this.agentChatPort = msg.agentChatPort || null;
                this.sessionUUID = msg.sessionUUID || null;
                this.previewProxyPort = msg.previewProxyPort || null;
                this.agentChatProxyPort = msg.agentChatProxyPort || null;
                this.filesProxyPort = msg.filesProxyPort || null;
                this.publicPort = msg.publicPort || null;
                this.cdpPort = msg.cdpPort || null;
                this.vncPort = msg.vncPort || null;
                this.vncProxyPort = msg.vncProxyPort || null;
                this.publicHostname = msg.publicHostname || '';
                // Tab popout-able state depends on URLs that arrive via
                // BroadcastStatus. Rerender when those URLs flip from null
                // to set (or vice-versa) so the dotted-underline affordance
                // and tooltip appear without waiting for an unrelated event.
                if ((!prevPreviewProxyPort) !== (!this.previewProxyPort)
                    || (!prevVncProxyPort) !== (!this.vncProxyPort)
                    || (!prevFilesProxyPort) !== (!this.filesProxyPort)) {
                    this._rerenderSlotTabs();
                    // Toggle the Files mobile-nav option in step with its
                    // availability (filesProxyPort), mirroring how Agent View
                    // toggles its option. switchMobileNav below then shows the
                    // popout button when Files is the selected pane.
                    const filesOpt = this.querySelector('.terminal-ui__mobile-nav-select option[value="files"]');
                    if (filesOpt) {
                        filesOpt.hidden = !this.filesProxyPort;
                        filesOpt.disabled = !this.filesProxyPort;
                    }
                    const sel = this.querySelector('.terminal-ui__mobile-nav-select');
                    if (sel) this.switchMobileNav(sel.value);
                }
                const prevTunnelStatus = this.tunnelStatus;
                this.tunnelStatus = msg.tunnelStatus || null;
                this._renderTunnelStatusBanner(prevTunnelStatus, this.tunnelStatus);
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
                    // Animate the Agent Chat tab label spinner during the
                    // probe. The ?session=chat path also calls this from the
                    // page-load auto-add (see line ~381), but non-chat session
                    // pages reach the probe via the WS init handler instead --
                    // without this call the spinner would render statically
                    // (single braille frame) because nothing ticks the timer.
                    if (!this._chatLoadingTimer) this.startChatLoadingAnimation();
                    this._rerenderSlotTabs();
                    const acPathUrl = buildAgentChatUrl(getBaseUrl(window.location), this.sessionUUID);
                    // Cross-origin agent-chat URL. Both modes route through
                    // the swe-swe-server auth proxy port (agentChatProxyPort)
                    // so the cookie is checked before forwarding to the raw
                    // agent-chat target -- tunnel mode demuxes
                    // {agentChatProxyPort}.{publicHostname} -> 127.0.0.1:{agentChatProxyPort}
                    // inside the container; legacy mode hits the same port via Traefik.
                    const acPortUrl = this.effectivePublicHostname
                        ? buildSubdomainAgentChatUrl(window.location, this.agentChatProxyPort, this.effectivePublicHostname)
                        : buildPortBasedAgentChatUrl(window.location, this.agentChatProxyPort);
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
                            this.stopChatLoadingAnimation();
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
                // Load files once filesProxyPort arrives. If the user dragged
                // the Files tab into a slot before the port was known,
                // _loadPaneIfNeeded('files') returned early (deferred) without
                // marking it loaded; re-kick now so the iframe src gets set and
                // the "Connecting to files..." placeholder clears.
                if (this.filesProxyPort && this._slotForPane('files') && !this._paneLoaded.has('files')) {
                    this._loadPaneIfNeeded('files');
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

        window.location.href = '/' + getDebugQueryString(this.debugMode);
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

        // Snapshot Profile + Appearance state so close-without-save reverts.
        // Theme/color apply live during the session for preview, but we
        // restore both visual + storage on revert.
        this._settingsSnapshot = this._snapshotSettings();

        // Default to the Profile pane each time the modal opens.
        this._switchSettingsTab('profile');

        panel.removeAttribute('hidden');
        statusBar.setAttribute('aria-expanded', 'true');

        // Store the element that opened the panel to restore focus on close
        this._settingsPanelOpener = document.activeElement;
    }

    closeSettingsPanel() {
        const panel = this.querySelector('.settings-panel');
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        if (!panel) return;

        // Discard unsaved Profile + Appearance changes by reverting to
        // the snapshot taken when the panel opened.
        this._revertProfile({ silent: true });
        this._revertAppearance({ silent: true });
        this._settingsSnapshot = null;

        // Hide the end-session confirm popover if it was open.
        const endConfirm = panel.querySelector('#settings-end-confirm');
        if (endConfirm) endConfirm.setAttribute('hidden', '');

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
        const footerClose = panel.querySelector('.settings-panel__footer-close');

        // Close on backdrop click
        if (backdrop) {
            backdrop.addEventListener('click', () => this.closeSettingsPanel());
        }

        // Close on X / footer Close button click
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.closeSettingsPanel());
        }
        if (footerClose) {
            footerClose.addEventListener('click', () => this.closeSettingsPanel());
        }

        // Close on Escape key (and dismiss the end-session popover first
        // if it's open so a single Escape doesn't blow away the whole modal).
        panel.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                e.stopPropagation();
                const endConfirm = panel.querySelector('#settings-end-confirm');
                if (endConfirm && !endConfirm.hasAttribute('hidden')) {
                    endConfirm.setAttribute('hidden', '');
                    return;
                }
                this.closeSettingsPanel();
            }
        });

        // Sidebar tab navigation
        panel.querySelectorAll('.settings-panel__nav-item').forEach(btn => {
            btn.addEventListener('click', () => {
                const tab = btn.dataset.tab;
                if (tab) this._switchSettingsTab(tab);
            });
        });

        // Profile pane: Save / Revert buttons. Username + session name
        // changes only commit on Save; close-without-save reverts.
        const profileSave = panel.querySelector('#settings-profile-save');
        if (profileSave) {
            profileSave.addEventListener('click', () => this._saveProfile());
        }
        const profileRevert = panel.querySelector('#settings-profile-revert');
        if (profileRevert) {
            profileRevert.addEventListener('click', () => this._revertProfile());
        }
        ['#settings-username', '#settings-session'].forEach(sel => {
            const el = panel.querySelector(sel);
            if (el) {
                el.addEventListener('input', () => {
                    const status = panel.querySelector('#settings-profile-status');
                    if (!status) return;
                    status.textContent = 'Unsaved changes are discarded on close';
                    status.removeAttribute('data-state');
                });
            }
        });

        // Theme mode toggle (light/dark/system) -- live preview only
        this.setupThemeToggle();

        // Theme color picker -- live preview only
        this.setupColorPicker();

        // Appearance pane: Save / Revert
        const apprSave = panel.querySelector('#settings-appearance-save');
        if (apprSave) {
            apprSave.addEventListener('click', () => this._saveAppearance());
        }
        const apprRevert = panel.querySelector('#settings-appearance-revert');
        if (apprRevert) {
            apprRevert.addEventListener('click', () => this._revertAppearance());
        }

        // End Session footer link -> show confirm popover
        const endLink = panel.querySelector('#settings-end-session');
        const endConfirm = panel.querySelector('#settings-end-confirm');
        const endCancel = panel.querySelector('#settings-end-cancel');
        const endYes = panel.querySelector('#settings-end-confirm-yes');
        if (endLink && endConfirm) {
            endLink.addEventListener('click', () => {
                endConfirm.removeAttribute('hidden');
            });
        }
        if (endCancel && endConfirm) {
            endCancel.addEventListener('click', () => endConfirm.setAttribute('hidden', ''));
            // Click the dimmed area (but not the inner card) to dismiss.
            endConfirm.addEventListener('click', (e) => {
                if (e.target === endConfirm) endConfirm.setAttribute('hidden', '');
            });
        }
        if (endYes) {
            endYes.addEventListener('click', () => {
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

        // Credentials Save button (Git HTTPS only)
        const credSaveBtn = panel.querySelector('#settings-cred-save');
        if (credSaveBtn) {
            credSaveBtn.addEventListener('click', () => this._saveCredentials());
        }
        // Test connection button (Git HTTPS)
        const credTestBtn = panel.querySelector('#settings-cred-test');
        if (credTestBtn) {
            credTestBtn.addEventListener('click', () => this._testCredentials());
        }
        // Forget HTTPS on this device (revokes the shared trust + stored PAT)
        const credForget = panel.querySelector('#settings-cred-forget');
        if (credForget) {
            credForget.addEventListener('click', () => this._forgetCredsOnThisDevice());
        }
        // Re-populate from localStorage when the host changes
        const credHost = panel.querySelector('#settings-cred-host');
        if (credHost) {
            credHost.addEventListener('change', () => this.populateCredentialsSection());
        }

        // SSH signing pane: Save key + Verify key + Forget on this device
        const sigSave = panel.querySelector('#settings-cred-signing-save');
        if (sigSave) {
            sigSave.addEventListener('click', () => this._saveSigningKey());
        }
        const sigVerify = panel.querySelector('#settings-cred-signing-verify');
        if (sigVerify) {
            sigVerify.addEventListener('click', () => this._verifySigningKey());
        }
        const sigForget = panel.querySelector('#settings-cred-signing-forget');
        if (sigForget) {
            sigForget.addEventListener('click', () => this._forgetSigningKeyOnThisDevice());
        }
        this._refreshSigningForgetButton();
    }

    // Show/hide the "Forget on this device" button based on whether
    // a trust entry exists for this (origin, init_sha) pair.
    _refreshSigningForgetButton() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const btn = panel.querySelector('#settings-cred-signing-forget');
        if (!btn) return;
        const initSha = this.dataset.initSha || '';
        const trust = initSha ? this._readSigningTrust(initSha) : null;
        btn.hidden = !(trust && !this._signingTrustExpired(trust));
    }

    _forgetSigningKeyOnThisDevice() {
        const initSha = this.dataset.initSha || '';
        if (!initSha) return;
        const trust = this._readSigningTrust(initSha);
        this._clearSigningTrust(initSha);
        // Best-effort: also clear the key blob if no other trust entry
        // references this fingerprint. Walk all swe-swe:signing-trust:*
        // keys looking for any survivor pointing at the same fingerprint.
        if (trust && trust.fingerprint) {
            let othersRefer = false;
            try {
                for (let i = 0; i < localStorage.length; i++) {
                    const k = localStorage.key(i);
                    if (!k || !k.startsWith('swe-swe:signing-trust:')) continue;
                    const raw = localStorage.getItem(k);
                    if (!raw) continue;
                    try {
                        const v = JSON.parse(raw);
                        if (v && v.fingerprint === trust.fingerprint) {
                            othersRefer = true;
                            break;
                        }
                    } catch (e) {}
                }
            } catch (e) { othersRefer = true; }
            if (!othersRefer) {
                try { localStorage.removeItem(this._signingKeyByFpKey(trust.fingerprint)); } catch (e) {}
            }
        }
        this._refreshSigningForgetButton();
        const sigStatus = this.querySelector('#settings-cred-signing-status');
        if (sigStatus) {
            sigStatus.textContent = 'Forgotten on this device. Re-save to trust again.';
            sigStatus.removeAttribute('data-state');
        }
    }

    // Offer to trust this browser to auto-send saved HTTPS credentials on
    // future visits to this repo. Mirrors the signing-key trust prompt and
    // reuses the same (origin, init_sha) entry so one decision covers both
    // the PAT and the key. Only when TLS/loopback-safe and not already
    // trusted.
    _maybePromptCredsTrust() {
        const initSha = this.dataset.initSha || '';
        if (!initSha || !this._signingAutoSendSafe()) return;
        const existing = this._readSigningTrust(initSha);
        if (existing && !this._signingTrustExpired(existing)) return;
        const shortSha = initSha.slice(0, 7);
        const msg = 'Trust this browser to auto-send your saved HTTPS credentials on future visits to this repo (init ' + shortSha + ')?\n\nYou can revoke this with "Forget HTTPS on this device" in Settings > Git HTTPS.';
        try {
            if (window.confirm(msg)) this._writeCredsTrust(initSha);
        } catch (e) {}
        this._refreshCredsForgetButton();
    }

    // Show/hide "Forget HTTPS on this device" based on whether a trust
    // entry exists for this (origin, init_sha) pair (shared with signing).
    _refreshCredsForgetButton() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const btn = panel.querySelector('#settings-cred-forget');
        if (!btn) return;
        const initSha = this.dataset.initSha || '';
        const trust = initSha ? this._readSigningTrust(initSha) : null;
        btn.hidden = !(trust && !this._signingTrustExpired(trust));
    }

    // Revoke trust + drop the stored PAT for the resolved host on this
    // browser. The trust entry is shared with signing, so this also stops
    // the signing key auto-restoring -- one decision covers both, matching
    // how the trust was granted.
    _forgetCredsOnThisDevice() {
        const initSha = this.dataset.initSha || '';
        if (initSha) this._clearSigningTrust(initSha);
        const host = this._resolvedCredHost();
        try { localStorage.removeItem(this._credsLocalKey(host)); } catch (e) {}
        this._refreshCredsForgetButton();
        this._refreshSigningForgetButton();
        const status = this.querySelector('#settings-cred-status');
        if (status) {
            status.textContent = 'Forgotten on this device. Re-save to trust again.';
            status.removeAttribute('data-state');
        }
    }

    // Switch between sidebar nav panes.
    _switchSettingsTab(tab) {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        panel.querySelectorAll('.settings-panel__nav-item').forEach(btn => {
            const active = btn.dataset.tab === tab;
            btn.classList.toggle('settings-panel__nav-item--active', active);
            btn.setAttribute('aria-selected', active ? 'true' : 'false');
        });
        panel.querySelectorAll('.settings-panel__pane').forEach(p => {
            const active = p.dataset.pane === tab;
            p.classList.toggle('settings-panel__pane--active', active);
            if (active) p.removeAttribute('hidden');
            else p.setAttribute('hidden', '');
        });
        // Pane-specific re-render hooks. Trust state can change at any
        // time (another tab updated localStorage, or the user just
        // pasted/cleared something) so the SSH tab re-derives its
        // Forget button visibility each time it is shown.
        if (tab === 'ssh') {
            this._refreshSigningForgetButton();
            this._refreshLocalSigningOverridesWarning();
        }
    }

    // Render a warning at the top of the SSH Signing pane when the
    // current workdir's .git/config has signing-related keys set. These
    // beat the per-session GIT_CONFIG_GLOBAL git resolves so even with
    // a registered signing key, commits silently route through whatever
    // the local config says -- typically a stale `gpg.format = openpgp`
    // left over from the host's gitconfig. The fix is one-line
    // `git config --local --unset <key>`.
    _refreshLocalSigningOverridesWarning() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const pane = panel.querySelector('.settings-panel__pane[data-pane="ssh"]');
        if (!pane) return;
        const overrides = (this.dataset.localGpgOverrides || '').trim();
        const id = 'settings-cred-signing-local-overrides';
        let banner = pane.querySelector('#' + id);
        if (!overrides) {
            if (banner) banner.remove();
            return;
        }
        if (!banner) {
            banner = document.createElement('p');
            banner.id = id;
            banner.className = 'settings-panel__hint settings-panel__hint--warn';
            const title = pane.querySelector('.settings-panel__pane-title');
            if (title && title.nextSibling) {
                title.parentNode.insertBefore(banner, title.nextSibling);
            } else {
                pane.prepend(banner);
            }
        }
        banner.textContent =
            "This repo's .git/config overrides signing config: " +
            overrides +
            ". Local config wins over the per-session settings; commits will use the local values. Unset with `git config --local --unset <key>` to let swe-swe handle signing.";
    }

    // Snapshot the values that are revertable on close-without-save.
    // Theme/color also have their localStorage state so revert can put
    // both the visual and the persistence back where they were.
    _snapshotSettings() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return null;
        return {
            username: this.currentUserName || '',
            sessionName: this.sessionName || '',
            themeMode: window.sweSweTheme?.getStoredMode?.() || 'system',
            color: window.sweSweTheme?.getCurrentColor?.() || '#7c3aed',
        };
    }

    _saveProfile() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const status = panel.querySelector('#settings-profile-status');
        const usernameInput = panel.querySelector('#settings-username');
        const sessionInput = panel.querySelector('#settings-session');

        // Validate username + session name before committing either.
        const usernameVal = (usernameInput?.value || '').trim();
        const sessionVal = (sessionInput?.value || '').trim();
        const userValid = validateUsername(usernameVal);
        const sessValid = validateSessionName(sessionVal);
        if (!userValid.valid) {
            if (status) {
                status.textContent = 'Username: ' + (userValid.error || 'invalid');
                status.setAttribute('data-state', 'err');
            }
            return;
        }
        if (!sessValid.valid) {
            if (status) {
                status.textContent = 'Session name: ' + (sessValid.error || 'invalid');
                status.setAttribute('data-state', 'err');
            }
            return;
        }

        if (userValid.name !== this.currentUserName) {
            this.setUsername(userValid.name);
        }
        if (sessValid.name !== this.sessionName) {
            this.setSessionName(sessValid.name);
        }

        // Update snapshot so a subsequent close doesn't revert what we just saved.
        if (this._settingsSnapshot) {
            this._settingsSnapshot.username = userValid.name;
            this._settingsSnapshot.sessionName = sessValid.name;
        }

        if (status) {
            status.textContent = 'Saved.';
            status.setAttribute('data-state', 'ok');
        }
    }

    _revertProfile({ silent = false } = {}) {
        const panel = this.querySelector('.settings-panel');
        if (!panel || !this._settingsSnapshot) return;
        const usernameInput = panel.querySelector('#settings-username');
        const sessionInput = panel.querySelector('#settings-session');
        if (usernameInput) usernameInput.value = this._settingsSnapshot.username || '';
        if (sessionInput) sessionInput.value = this._settingsSnapshot.sessionName || '';
        if (!silent) {
            const status = panel.querySelector('#settings-profile-status');
            if (status) {
                status.textContent = 'Reverted.';
                status.removeAttribute('data-state');
            }
        }
    }

    _saveAppearance() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const status = panel.querySelector('#settings-appearance-status');

        // Theme mode + color are already applied live; persist them now.
        const mode = this._currentPreviewThemeMode || window.sweSweTheme?.getStoredMode?.() || 'system';
        const color = (panel.querySelector('#settings-color-input')?.value || '#7c3aed').toLowerCase();
        if (window.sweSweTheme?.setThemeMode) {
            window.sweSweTheme.setThemeMode(mode);
        }
        if (window.sweSweTheme?.saveColorPreference && this.uuid) {
            const sessionKey = window.sweSweTheme.COLOR_STORAGE_KEYS.SESSION_PREFIX + this.uuid;
            window.sweSweTheme.saveColorPreference(sessionKey, color);
        }
        // Update URL once on save (not on every preview tweak).
        this.updateUrlColor(color);

        // Refresh snapshot so close-without-save doesn't revert.
        if (this._settingsSnapshot) {
            this._settingsSnapshot.themeMode = mode;
            this._settingsSnapshot.color = color;
        }
        if (status) {
            status.textContent = 'Saved.';
            status.setAttribute('data-state', 'ok');
        }
    }

    _revertAppearance({ silent = false } = {}) {
        if (!this._settingsSnapshot) return;
        const snap = this._settingsSnapshot;
        // Re-apply visual mode + color from snapshot.
        if (window.sweSweTheme?.applyMode) {
            window.sweSweTheme.applyMode(snap.themeMode);
        } else if (window.sweSweTheme?.setThemeMode) {
            // Fallback: setThemeMode persists too, which matches snapshot's stored value.
            window.sweSweTheme.setThemeMode(snap.themeMode);
        }
        if (window.sweSweTheme?.applyTheme) {
            window.sweSweTheme.applyTheme(snap.color);
        }
        this._currentPreviewThemeMode = snap.themeMode;

        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        // Sync UI controls back to snapshot values.
        this.populateThemeToggle();
        this.populateColorPicker();
        if (!silent) {
            const status = panel.querySelector('#settings-appearance-status');
            if (status) {
                status.textContent = 'Reverted.';
                status.removeAttribute('data-state');
            }
        }
    }

    _saveCredentials() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const host = (panel.querySelector('#settings-cred-host')?.value || 'github.com').trim();
        const username = (panel.querySelector('#settings-cred-username')?.value || '').trim();
        const token = panel.querySelector('#settings-cred-token')?.value || '';
        const name = (panel.querySelector('#settings-cred-name')?.value || '').trim();
        const email = (panel.querySelector('#settings-cred-email')?.value || '').trim();
        if (!host || !token) {
            const status = panel.querySelector('#settings-cred-status');
            if (status) {
                status.textContent = 'Host and token are required.';
                status.setAttribute('data-state', 'err');
            }
            return;
        }
        this._writeCredsLocal(host, { username, token, name, email });
        // Mark this as a user-initiated save so the credentials_stored ack
        // can offer to trust this browser for auto-send (vs an auto-send,
        // which must not re-prompt).
        this._credsManualSaveInFlight = true;
        this.sendJSON({ type: 'set_credentials', data: { host, username, token, name, email } });
        const status = panel.querySelector('#settings-cred-status');
        if (status) {
            status.textContent = 'Sending...';
            status.removeAttribute('data-state');
        }
    }

    _testCredentials() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const status = panel.querySelector('#settings-cred-status');
        const host = (panel.querySelector('#settings-cred-host')?.value || 'github.com').trim();
        const username = (panel.querySelector('#settings-cred-username')?.value || '').trim();
        const token = panel.querySelector('#settings-cred-token')?.value || '';
        if (!host || !token) {
            if (status) {
                status.textContent = 'Host and token are required to test.';
                status.setAttribute('data-state', 'err');
            }
            return;
        }
        this.sendJSON({ type: 'test_credentials', data: { host, username, token } });
        if (status) {
            status.textContent = 'Testing connection...';
            status.removeAttribute('data-state');
        }
    }

    _saveSigningKey() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const sigStatus = panel.querySelector('#settings-cred-signing-status');
        const signingKey = panel.querySelector('#settings-cred-signing-key')?.value || '';
        const signingPassphrase = panel.querySelector('#settings-cred-signing-passphrase')?.value || '';
        const signingLabel = (panel.querySelector('#settings-cred-signing-label')?.value || '').trim();
        if (!signingKey.trim()) {
            if (sigStatus) {
                sigStatus.textContent = 'Paste a private key first.';
                sigStatus.setAttribute('data-state', 'err');
            }
            return;
        }
        this._writeSigningLocal({ privateKey: signingKey, label: signingLabel });
        // Stash the just-pasted PEM so the signing_key_stored handler can
        // persist it under the fingerprint slot for auto-restore. The
        // user could have closed the Settings panel by the time the ack
        // arrives, so reading from the form again is unreliable.
        this._signingPendingSave = { pem: signingKey, label: signingLabel };
        this.sendJSON({
            type: 'set_signing_key',
            data: {
                signing_private_key_pem: signingKey,
                signing_passphrase: signingPassphrase,
                signing_key_label: signingLabel,
            },
        });
        if (sigStatus) {
            sigStatus.textContent = 'Sending signing key...';
            sigStatus.removeAttribute('data-state');
        }
    }

    // Fired from the signing_key_stored handler when the server confirms
    // a fingerprint. Persists the key under its fingerprint slot and, if
    // this came from a user-initiated Save (not an auto-restore) and no
    // trust binding exists for this repo, prompts the user to add one.
    _handleSigningKeyStored(fingerprint) {
        if (this._signingAutoRestoreInFlight) {
            // Auto-restored from an existing trust entry. Refresh the
            // savedAt timestamp so the 90-day TTL extends with use.
            const initSha = this.dataset.initSha || '';
            if (initSha) this._writeSigningTrust(initSha, fingerprint);
            return;
        }
        if (this._signingPendingSave && this._signingPendingSave.pem) {
            this._writeSigningKeyByFp(fingerprint, this._signingPendingSave);
            this._signingPendingSave = null;
        }
        // Prompt for trust binding only when (a) this server told us
        // there is a repo to bind to, (b) we have not already trusted
        // this fingerprint for this repo, and (c) the connection is
        // TLS-protected so the auto-restore could actually fire later.
        const initSha = this.dataset.initSha || '';
        if (!initSha || !this._signingAutoSendSafe()) return;
        const existing = this._readSigningTrust(initSha);
        if (existing && existing.fingerprint === fingerprint) return;
        const shortSha = initSha.slice(0, 7);
        const msg = 'Trust this browser to auto-load this signing key on future visits to this repo (init ' + shortSha + ')?\n\nYou can revoke this with "Forget on this device" in Settings > SSH Signing.';
        try {
            if (window.confirm(msg)) {
                this._writeSigningTrust(initSha, fingerprint);
            }
        } catch (e) {}
        this._refreshSigningForgetButton();
    }

    _verifySigningKey() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const sigStatus = panel.querySelector('#settings-cred-signing-status');
        const signingKey = panel.querySelector('#settings-cred-signing-key')?.value || '';
        const signingPassphrase = panel.querySelector('#settings-cred-signing-passphrase')?.value || '';
        const signingLabel = (panel.querySelector('#settings-cred-signing-label')?.value || '').trim();
        if (!signingKey.trim()) {
            // Form is empty -- but a key may already be registered server-side
            // (e.g. auto-restored this session, with the PEM not re-pasted).
            // Verify that stored key instead of telling the user to paste one.
            if (this._signingFingerprint) {
                this.sendJSON({ type: 'verify_stored_signing_key' });
                if (sigStatus) {
                    sigStatus.textContent = 'Verifying registered key...';
                    sigStatus.removeAttribute('data-state');
                }
                return;
            }
            if (sigStatus) {
                sigStatus.textContent = 'Paste a private key first.';
                sigStatus.setAttribute('data-state', 'err');
            }
            return;
        }
        this.sendJSON({
            type: 'verify_signing_key',
            data: {
                signing_private_key_pem: signingKey,
                signing_passphrase: signingPassphrase,
                signing_key_label: signingLabel,
            },
        });
        if (sigStatus) {
            sigStatus.textContent = 'Verifying...';
            sigStatus.removeAttribute('data-state');
        }
    }

    // localStorage layout for signing:
    //
    //   swe-swe-signing:default
    //     Legacy single-bag layout (kept so the textarea rehydrate
    //     path in renderSettingsPanel keeps working). { privateKey, label }
    //
    //   swe-swe:signing-key:<fingerprint>
    //     One entry per ed25519 key seen. { pem, label, fingerprint, savedAt }
    //     savedAt is ms-since-epoch for the 90-day TTL check.
    //
    //   swe-swe:signing-trust:<origin>|<init_sha>
    //     One entry per (this browser, this server, this repo).
    //     { fingerprint, savedAt }. Presence of this entry is the user's
    //     explicit "auto-restore the key with this fingerprint when I
    //     visit this server+repo from this browser" consent.
    //
    // Why two-level: a user can have one signing identity that they
    // trust in multiple repos -- the key blob lives once, the trust
    // entry references it by fingerprint. "Forget on this device" for
    // a single repo removes the trust entry but leaves the key, so
    // other trusted repos keep working.
    //
    // The passphrase is intentionally NEVER stored. Encrypted keys
    // require the user to retype the passphrase the first time per
    // browser; once we hand the parsed key to the server, it stays in
    // server memory for the lifetime of the session.
    _signingLocalKey() {
        return 'swe-swe-signing:default';
    }
    _signingKeyByFpKey(fp) {
        return 'swe-swe:signing-key:' + fp;
    }
    _signingTrustKey(initSha) {
        return 'swe-swe:signing-trust:' + window.location.origin + '|' + initSha;
    }
    _readSigningLocal() {
        try {
            const raw = localStorage.getItem(this._signingLocalKey());
            return raw ? JSON.parse(raw) : null;
        } catch (e) {
            return null;
        }
    }
    _writeSigningLocal(bag) {
        try {
            const payload = {
                privateKey: bag.privateKey || '',
                label: bag.label || '',
            };
            localStorage.setItem(this._signingLocalKey(), JSON.stringify(payload));
        } catch (e) {}
    }
    _readSigningKeyByFp(fp) {
        if (!fp) return null;
        try {
            const raw = localStorage.getItem(this._signingKeyByFpKey(fp));
            return raw ? JSON.parse(raw) : null;
        } catch (e) {
            return null;
        }
    }
    _writeSigningKeyByFp(fp, payload) {
        if (!fp) return;
        try {
            const data = {
                pem: payload.pem || '',
                label: payload.label || '',
                fingerprint: fp,
                savedAt: Date.now(),
            };
            localStorage.setItem(this._signingKeyByFpKey(fp), JSON.stringify(data));
        } catch (e) {}
    }
    _readSigningTrust(initSha) {
        if (!initSha) return null;
        try {
            const raw = localStorage.getItem(this._signingTrustKey(initSha));
            return raw ? JSON.parse(raw) : null;
        } catch (e) {
            return null;
        }
    }
    _writeSigningTrust(initSha, fingerprint) {
        if (!initSha || !fingerprint) return;
        try {
            const data = { fingerprint: fingerprint, savedAt: Date.now() };
            localStorage.setItem(this._signingTrustKey(initSha), JSON.stringify(data));
        } catch (e) {}
    }
    _clearSigningTrust(initSha) {
        if (!initSha) return;
        try {
            localStorage.removeItem(this._signingTrustKey(initSha));
        } catch (e) {}
    }
    // Refresh only the savedAt of an existing trust entry (extends the
    // 90-day TTL), preserving any bound signing fingerprint. Used after a
    // successful auto-send so active use keeps the trust fresh. No-op when
    // there is no entry.
    _touchSigningTrust(initSha) {
        if (!initSha) return;
        const existing = this._readSigningTrust(initSha);
        if (!existing) return;
        try {
            localStorage.setItem(this._signingTrustKey(initSha), JSON.stringify({
                fingerprint: existing.fingerprint || '',
                savedAt: Date.now(),
            }));
        } catch (e) {}
    }
    // Create/refresh the (origin, init_sha) trust entry from the HTTPS
    // side, preserving any signing fingerprint already bound. Presence of
    // this entry is the user's consent to auto-send stored secrets for
    // this repo -- the same entry the signing key uses.
    _writeCredsTrust(initSha) {
        if (!initSha) return;
        const existing = this._readSigningTrust(initSha) || {};
        try {
            localStorage.setItem(this._signingTrustKey(initSha), JSON.stringify({
                fingerprint: existing.fingerprint || '',
                savedAt: Date.now(),
            }));
        } catch (e) {}
    }

    // Trust entries older than 90 days require the user to re-confirm.
    // Cuts long-tail exposure from stale trust on a browser that hasn't
    // been used in a while.
    static get SIGNING_TRUST_TTL_MS() { return 90 * 24 * 60 * 60 * 1000; }
    _signingTrustExpired(trust) {
        if (!trust || typeof trust.savedAt !== 'number') return true;
        return Date.now() - trust.savedAt > TerminalUI.SIGNING_TRUST_TTL_MS;
    }

    // Auto-restore is gated on the connection being TLS-protected. On
    // plain ws://, an on-path attacker would receive the PEM in the
    // clear, so we refuse to auto-send and require the user to use the
    // form (which makes the trust gesture explicit). Loopback hostnames
    // are treated as safe because there is no network for an attacker
    // to sit on -- this mirrors how browsers themselves treat localhost
    // as a "secure context" for capabilities like service workers.
    _signingAutoSendSafe() {
        try {
            if (window.location.protocol === 'https:') return true;
            const host = window.location.hostname;
            return host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1' || host === 'host.docker.internal';
        } catch (e) {
            return false;
        }
    }

    // The host the stored HTTPS credentials are keyed under for this repo:
    // the workdir's origin remote host (server-injected), else github.com.
    _resolvedCredHost() {
        const h = (this.dataset.localRemoteHost || '').trim();
        return h || 'github.com';
    }

    // Called from this.ws.onopen. Looks up the (origin, init_sha) trust
    // entry; if present, valid, and TLS-protected (or loopback), auto-sends
    // the trusted secrets so the user does not re-enter them each session.
    //
    // When both a bound signing key AND stored HTTPS creds for the resolved
    // host exist, sends ONE combined set_credentials carrying both -- the
    // server stores creds + author + key and rewrites the per-session
    // gitconfig exactly once. Key-only falls back to set_signing_key;
    // creds-only sends set_credentials without signing fields.
    //
    // Security: this widens the window where a browser-held PAT leaves the
    // browser, gated identically to the shipped signing auto-restore --
    // explicit per-device trust bound to (origin, init_sha) + TLS/loopback
    // only. The init_sha binding covers the recycled-hostname attack. No
    // new trust assumption beyond what signing already made.
    _maybeAutoConnectSecrets() {
        const initSha = this.dataset.initSha || '';
        if (!initSha) return;
        if (!this._signingAutoSendSafe()) return;
        const trust = this._readSigningTrust(initSha);
        if (!trust) return;
        if (this._signingTrustExpired(trust)) {
            this._clearSigningTrust(initSha);
            return;
        }
        // Encrypted keys would need a passphrase, which we never persist,
        // so only unencrypted keys auto-restore. If the server rejects a
        // PEM, _signingError surfaces it and we drop the trust below.
        const key = trust.fingerprint ? this._readSigningKeyByFp(trust.fingerprint) : null;
        const haveKey = !!(key && key.pem);
        const host = this._resolvedCredHost();
        const creds = this._readCredsLocal(host);
        const haveCreds = !!(creds && creds.token);

        if (haveCreds) {
            // ONE combined message (carries the key too when present).
            this._autoConnectInFlight = true;
            this.sendJSON({
                type: 'set_credentials',
                data: {
                    host: host,
                    username: creds.username || '',
                    token: creds.token,
                    name: creds.name || '',
                    email: creds.email || '',
                    signing_private_key_pem: haveKey ? key.pem : '',
                    signing_passphrase: '',
                    signing_key_label: haveKey ? (key.label || '') : '',
                },
            });
        } else if (haveKey) {
            this._signingAutoRestoreInFlight = true;
            this.sendJSON({
                type: 'set_signing_key',
                data: {
                    signing_private_key_pem: key.pem,
                    signing_passphrase: '',
                    signing_key_label: key.label || '',
                },
            });
        }
    }

    _refreshSigningStatus() {
        const status = this.querySelector('#settings-cred-signing-status');
        if (status) {
            if (this._signingError) {
                status.textContent = 'Signing key error: ' + this._signingError;
                status.setAttribute('data-state', 'err');
            } else if (this._signingFingerprint) {
                status.textContent = 'Signing key registered: ' + this._signingFingerprint;
                status.setAttribute('data-state', 'ok');
            } else if (this._signingVerified) {
                status.textContent = 'Verified: ' + this._signingVerified;
                status.setAttribute('data-state', 'ok');
            } else {
                status.textContent = '';
                status.removeAttribute('data-state');
            }
        }
        this._refreshSettingsNavBadges();
        this._refreshSigningActiveIndicator();
    }

    // One-line indicator on the SSH Signing pane stating whether commit
    // signing will actually verify locally. Driven by the server-computed
    // session_cred_state snapshot: "verifies locally" when a key is
    // registered, an author email is resolvable, and no local .git/config
    // override shadows the per-session config; otherwise the blocking
    // reason ("no email", "local .git/config override", ...). Only shown
    // once we have heard a snapshot AND a key is registered -- with no key
    // the pane's normal "paste a key" affordance is the right message.
    _refreshSigningActiveIndicator() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const pane = panel.querySelector('.settings-panel__pane[data-pane="ssh"]');
        if (!pane) return;
        const id = 'settings-cred-signing-active';
        let line = pane.querySelector('#' + id);
        if (!this._signingStateKnown || !this._signingFingerprint) {
            if (line) line.remove();
            return;
        }
        if (!line) {
            line = document.createElement('p');
            line.id = id;
            line.className = 'settings-panel__hint';
            const footer = pane.querySelector('.settings-panel__pane-footer');
            if (footer) {
                footer.parentNode.insertBefore(line, footer);
            } else {
                pane.appendChild(line);
            }
        }
        if (this._signingActive) {
            line.textContent = 'Signing: verifies locally';
            line.setAttribute('data-state', 'ok');
            line.classList.remove('settings-panel__hint--warn');
        } else {
            const reason = this._signingInactiveReason || 'inactive';
            line.textContent = 'Signing: inactive -- ' + reason;
            line.setAttribute('data-state', 'warn');
            line.classList.add('settings-panel__hint--warn');
        }
    }

    // Update the small badges next to "Git HTTPS" / "SSH Signing" in the
    // settings sidebar so users can see config state at a glance.
    _refreshSettingsNavBadges() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const gitBadge = panel.querySelector('#settings-nav-badge-git');
        const sshBadge = panel.querySelector('#settings-nav-badge-ssh');
        const hasGit = Array.isArray(this._credsStoredHosts) && this._credsStoredHosts.length > 0;
        if (gitBadge) {
            if (hasGit) {
                gitBadge.textContent = 'Saved';
                gitBadge.removeAttribute('hidden');
                gitBadge.setAttribute('data-state', 'ok');
            } else {
                gitBadge.setAttribute('hidden', '');
            }
        }
        if (sshBadge) {
            if (this._signingFingerprint) {
                sshBadge.textContent = 'On';
                sshBadge.removeAttribute('hidden');
                sshBadge.setAttribute('data-state', 'ok');
            } else {
                sshBadge.setAttribute('hidden', '');
            }
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
        // Mobile popout button: opens the currently-selected pane in a new
        // browser tab. switchMobileNav() controls hidden-state.
        const mobileNavPopout = this.querySelector('.terminal-ui__mobile-nav-popout');
        if (mobileNavPopout) {
            mobileNavPopout.title = this._popoutHintText();
            mobileNavPopout.addEventListener('click', () => {
                const sel = this.querySelector('.terminal-ui__mobile-nav-select');
                const url = sel && this.panePopoutUrl(sel.value);
                if (url) window.open(url, '_blank');
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

        // Per-session git credentials
        this.populateCredentialsSection();
    }

    // Populate the credentials section from localStorage. Token is intentionally
    // re-prompted each session if not already in localStorage, so a shared
    // browser doesn't auto-leak the secret to a different operator.
    populateCredentialsSection() {
        const panel = this.querySelector('.settings-panel');
        if (!panel) return;
        const hostInput = panel.querySelector('#settings-cred-host');
        const userInput = panel.querySelector('#settings-cred-username');
        const tokenInput = panel.querySelector('#settings-cred-token');
        const nameInput = panel.querySelector('#settings-cred-name');
        const emailInput = panel.querySelector('#settings-cred-email');
        if (!hostInput) return;
        // Autofill the Host from the repo's origin remote on first open so a
        // non-github forge's stored creds apply without switching Host.
        // Only when empty -- never clobber a value the user typed.
        if (!hostInput.value) {
            hostInput.value = (this.dataset.localRemoteHost || 'github.com').trim();
        }
        const host = (hostInput.value || 'github.com').trim();
        const stored = this._readCredsLocal(host);
        if (stored) {
            if (userInput) userInput.value = stored.username || '';
            if (tokenInput) tokenInput.value = stored.token || '';
            if (nameInput) nameInput.value = stored.name || '';
            if (emailInput) emailInput.value = stored.email || '';
        }
        // If the session's WorkDir has local user.name/user.email in
        // .git/config, those override anything we'd inject via
        // GIT_CONFIG_GLOBAL. Show the local values readonly with an
        // explainer so the user understands why the form isn't editable.
        const localName = this.dataset.localUserName || '';
        const localEmail = this.dataset.localUserEmail || '';
        const overrideActive = !!(localName || localEmail);
        if (nameInput) {
            if (overrideActive) {
                nameInput.value = localName;
                nameInput.readOnly = true;
                nameInput.classList.add('settings-panel__input--readonly');
            } else {
                nameInput.readOnly = false;
                nameInput.classList.remove('settings-panel__input--readonly');
            }
        }
        if (emailInput) {
            if (overrideActive) {
                emailInput.value = localEmail;
                emailInput.readOnly = true;
                emailInput.classList.add('settings-panel__input--readonly');
            } else {
                emailInput.readOnly = false;
                emailInput.classList.remove('settings-panel__input--readonly');
            }
        }
        const explainerId = 'settings-cred-local-override';
        let explainer = panel.querySelector('#' + explainerId);
        if (overrideActive) {
            if (!explainer && nameInput) {
                explainer = document.createElement('p');
                explainer.id = explainerId;
                explainer.className = 'settings-panel__hint settings-panel__hint--warn';
                explainer.textContent = "Author name and email are set in this repo's local .git/config";
                nameInput.closest('.settings-panel__field-row').parentNode.insertBefore(
                    explainer,
                    nameInput.closest('.settings-panel__field-row')
                );
            }
        } else if (explainer) {
            explainer.remove();
        }
        this._refreshCredsStatus();
        this._refreshCredsForgetButton();

        // Rehydrate the signing key textarea + label from localStorage.
        // Passphrase intentionally stays blank -- not persisted.
        const sigKey = panel.querySelector('#settings-cred-signing-key');
        const sigLabel = panel.querySelector('#settings-cred-signing-label');
        const sigPass = panel.querySelector('#settings-cred-signing-passphrase');
        const sigBag = this._readSigningLocal();
        if (sigKey && sigBag) sigKey.value = sigBag.privateKey || '';
        if (sigLabel && sigBag) sigLabel.value = sigBag.label || '';
        if (sigPass) sigPass.value = '';
        this._refreshSigningStatus();
    }

    _credsLocalKey(host) {
        return 'swe-swe-creds:' + host;
    }

    _readCredsLocal(host) {
        try {
            const raw = localStorage.getItem(this._credsLocalKey(host));
            return raw ? JSON.parse(raw) : null;
        } catch (e) {
            return null;
        }
    }

    _writeCredsLocal(host, bag) {
        try {
            localStorage.setItem(this._credsLocalKey(host), JSON.stringify(bag));
        } catch (e) {}
    }

    _refreshCredsStatus() {
        const status = this.querySelector('#settings-cred-status');
        if (!status) return;
        const hosts = Array.isArray(this._credsStoredHosts) ? this._credsStoredHosts : [];
        if (hosts.length === 0) {
            status.textContent = 'Not yet sent to server.';
            status.removeAttribute('data-state');
        } else {
            status.textContent = 'Stored on server for: ' + hosts.join(', ');
            status.setAttribute('data-state', 'ok');
        }
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

    // Setup theme toggle click handler. Live preview only -- the change
    // is visible immediately but does not persist to localStorage until
    // the user presses Save in the Appearance pane (or revert kicks in).
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

            // Visual-only apply (no localStorage write).
            this._currentPreviewThemeMode = mode;
            if (window.sweSweTheme?.applyMode) {
                window.sweSweTheme.applyMode(mode);
            } else if (window.sweSweTheme?.setThemeMode) {
                // Old API fallback -- this also persists, but the snapshot
                // restore will undo it on close-without-save.
                window.sweSweTheme.setThemeMode(mode);
            }
            this._markAppearanceDirty();
        });

        // Listen for theme mode changes to update xterm
        window.addEventListener('theme-mode-changed', (e) => {
            if (this.term) {
                const resolved = e.detail?.resolved;
                this.term.options.theme = resolved === 'light' ? LIGHT_XTERM_THEME : DARK_XTERM_THEME;
            }
        });
    }

    _markAppearanceDirty() {
        const status = this.querySelector('#settings-appearance-status');
        if (!status) return;
        status.textContent = 'Unsaved preview -- close without saving to revert.';
        status.removeAttribute('data-state');
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

    // Setup color picker event listeners. All paths route through
    // selectColor() which is now preview-only; persistence happens on
    // explicit Save in the Appearance pane.
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

        // Reset button: preview the default color. Save persists; close-no-save reverts.
        if (colorReset) {
            colorReset.addEventListener('click', () => {
                this.selectColor('#7c3aed');
            });
        }
    }

    // Preview a color. Visual-only: applies CSS vars across the app so
    // the user can see the change but does not persist to localStorage
    // or update the URL until Save is pressed in the Appearance pane.
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

        // Apply theme (visual only)
        if (window.sweSweTheme?.applyTheme) {
            window.sweSweTheme.applyTheme(color);
        }
        this._markAppearanceDirty();
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
                this._positionGutters();
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

        // Apply persisted track-size overrides so the grid cells already
        // resolve to the user's saved widths/heights before pane-hosts are
        // attached. Gutter positioning happens AFTER pane-host assignment
        // because it reads pane-host bounding rects to place itself.
        this._applySizes();

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

        // Render gutters AFTER pane-host gridArea assignment so positioning
        // can read the correct cell rects. Positioning still goes through
        // one rAF because grid layout flushes asynchronously after style
        // mutations.
        this._renderGutters();
        requestAnimationFrame(() => this._positionGutters());

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
            saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot, sizesByPreset: this.sizesByPreset });
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
            saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot, sizesByPreset: this.sizesByPreset });
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
        saveLayoutState({ preset: this.preset, activeBySlot: this.activeBySlot, sizesByPreset: this.sizesByPreset });
        this.applyPreset();
    }

    // Persist a single gutter-resized track. Called from the gutter drag end
    // handler. `axis` is 'cols' or 'rows'. Pass `sizes === null` (e.g. on
    // dblclick reset) to clear the entry; the preset then falls back to the
    // default fr ratios baked into terminal-ui.css.
    _persistGutterSize(presetId, axis, sizes) {
        const cur = this.sizesByPreset[presetId] || {};
        if (sizes === null) {
            delete cur[axis];
            if (!cur.cols && !cur.rows) delete this.sizesByPreset[presetId];
            else this.sizesByPreset[presetId] = cur;
        } else {
            cur[axis] = sizes;
            this.sizesByPreset[presetId] = cur;
        }
        saveLayoutState({
            preset: this.preset,
            activeBySlot: this.activeBySlot,
            sizesByPreset: this.sizesByPreset,
        });
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
    //   3. Iframe-like panes (preview/shell/browser) share the slot
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
            case 'files': {
                // Cross-origin only -- md-serve emits root-relative links, so a
                // path prefix would break navigation. Tunnel mode demuxes
                // {filesProxyPort}.{publicHostname} -> 127.0.0.1:{filesProxyPort};
                // legacy mode hits the same auth-proxy port directly.
                const filesUrl = this.effectivePublicHostname
                    ? buildSubdomainFilesUrl(window.location, this.filesProxyPort, this.effectivePublicHostname)
                    : buildPortBasedFilesUrl(window.location, this.filesProxyPort);
                // Defer if the proxy port hasn't arrived yet -- the WS Status
                // handler re-kicks us once filesProxyPort flips from null.
                if (!filesUrl) return;
                this._paneLoaded.add('files');
                this.setIframeUrl(filesUrl + '/', 'files');
                break;
            }
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
        saveLayoutState({ preset: newPresetId, activeBySlot: newActiveBySlot, sizesByPreset: this.sizesByPreset });
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

    // Surface the tunnel client's lifecycle into the UI. State="connected"
    // hides the banner; "reconnecting" shows a countdown using
    // retryAfterMs (so a 5-min rate-limit floor reads as "Retrying in
    // 5m" instead of an indefinite spinner); "fatal" shows a permanent
    // failure message with the reason. Idempotent: skips when nothing
    // user-visible changed.
    _renderTunnelStatusBanner(prev, next) {
        const sameState = (prev && next && prev.state === next.state
            && prev.retryAfterMs === next.retryAfterMs
            && prev.reason === next.reason);
        if (sameState) return;

        const remove = () => {
            const el = document.getElementById('tunnel-status-banner');
            if (el) el.remove();
            if (this._tunnelStatusTimer) {
                clearInterval(this._tunnelStatusTimer);
                this._tunnelStatusTimer = null;
            }
        };

        if (!next || !next.state || next.state === 'connected') {
            remove();
            return;
        }

        let banner = document.getElementById('tunnel-status-banner');
        if (!banner) {
            banner = document.createElement('div');
            banner.id = 'tunnel-status-banner';
            banner.style.cssText = [
                'position:fixed', 'top:0', 'left:0', 'right:0', 'z-index:9999',
                'padding:6px 12px', 'font:12px/1.4 monospace',
                'text-align:center', 'pointer-events:none',
            ].join(';');
            document.body.appendChild(banner);
        }

        const colors = {
            connecting:   { bg: '#1e3a8a', fg: '#fff' },
            reconnecting: { bg: '#92400e', fg: '#fff' },
            disconnected: { bg: '#525252', fg: '#fff' },
            error:        { bg: '#991b1b', fg: '#fff' },
            fatal:        { bg: '#7f1d1d', fg: '#fff' },
        };
        const palette = colors[next.state] || colors.error;
        banner.style.background = palette.bg;
        banner.style.color = palette.fg;

        const reasonSuffix = next.reason ? ` (${next.reason})` : '';
        const formatRetry = (ms) => {
            if (!ms || ms <= 0) return '';
            const sec = Math.ceil(ms / 1000);
            if (sec < 60) return `${sec}s`;
            const m = Math.floor(sec / 60);
            const s = sec % 60;
            return s ? `${m}m ${s}s` : `${m}m`;
        };

        if (this._tunnelStatusTimer) {
            clearInterval(this._tunnelStatusTimer);
            this._tunnelStatusTimer = null;
        }

        if (next.state === 'reconnecting' && next.retryAfterMs > 0) {
            const target = Date.now() + next.retryAfterMs;
            const tick = () => {
                const remaining = Math.max(0, target - Date.now());
                banner.textContent = `Tunnel reconnecting in ${formatRetry(remaining)}${reasonSuffix}`;
                if (remaining <= 0 && this._tunnelStatusTimer) {
                    clearInterval(this._tunnelStatusTimer);
                    this._tunnelStatusTimer = null;
                }
            };
            tick();
            this._tunnelStatusTimer = setInterval(tick, 1000);
            return;
        }

        const labels = {
            connecting:   'Tunnel connecting...',
            reconnecting: 'Tunnel reconnecting...',
            disconnected: 'Tunnel disconnected',
            error:        'Tunnel error',
            fatal:        'Tunnel permanent failure -- restart the container',
        };
        banner.textContent = (labels[next.state] || `Tunnel ${next.state}`) + reasonSuffix;
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

    // Apply persisted track-size overrides for the current preset by writing
    // CSS variables on the workspace. Variables not set fall back to the
    // defaults baked into terminal-ui.css (--col-1: 1fr, etc.).
    _applySizes() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        // Clear any prior inline overrides so a freshly-reset preset reverts
        // cleanly to its CSS default.
        ['--col-1', '--col-2', '--row-1', '--row-2'].forEach(v => {
            workspace.style.removeProperty(v);
        });
        const sizes = this.sizesByPreset[this.preset];
        if (!sizes) return;
        if (sizes.cols) {
            workspace.style.setProperty('--col-1', `${sizes.cols[0]}fr`);
            workspace.style.setProperty('--col-2', `${sizes.cols[1]}fr`);
        }
        if (sizes.rows) {
            workspace.style.setProperty('--row-1', `${sizes.rows[0]}fr`);
            workspace.style.setProperty('--row-2', `${sizes.rows[1]}fr`);
        }
    }

    // (Re)render gutters for the active preset. Gutters are absolutely-
    // positioned overlay strips between adjacent slots; their geometry is
    // derived from the slot-frame bounding rects so we don't have to know
    // the current fr-resolved track sizes. Safe to call repeatedly --
    // existing gutters are torn down first.
    _renderGutters() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        workspace.querySelectorAll('.terminal-ui__gutter').forEach(el => el.remove());
        const spec = PRESET_GUTTERS[this.preset];
        if (!spec) return;
        (spec.cols || []).forEach(g => this._mountGutter(workspace, 'cols', g));
        (spec.rows || []).forEach(g => this._mountGutter(workspace, 'rows', g));
        this._positionGutters();
    }

    _mountGutter(workspace, axis, g) {
        const el = document.createElement('div');
        el.className = `terminal-ui__gutter terminal-ui__gutter--${axis === 'cols' ? 'col' : 'row'}`;
        el.dataset.axis = axis;
        el.dataset.between = g.between.join(',');
        // spanRange = slots whose union rect defines the perpendicular span
        // (defaults to `between` slots). See PRESET_GUTTERS for rationale.
        el.dataset.span = (g.spanRange || g.between).join(',');
        el.title = axis === 'cols' ? 'Drag to resize columns (double-click to reset)'
                                   : 'Drag to resize rows (double-click to reset)';
        workspace.appendChild(el);
        this._setupGutterDrag(el, axis, g);
    }

    // Position every gutter element using the bounding rects of the two
    // slots it separates. Slot-frame rects are unusable here because
    // align-self:start shrinks them to the tab-bar; pane-hosts (when their
    // active tab is mounted) fill the cell and have data-slot set. If a
    // slot has no active pane-host, we fall back to the workspace's full
    // dimensions for that slot (the gutter still works for the other slot).
    // Safe to call on resize, preset apply, or after a drag completes.
    _positionGutters() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        const wsRect = workspace.getBoundingClientRect();
        const slotRect = (slotId) => {
            const host = workspace.querySelector(`.terminal-ui__pane-host[data-slot="${slotId}"]`);
            if (host) return host.getBoundingClientRect();
            return wsRect;
        };
        const gutters = workspace.querySelectorAll('.terminal-ui__gutter');
        gutters.forEach(g => {
            // Position uses `between` slots (those adjacent to the boundary
            // line). Span uses `span` slots (which may extend further when
            // the gutter actually controls more cells, e.g. quadrants).
            const [slotA, slotB] = g.dataset.between.split(',');
            const spanIds = (g.dataset.span || g.dataset.between).split(',');
            const a = slotRect(slotA);
            const b = slotRect(slotB);
            const spanRects = spanIds.map(slotRect);
            if (g.dataset.axis === 'cols') {
                const x = ((a.right + b.left) / 2) - wsRect.left;
                const top = Math.min(...spanRects.map(r => r.top)) - wsRect.top;
                const bot = Math.max(...spanRects.map(r => r.bottom)) - wsRect.top;
                g.style.left = `${x}px`;
                g.style.top = `${top}px`;
                g.style.height = `${bot - top}px`;
            } else {
                const y = ((a.bottom + b.top) / 2) - wsRect.top;
                const left = Math.min(...spanRects.map(r => r.left)) - wsRect.left;
                const right = Math.max(...spanRects.map(r => r.right)) - wsRect.left;
                g.style.top = `${y}px`;
                g.style.left = `${left}px`;
                g.style.width = `${right - left}px`;
            }
        });
    }

    // Wire pointer drag on a single gutter. On move, rewrites the affected
    // CSS variable pair (--col-1/--col-2 or --row-1/--row-2) using fr units
    // computed from pixel deltas, so the grid resolves to the new ratio.
    // On release, persists the ratio to localStorage. Double-click resets.
    _setupGutterDrag(gutter, axis, gSpec) {
        const workspace = gutter.parentElement;
        const isCol = axis === 'cols';
        const varA = isCol ? '--col-1' : '--row-1';
        const varB = isCol ? '--col-2' : '--row-2';
        const slotA = gSpec.between[0];
        const slotB = gSpec.between[1];

        // Tooltip showing live px sizes during drag.
        const tooltip = document.createElement('div');
        tooltip.className = 'terminal-ui__gutter-tooltip';
        gutter.appendChild(tooltip);

        const getSlotRects = () => {
            // Use pane-host (cell-filling) rects rather than slot-frame
            // (tab-bar-only because of align-self:start). Both slots must
            // currently have an active pane mounted for accurate drag math.
            const aHost = workspace.querySelector(`.terminal-ui__pane-host[data-slot="${slotA}"]`);
            const bHost = workspace.querySelector(`.terminal-ui__pane-host[data-slot="${slotB}"]`);
            return aHost && bHost
                ? { a: aHost.getBoundingClientRect(), b: bHost.getBoundingClientRect() }
                : null;
        };

        // Build the ordered list of snap candidates for the current drag.
        // `value` is the size (px) that slot A would take if snapped.
        // `kind`: 'device' beats 'ratio' when both are within threshold.
        const buildSnaps = (totalSize) => {
            const minA = GUTTER_MIN_PX;
            const minB = GUTTER_MIN_PX;
            const inRange = v => v >= minA && totalSize - v >= minB;
            const out = [];
            // 50% ratio snap (always, both axes).
            const mid = totalSize / 2;
            if (inRange(mid)) {
                const t = Math.min(totalSize * GUTTER_SNAP_RATIO_THRESHOLD_FRAC,
                                   GUTTER_SNAP_RATIO_THRESHOLD_PX);
                out.push({ value: mid, label: '50%', threshold: t, kind: 'ratio' });
            }
            // Device-width snaps on column gutter only. Each device width is
            // tested for both panes (left = device, and right = device).
            if (isCol) {
                for (const d of GUTTER_DEVICE_SNAPS) {
                    const t = GUTTER_SNAP_DEVICE_THRESHOLD_PX;
                    if (inRange(d.px)) {
                        out.push({ value: d.px, label: d.label, threshold: t, kind: 'device' });
                    }
                    const right = totalSize - d.px;
                    if (right !== d.px && inRange(right)) {
                        out.push({ value: right, label: d.label, threshold: t, kind: 'device' });
                    }
                }
            }
            return out;
        };

        // Pick best snap for newA. Prefer device over ratio, then closest.
        const findSnap = (snaps, newA) => {
            const hits = snaps.filter(s => Math.abs(newA - s.value) <= s.threshold);
            if (!hits.length) return null;
            hits.sort((a, b) => {
                const ka = a.kind === 'device' ? 0 : 1;
                const kb = b.kind === 'device' ? 0 : 1;
                if (ka !== kb) return ka - kb;
                return Math.abs(newA - a.value) - Math.abs(newA - b.value);
            });
            return hits[0];
        };

        // Tick marks rendered while dragging at every snap target. Mounted on
        // the workspace so they span across the full slot dimension.
        let tickContainer = null;
        const renderTicks = (snaps, originPx) => {
            tickContainer = document.createElement('div');
            tickContainer.className = 'terminal-ui__snap-ticks';
            for (const s of snaps) {
                const tick = document.createElement('div');
                tick.className = `terminal-ui__snap-tick terminal-ui__snap-tick--${isCol ? 'col' : 'row'}`;
                tick.dataset.kind = s.kind;
                if (isCol) tick.style.left = `${originPx + s.value}px`;
                else       tick.style.top  = `${originPx + s.value}px`;
                const label = document.createElement('span');
                label.textContent = s.label;
                tick.appendChild(label);
                tickContainer.appendChild(tick);
            }
            workspace.appendChild(tickContainer);
        };
        const removeTicks = () => {
            if (tickContainer) { tickContainer.remove(); tickContainer = null; }
        };

        let dragging = false;
        let startCoord = 0;       // initial pointer x or y
        let startSizeA = 0;       // initial slot A size in px (along drag axis)
        let totalSize = 0;        // sum of A + B sizes (drag axis)
        let snapList = [];        // snap candidates for the current drag

        const onPointerMove = (e) => {
            if (!dragging) return;
            const coord = isCol ? e.clientX : e.clientY;
            const delta = coord - startCoord;
            let newA = startSizeA + delta;
            const minA = GUTTER_MIN_PX;
            const minB = GUTTER_MIN_PX;
            newA = Math.max(minA, Math.min(totalSize - minB, newA));
            // Apply snap unless Alt is held (escape hatch for fine adjust).
            const snapped = e.altKey ? null : findSnap(snapList, newA);
            if (snapped) newA = snapped.value;
            const newB = totalSize - newA;
            // Use fr units (ratios) so the grid stays responsive on resize.
            workspace.style.setProperty(varA, `${newA}fr`);
            workspace.style.setProperty(varB, `${newB}fr`);
            this._positionGutters();
            const sizes = `${Math.round(newA)}px / ${Math.round(newB)}px`;
            tooltip.textContent = snapped ? `${sizes} - ${snapped.label}` : sizes;
            tooltip.classList.toggle('snapped', !!snapped);
        };

        const onPointerUp = (e) => {
            if (!dragging) return;
            dragging = false;
            gutter.classList.remove('dragging');
            tooltip.classList.remove('visible');
            tooltip.classList.remove('snapped');
            removeTicks();
            document.body.style.cursor = '';
            window.removeEventListener('pointermove', onPointerMove);
            window.removeEventListener('pointerup', onPointerUp);
            // Restore iframe pointer events so subsequent clicks land normally.
            this._setIframePointerEvents('');
            // Persist the final ratio. Read back the resolved sizes so we
            // store the actual fr ratios that produced this layout.
            const rects = getSlotRects();
            if (!rects) return;
            const a = isCol ? rects.a.width  : rects.a.height;
            const b = isCol ? rects.b.width  : rects.b.height;
            this._persistGutterSize(this.preset, axis, [a, b]);
            // Notify terminal so xterm refits to its new pane size.
            if (typeof this.fitAndPreserveScroll === 'function') {
                try { this.fitAndPreserveScroll(); } catch (e) { /* ignore */ }
            }
        };

        const onPointerDown = (e) => {
            // Ignore non-primary buttons; allow touch (e.button is 0).
            if (e.button !== undefined && e.button !== 0) return;
            const rects = getSlotRects();
            if (!rects) return;
            dragging = true;
            startCoord = isCol ? e.clientX : e.clientY;
            startSizeA = isCol ? rects.a.width  : rects.a.height;
            const sizeB  = isCol ? rects.b.width  : rects.b.height;
            totalSize = startSizeA + sizeB;
            snapList = buildSnaps(totalSize);
            // Origin of slot A in workspace-relative coords; tick positions
            // are originPx + snapValue.
            const wsRect = workspace.getBoundingClientRect();
            const originPx = isCol ? rects.a.left - wsRect.left
                                   : rects.a.top  - wsRect.top;
            renderTicks(snapList, originPx);
            gutter.classList.add('dragging');
            document.body.style.cursor = isCol ? 'col-resize' : 'row-resize';
            // Position and show tooltip near gutter center.
            tooltip.textContent = `${Math.round(startSizeA)}px / ${Math.round(sizeB)}px`;
            if (isCol) { tooltip.style.left = '12px'; tooltip.style.top = '50%'; tooltip.style.transform = 'translateY(-50%)'; }
            else       { tooltip.style.top = '12px'; tooltip.style.left = '50%'; tooltip.style.transform = 'translateX(-50%)'; }
            tooltip.classList.add('visible');
            // Disable iframe pointer events so the drag isn't interrupted when
            // the cursor crosses an iframe (iframes capture pointer events).
            this._setIframePointerEvents('none');
            window.addEventListener('pointermove', onPointerMove);
            window.addEventListener('pointerup', onPointerUp);
            e.preventDefault();
        };

        gutter.addEventListener('pointerdown', onPointerDown);
        gutter.addEventListener('dblclick', () => {
            // Reset this axis to the preset's CSS default and persist removal.
            workspace.style.removeProperty(varA);
            workspace.style.removeProperty(varB);
            this._persistGutterSize(this.preset, axis, null);
            this._positionGutters();
            if (typeof this.fitAndPreserveScroll === 'function') {
                try { this.fitAndPreserveScroll(); } catch (e) { /* ignore */ }
            }
        });
    }

    // Toggle pointer-events on every iframe inside the workspace. Used during
    // gutter drag so a moving cursor that crosses an iframe doesn't get
    // captured (iframes are separate event targets that swallow pointer
    // events from the parent document otherwise).
    _setIframePointerEvents(value) {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        workspace.querySelectorAll('iframe').forEach(f => {
            f.style.pointerEvents = value;
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

            // Popout gesture: tabs whose pane resolves to a URL get the
            // popout-able marker (drives the dotted hover underline) and a
            // tooltip explaining middle-click / cmd-click. URL is resolved
            // at click time so navigation/probe state changes are reflected.
            if (this.panePopoutUrl(paneId) != null) {
                btn.classList.add('popout-able');
                btn.title = this._popoutHintText();
            }
            const tryPopout = (e) => {
                const url = this.panePopoutUrl(paneId);
                if (!url) return false;
                e.preventDefault();
                e.stopPropagation();
                window.open(url, '_blank');
                return true;
            };
            btn.addEventListener('click', (e) => {
                if (e.metaKey || e.ctrlKey) {
                    if (tryPopout(e)) return;
                }
                this.setActiveInSlot(slotId, paneId);
            });
            btn.addEventListener('auxclick', (e) => {
                if (e.button === 1) tryPopout(e);
            });
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
        if (paneId === 'files') return !!this.filesProxyPort;
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
    // (chat/vnc becoming available mid-session).
    _rerenderSlotTabs() {
        const workspace = this.querySelector('.terminal-ui__workspace');
        if (!workspace) return;
        const preset = LAYOUT_PRESETS[this.preset] || LAYOUT_PRESETS.classic;
        this._renderSlotFrames(preset);
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

    // Per-pane iframe lookups: each pane ("preview", "shell", "browser")
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
        const vQs = v ? '&v=' + v : '';
        // Tunnel mode: page loads via {vncProxyPort}.{publicHostname} subdomain.
        // vnc_lite.html falls back to window.location.hostname / port when host=
        // and port= query params are missing, so we omit them and let it derive
        // the right wss target from its own page origin.
        if (this.effectivePublicHostname) {
            return `${loc.protocol}//${this.vncProxyPort}.${this.effectivePublicHostname}/vnc_lite.html?reconnect=true&resize=scale&autoconnect=true${vQs}`;
        }
        // Legacy port-based mode: same hostname, different port. Pass host=
        // and port= explicitly so noVNC dials the correct WebSocket regardless
        // of how the iframe parses its own location.
        return `${loc.protocol}//${loc.hostname}:${this.vncProxyPort}/vnc_lite.html?host=${loc.hostname}&port=${this.vncProxyPort}&reconnect=true&resize=scale&autoconnect=true${vQs}`;
    }

    getPreviewBaseUrl() {
        // Tunnel mode wins over port-based mode: the swe-swe-tunnel demuxes
        // {previewProxyPort}.{publicHostname} -> 127.0.0.1:{previewProxyPort}
        // (the swe-swe-server auth proxy port = previewPort + proxyPortOffset),
        // so the cookie is validated before forwarding to the raw preview target.
        // Cookie.Domain is scoped to publicHostname so the apex login cookie
        // is automatically presented on every {port}.publicHostname subdomain.
        if (this.effectivePublicHostname && this.previewProxyPort) {
            return buildSubdomainPreviewUrl(window.location, this.previewProxyPort, this.effectivePublicHostname);
        }
        if (this._proxyMode === 'port' && this.previewProxyPort) {
            return buildPortBasedPreviewUrl(window.location, this.previewProxyPort);
        }
        return buildPreviewUrl(getBaseUrl(window.location), this.sessionUUID);
    }

    updatePreviewBaseUrl() {
        this.previewBaseUrl = this.getPreviewBaseUrl();
    }

    // Resolve the standalone URL for a pane, or null if the pane has no
    // shareable URL (in-page xterm-style panes). Drives the tab popout
    // gesture (middle-click / cmd-click) and the mobile dropdown popout
    // button. Read at click time so URL state changes (preview navigation,
    // late VNC port assignment) are reflected.
    panePopoutUrl(paneId) {
        switch (paneId) {
            case 'preview': {
                if (this._lastUrlChangeUrl) return this._lastUrlChangeUrl;
                const base = this.getPreviewBaseUrl();
                return base ? base + '/' : null;
            }
            case 'browser':
                return this.getBrowserViewUrl();
            case 'files': {
                const filesUrl = this.effectivePublicHostname
                    ? buildSubdomainFilesUrl(window.location, this.filesProxyPort, this.effectivePublicHostname)
                    : buildPortBasedFilesUrl(window.location, this.filesProxyPort);
                return filesUrl ? filesUrl + '/' : null;
            }
            case 'agent-chat': {
                const chatIframe = this.querySelector('.terminal-ui__agent-chat-iframe');
                const src = chatIframe && chatIframe.src;
                return src && src !== 'about:blank' ? src : null;
            }
            default:
                return null;
        }
    }

    // Tooltip explaining the popout gesture. Platform-aware so the modifier
    // key matches the user's keyboard.
    _popoutHintText() {
        const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
        const mod = isMac ? '⌘' : 'Ctrl';
        return `Middle-click or ${mod}+click to open in new browser tab`;
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
        // Mobile mirrors desktop: stay on Agent Terminal during the chat
        // probe even when ?session=chat. The probe-success handler flips
        // the mobile nav to agent-chat once the iframe is loadable. Without
        // this, mobile users saw a blank "Connecting to chat..." view while
        // the probe ran, hiding any prompt the agent-terminal had emitted.
        const mobileSel = this.querySelector('.terminal-ui__mobile-nav-select');
        if (mobileSel) {
            this.switchMobileNav(mobileSel.value || 'agent-terminal');
        }

        this.updatePreviewBaseUrl();

        // Setup iframe navigation buttons
        const homeBtn = this.querySelector('.terminal-ui__iframe-home');
        const refreshBtn = this.querySelector('.terminal-ui__iframe-refresh');

        // Diagnostic: log WS state + time since last server message on each
        // nav-button click. Helps distinguish "WS-down" from "WS-stuck" when
        // the user reports the buttons aren't taking effect. Cheap, no-op on
        // success path.
        const logNavClick = (btn) => {
            const ws = this._debugWs;
            const states = { 0: 'CONNECTING', 1: 'OPEN', 2: 'CLOSING', 3: 'CLOSED' };
            const state = ws ? (states[ws.readyState] || ws.readyState) : 'NULL';
            const lastMsg = this._debugWsLastMessageAt
                ? `${Date.now() - this._debugWsLastMessageAt}ms ago`
                : 'never';
            const ever = this._debugWsEverConnected ? 'yes' : 'no';
            console.log(`[NavBtn] ${btn} click -- debugWs:${state} everConnected:${ever} lastServerMsg:${lastMsg}`);
        };

        if (homeBtn) {
            homeBtn.addEventListener('click', () => {
                logNavClick('home');
                if (this._debugWs?.readyState === WebSocket.OPEN) {
                    this._debugWs.send(JSON.stringify({ t: 'navigate', url: '/' }));
                } else {
                    this.setPreviewURL(null);
                }
            });
        }
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => {
                logNavClick('refresh');
                this.refreshIframe();
            });
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

        // Open preview tab by default on desktop (if wide enough for split view)
        // Skip when embedded in iframe (right panel) - avoid nested iframes
        if (this.canShowSplitPane() && !this.classList.contains('embedded-in-iframe')) {
            // Defer until first BroadcastStatus delivers previewPort
            this._wantsPreviewOnConnect = true;
        }

    }

    // Check if viewport is wide enough for two side-by-side panels.
    canShowSplitPane() {
        return window.innerWidth >= this.MIN_PANEL_WIDTH * 2;
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
        // form inputs, xterm-over-iframe buffer) persists.
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

        // Show/hide the adjacent popout button based on whether the new
        // active pane has a shareable URL. Hidden by default in HTML.
        const popoutBtn = this.querySelector('.terminal-ui__mobile-nav-popout');
        if (popoutBtn) popoutBtn.hidden = (this.panePopoutUrl(value) == null);
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
     * Set iframe src for a specific pane (shell / browser).
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

        // Localhost URLs: route through the proxy shell page.
        // probeBase is always path-based (same-origin, no CORS/credentials
        // problems) so the readiness probe works regardless of tunnel mode.
        // iframeBase is what the iframe will actually load: in tunnel mode
        // this is the subdomain URL so the shell's inner iframe is in the
        // right origin and the cookie (Cookie.Domain=publicHostname) reaches
        // the auth-proxy port.
        const probeBase = buildPreviewUrl(getBaseUrl(window.location), this.sessionUUID);
        if (!probeBase) return;
        const subdomainBase = (this.effectivePublicHostname && this.previewProxyPort)
            ? buildSubdomainPreviewUrl(window.location, this.previewProxyPort, this.effectivePublicHostname)
            : null;
        const base = subdomainBase || probeBase;
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
            // Phase 1: Probe path-based URL (same-origin) to wait for the
            //          proxy handler to be up. We always probe path-based
            //          even when the iframe will load via subdomain, because
            //          path-based shares the page's origin and cookie.
            // Phase 2: Legacy port-based mode probe -- skipped entirely in
            //          tunnel mode since `base` is already the subdomain URL.
            this._previewProbeController = new AbortController();
            // portBasedBase is the legacy "host:port" URL; meaningless when
            // window.location.hostname already encodes the port via subdomain
            // (tunnel mode), so we set it to null in that case to skip the probe.
            const portBasedBase = subdomainBase
                ? null
                : buildPortBasedPreviewUrl(window.location, this.previewProxyPort);
            probeUntilReady(probeBase + '/', {
                method: 'GET',
                maxAttempts: 10, baseDelay: 2000, maxDelay: 30000,
                isReady: (resp) => resp.headers.has('X-Agent-Reverse-Proxy'),
                signal: this._previewProbeController.signal,
            }).then(() => {
                if (subdomainBase) {
                    // Tunnel mode: iframe already targets the subdomain URL.
                    this._proxyMode = 'subdomain';
                    return;
                }
                // Legacy: try port-based if available.
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
            // Fallback: hard-reload the preview iframe specifically. Older
            // code used `_iframeFor(this.activeTab)`, but `activeTab` is a
            // legacy mirror (see comment near constructor) that can lag
            // behind the actual visible pane in multi-iframe presets. The
            // URL bar and refresh button live inside the preview pane-host,
            // so 'preview' is always the right target here.
            const iframe = this._iframeFor('preview');
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
                this._debugWsLastMessageAt = Date.now();
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
