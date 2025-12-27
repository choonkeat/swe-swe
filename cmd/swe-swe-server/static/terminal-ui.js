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
        if (this.statusRestoreTimeout) {
            clearTimeout(this.statusRestoreTimeout);
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
                    background: #4a4a4a;
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
                    background: #007acc;
                    color: #fff;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    font-size: 12px;
                    transition: background-color 0.3s ease;
                }
                .terminal-ui__status-bar.error,
                .terminal-ui__status-bar.reconnecting {
                    cursor: pointer;
                }
                .terminal-ui__status-bar.error {
                    background: #d32f2f;
                }
                .terminal-ui__status-bar.reconnecting {
                    background: #f57c00;
                }
                .terminal-ui__status-icon {
                    width: 8px;
                    height: 8px;
                    border-radius: 50%;
                    background: #4caf50;
                    margin-right: 8px;
                }
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
                <div class="terminal-ui__terminal"></div>
                <div class="terminal-ui__extra-keys">
                    <button data-key="Escape">ESC</button>
                    <button data-key="Tab">TAB</button>
                    <button data-modifier="ctrl" class="modifier">Ctrl</button>
                    <button data-key="ArrowUp">↑</button>
                    <button data-key="ArrowDown">↓</button>
                    <button data-key="ArrowLeft">←</button>
                    <button data-key="ArrowRight">→</button>
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

    updateStatus(state, message) {
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        const statusText = this.querySelector('.terminal-ui__status-text');
        const terminalEl = this.querySelector('.terminal-ui__terminal');

        statusBar.className = 'terminal-ui__status-bar ' + state;
        statusText.textContent = message;
        terminalEl.classList.toggle('disconnected', state !== '');

        // Clear status info when not connected
        if (state !== '') {
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

    scheduleReconnect() {
        const delay = this.getReconnectDelay();
        this.reconnectAttempts++;

        let remaining = Math.ceil(delay / 1000);
        this.updateStatus('reconnecting', `Reconnecting in ${remaining}s...`);

        this.countdownInterval = setInterval(() => {
            remaining--;
            if (remaining > 0) {
                this.updateStatus('reconnecting', `Reconnecting in ${remaining}s...`);
            }
        }, 1000);

        this.reconnectTimeout = setTimeout(() => {
            clearInterval(this.countdownInterval);
            this.connect();
        }, delay);
    }

    sendResize() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            const rows = this.term.rows;
            const cols = this.term.cols;
            const msg = new Uint8Array([
                0x00,
                (rows >> 8) & 0xFF, rows & 0xFF,
                (cols >> 8) & 0xFF, cols & 0xFF
            ]);
            this.ws.send(msg);
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

        this.updateStatus('', 'Connecting...');
        const timerEl = this.querySelector('.terminal-ui__status-timer');
        if (timerEl) timerEl.textContent = '';

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        this.ws = new WebSocket(protocol + '//' + window.location.host + '/ws/' + this.uuid + '?assistant=' + encodeURIComponent(this.assistant));
        this.ws.binaryType = 'arraybuffer';

        this.ws.onopen = () => {
            this.reconnectAttempts = 0;
            this.updateStatus('', 'Connected');
            this.startUptimeTimer();
            this.sendResize();
            this.startHeartbeat();
        };

        this.ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                // Binary = terminal output
                this.term.write(new Uint8Array(event.data));
            } else if (typeof event.data === 'string') {
                // Text = JSON control message
                try {
                    const msg = JSON.parse(event.data);
                    this.handleJSONMessage(msg);
                } catch (e) {
                    console.error('Invalid JSON from server:', e);
                }
            }
        };

        this.ws.onclose = () => {
            this.stopUptimeTimer();
            this.stopHeartbeat();
            this.updateStatus('error', 'Connection closed');
            this.scheduleReconnect();
        };

        this.ws.onerror = () => {
            this.stopUptimeTimer();
            this.stopHeartbeat();
            this.updateStatus('error', 'Connection error');
        };
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
            default:
                console.log('Unknown JSON message:', msg);
        }
    }

    updateStatusInfo() {
        const statusText = this.querySelector('.terminal-ui__status-text');
        const dimsEl = this.querySelector('.terminal-ui__status-dims');

        if (!statusText) return;

        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            // Build "Connected as {name} with {agent}" message with separate clickable parts
            const userName = this.currentUserName;
            let html = `Connected as <span class="terminal-ui__status-link terminal-ui__status-name">${userName}</span>`;

            if (this.assistantName) {
                html += ` with ${this.assistantName}`;
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
            // For error/reconnecting states, updateStatus() handles the text
            if (dimsEl) {
                dimsEl.textContent = '';
            }
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

    setupEventListeners() {
        // Terminal data handler - send as binary to distinguish from JSON control messages
        this.term.onData(data => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                // Convert string to Uint8Array for binary transmission
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

                const key = btn.dataset.key;
                if (key && keyMap[key]) {
                    let data = keyMap[key];

                    if (this.ctrlPressed && key.length === 1) {
                        data = String.fromCharCode(key.charCodeAt(0) - 64);
                    }

                    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                        // Send as binary to distinguish from JSON control messages
                        const encoder = new TextEncoder();
                        this.ws.send(encoder.encode(data));
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

        // Status bar click to reconnect immediately when disconnected
        const statusBar = this.querySelector('.terminal-ui__status-bar');
        statusBar.addEventListener('click', () => {
            if (statusBar.classList.contains('error') || statusBar.classList.contains('reconnecting')) {
                this.connect();
            }
        });

        // Status text (left side) click handler for connected state
        // Delegate to handle clicks on separate hyperlinks
        const statusText = this.querySelector('.terminal-ui__status-text');
        statusText.addEventListener('click', (e) => {
            e.stopPropagation(); // Don't trigger status bar click handler

            // Only handle clicks when WebSocket is open
            if (!(this.ws && this.ws.readyState === WebSocket.OPEN)) {
                return;
            }

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
            this.showTemporaryStatus('Not connected', 3000);
            return;
        }

        const encoder = new TextEncoder();

        if (this.isTextFile(file)) {
            // Read and paste text directly to terminal
            const text = await this.readFileAsText(file);
            if (text === null) {
                this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
                return;
            }
            this.ws.send(encoder.encode(text));
            this.showTemporaryStatus(`Pasted: ${file.name} (${this.formatFileSize(text.length)})`);
        } else {
            // Binary file: send as binary upload with 0x01 prefix
            // Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
            const fileData = await this.readFileAsBinary(file);
            if (fileData === null) {
                this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
                return;
            }
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
