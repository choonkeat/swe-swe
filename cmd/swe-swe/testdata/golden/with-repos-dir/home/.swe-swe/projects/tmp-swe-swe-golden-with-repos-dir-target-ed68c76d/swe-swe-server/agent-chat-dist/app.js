// Agent Chat — browser client
// Plain JS, no build step. Connects to the Go server via WebSocket.

'use strict';

// --- Theme detection ---

function getCookie(name) {
  var match = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
  return match ? decodeURIComponent(match[1]) : null;
}

function applyTheme() {
  var cookieName = (typeof THEME_COOKIE_NAME !== 'undefined') ? THEME_COOKIE_NAME : 'agent-chat-theme';
  var theme = getCookie(cookieName) || 'dark';
  document.documentElement.setAttribute('data-theme', theme);
}

applyTheme();
setInterval(applyTheme, 2000);

var messages = document.getElementById('messages');
var chatInput = document.getElementById('chat-input');
var sendBtn = document.getElementById('btn-send');
var statusDot = document.getElementById('status-dot');
var quickReplies = document.getElementById('quick-replies');

var activeWs = null;
var isUserScrolledUp = false;
var pendingAckId = null;
var pendingNotifyParent = false;
var firstMessageSent = false;

// --- Scroll tracking ---

messages.addEventListener('scroll', function () {
  var threshold = 40;
  var distFromBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight;
  isUserScrolledUp = distFromBottom > threshold;
});

function scrollToBottom(force) {
  if (!force && isUserScrolledUp) return;
  messages.scrollTop = messages.scrollHeight;
}

// --- Timestamp helper ---

function ts() {
  return new Date().toISOString().slice(11, 23);
}

// --- Canvas constants ---

var CANVAS_W = 900;
var CANVAS_H = 550;
var DPR = window.devicePixelRatio || 1;

// --- Message rendering ---

function clearMessages() {
  messages.innerHTML = '';
}

// --- Syntax highlighting ---

