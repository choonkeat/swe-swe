// Transport abstraction for WebSocket and polling fallback
class WebSocketTransport {
    constructor(ui) {
        this.ui = ui;
        this.ws = null;
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = protocol + '//' + window.location.host + '/ws/' + this.ui.uuid + '?assistant=' + encodeURIComponent(this.ui.assistant);

        this.ws = new WebSocket(url);
        this.ws.binaryType = 'arraybuffer';

        this.ws.onopen = () => {
            this.ui.onTransportOpen();
        };

        this.ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                this.ui.onTerminalData(new Uint8Array(event.data));
            } else if (typeof event.data === 'string') {
                try {
                    const msg = JSON.parse(event.data);
                    this.ui.onJSONMessage(msg);
                } catch (e) {
                    console.error('Invalid JSON from server:', e);
                }
            }
        };

        this.ws.onclose = () => {
            this.ui.onTransportClose();
        };

        this.ws.onerror = () => {
            this.ui.onTransportError();
        };
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
    }

    isConnected() {
        return this.ws && this.ws.readyState === WebSocket.OPEN;
    }

    // Send binary terminal input
    send(data) {
        if (this.isConnected()) {
            if (typeof data === 'string') {
                const encoder = new TextEncoder();
                this.ws.send(encoder.encode(data));
            } else {
                this.ws.send(data);
            }
        }
    }

    // Send JSON control message
    sendJSON(obj) {
        if (this.isConnected()) {
            this.ws.send(JSON.stringify(obj));
        }
    }

    // Send resize message (binary with 0x00 prefix)
    sendResize(rows, cols) {
        if (this.isConnected()) {
            const msg = new Uint8Array([
                0x00,
                (rows >> 8) & 0xFF, rows & 0xFF,
                (cols >> 8) & 0xFF, cols & 0xFF
            ]);
            this.ws.send(msg);
        }
    }
}

// Polling transport for fallback when WebSocket fails
class PollingTransport {
    constructor(ui) {
        this.ui = ui;
        this.clientId = 'poll-' + Math.random().toString(36).substr(2, 9);
        this.active = false;
        this.pollInterval = 250;
        this.pollTimeout = null;
        this.currentSize = { rows: 24, cols: 80 };
    }

    connect() {
        this.active = true;
        this.ui.onTransportOpen();
        this.pollLoop();
    }

    disconnect() {
        this.active = false;
        if (this.pollTimeout) {
            clearTimeout(this.pollTimeout);
            this.pollTimeout = null;
        }
    }

    isConnected() {
        return this.active;
    }

    async pollLoop() {
        if (!this.active) return;

        try {
            const url = `/session/${this.ui.uuid}/client/${this.clientId}/poll?assistant=${encodeURIComponent(this.ui.assistant)}`;
            const resp = await fetch(url);

            if (!resp.ok) {
                console.error('Poll failed:', resp.status);
                this.ui.onTransportError();
                return;
            }

            const data = await resp.json();

            // Decode base64 terminal snapshot and write to terminal
            if (data.terminal) {
                const terminalData = atob(data.terminal);
                const bytes = new Uint8Array(terminalData.length);
                for (let i = 0; i < terminalData.length; i++) {
                    bytes[i] = terminalData.charCodeAt(i);
                }
                this.ui.onTerminalData(bytes);
            }

            // Send status update
            this.ui.onJSONMessage({
                type: 'status',
                viewers: data.viewers,
                cols: data.cols,
                rows: data.rows,
                assistant: data.assistant
            });

        } catch (err) {
            console.error('Poll error:', err);
            this.ui.onTransportError();
            return;
        }

        // Schedule next poll
        this.pollTimeout = setTimeout(() => this.pollLoop(), this.pollInterval);
    }

    // Send terminal input
    send(data) {
        if (!this.active) return;

        let inputData;
        if (typeof data === 'string') {
            inputData = data;
        } else if (data instanceof Uint8Array) {
            // Convert Uint8Array to string
            inputData = new TextDecoder().decode(data);
        } else {
            console.warn('PollingTransport.send: unsupported data type');
            return;
        }

        const url = `/session/${this.ui.uuid}/client/${this.clientId}/send`;
        fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: 'input', data: inputData })
        }).catch(err => console.error('Send error:', err));
    }

    // Send JSON control message
    sendJSON(obj) {
        if (!this.active) return;

        // Handle ping locally (fake pong)
        if (obj.type === 'ping') {
            this.ui.onJSONMessage({ type: 'pong', data: obj.data });
            return;
        }

        // Handle other JSON messages (currently not needed for polling)
        console.log('PollingTransport.sendJSON (ignored):', obj);
    }

    // Send resize
    sendResize(rows, cols) {
        if (!this.active) return;

        this.currentSize = { rows, cols };
        const url = `/session/${this.ui.uuid}/client/${this.clientId}/send`;
        fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: 'resize', rows, cols })
        }).catch(err => console.error('Resize error:', err));
    }
}

// Connection states for fallback logic
const CONNECTION_STATES = {
    DISCONNECTED: 'disconnected',
    WS_CONNECTING: 'ws_connecting',
    WS_ACTIVE: 'ws_active',
    FALLBACK: 'fallback'  // Polling active, WS retrying in background
};