function highlightCode(code, lang) {
  var parts = [];
  var idx = 0;
  function save(cls, text) {
    var id = idx++;
    parts[id] = '<span class="hl-' + cls + '">' + text + '</span>';
    return '\x00' + id + '\x01';
  }

  var useHash = /^(python|py|ruby|rb|bash|sh|shell|zsh|yaml|yml|toml|perl|r|makefile|dockerfile|coffee)$/i.test(lang);
  var useDash = /^(sql|lua|haskell|hs|elm|ada)$/i.test(lang);

  // 1. Protect strings
  code = code.replace(/"(?:[^"\\]|\\.)*"/g, function(m) { return save('s', m); });
  code = code.replace(/'(?:[^'\\]|\\.)*'/g, function(m) { return save('s', m); });

  // 2. Protect comments
  code = code.replace(/\/\/[^\n]*/g, function(m) { return save('c', m); });
  code = code.replace(/\/\*[\s\S]*?\*\//g, function(m) { return save('c', m); });
  if (useHash) {
    code = code.replace(/#[^\n]*/g, function(m) { return save('c', m); });
  }
  if (useDash) {
    code = code.replace(/--[^\n]*/g, function(m) { return save('c', m); });
  }

  // 3. Keywords
  var kw = 'abstract|async|await|bool|break|case|catch|chan|char|class|const|continue|debugger|def|default|defer|delete|do|double|elif|else|enum|export|extends|extern|false|final|finally|float|fn|for|from|func|function|go|if|impl|implements|import|in|instanceof|int|interface|is|lambda|let|long|match|mod|mut|new|nil|none|not|null|of|or|package|pass|private|protected|pub|public|raise|range|return|select|self|short|signed|static|string|struct|super|switch|this|throw|trait|true|try|type|typeof|undefined|unless|unsigned|until|use|var|void|where|while|with|yield';
  code = code.replace(new RegExp('\\b(' + kw + ')\\b', 'g'), function(m) { return save('k', m); });

  // 4. Numbers
  code = code.replace(/\b(\d+\.?\d*(?:e[+-]?\d+)?)\b/gi, function(m) { return save('n', m); });

  // Restore placeholders
  return code.replace(/\x00(\d+)\x01/g, function(_, id) { return parts[parseInt(id)]; });
}

// --- Table helpers ---

function parseTableRow(row) {
  return row.replace(/^\||\|$/g, '').split('|');
}

function parseTableAlign(sep) {
  return sep.replace(/^\||\|$/g, '').split('|').map(function(c) {
    c = c.trim();
    if (c[0] === ':' && c[c.length - 1] === ':') return 'center';
    if (c[c.length - 1] === ':') return 'right';
    return '';
  });
}

// --- Markdown rendering ---

function renderMarkdown(text) {
  // Escape HTML
  var html = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  // Code blocks with syntax highlighting; use &#10; for newlines to prevent table regex matching inside
  html = html.replace(/```(\w*)\n?([\s\S]*?)```/g, function(_, lang, code) {
    var highlighted = lang ? highlightCode(code, lang) : code;
    highlighted = highlighted.replace(/\n/g, '&#10;');
    var cls = lang ? ' class="language-' + lang + '"' : '';
    return '<pre><code' + cls + '>' + highlighted + '</code></pre>';
  });
  // Inline code
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  // Tables
  html = html.replace(/(\|[^\n]+\|\n\|[-:| ]+\|\n(?:\|[^\n]+\|\n?)+)/g, function(block) {
    var lines = block.trim().split('\n');
    if (lines.length < 3) return block;
    if (!/^\|[-:| ]+\|$/.test(lines[1])) return block;
    var headers = parseTableRow(lines[0]);
    var aligns = parseTableAlign(lines[1]);
    var out = '<table><thead><tr>';
    headers.forEach(function(h, i) {
      var a = aligns[i] ? ' style="text-align:' + aligns[i] + '"' : '';
      out += '<th' + a + '>' + h.trim() + '</th>';
    });
    out += '</tr></thead><tbody>';
    for (var r = 2; r < lines.length; r++) {
      if (!lines[r].trim()) continue;
      var cells = parseTableRow(lines[r]);
      out += '<tr>';
      cells.forEach(function(c, i) {
        var a = aligns[i] ? ' style="text-align:' + aligns[i] + '"' : '';
        out += '<td' + a + '>' + c.trim() + '</td>';
      });
      out += '</tr>';
    }
    out += '</tbody></table>';
    return out;
  });
  // Bold (**text** or __text__)
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/__(.+?)__/g, '<strong>$1</strong>');
  // Italic (*text* or _text_)
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
  html = html.replace(/(?<!\w)_(.+?)_(?!\w)/g, '<em>$1</em>');
  // Links [text](url)
  html = html.replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  // Bare URLs
  html = html.replace(/(?<!["=])(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>');
  // Newlines
  html = html.replace(/\n/g, '<br>');
  return html;
}

function addBubble(text, role) {
  var div = document.createElement('div');
  div.className = 'bubble ' + role;
  div.innerHTML = renderMarkdown(text);
  messages.appendChild(div);
  scrollToBottom(false);
}

function addAgentMessage(text) {
  if (text) {
    addBubble(text, 'agent');
  }
}

function addUserMessage(text) {
  if (text) {
    addBubble(text, 'user');
  }
}

// --- Canvas bubble ---

function canvasToImg(canvas, div) {
  var img = document.createElement('img');
  img.src = canvas.toDataURL('image/png');
  var w = div.getBoundingClientRect().width;
  div.style.height = (w * CANVAS_H / CANVAS_W) + 'px';
  div.replaceChild(img, canvas);
}

function addCanvasBubble(instructions, skipAnimation, onDone) {
  var div = document.createElement('div');
  div.className = 'bubble agent canvas-bubble';

  var canvas = document.createElement('canvas');
  canvas.width = CANVAS_W * DPR;
  canvas.height = CANVAS_H * DPR;
  div.appendChild(canvas);

  messages.appendChild(div);
  scrollToBottom(false);

  var finalize = function () {
    // Wait two frames so the renderer composites before we snapshot
    requestAnimationFrame(function () {
      requestAnimationFrame(function () {
        canvasToImg(canvas, div);
        scrollToBottom(false);
        if (onDone) onDone();
      });
    });
  };

  var board = new CanvasBundle.AgentWhiteboard(canvas, {
    width: CANVAS_W,
    height: CANVAS_H,
    backgroundColor: '#0d1525',
    onQueueEmpty: finalize,
  });
  board.resize(CANVAS_W, CANVAS_H, DPR);

  if (skipAnimation) {
    board.setSkipAnimation(true);
  }

  // Validate instructions
  var result = CanvasBundle.validateInstructions(instructions);
  if (result.errors.length > 0) {
    console.warn('Canvas instruction validation errors:', result.errors);
  }
  board.addInstructions(result.valid);

  return { div: div, board: board, canvas: canvas };
}

// --- Input enable/disable ---

function setQuickReplies(replies) {
  quickReplies.innerHTML = '';
  if (!replies || replies.length === 0) return;
  for (var i = 0; i < replies.length; i++) {
    var btn = document.createElement('button');
    btn.className = 'chip';
    btn.dataset.message = replies[i];
    btn.textContent = replies[i];
    quickReplies.appendChild(btn);
  }
  scrollToBottom(false);
}

function enableInput(replies) {
  setQuickReplies(replies);
  chatInput.disabled = false;
  sendBtn.disabled = false;
  if (replies && replies.length > 0) {
    quickReplies.classList.add('visible');
  } else {
    quickReplies.classList.remove('visible');
  }
  chatInput.focus();
  setTimeout(function () { scrollToBottom(true); }, 100);
}

function showLoading() {
  removeLoading();
  var div = document.createElement('div');
  div.className = 'bubble agent loading';
  div.id = 'loading-bubble';
  div.innerHTML = '<span class="dot"></span><span class="dot"></span><span class="dot"></span>';
  messages.appendChild(div);
  scrollToBottom(false);
}

function removeLoading() {
  var el = document.getElementById('loading-bubble');
  if (el) el.remove();
}

// --- Send ---

function sendMessage(text) {
  if (!activeWs || activeWs.readyState !== WebSocket.OPEN) return;
  if (pendingAckId) {
    activeWs.send(JSON.stringify({ type: 'ack', id: pendingAckId, message: text }));
    pendingAckId = null;
  } else {
    activeWs.send(JSON.stringify({ type: 'message', text: text }));
  }
}

function handleSend() {
  var text = chatInput.value.trim();
  if (!text) return;

  addUserMessage(text);
  isUserScrolledUp = false;
  // Send message to server first; postMessage to parent happens after
  // server confirms the message is queued (see 'messageQueued' handler).
  if (window.parent !== window) {
    pendingNotifyParent = true;
  }
  sendMessage(text);
  chatInput.value = '';
  chatInput.style.height = 'auto';
  quickReplies.classList.remove('visible');
  showLoading();
}

// Auto-grow textarea
function autoGrow() {
  chatInput.style.height = 'auto';
  chatInput.style.height = Math.min(chatInput.scrollHeight, 150) + 'px';
  chatInput.style.overflowY = chatInput.scrollHeight > 150 ? 'auto' : 'hidden';
}

chatInput.addEventListener('input', autoGrow);

// Enter sends, Shift+Enter / Alt+Enter inserts newline
chatInput.addEventListener('keydown', function (e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
    e.preventDefault();
    handleSend();
  } else if (e.key === 'Enter' && e.altKey) {
    e.preventDefault();
    var start = chatInput.selectionStart;
    var end = chatInput.selectionEnd;
    chatInput.value = chatInput.value.substring(0, start) + '\n' + chatInput.value.substring(end);
    chatInput.selectionStart = chatInput.selectionEnd = start + 1;
    autoGrow();
  }
});

sendBtn.addEventListener('click', handleSend);

// Quick-reply chips
quickReplies.addEventListener('click', function (e) {
  var chip = e.target.closest('.chip');
  if (!chip || chip.disabled) return;

  var message = chip.dataset.message || '';
  if (!message) return;
  addUserMessage(message);
  isUserScrolledUp = false;
  // Send message to server first; postMessage to parent happens after
  // server confirms the message is queued (see 'messageQueued' handler).
  if (window.parent !== window) {
    pendingNotifyParent = true;
  }
  sendMessage(message);
  chatInput.value = '';
  quickReplies.classList.remove('visible');
  showLoading();
});

// --- Connection status ---

function setStatus(state) {
  statusDot.className = state;
}

// --- WebSocket connection with exponential backoff ---

var BACKOFF_INITIAL = 1000;
var BACKOFF_MAX = 30000;
var backoffDelay = BACKOFF_INITIAL;
var reconnectTimer = null;

// --- History replay for browser reconnect ---

function replayHistory(history) {
  console.log('[' + ts() + '] Replaying ' + history.length + ' history events');
  clearMessages();

  for (var i = 0; i < history.length; i++) {
    var event = history[i];
    switch (event.type) {
      case 'agentMessage':
        if (event.text) {
          addBubble(event.text, 'agent');
        }
        break;
      case 'userMessage':
        if (event.text) {
          addBubble(event.text, 'user');
        }
        break;
      case 'draw':
        if (event.instructions) {
          addCanvasBubble(event.instructions, true, null);
        }
        break;
    }
  }
}

// --- WebSocket connection ---

function teardown() {
  if (activeWs) {
    activeWs.onopen = null;
    activeWs.onmessage = null;
    activeWs.onclose = null;
    activeWs.onerror = null;
    activeWs.close();
    activeWs = null;
  }
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function scheduleReconnect() {
  if (reconnectTimer !== null) return;
  reconnectTimer = setTimeout(function () {
    reconnectTimer = null;
    connect();
  }, backoffDelay);
  backoffDelay = Math.min(backoffDelay * 2, BACKOFF_MAX);
}

function connect() {
  teardown();
  setStatus('connecting');

  var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var basePath = location.pathname.replace(/\/+$/, '');
  var wsUrl = proto + '//' + location.host + basePath + '/ws';
  var ws = new WebSocket(wsUrl);
  activeWs = ws;

  ws.onopen = function () {
    console.log('[' + ts() + '] WebSocket onopen');
    setStatus('connected');
    backoffDelay = BACKOFF_INITIAL;
  };

  ws.onmessage = function (event) {
    if (ws !== activeWs) return;
    var data = JSON.parse(event.data);

    switch (data.type) {
      case 'connected':
        console.log('[' + ts() + '] Connected event received');
        setStatus('connected');
        if (data.history && Array.isArray(data.history) && data.history.length > 0) {
          replayHistory(data.history);
        }
        if (data.pendingAckId) {
          pendingAckId = data.pendingAckId;
        }
        enableInput();
        break;

      case 'agentMessage':
        console.log('[' + ts() + '] Agent message received: "' + data.text + '"');
        removeLoading();
        addAgentMessage(data.text || '');
        enableInput(data.quick_replies);
        // Progress updates (no quick_replies) mean agent is still working — show thinking indicator
        if (!data.quick_replies || data.quick_replies.length === 0) {
          showLoading();
        }
        break;

      case 'draw':
        console.log('[' + ts() + '] Draw event received (' + (data.instructions || []).length + ' instructions)');
        removeLoading();

        // Store ack_id so quick-reply/send resolves the draw ack
        if (data.ack_id) {
          pendingAckId = data.ack_id;
        }

        addCanvasBubble(data.instructions || [], false, function () {
          enableInput(data.quick_replies);
        });
        break;

      case 'messageQueued':
        // Server confirmed the message is in the queue — now safe to
        // tell the parent frame so it can trigger check_messages.
        if (pendingNotifyParent && window.parent !== window) {
          var msg = { type: 'agent-chat-first-user-message' };
          // First message includes hint text for the parent to type into the terminal.
          if (!firstMessageSent) {
            msg.text = 'check_messages; i sent u a chat message';
            firstMessageSent = true;
          }
          window.parent.postMessage(msg, '*');
          pendingNotifyParent = false;
        }
        break;
    }
  };

  ws.onclose = function () {
    if (ws !== activeWs) return;
    console.log('[' + ts() + '] WebSocket closed, reconnecting...');
    teardown();
    setStatus('connecting');
    scheduleReconnect();
  };

  ws.onerror = function () {
    if (ws !== activeWs) return;
    console.log('[' + ts() + '] WebSocket error');
  };
}

// --- Export / Download ---

document.getElementById('btn-download').addEventListener('click', function () {
  var bubbles = messages.querySelectorAll('.bubble');
  var items = [];

  for (var i = 0; i < bubbles.length; i++) {
    var b = bubbles[i];
    if (b.id === 'loading-bubble') continue;

    if (b.classList.contains('canvas-bubble')) {
      var img = b.querySelector('img');
      if (img) {
        items.push('<div class="bubble agent canvas-bubble"><img src="' + img.src + '" style="width:100%;height:auto;display:block;border-radius:8px;"></div>');
      }
    } else {
      var role = b.classList.contains('user') ? 'user' : b.classList.contains('system') ? 'system' : 'agent';
      items.push('<div class="bubble ' + role + '">' + b.innerHTML + '</div>');
    }
  }

  var html = '<!DOCTYPE html>\n<html lang="en"><head><meta charset="UTF-8"><title>Chat Export</title><style>'
    + 'body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#1a1a2e;color:#e0e0e0;margin:0;padding:2rem;display:flex;justify-content:center;}'
    + '.chat{max-width:800px;width:100%;display:flex;flex-direction:column;gap:0.4rem;}'
    + '.bubble{max-width:80%;padding:0.5rem 0.75rem;border-radius:12px;font-size:0.9rem;line-height:1.45;word-wrap:break-word;}'
    + '.bubble.agent{align-self:flex-start;background:#16213e;color:#ccc;border-bottom-left-radius:3px;}'
    + '.bubble.user{align-self:flex-end;background:#2563eb;color:#fff;border-bottom-right-radius:3px;}'
    + '.bubble.system{align-self:center;color:#666;font-size:0.75rem;}'
    + '.bubble.canvas-bubble{padding:0;background:#0d1525;overflow:hidden;max-width:90%;}'
    + '.bubble code{background:rgba(255,255,255,0.1);padding:0.1rem 0.3rem;border-radius:3px;font-size:0.85em;}'
    + '.bubble pre{background:rgba(0,0,0,0.3);padding:0.5rem;border-radius:6px;overflow-x:auto;margin:0.3rem 0;}'
    + '.bubble pre code{background:none;padding:0;}'
    + '.bubble a{color:#60a5fa;text-decoration:underline;}'
    + '</style></head><body><div class="chat">'
    + items.join('\n')
    + '</div></body></html>';

  var blob = new Blob([html], { type: 'text/html' });
  var url = URL.createObjectURL(blob);
  var filename = 'chat-export-' + new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-') + '.html';
  var a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
});

// --- Playback mode ---

function startPlaybackMode(url) {
  // Hide interactive elements
  document.getElementById('input-bar').style.display = 'none';
  document.getElementById('quick-replies').style.display = 'none';
  document.getElementById('btn-download').style.display = 'none';
  setStatus('connected');

  fetch(url)
    .then(function (resp) {
      if (!resp.ok) throw new Error('Failed to load events: ' + resp.status);
      return resp.text();
    })
    .then(function (text) {
      var events = text.trim().split('\n').filter(Boolean).map(function (line) {
        return JSON.parse(line);
      });
      replayHistory(events);
    })
    .catch(function (err) {
      addBubble('Playback error: ' + err.message, 'agent');
    });
}

// --- Startup ---
// If AGENT_CHAT_DEFER_STARTUP is set, skip automatic connect/playback
// (allows embedding pages to call startPlaybackMode() manually).

if (typeof AGENT_CHAT_DEFER_STARTUP === 'undefined' || !AGENT_CHAT_DEFER_STARTUP) {
  var params = new URLSearchParams(window.location.search);
  var playbackUrl = params.get('playback');
  if (playbackUrl) {
    startPlaybackMode(playbackUrl);
  } else {
    connect();
  }
}