class TerminalUI extends HTMLElement {
    constructor() {
        super();
        this.transport = null;
        this.term = null;
        this.fitAddon = null;
        this.connectedAt = null;
        this.reconnectAttempts = 0;
        this.reconnectTimeout = null;
        this.countdownInterval = null;
        this.uptimeInterval = null;
        this.heartbeatInterval = null;
        this.ctrlPressed = false;
        // Session status from server
        this.viewers = 0;
        this.ptyRows = 0;
        this.ptyCols = 0;
        this.assistantName = '';
        this.statusRestoreTimeout = null;
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
        // Polling mode
        this.isPollingMode = false;
        // Connection state machine
        this.connectionState = CONNECTION_STATES.DISCONNECTED;
        this.wsFailureCount = 0;
        this.forcedPolling = false;  // True when ?transport=polling is set
        this.wsRetryTimeout = null;  // For background WS retries during fallback
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
        // Redirect to homepage if no assistant specified
        if (!this.assistant) {
            window.location.href = '/';
            return;
        }
        // Load username from localStorage if available
        let storedName = localStorage.getItem('swe-swe-username');
        if (storedName) {
            this.currentUserName = storedName;
        } else {
            // Auto-generate a random username and store it immediately
            this.currentUserName = this.generateRandomUsername();
            localStorage.setItem('swe-swe-username', this.currentUserName);
        }
        this.render();
        this.initTerminal();
        this.connect();
        this.setupEventListeners();
        this.renderLinks();
        this.renderServiceLinks();

        // Expose for console testing
        window.terminalUI = this;
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
        if (this.transport) {
            this.transport.disconnect();
            this.transport = null;
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
        if (this.statusRestoreTimeout) {
            clearTimeout(this.statusRestoreTimeout);
        }
        if (this.wsRetryTimeout) {
            clearTimeout(this.wsRetryTimeout);
        }
        // Clean up chat message timeouts
        this.chatMessageTimeouts.forEach(timeout => clearTimeout(timeout));
        this.chatMessageTimeouts = [];
        if (this.term) {
            this.term.dispose();
        }
    }

    render() {
        this.innerHTML = `
            <style>
                .terminal-ui {
                    display: flex;
                    flex-direction: column;
                    height: 100%;
                    background: #1e1e1e;
                    position: relative;
                }
                .terminal-ui__terminal {
                    flex: 1;
                    width: 100%;
                    overflow: hidden;
                    transition: opacity 0.3s ease;
                }
                .terminal-ui__terminal.disconnected {
                    opacity: 0.5;
                }
                .terminal-ui__extra-keys {
                    display: flex;
                    flex-wrap: wrap;
                    gap: 4px;
                    padding: 8px;
                    background: #2d2d2d;
                    border-top: 1px solid #404040;
                }
                .terminal-ui__extra-keys button {
                    flex: 1;
                    min-width: 40px;
                    padding: 12px 8px;
                    font-size: 14px;
                    font-family: monospace;
                    background: #3c3c3c;
                    color: #d4d4d4;
                    border: 1px solid #505050;
                    border-radius: 4px;
                    cursor: pointer;
                    touch-action: manipulation;
                    -webkit-tap-highlight-color: transparent;
                }
                .terminal-ui__extra-keys button:active {
                    background: #505050;
                }
                .terminal-ui__extra-keys button.modifier {
                    /* No special background when inactive - matches other buttons */
                }
                .terminal-ui__extra-keys button.modifier.active {
                    background: #007acc;
                    border-color: #007acc;
                }
                @media (min-width: 768px) {
                    .terminal-ui__extra-keys {
                        display: none;
                    }
                }
                .terminal-ui__status-bar {
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    padding: 6px 12px;
                    background: #f57c00;
                    color: #fff;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    font-size: 12px;
                    transition: background-color 0.3s ease, border-color 0.3s ease;
                    border-top: 3px solid transparent;
                }
                .terminal-ui__status-bar.connected {
                    background: #007acc;
                }
                .terminal-ui__status-bar.error {
                    background: #c62828;
                }
                .terminal-ui__status-bar.multiuser {
                    border-top-color: #4fc3f7;
                }
                .terminal-ui__status-bar.connecting,
                .terminal-ui__status-bar.error,
                .terminal-ui__status-bar.reconnecting {
                    cursor: pointer;
                }
                .terminal-ui__status-icon {
                    width: 8px;
                    height: 8px;
                    border-radius: 50%;
                    background: #4caf50;
                    margin-right: 8px;
                }
                .terminal-ui__status-bar.connecting .terminal-ui__status-icon,
                .terminal-ui__status-bar.error .terminal-ui__status-icon,
                .terminal-ui__status-bar.reconnecting .terminal-ui__status-icon {
                    background: #fff;
                    animation: terminal-ui-pulse 1s infinite;
                }
                @keyframes terminal-ui-pulse {
                    0%, 100% { opacity: 1; }
                    50% { opacity: 0.3; }
                }
                .terminal-ui__status-left {
                    display: flex;
                    align-items: center;
                }
                .terminal-ui__status-right {
                    display: flex;
                    align-items: center;
                    gap: 12px;
                }
                .terminal-ui__status-links {
                    display: flex;
                    align-items: center;
                    gap: 8px;
                }
                .terminal-ui__status-service-links {
                    display: flex;
                    align-items: center;
                    gap: 8px;
                    margin-right: 8px;
                }
                .terminal-ui__status-link {
                    color: #fff;
                    text-decoration: none;
                    cursor: pointer;
                    transition: opacity 0.2s;
                    white-space: nowrap;
                }
                .terminal-ui__status-link:hover {
                    opacity: 0.8;
                    text-decoration: underline;
                }
                .terminal-ui__status-link-sep {
                    opacity: 0.5;
                }
                .terminal-ui__status-bar.connecting:hover,
                .terminal-ui__status-bar.error:hover,
                .terminal-ui__status-bar.reconnecting:hover {
                    opacity: 0.9;
                    filter: brightness(1.1);
                }
                .terminal-ui__status-text {
                    display: inline;
                }
                .terminal-ui__status-link {
                    cursor: pointer;
                    text-decoration: dotted underline #fff;
                    text-underline-offset: 2px;
                    transition: opacity 0.2s;
                }
                .terminal-ui__status-link:hover {
                    opacity: 0.8;
                }
                .terminal-ui__status-info {
                    opacity: 0.9;
                }
                .terminal-ui__status-dims {
                    opacity: 0.9;
                }
                /* Polling Mode Styles */
                .terminal-ui__status-bar.polling-mode {
                    background: #795548;
                }
                .terminal-ui__polling-input {
                    display: none;
                    padding: 8px;
                    background: #2d2d2d;
                    border-top: 1px solid #404040;
                    gap: 8px;
                }
                .terminal-ui__polling-input.visible {
                    display: flex;
                }
                .terminal-ui__polling-input input {
                    flex: 1;
                    padding: 10px 12px;
                    font-size: 14px;
                    font-family: monospace;
                    background: #1e1e1e;
                    color: #d4d4d4;
                    border: 1px solid #505050;
                    border-radius: 4px;
                    outline: none;
                }
                .terminal-ui__polling-input input:focus {
                    border-color: #007acc;
                }
                .terminal-ui__polling-input button {
                    padding: 10px 16px;
                    font-size: 14px;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    background: #007acc;
                    color: #fff;
                    border: none;
                    border-radius: 4px;
                    cursor: pointer;
                }
                .terminal-ui__polling-input button:hover {
                    background: #005a9e;
                }
                .terminal-ui__polling-actions {
                    display: none;
                    padding: 8px;
                    background: #2d2d2d;
                    gap: 4px;
                    flex-wrap: wrap;
                }
                .terminal-ui__polling-actions.visible {
                    display: flex;
                }
                .terminal-ui__polling-actions button {
                    flex: 1;
                    min-width: 50px;
                    padding: 10px 8px;
                    font-size: 13px;
                    font-family: monospace;
                    background: #3c3c3c;
                    color: #d4d4d4;
                    border: 1px solid #505050;
                    border-radius: 4px;
                    cursor: pointer;
                }
                .terminal-ui__polling-actions button:hover {
                    background: #505050;
                }
                .terminal-ui__polling-actions button:active {
                    background: #007acc;
                }
                .terminal-ui__drop-overlay {
                    display: none;
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    bottom: 0;
                    background: rgba(0, 122, 204, 0.9);
                    color: #fff;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    font-size: 18px;
                    justify-content: center;
                    align-items: center;
                    flex-direction: column;
                    gap: 12px;
                    z-index: 100;
                    pointer-events: none;
                }
                .terminal-ui__drop-overlay.visible {
                    display: flex;
                }
                .terminal-ui__drop-icon {
                    font-size: 48px;
                }
                /* Upload Progress Overlay Styles */
                .terminal-ui__upload-overlay {
                    display: none;
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    bottom: 0;
                    background: rgba(0, 0, 0, 0.85);
                    color: #fff;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    font-size: 14px;
                    justify-content: center;
                    align-items: center;
                    flex-direction: column;
                    gap: 16px;
                    z-index: 200;
                    pointer-events: auto;
                    animation: terminal-ui-fade-in 0.2s ease-out;
                }
                .terminal-ui__upload-overlay.visible {
                    display: flex;
                }
                .terminal-ui__upload-overlay.hidden {
                    animation: terminal-ui-fade-out 0.2s ease-out;
                }
                @keyframes terminal-ui-fade-in {
                    from { opacity: 0; }
                    to { opacity: 1; }
                }
                @keyframes terminal-ui-fade-out {
                    from { opacity: 1; }
                    to { opacity: 0; }
                }
                .terminal-ui__upload-spinner {
                    width: 40px;
                    height: 40px;
                    border: 3px solid rgba(255, 255, 255, 0.3);
                    border-top-color: #fff;
                    border-radius: 50%;
                    animation: terminal-ui-spin 1s linear infinite;
                }
                @keyframes terminal-ui-spin {
                    to { transform: rotate(360deg); }
                }
                .terminal-ui__upload-text {
                    text-align: center;
                }
                .terminal-ui__upload-filename {
                    font-weight: 500;
                    opacity: 0.9;
                }
                .terminal-ui__upload-queue {
                    font-size: 12px;
                    opacity: 0.7;
                    margin-top: 4px;
                }
                /* Chat Overlay Styles */
                .terminal-ui__chat-overlay {
                    position: absolute;
                    top: 0;
                    right: 0;
                    left: auto;
                    padding: 12px 20px;
                    pointer-events: none;
                    z-index: 100;
                    max-height: 50%;
                    max-width: 40%;
                    overflow-y: auto;
                    display: flex;
                    flex-direction: column;
                    align-items: flex-end;
                    gap: 6px;
                }
                .terminal-ui__chat-message {
                    background: rgba(0, 0, 0, 0.7);
                    color: #e0e0e0;
                    padding: 8px 12px;
                    border-radius: 4px;
                    font-size: 13px;
                    line-height: 1.4;
                    max-width: 600px;
                    word-wrap: break-word;
                    animation: terminal-ui-fadeInDown 0.3s ease-out;
                    pointer-events: auto;
                    cursor: pointer;
                    transition: opacity 0.3s ease, background 0.2s ease;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
                }
                @keyframes terminal-ui-fadeInDown {
                    from {
                        opacity: 0;
                        transform: translateY(-10px);
                    }
                    to {
                        opacity: 1;
                        transform: translateY(0);
                    }
                }
                @keyframes terminal-ui-fadeOut {
                    from {
                        opacity: 1;
                    }
                    to {
                        opacity: 0;
                    }
                }
                .terminal-ui__chat-message.fading {
                    animation: terminal-ui-fadeOut 0.4s ease-out forwards;
                }
                .terminal-ui__chat-message:hover {
                    background: rgba(0, 0, 0, 0.85);
                }
                .terminal-ui__chat-message.own {
                    background: rgba(0, 122, 255, 0.75);
                    color: white;
                }
                .terminal-ui__chat-message.own:hover {
                    background: rgba(0, 100, 220, 0.9);
                }
                .terminal-ui__chat-message.other {
                    background: rgba(100, 100, 100, 0.8);
                    color: #fff;
                }
                .terminal-ui__chat-message.other:hover {
                    background: rgba(100, 100, 100, 0.95);
                }
                .terminal-ui__chat-message-username {
                    font-weight: 600;
                    margin-right: 4px;
                }
                /* Chat Input Overlay */
                .terminal-ui__chat-input-overlay {
                    position: absolute;
                    bottom: 60px;
                    left: 50%;
                    transform: translateX(-50%);
                    width: 90%;
                    max-width: 600px;
                    display: none;
                    pointer-events: all;
                    z-index: 101;
                    animation: terminal-ui-slideUp 0.3s ease-out;
                }
                @keyframes terminal-ui-slideUp {
                    from {
                        transform: translateX(-50%) translateY(20px);
                        opacity: 0;
                    }
                    to {
                        transform: translateX(-50%) translateY(0);
                        opacity: 1;
                    }
                }
                .terminal-ui__chat-input-overlay.active {
                    display: flex;
                    gap: 8px;
                }
                .terminal-ui__chat-input {
                    flex: 1;
                    padding: 10px 14px;
                    border: 1px solid #666;
                    border-radius: 4px;
                    background: rgba(40, 40, 40, 0.95);
                    color: #e0e0e0;
                    font-size: 13px;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
                    outline: none;
                    transition: border-color 0.2s;
                }
                .terminal-ui__chat-input:focus {
                    border-color: #007AFF;
                    box-shadow: 0 0 8px rgba(0, 122, 255, 0.3);
                }
                .terminal-ui__chat-button-group {
                    display: flex;
                    gap: 6px;
                }
                .terminal-ui__chat-send-btn, .terminal-ui__chat-cancel-btn {
                    padding: 10px 14px;
                    background: #007AFF;
                    color: white;
                    border: none;
                    border-radius: 4px;
                    font-size: 13px;
                    font-weight: 600;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
                    cursor: pointer;
                    transition: background 0.2s;
                    white-space: nowrap;
                }
                .terminal-ui__chat-send-btn:hover {
                    background: #0051D5;
                }
                .terminal-ui__chat-send-btn:disabled {
                    opacity: 0.5;
                    cursor: not-allowed;
                }
                .terminal-ui__chat-cancel-btn {
                    background: #666;
                }
                .terminal-ui__chat-cancel-btn:hover {
                    background: #777;
                }
                /* Paste Overlay */
                .terminal-ui__paste-overlay {
                    position: absolute;
                    bottom: 60px;
                    left: 50%;
                    transform: translateX(-50%);
                    width: 90%;
                    max-width: 600px;
                    display: none;
                    flex-direction: column;
                    gap: 8px;
                    pointer-events: all;
                    z-index: 101;
                    animation: terminal-ui-slideUp 0.3s ease-out;
                }
                .terminal-ui__paste-overlay.active {
                    display: flex;
                }
                .terminal-ui__paste-textarea {
                    width: 100%;
                    min-height: 120px;
                    padding: 10px 14px;
                    border: 1px solid #666;
                    border-radius: 4px;
                    background: rgba(40, 40, 40, 0.95);
                    color: #e0e0e0;
                    font-size: 13px;
                    font-family: monospace;
                    outline: none;
                    resize: vertical;
                    transition: border-color 0.2s;
                }
                .terminal-ui__paste-textarea:focus {
                    border-color: #007AFF;
                    box-shadow: 0 0 8px rgba(0, 122, 255, 0.3);
                }
                .terminal-ui__paste-button-group {
                    display: flex;
                    gap: 6px;
                    justify-content: flex-end;
                }
                .terminal-ui__paste-send-btn, .terminal-ui__paste-cancel-btn {
                    padding: 10px 14px;
                    background: #007AFF;
                    color: white;
                    border: none;
                    border-radius: 4px;
                    font-size: 13px;
                    font-weight: 600;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
                    cursor: pointer;
                    transition: background 0.2s;
                    white-space: nowrap;
                }
                .terminal-ui__paste-send-btn:hover {
                    background: #0051D5;
                }
                .terminal-ui__paste-cancel-btn {
                    background: #666;
                }
                .terminal-ui__paste-cancel-btn:hover {
                    background: #777;
                }
                /* Chat notification badge */
                .terminal-ui__chat-notification {
                    position: absolute;
                    top: -8px;
                    right: -8px;
                    background: #ff4444;
                    color: white;
                    font-size: 10px;
                    padding: 2px 4px;
                    border-radius: 3px;
                    min-width: 16px;
                    text-align: center;
                    font-weight: bold;
                }
            </style>
            <div class="terminal-ui">
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
                <div class="terminal-ui__terminal"></div>
                <div class="terminal-ui__extra-keys">
                    <button data-key="Escape">ESC</button>
                    <button data-key="Tab">TAB</button>
                    <button data-modifier="ctrl" class="modifier">Ctrl</button>
                    <button data-key="ArrowUp">↑</button>
                    <button data-key="ArrowDown">↓</button>
                    <button data-key="ArrowLeft">←</button>
                    <button data-key="ArrowRight">→</button>
                    <button data-action="paste">Paste</button>
                </div>
                <div class="terminal-ui__polling-actions">
                    <button data-send="\x03">Ctrl+C</button>
                    <button data-send="\x04">Ctrl+D</button>
                    <button data-send="\t">Tab</button>
                    <button data-send="\x1b[A">↑</button>
                    <button data-send="\x1b[B">↓</button>
                    <button data-send="\n">Enter</button>
                </div>
                <div class="terminal-ui__polling-input">
                    <input type="text" placeholder="Type command..." class="terminal-ui__polling-command">
                    <button class="terminal-ui__polling-send">Send</button>
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
            theme: {
                background: '#1e1e1e',
                foreground: '#d4d4d4'
            }
        });

        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);
        this.term.open(terminalEl);
        this.fitAddon.fit();

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

    renderServiceLinks() {
        const statusRight = this.querySelector('.terminal-ui__status-right');
        if (!statusRight) return;

        // Remove any existing service links container
        const existingContainer = statusRight.querySelector('.terminal-ui__status-service-links');
        if (existingContainer) {
            existingContainer.remove();
        }

        // All services use path-based routing
        const protocol = window.location.protocol;
        const port = window.location.port;
        const baseUrl = port ? `${protocol}//${window.location.hostname}:${port}` : `${protocol}//${window.location.hostname}`;
        const services = [
            { name: 'vscode', url: `${baseUrl}/vscode/` },
            { name: 'browser', url: `${baseUrl}/chrome/` }
        ];

        const container = document.createElement('div');
        container.className = 'terminal-ui__status-service-links';

        services.forEach((service, index) => {
            const a = document.createElement('a');
            a.href = service.url;
            a.target = `swe-swe-${service.name}`;
            a.className = 'terminal-ui__status-link';
            a.textContent = service.name;
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

    getAssistantLink() {
        const name = this.assistantName || this.assistant;
        return `<a href="/" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${name}</a>`;
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
        if (this.transport && this.transport.isConnected()) {
            this.transport.sendResize(this.term.rows, this.term.cols);
        }
    }

    connect() {
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

        // Check for forced transport type via query param
        const params = new URLSearchParams(window.location.search);
        const transportType = params.get('transport');

        if (transportType === 'polling') {
            // Forced polling mode - skip WebSocket entirely
            this.forcedPolling = true;
            this.connectionState = CONNECTION_STATES.FALLBACK;
            this.setPollingMode(true);
            this.transport = new PollingTransport(this);
            this.transport.connect();
            return;
        }

        // Default to WebSocket transport
        this.forcedPolling = false;
        this.connectionState = CONNECTION_STATES.WS_CONNECTING;
        this.setPollingMode(false);
        this.transport = new WebSocketTransport(this);
        this.transport.connect();
    }

    // Transport callback: connection opened
    onTransportOpen() {
        // If we were in fallback and WS reconnected, handle recovery
        if (this.connectionState === CONNECTION_STATES.FALLBACK && !this.forcedPolling) {
            // This is WS recovery during fallback
            console.log('WebSocket recovered, switching from polling');
            this.wsFailureCount = 0;
            // Cancel any pending WS retry
            if (this.wsRetryTimeout) {
                clearTimeout(this.wsRetryTimeout);
                this.wsRetryTimeout = null;
            }
        }

        this.connectionState = CONNECTION_STATES.WS_ACTIVE;
        this.reconnectAttempts = 0;
        this.updateStatus('connected', 'Connected');
        this.startUptimeTimer();
        this.sendResize();
        this.startHeartbeat();
    }

    // Transport callback: connection closed
    onTransportClose() {
        this.stopUptimeTimer();
        this.stopHeartbeat();

        // If forced polling or already in fallback, just show error
        if (this.forcedPolling) {
            this.updateStatus('error', 'Connection closed');
            return;
        }

        // If we're in WS_ACTIVE or WS_CONNECTING, enter fallback
        if (this.connectionState === CONNECTION_STATES.WS_ACTIVE ||
            this.connectionState === CONNECTION_STATES.WS_CONNECTING) {
            this.enterFallbackMode();
        }
    }

    // Transport callback: connection error
    onTransportError() {
        this.stopUptimeTimer();
        this.stopHeartbeat();

        // If forced polling, just show error
        if (this.forcedPolling) {
            this.updateStatus('error', 'Connection error');
            return;
        }

        // If we're trying to connect via WS, enter fallback
        if (this.connectionState === CONNECTION_STATES.WS_CONNECTING ||
            this.connectionState === CONNECTION_STATES.WS_ACTIVE) {
            this.wsFailureCount++;
            this.enterFallbackMode();
        }
    }

    // Enter fallback mode: start polling, schedule WS retries
    enterFallbackMode() {
        console.log('Entering fallback mode, wsFailureCount:', this.wsFailureCount);
        this.connectionState = CONNECTION_STATES.FALLBACK;

        // Start polling transport
        this.setPollingMode(true);
        if (this.transport) {
            this.transport.disconnect();
        }
        this.transport = new PollingTransport(this);
        this.transport.connect();

        // Schedule background WS retry
        this.scheduleWsRetry();
    }

    // Schedule a background WS reconnection attempt
    scheduleWsRetry() {
        if (this.wsRetryTimeout) {
            clearTimeout(this.wsRetryTimeout);
        }

        // Backoff: 10s, 15s, 20s, 30s max
        const delay = Math.min(10000 + (this.wsFailureCount * 5000), 30000);
        console.log(`Scheduling WS retry in ${delay}ms`);

        this.wsRetryTimeout = setTimeout(() => {
            this.attemptWsReconnect();
        }, delay);
    }

    // Attempt to reconnect WS while in fallback mode
    attemptWsReconnect() {
        if (this.connectionState !== CONNECTION_STATES.FALLBACK) {
            return; // Not in fallback, don't retry
        }
        if (this.forcedPolling) {
            return; // Forced polling, don't try WS
        }

        console.log('Attempting WS reconnect during fallback...');

        // Create a new WS transport and try to connect
        const wsTransport = new WebSocketTransport(this);

        // Temporarily override callbacks to handle this probe
        const originalOnOpen = this.onTransportOpen.bind(this);
        const originalOnClose = this.onTransportClose.bind(this);
        const originalOnError = this.onTransportError.bind(this);

        wsTransport.ui = {
            uuid: this.uuid,
            assistant: this.assistant,
            onTransportOpen: () => {
                // WS connected! Switch from polling
                console.log('WS probe succeeded, switching to WebSocket');

                // Stop polling
                if (this.transport) {
                    this.transport.disconnect();
                }

                // Use the new WS transport
                this.transport = wsTransport;
                wsTransport.ui = this;

                // Update state
                this.connectionState = CONNECTION_STATES.WS_ACTIVE;
                this.setPollingMode(false);
                this.wsFailureCount = 0;

                // Call original handler for uptime/heartbeat/etc
                this.reconnectAttempts = 0;
                this.updateStatus('connected', 'Connected');
                this.startUptimeTimer();
                this.sendResize();
                this.startHeartbeat();
            },
            onTransportClose: () => {
                // WS probe failed, stay in fallback
                console.log('WS probe closed, staying in fallback');
                this.wsFailureCount++;
                this.scheduleWsRetry();
            },
            onTransportError: () => {
                // WS probe failed, stay in fallback
                console.log('WS probe error, staying in fallback');
                this.wsFailureCount++;
                this.scheduleWsRetry();
            },
            onTerminalData: () => {},  // Ignore, polling is handling terminal
            onJSONMessage: () => {}    // Ignore, polling is handling status
        };

        wsTransport.connect();
    }

    // Transport callback: terminal data received
    onTerminalData(data) {
        this.term.write(data);
    }

    // Transport callback: JSON message received
    onJSONMessage(msg) {
        this.handleJSONMessage(msg);
    }

    sendJSON(obj) {
        if (this.transport && this.transport.isConnected()) {
            this.transport.sendJSON(obj);
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
                    this.showTemporaryStatus(`Saved: ${msg.filename}`, 3000);
                } else {
                    this.showTemporaryStatus(`Upload failed: ${msg.error || 'Unknown error'}`, 5000);
                }
                break;
            case 'exit':
                // Process exited - prompt user to return to home
                this.handleProcessExit(msg.exitCode);
                break;
            default:
                console.log('Unknown JSON message:', msg);
        }
    }

    handleProcessExit(exitCode) {
        // Update status bar to show exited state
        this.updateStatus('', 'Session ended');

        // Stop uptime timer
        this.stopUptimeTimer();

        // Show confirmation dialog
        const message = exitCode === 0
            ? 'The session has ended successfully.\n\nReturn to the home page to start a new session?'
            : `The session ended with exit code ${exitCode}.\n\nReturn to the home page to start a new session?`;

        if (confirm(message)) {
            window.location.href = '/';
        }
    }

    updateStatusInfo() {
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const statusText = this.querySelector('.terminal-ui__status-text');
        const dimsEl = this.querySelector('.terminal-ui__status-dims');

        if (!statusText) return;

        // Toggle multiuser class based on viewer count
        statusBar.classList.toggle('multiuser', this.viewers > 1);

        if (this.transport && this.transport.isConnected()) {
            // Build "Connected as {name} with {agent}" message with separate clickable parts
            const userName = this.currentUserName;
            let html = '';

            if (this.isPollingMode) {
                html = `Slow connection mode with <a href="/" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${this.assistantName || this.assistant}</a>`;
            } else {
                html = `Connected as <span class="terminal-ui__status-link terminal-ui__status-name">${userName}</span>`;
                if (this.assistantName) {
                    html += ` with <a href="/" target="swe-swe-model-selector" class="terminal-ui__status-link terminal-ui__status-agent">${this.assistantName}</a>`;
                }
            }

            // Add viewer suffix if more than 1 viewer
            if (this.viewers > 1) {
                html += ` and <span class="terminal-ui__status-link terminal-ui__status-others">${this.viewers - 1} others</span>`;
            }

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

    setPollingMode(enabled) {
        this.isPollingMode = enabled;
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const pollingInput = this.querySelector('.terminal-ui__polling-input');
        const pollingActions = this.querySelector('.terminal-ui__polling-actions');
        const extraKeys = this.querySelector('.terminal-ui__extra-keys');

        if (enabled) {
            statusBar.classList.add('polling-mode');
            pollingInput.classList.add('visible');
            pollingActions.classList.add('visible');
            // Hide extra keys on mobile when in polling mode (we have polling actions instead)
            extraKeys.style.display = 'none';
        } else {
            statusBar.classList.remove('polling-mode');
            pollingInput.classList.remove('visible');
            pollingActions.classList.remove('visible');
            extraKeys.style.display = '';
        }
    }

    showTemporaryStatus(message, durationMs = 3000) {
        const infoEl = this.querySelector('.terminal-ui__status-info');
        if (!infoEl) return;

        // Clear any pending restore
        if (this.statusRestoreTimeout) {
            clearTimeout(this.statusRestoreTimeout);
        }

        infoEl.textContent = message;

        // Restore normal status after duration
        this.statusRestoreTimeout = setTimeout(() => {
            this.updateStatusInfo();
        }, durationMs);
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
                this.currentUserName = validation.name;
                localStorage.setItem('swe-swe-username', validation.name);
                this.updateUsernameDisplay();
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
                this.currentUserName = validation.name;
                localStorage.setItem('swe-swe-username', validation.name);
                this.updateUsernameDisplay();
                return;
            } else {
                alert('Invalid name: ' + validation.error + '\nPlease try again.');
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

        if (text && this.transport && this.transport.isConnected()) {
            this.transport.send(text);
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
        if (this.transport && this.transport.isConnected()) {
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

    sendPollingCommand() {
        const input = this.querySelector('.terminal-ui__polling-command');
        if (!input) return;

        const text = input.value;
        if (!text) return;

        if (this.transport && this.transport.isConnected()) {
            // Send command + newline to execute
            this.transport.send(text + '\n');
            // Clear input
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

    setupEventListeners() {
        // Terminal data handler - send as binary to distinguish from JSON control messages
        this.term.onData(data => {
            if (this.transport && this.transport.isConnected()) {
                // Apply Ctrl modifier if active (for mobile keyboard input)
                if (this.ctrlPressed && data.length === 1) {
                    const char = data.toUpperCase();
                    if (char >= 'A' && char <= 'Z') {
                        // Convert to Ctrl sequence (Ctrl+A = 1, Ctrl+C = 3, etc.)
                        data = String.fromCharCode(char.charCodeAt(0) - 64);
                        // Reset Ctrl state after use
                        this.ctrlPressed = false;
                        this.querySelector('[data-modifier="ctrl"]').classList.remove('active');
                    }
                }

                // Send via transport (handles encoding)
                this.transport.send(data);
            }
        });

        // Window resize
        this._resizeHandler = () => {
            this.fitAddon.fit();
            this.sendResize();
        };
        window.addEventListener('resize', this._resizeHandler);

        // Extra keys
        const keyMap = {
            'Escape': '\x1b',
            'Tab': '\t',
            'ArrowUp': '\x1b[A',
            'ArrowDown': '\x1b[B',
            'ArrowRight': '\x1b[C',
            'ArrowLeft': '\x1b[D'
        };

        this.querySelectorAll('.terminal-ui__extra-keys button').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();

                if (btn.dataset.modifier === 'ctrl') {
                    this.ctrlPressed = !this.ctrlPressed;
                    btn.classList.toggle('active', this.ctrlPressed);
                    return;
                }

                if (btn.dataset.action === 'paste') {
                    if (navigator.clipboard && navigator.clipboard.readText) {
                        navigator.clipboard.readText().then(text => {
                            if (text && this.transport && this.transport.isConnected()) {
                                this.transport.send(text);
                            }
                            this.term.focus();
                        }).catch(err => {
                            console.error('Failed to read clipboard:', err);
                            // Fallback to paste overlay on error
                            this.showPasteOverlay();
                        });
                    } else {
                        // Clipboard API not available, show paste overlay
                        this.showPasteOverlay();
                    }
                    return;
                }

                const key = btn.dataset.key;
                if (key && keyMap[key]) {
                    let data = keyMap[key];

                    if (this.ctrlPressed && key.length === 1) {
                        data = String.fromCharCode(key.charCodeAt(0) - 64);
                    }

                    if (this.transport && this.transport.isConnected()) {
                        this.transport.send(data);
                    }

                    if (this.ctrlPressed) {
                        this.ctrlPressed = false;
                        this.querySelector('[data-modifier="ctrl"]').classList.remove('active');
                    }
                }

                this.term.focus();
            });
        });

        // Terminal click to focus
        this.querySelector('.terminal-ui__terminal').addEventListener('click', () => {
            this.term.focus();
        });

        // Status bar click to reconnect immediately when disconnected or connecting
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        statusBar.addEventListener('click', () => {
            if (statusBar.classList.contains('connecting') || statusBar.classList.contains('error') || statusBar.classList.contains('reconnecting')) {
                // Close existing connection attempt if any
                if (this.transport) {
                    this.transport.disconnect();
                    this.transport = null;
                }
                // Reset backoff on manual retry
                this.reconnectAttempts = 0;
                this.connect();
            }
        });

        // Status text (left side) click handler for connected state
        // Delegate to handle clicks on separate hyperlinks
        const statusText = this.querySelector('.terminal-ui__status-text');
        statusText.addEventListener('click', (e) => {
            // If clicking on an anchor, let the link work but don't trigger reconnect
            if (e.target.tagName === 'A') {
                e.stopPropagation();
                return;
            }

            // Only handle clicks when transport is connected
            if (!(this.transport && this.transport.isConnected())) {
                // Let click bubble to status bar handler for reconnect
                return;
            }
            e.stopPropagation(); // Don't trigger status bar click handler

            // Check if clicked on name link
            if (e.target.classList.contains('terminal-ui__status-name')) {
                // Prompt to set or rename username
                if (!this.currentUserName) {
                    this.getUserName();
                } else {
                    this.promptRenameUsername();
                }
            }
            // Check if clicked on "others" link
            else if (e.target.classList.contains('terminal-ui__status-others')) {
                // Open chat input
                this.toggleChatInput();
            }
            // Note: agent link is a real <a> tag, no JS handler needed
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

        // Polling mode input handlers
        const pollingInput = this.querySelector('.terminal-ui__polling-command');
        const pollingSendBtn = this.querySelector('.terminal-ui__polling-send');

        if (pollingInput) {
            pollingInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    e.preventDefault();
                    this.sendPollingCommand();
                }
            });
        }

        if (pollingSendBtn) {
            pollingSendBtn.addEventListener('click', () => {
                this.sendPollingCommand();
            });
        }

        // Polling mode quick action buttons
        this.querySelectorAll('.terminal-ui__polling-actions button').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                const sendData = btn.dataset.send;
                if (sendData && this.transport && this.transport.isConnected()) {
                    // Unescape the data string (e.g., "\x03" -> actual Ctrl+C character)
                    const unescaped = sendData
                        .replace(/\\x([0-9a-fA-F]{2})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)))
                        .replace(/\\t/g, '\t')
                        .replace(/\\n/g, '\n')
                        .replace(/\\r/g, '\r');
                    this.transport.send(unescaped);
                }
            });
        });

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

        if (!this.transport || !this.transport.isConnected()) {
            this.showTemporaryStatus('Not connected', 3000);
            return;
        }

        if (this.isTextFile(file)) {
            // Read and paste text directly to terminal
            const text = await this.readFileAsText(file);
            if (text === null) {
                this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
                return;
            }
            this.transport.send(text);
            this.showTemporaryStatus(`Pasted: ${file.name} (${this.formatFileSize(text.length)})`);
        } else {
            // Binary file: send as binary upload with 0x01 prefix
            // Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
            const fileData = await this.readFileAsBinary(file);
            if (fileData === null) {
                this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
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

            this.transport.send(message);
            this.showTemporaryStatus(`Uploaded: ${file.name} (${this.formatFileSize(file.size)})`);
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
}

customElements.define('terminal-ui', TerminalUI);
