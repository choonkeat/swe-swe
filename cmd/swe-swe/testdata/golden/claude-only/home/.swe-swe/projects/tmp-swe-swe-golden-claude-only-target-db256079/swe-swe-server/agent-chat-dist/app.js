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
var chatEl = document.getElementById('chat');
var quickReplies = document.getElementById('quick-replies');
var btnAttach = document.getElementById('btn-attach');
var filePicker = document.getElementById('file-picker');
var inputContainer = document.getElementById('input-container');
var fileStaging = document.getElementById('file-staging');
var dropZone = document.body;

var btnVoice = document.getElementById('btn-voice');
var voiceControls = document.getElementById('voice-controls');
var voiceSelect = document.getElementById('voice-select');

var activeWs = null;
var isUserScrolledUp = false;
var pendingAckId = null;
var pendingNotifyParent = false;
var pendingInterrupt = false;
var firstMessageSent = false;
var stagedFiles = []; // [{file: File, name: string, previewUrl: string|null, ref: FileRef|null, uploading: bool, uploadFailed: bool, abortController: AbortController|null}]
var lastSeq = 0; // highest event seq received — sent as cursor on reconnect
var interruptPhrases = ['stop', 'wait', 'cancel', 'hold on', 'abort', 'halt', 'pause'];
var warningShown = false; // show "type check_messages" warning only once

// --- Voice mode state ---
var voiceMode = false;
var voiceRecognition = null;
var selectedVoice = null;
var isSpeaking = false;
var isListening = false;
var micRetryCount = 0;
var micRetryMax = 5;
var micRetryBaseDelay = 500; // ms, doubles each retry
var micRetryTimer = null; // guards against concurrent retry scheduling
var ttsUnlocked = false; // tracks whether the TTS warmup completed successfully
var ttsSafetyTimer = null; // safety timeout for stuck TTS
var ttsQueue = []; // queued verbal replies waiting to be spoken

// --- Scroll tracking ---

window.addEventListener('scroll', function () {
  var threshold = 40;
  var distFromBottom = document.documentElement.scrollHeight - window.scrollY - window.innerHeight;
  isUserScrolledUp = distFromBottom > threshold;
});

// Auto-focus chat input when window/iframe regains focus
function focusChatInput() {
  if (chatInput && !chatInput.disabled && !chatInput.readOnly) {
    chatInput.focus();
  }
}
window.addEventListener('focus', focusChatInput);
document.addEventListener('visibilitychange', function () {
  if (!document.hidden) focusChatInput();
});

function scrollToBottom(force) {
  if (!force && isUserScrolledUp) return;
  window.scrollTo(0, document.documentElement.scrollHeight);
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
  messages.appendChild(quickReplies); // keep as last child for appendMessage
  lastBubbleTs = 0;
}

// --- Syntax highlighting ---

function highlightCode(code, lang) {
  var parts = [];
  var idx = 0;
  function save(cls, text) {
    var id = idx++;
    parts[id] = '<span class="hl-' + cls + '">' + text + '</span>';
    return '\x00P' + id + '\x01';
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
  return code.replace(/\x00P(\d+)\x01/g, function(_, id) { return parts[parseInt(id)]; });
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
  // Inline code (same line only to prevent dangling backtick eating content).
  // Stash code spans behind placeholders so later inline rules (bold/italic/links)
  // don't mangle their contents — per CommonMark, code span text is literal.
  var codeSpans = [];
  html = html.replace(/`([^`\n]+)`/g, function(_, content) {
    var idx = codeSpans.push('<code>' + content + '</code>') - 1;
    return '\u0000CODE' + idx + '\u0000';
  });
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
  // Headings (# through ######)
  html = html.replace(/^(#{1,6}) (.+)$/gm, function(_, hashes, text) {
    var level = hashes.length;
    return '<h' + level + '>' + text + '</h' + level + '>';
  });
  // Horizontal rules (---, ***, ___ on their own line)
  html = html.replace(/^(---|\*\*\*|___)$/gm, '<hr>');
  // Unordered lists (consecutive lines starting with - or * , allowing blank lines between items)
  html = html.replace(/(^[-*] .+(?:\n\n?[-*] .+)*)/gm, function(block) {
    var items = block.split(/\n\n?(?=[-*] )/).map(function(line) {
      return '<li>' + line.replace(/^[-*] /, '') + '</li>';
    }).join('');
    return '<ul>' + items + '</ul>';
  });
  // Ordered lists (consecutive lines starting with 1. 2. etc., allowing blank lines between items)
  html = html.replace(/(^\d+\. .+(?:\n\n?\d+\. .+)*)/gm, function(block) {
    var items = block.split(/\n\n?(?=\d+\. )/).map(function(line) {
      return '<li>' + line.replace(/^\d+\. /, '') + '</li>';
    }).join('');
    return '<ol>' + items + '</ol>';
  });
  // Blockquotes (consecutive lines starting with > , supports nesting with >> )
  function parseBlockquotes(text) {
    return text.replace(/(^&gt;[ &].+(?:\n&gt;[ &].+)*)/gm, function(block) {
      var inner = block.replace(/^&gt; ?/gm, '');
      inner = parseBlockquotes(inner);
      return '<blockquote>' + inner + '</blockquote>';
    });
  }
  html = parseBlockquotes(html);
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
  // Clean up newlines adjacent to block-level elements to avoid double spacing
  html = html.replace(/\n*(<\/?(h[1-6]|hr|ul|ol|li|pre|table|thead|tbody|tr|th|td|blockquote)[\s>])/g, '$1');
  html = html.replace(/(<\/(h[1-6]|hr|ul|ol|li|pre|table|thead|tbody|tr|th|td|blockquote)>)\n*/g, '$1');
  // Newlines
  html = html.replace(/\n/g, '<br>');
  // Restore stashed inline code spans
  html = html.replace(/\u0000CODE(\d+)\u0000/g, function(_, idx) {
    return codeSpans[parseInt(idx, 10)];
  });
  return html;
}

function renderFileAttachments(files) {
  if (!files || files.length === 0) return null;
  var container = document.createElement('div');
  container.className = 'file-attachments';
  for (var i = 0; i < files.length; i++) {
    var f = files[i];
    var isImage = f.type && f.type.indexOf('image/') === 0;
    if (isImage) {
      var img = document.createElement('img');
      img.className = 'file-thumb';
      img.src = f.url;
      img.alt = f.name;
      img.title = f.name;
      img.addEventListener('click', (function(url) {
        return function() { window.open(url, '_blank'); };
      })(f.url));
      container.appendChild(img);
    } else {
      var link = document.createElement('a');
      link.className = 'file-attachment-link';
      link.href = f.url;
      link.target = '_blank';
      link.rel = 'noopener';
      link.textContent = f.name;
      container.appendChild(link);
    }
  }
  return container;
}

function createTtsButton(bubble) {
  var btn = document.createElement('button');
  btn.className = 'bubble-tts-btn';
  btn.title = 'Speak aloud';
  btn.innerHTML = '<svg viewBox="0 0 24 24"><polygon points="6,4 20,12 6,20"/></svg>';
  btn.addEventListener('click', function(e) {
    e.stopPropagation();
    if (btn.classList.contains('playing')) return;
    btn.classList.add('playing');
    // This speak is inside a user gesture — unlocks iOS TTS
    speakText(bubble.innerText, function() {
      btn.classList.remove('playing');
      ttsUnlocked = true;
    });
  });
  return btn;
}

var lastBubbleTs = 0;

function formatElapsed(ms) {
  if (ms < 1000) return ms + 'ms';
  var secs = ms / 1000;
  if (secs < 60) return secs.toFixed(1) + 's';
  var mins = Math.floor(secs / 60);
  var remSecs = Math.round(secs % 60);
  return mins + 'm ' + remSecs + 's';
}

function addBubble(text, role, files, extraClass, timestamp) {
  // Show elapsed time before agent messages (how long the agent took to reply)
  if (role !== 'user' && timestamp && lastBubbleTs) {
    var delta = timestamp - lastBubbleTs;
    if (delta > 0) {
      var elapsed = document.createElement('div');
      elapsed.className = 'elapsed-time';
      elapsed.textContent = formatElapsed(delta);
      appendMessage(elapsed);
    }
  }
  if (timestamp) lastBubbleTs = timestamp;

  var div = document.createElement('div');
  div.className = 'bubble ' + role + (extraClass ? ' ' + extraClass : '');
  if (text) {
    div.innerHTML = renderMarkdown(text);
  }
  var attachments = renderFileAttachments(files);
  if (attachments) {
    div.appendChild(attachments);
  }
  // Add TTS play button to agent bubbles
  if (role === 'agent') {
    div.appendChild(createTtsButton(div));
  }
  appendMessage(div);
  scrollToBottom(false);
}

function addAgentMessage(text, files, extraClass, timestamp) {
  if (text || (files && files.length > 0)) {
    addBubble(text, 'agent', files, extraClass, timestamp);
  }
}

function addUserMessage(text, files, extraClass, timestamp) {
  if (text || (files && files.length > 0)) {
    addBubble(text, 'user', files, extraClass, timestamp);
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

  appendMessage(div);
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

// Freeze active quick-reply chips into the message log as inert elements.
// chosenText is excluded (it becomes the user bubble). Idempotent — no-op if
// there are no active chips.
function freezeCurrentReplies(chosenText) {
  var chips = quickReplies.querySelectorAll('.chip');
  if (chips.length === 0) return;

  var remaining = [];
  for (var i = 0; i < chips.length; i++) {
    var text = chips[i].dataset.message || chips[i].textContent;
    if (text !== chosenText) {
      remaining.push(text);
    }
  }
  // Clear active quick replies so they won't be frozen again
  quickReplies.innerHTML = '';

  if (remaining.length === 0) return;
  appendFrozenReplies(remaining);
}

// Render an array of reply texts as inert chip elements in the message flow.
function appendFrozenReplies(replies) {
  var div = document.createElement('div');
  div.className = 'frozen-replies';
  for (var i = 0; i < replies.length; i++) {
    var span = document.createElement('span');
    span.className = 'chip frozen';
    span.textContent = replies[i];
    div.appendChild(span);
  }
  appendMessage(div);
}

// --- File staging ---

function addStagedFiles(fileList) {
  for (var i = 0; i < fileList.length; i++) {
    var file = fileList[i];
    var isImage = file.type && file.type.indexOf('image/') === 0;
    var entry = {
      file: file,
      name: file.name,
      previewUrl: isImage ? URL.createObjectURL(file) : null,
      ref: null,
      uploading: true,
      uploadFailed: false,
      abortController: null
    };
    stagedFiles.push(entry);
    startUpload(entry);
  }
  renderStaging();
  updateSendButton();
}

function startUpload(entry, isRetry) {
  var controller = typeof AbortController !== 'undefined' ? new AbortController() : null;
  entry.abortController = controller;
  entry.uploading = true;
  entry.uploadFailed = false;
  renderStaging();
  updateSendButton();

  var formData = new FormData();
  formData.append('files', entry.file);
  var opts = { method: 'POST', body: formData };
  if (controller) opts.signal = controller.signal;

  fetch('upload', opts)
    .then(function(resp) {
      if (!resp.ok) throw new Error('Upload failed: ' + resp.status);
      return resp.json();
    })
    .then(function(refs) {
      entry.ref = refs[0];
      entry.uploading = false;
      entry.abortController = null;
      renderStaging();
      updateSendButton();
    })
    .catch(function(err) {
      entry.abortController = null;
      if (err.name === 'AbortError') return; // user removed chip
      if (!isRetry) {
        // Auto-retry once after 1s
        setTimeout(function() {
          if (stagedFiles.indexOf(entry) !== -1) {
            startUpload(entry, true);
          }
        }, 1000);
      } else {
        entry.uploading = false;
        entry.uploadFailed = true;
        console.error('Upload failed after retry:', err);
        renderStaging();
        updateSendButton();
      }
    });
}

function hasUploadsPending() {
  for (var i = 0; i < stagedFiles.length; i++) {
    if (stagedFiles[i].uploading || stagedFiles[i].uploadFailed) return true;
  }
  return false;
}

function updateSendButton() {
  // Only act within the "enabled" branch — don't override existing disable reasons.
  // If input is in sending state (waiting for server), leave button as-is.
  if (chatInput.readOnly && chatInput.classList.contains('sending')) return;
  if (hasUploadsPending()) {
    sendBtn.disabled = true;
    if (stagedFiles.some(function(sf) { return sf.uploading; })) {
      sendBtn.classList.add('loading');
    } else {
      sendBtn.classList.remove('loading');
    }
  } else {
    sendBtn.disabled = false;
    sendBtn.classList.remove('loading');
  }
}

function renderStaging() {
  fileStaging.innerHTML = '';
  if (stagedFiles.length === 0) {
    fileStaging.classList.remove('visible');
    return;
  }
  fileStaging.classList.add('visible');
  for (var i = 0; i < stagedFiles.length; i++) {
    var sf = stagedFiles[i];
    var chip = document.createElement('div');
    chip.className = 'file-chip';
    if (sf.uploading) chip.classList.add('uploading');
    if (sf.uploadFailed) chip.classList.add('upload-failed');

    if (sf.previewUrl) {
      var img = document.createElement('img');
      img.src = sf.previewUrl;
      img.alt = sf.name;
      chip.appendChild(img);
    } else {
      var icon = document.createElement('div');
      icon.className = 'file-icon';
      var ext = sf.name.split('.').pop().toUpperCase();
      icon.textContent = ext.length <= 4 ? ext : 'FILE';
      chip.appendChild(icon);
    }

    var nameSpan = document.createElement('span');
    nameSpan.className = 'file-name';
    nameSpan.textContent = sf.name;
    nameSpan.title = sf.uploadFailed ? 'Upload failed \u2014 remove and re-add' : sf.name;
    chip.appendChild(nameSpan);

    var removeBtn = document.createElement('button');
    removeBtn.className = 'file-remove';
    removeBtn.textContent = '\u00d7';
    removeBtn.dataset.index = i;
    removeBtn.addEventListener('click', function(e) {
      var idx = parseInt(e.currentTarget.dataset.index);
      var entry = stagedFiles[idx];
      if (entry) {
        if (entry.previewUrl) URL.revokeObjectURL(entry.previewUrl);
        if (entry.abortController) entry.abortController.abort();
      }
      stagedFiles.splice(idx, 1);
      renderStaging();
      updateSendButton();
    });
    chip.appendChild(removeBtn);

    fileStaging.appendChild(chip);
  }
}

// Paperclip button
btnAttach.addEventListener('click', function() {
  filePicker.click();
});

filePicker.addEventListener('change', function() {
  if (filePicker.files.length > 0) {
    addStagedFiles(filePicker.files);
  }
  filePicker.value = '';
});

// Drag and drop on entire window
dropZone.addEventListener('dragover', function(e) {
  e.preventDefault();
  chatEl.classList.add('drag-over');
});

dropZone.addEventListener('dragenter', function(e) {
  e.preventDefault();
  chatEl.classList.add('drag-over');
});

dropZone.addEventListener('dragleave', function(e) {
  // Only remove if we've left the drop zone entirely
  if (!dropZone.contains(e.relatedTarget)) {
    chatEl.classList.remove('drag-over');
  }
});

dropZone.addEventListener('drop', function(e) {
  e.preventDefault();
  chatEl.classList.remove('drag-over');
  if (e.dataTransfer.files.length > 0) {
    addStagedFiles(e.dataTransfer.files);
  }
});

function enableInput(replies) {
  setQuickReplies(replies);
  chatInput.disabled = false;
  chatInput.readOnly = false;
  chatInput.classList.remove('sending');
  sendBtn.disabled = false;
  sendBtn.classList.remove('sending');
  btnAttach.disabled = false;
  if (replies && replies.length > 0) {
    removeLoading(); // loading and quick replies are mutually exclusive
    quickReplies.classList.add('visible');
  } else {
    quickReplies.classList.remove('visible');
  }
  updateSendButton(); // re-disable if uploads still pending
  chatInput.focus();
  setTimeout(function () { scrollToBottom(true); }, 100);
}

// Insert element into messages, always before the loading bubble so it stays last.
function appendMessage(el) {
  var loader = document.getElementById('loading-bubble');
  if (loader) {
    messages.insertBefore(el, loader);
  } else {
    // quick-replies is always the last child of #messages; insert before it
    messages.insertBefore(el, quickReplies);
  }
}

function showLoading() {
  removeLoading();
  quickReplies.classList.remove('visible'); // loading and quick replies are mutually exclusive
  var div = document.createElement('div');
  div.className = 'bubble agent loading';
  div.id = 'loading-bubble';
  div.innerHTML = '<span class="dot"></span><span class="dot"></span><span class="dot"></span>';
  messages.insertBefore(div, quickReplies);
  scrollToBottom(false);
}

function removeLoading() {
  var el = document.getElementById('loading-bubble');
  if (el) el.remove();
}

// --- Send ---

function sendMessage(text, files) {
  if (!activeWs || activeWs.readyState !== WebSocket.OPEN) return;
  if (pendingAckId) {
    activeWs.send(JSON.stringify({ type: 'ack', id: pendingAckId, message: text }));
    pendingAckId = null;
  } else {
    var msg = { type: 'message', text: text };
    if (files && files.length > 0) {
      msg.files = files;
    }
    activeWs.send(JSON.stringify(msg));
  }
}


function handleSend() {
  var text = chatInput.value.trim();
  var fileRefs = [];
  for (var i = 0; i < stagedFiles.length; i++) {
    if (stagedFiles[i].ref) fileRefs.push(stagedFiles[i].ref);
  }
  if (!text && fileRefs.length === 0) return;

  // Don't display the bubble yet — wait for the server to broadcast it back.
  // Use readOnly instead of disabled to preserve focus and keep mobile keyboard up.
  chatInput.focus();
  chatInput.readOnly = true;
  chatInput.classList.add('sending');
  sendBtn.disabled = true;
  sendBtn.classList.add('sending');
  sendBtn.classList.remove('loading');
  // Clean up staged file resources
  for (var j = 0; j < stagedFiles.length; j++) {
    if (stagedFiles[j].previewUrl) URL.revokeObjectURL(stagedFiles[j].previewUrl);
  }
  stagedFiles = [];
  renderStaging();
  freezeCurrentReplies(text);
  showLoading(); // hides quick replies via mutual exclusivity

  pendingNotifyParent = true;
  // Detect interrupt phrases for typed messages (exact match only)
  var lowerText = text.toLowerCase().replace(/[^a-z ]/g, '').trim();
  if (interruptPhrases.indexOf(lowerText) !== -1) {
    pendingInterrupt = true;
  }

  sendMessage(text, fileRefs.length > 0 ? fileRefs : undefined);
}

// Auto-grow textarea
function autoGrow() {
  chatInput.style.height = 'auto';
  chatInput.style.height = Math.min(chatInput.scrollHeight, 150) + 'px';
  chatInput.style.overflowY = chatInput.scrollHeight > 150 ? 'auto' : 'hidden';
}

chatInput.addEventListener('input', autoGrow);

// --- Autocomplete ---

var acDropdown = document.getElementById('autocomplete-dropdown');
// AUTOCOMPLETE_TRIGGERS is an array of trigger characters (e.g. ["@", "/"]).
var acTriggersArr = (typeof AUTOCOMPLETE_TRIGGERS !== 'undefined') ? AUTOCOMPLETE_TRIGGERS : [];
// Build a Set for O(1) lookup.
var acTriggers = {};
for (var _i = 0; _i < acTriggersArr.length; _i++) { acTriggers[acTriggersArr[_i]] = true; }
var acDebounceTimer = null;
var acActiveIndex = -1;   // currently highlighted option in dropdown
var acTriggerPos = -1;    // position of the trigger character in the textarea
var acTriggerChar = '';    // the trigger character that activated autocomplete
var acVisible = false;
// Cache: { type, query, results } — if new query starts with cached query,
// filter client-side instead of re-fetching.
var acCache = null;

// Find the nearest trigger character before the cursor, only if it's at position 0
// or preceded by whitespace.
function findTrigger(text, cursorPos) {
  for (var i = cursorPos - 1; i >= 0; i--) {
    var ch = text[i];
    if (ch === ' ' || ch === '\n' || ch === '\t') {
      return null; // hit whitespace before finding trigger
    }
    if (acTriggers[ch] !== undefined) {
      // Valid only at start of input or after whitespace
      if (i === 0 || /\s/.test(text[i - 1])) {
        return { char: ch, pos: i, query: text.substring(i + 1, cursorPos) };
      }
      continue;
    }
  }
  return null;
}

function acShowStatus(text) {
  acDropdown.innerHTML = '';
  var div = document.createElement('div');
  div.className = 'ac-status';
  div.textContent = text;
  acDropdown.appendChild(div);
  acActiveIndex = -1;
  acDropdown.classList.add('visible');
  acVisible = true;
}

function acShow(options, query) {
  acDropdown.innerHTML = '';
  if (options.length === 0) {
    acShowStatus('No results');
    return;
  }
  for (var i = 0; i < options.length; i++) {
    var opt = typeof options[i] === 'string' ? { v: options[i], h: '' } : options[i];
    var div = document.createElement('div');
    div.className = 'ac-option';
    div.dataset.index = i;
    div.dataset.value = opt.v;
    var hl = acHighlightCombined(opt.v, opt.h, query);
    div.innerHTML = hl.valueHtml;
    if (opt.h) {
      var hint = document.createElement('span');
      hint.className = 'ac-hint';
      hint.innerHTML = hl.hintHtml;
      div.appendChild(hint);
    }
    acDropdown.appendChild(div);
  }
  acActiveIndex = 0;
  acUpdateActive();
  acDropdown.classList.add('visible');
  acVisible = true;
}

function acHide() {
  acDropdown.classList.remove('visible');
  acDropdown.innerHTML = '';
  acVisible = false;
  acActiveIndex = -1;
  acTriggerPos = -1;
  acTriggerChar = '';
  acCache = null;
}

function acUpdateActive() {
  var items = acDropdown.querySelectorAll('.ac-option');
  for (var i = 0; i < items.length; i++) {
    items[i].classList.toggle('active', i === acActiveIndex);
  }
  // Scroll active item into view
  if (items[acActiveIndex]) {
    items[acActiveIndex].scrollIntoView({ block: 'nearest' });
  }
}

function acSelect(value) {
  // Replace from trigger position through current cursor with chosen value.
  // When replace_trigger is set by the provider, omit the trigger character
  // (e.g. emoji shortcodes: `:heart:` → `❤️` instead of `:❤️`).
  var before = chatInput.value.substring(0, acTriggerPos);
  var after = chatInput.value.substring(chatInput.selectionStart);
  var prefix = (acCache && acCache.replaceTrigger) ? '' : acTriggerChar;
  chatInput.value = before + prefix + value + after;
  var newPos = before.length + prefix.length + value.length;
  chatInput.setSelectionRange(newPos, newPos);
  acHide();
  autoGrow();
  chatInput.focus();
}

// Fuzzy highlight: highlight characters from query that appear in option, preferring
// closest-together matches. Simple greedy left-to-right match.
function acHighlight(option, query) {
  if (!query) return escapeHTML(option);
  var result = '';
  var qi = 0;
  var lowerOption = option.toLowerCase();
  var lowerQuery = query.toLowerCase();
  for (var i = 0; i < option.length; i++) {
    if (qi < lowerQuery.length && lowerOption[i] === lowerQuery[qi]) {
      result += '<span class="ac-highlight">' + escapeHTML(option[i]) + '</span>';
      qi++;
    } else {
      result += escapeHTML(option[i]);
    }
  }
  return result;
}

// Highlight matched chars in EITHER the value or the hint, never split.
// Mirrors acFuzzyMatch's strict in-value-OR-in-hint match rule. Prefer the
// value side when value alone satisfies the query so the user's eye lands
// on a strong match first; otherwise highlight in the hint. Both halves
// are HTML-escaped per character (XSS-safe).
function acHighlightCombined(value, hint, query) {
  if (!query) {
    return { valueHtml: escapeHTML(value), hintHtml: escapeHTML(hint || '') };
  }
  var lq = query.toLowerCase();
  if (acIsSubsequence(value.toLowerCase(), lq)) {
    return { valueHtml: acHighlight(value, query), hintHtml: escapeHTML(hint || '') };
  }
  if (hint && acIsSubsequence(hint.toLowerCase(), lq)) {
    return { valueHtml: escapeHTML(value), hintHtml: acHighlight(hint, query) };
  }
  // Defensive: filter step should have removed non-matching items already.
  return { valueHtml: escapeHTML(value), hintHtml: escapeHTML(hint || '') };
}

function escapeHTML(s) {
  var div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

// Normalize an autocomplete result to {v, h} format.
// Accepts either a plain string or an object with v and optional h.
function acNormalize(item) {
  if (typeof item === 'string') return { v: item, h: '' };
  return { v: item.v || '', h: item.h || '' };
}

// Normalize an array of results.
function acNormalizeAll(items) {
  return items.map(acNormalize);
}

// Check if option fuzzy-matches query (all query chars appear in order).
// Strict: the query must be a subsequence of the value, OR a subsequence of
// the hint — never split across both. Splitting causes confusing matches
// where you can't tell why a row was returned.
function acFuzzyMatch(option, query) {
  if (!query) return true;
  var v = typeof option === 'string' ? option : option.v;
  var h = (typeof option === 'string' ? '' : option.h) || '';
  var lq = query.toLowerCase();
  return acIsSubsequence(v.toLowerCase(), lq) ||
    (h !== '' && acIsSubsequence(h.toLowerCase(), lq));
}

// Returns true if every character of `query` appears in `text` in order,
// greedy left-to-right. Both inputs must already be lowercased.
function acIsSubsequence(text, query) {
  var qi = 0;
  for (var i = 0; i < text.length && qi < query.length; i++) {
    if (text[i] === query[qi]) qi++;
  }
  return qi === query.length;
}

// Mirrors server-side fuzzyMetrics in autocomplete.go. Returns
// {ok, longestRun, span} for a greedy left-to-right subsequence match
// of `query` in `s`. Both inputs assumed already lowercased.
function acFuzzyMetrics(s, query) {
  var qi = 0, first = -1, prevPos = -2, curRun = 0, longestRun = 0;
  for (var i = 0; i < s.length && qi < query.length; i++) {
    if (s.charCodeAt(i) === query.charCodeAt(qi)) {
      if (first < 0) first = i;
      curRun = (i === prevPos + 1) ? curRun + 1 : 1;
      if (curRun > longestRun) longestRun = curRun;
      prevPos = i;
      qi++;
    }
  }
  if (qi === query.length) {
    return { ok: true, longestRun: longestRun, span: prevPos - first + 1 };
  }
  return { ok: false, longestRun: 0, span: 0 };
}

// Mirrors server-side sortAutocomplete in autocomplete.go. Stable
// in-place sort by (tier desc, longestRun desc, span asc, length asc).
// Tiers:
//   5  value equals query
//   4  value has query as a prefix
//   3  value contains query as a contiguous substring
//   2  value fuzzy-matches (non-contiguous)
//   1  hint contains query as a contiguous substring
//   0  hint fuzzy-matches only
function acSortByQuery(items, query) {
  if (items.length < 2 || !query) return;
  var q = query.toLowerCase();
  var qLen = q.length;
  function score(it) {
    var v = (typeof it === 'string') ? it : (it.v || '');
    var h = (typeof it === 'string') ? '' : (it.h || '');
    var lv = v.toLowerCase();
    var length = v.length;
    if (lv === q)              return [5, qLen, qLen, length];
    if (lv.indexOf(q) === 0)   return [4, qLen, qLen, length];
    if (lv.indexOf(q) >= 0)    return [3, qLen, qLen, length];
    var mv = acFuzzyMetrics(lv, q);
    if (mv.ok)                 return [2, mv.longestRun, mv.span, length];
    if (h === '')              return [0, 0, 0, length];
    var lh = h.toLowerCase();
    if (lh.indexOf(q) >= 0)    return [1, qLen, qLen, length];
    var mh = acFuzzyMetrics(lh, q);
    if (mh.ok)                 return [0, mh.longestRun, mh.span, length];
    return [0, 0, 0, length];
  }
  // Decorate-sort-undecorate so score() runs O(n) not O(n log n).
  var decorated = items.map(function(it, i) { return { it: it, s: score(it), i: i }; });
  decorated.sort(function(a, b) {
    if (a.s[0] !== b.s[0]) return b.s[0] - a.s[0]; // tier desc
    if (a.s[1] !== b.s[1]) return b.s[1] - a.s[1]; // longestRun desc
    if (a.s[2] !== b.s[2]) return a.s[2] - b.s[2]; // span asc
    if (a.s[3] !== b.s[3]) return a.s[3] - b.s[3]; // length asc
    return a.i - b.i;                              // stable
  });
  for (var i = 0; i < decorated.length; i++) items[i] = decorated[i].it;
}

function acFetch(trigger, query) {
  // Check cache: if query extends the cached query, filter client-side.
  // Skip cache if the cached results were empty, if the server indicated
  // there are more results beyond the returned set (has_more), or if the
  // cached query is empty. An empty-query cache is returned in
  // provider/discovery order (server-side sortAutocomplete only runs for
  // non-empty queries), so filtering it client-side would preserve that
  // unranked order. Forcing a real fetch on the first keystroke after the
  // trigger seeds the cache with a properly-ranked result set.
  if (acCache && acCache.query !== '' && acCache.trigger === trigger && acCache.results.length > 0 && !acCache.hasMore && query.indexOf(acCache.query) === 0) {
    var filtered = acCache.results.filter(function(opt) {
      return acFuzzyMatch(opt, query);
    });
    // Re-rank for the extended query. Even though the cache was seeded
    // in ranked order for acCache.query, that order was computed against
    // a shorter query — extending it can change tier and tiebreakers.
    acSortByQuery(filtered, query);
    if (acTriggerPos >= 0) {
      acShow(filtered, query);
    }
    return;
  }

  acShowStatus('Loading\u2026');

  fetch('autocomplete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ trigger: trigger, query: query })
  }).then(function(r) {
      if (!r.ok) return r.text().then(function(t) { throw new Error(t || ('HTTP ' + r.status)); });
      return r.json();
    })
    .then(function(data) {
      // Support structured {results, info} or plain array.
      var raw = Array.isArray(data) ? data : (data.results || []);
      var options = acNormalizeAll(raw);
      var info = (!Array.isArray(data) && data.info) ? data.info : '';
      var hasMore = (!Array.isArray(data) && data.has_more) ? true : false;
      var replaceTrigger = (!Array.isArray(data) && data.replace_trigger) ? true : false;
      // Cache the full result set for client-side filtering.
      acCache = { trigger: trigger, query: query, results: options, hasMore: hasMore, replaceTrigger: replaceTrigger };
      // Only show if we're still in the same trigger context
      if (acVisible || acTriggerPos >= 0) {
        if (options.length === 0 && info) {
          acShowStatus(info);
        } else {
          acShow(options, query);
        }
      }
    })
    .catch(function(err) {
      if (acVisible || acTriggerPos >= 0) {
        acShowStatus('Error: ' + (err.message || 'failed to load'));
      }
    });
}

chatInput.addEventListener('input', function () {
  if (Object.keys(acTriggers).length === 0) return;

  var cursorPos = chatInput.selectionStart;
  var text = chatInput.value;
  var trigger = findTrigger(text, cursorPos);

  if (!trigger) {
    acHide();
    return;
  }

  acTriggerPos = trigger.pos;
  acTriggerChar = trigger.char;

  // No debounce needed for cache hits (client-side filtering is instant).
  // Must mirror the guard in acFetch — specifically, an empty cached query
  // is not a cache hit, because the empty-query response is unranked.
  var triggerCh = trigger.char;
  if (acCache && acCache.query !== '' && acCache.trigger === triggerCh && acCache.results.length > 0 && !acCache.hasMore && trigger.query.indexOf(acCache.query) === 0) {
    acFetch(triggerCh, trigger.query);
    return;
  }

  acShowStatus('Loading\u2026');
  clearTimeout(acDebounceTimer);
  acDebounceTimer = setTimeout(function () {
    acFetch(triggerCh, trigger.query);
  }, 200);
});

// Click on dropdown option
acDropdown.addEventListener('mousedown', function (e) {
  e.preventDefault(); // prevent blur
  var option = e.target.closest('.ac-option');
  if (option) {
    acSelect(option.dataset.value);
  }
});

// Desktop: Enter sends, Shift+Enter inserts newline
// Mobile/touch: Enter inserts newline, send button only
var isMobile = /Mobi|Android|iPhone|iPad|iPod/i.test(navigator.userAgent) || ('ontouchstart' in window && window.innerWidth < 768);

chatInput.addEventListener('keydown', function (e) {
  // Autocomplete keyboard navigation
  if (acVisible) {
    var items = acDropdown.querySelectorAll('.ac-option');
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      acActiveIndex = (acActiveIndex + 1) % items.length;
      acUpdateActive();
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      acActiveIndex = (acActiveIndex - 1 + items.length) % items.length;
      acUpdateActive();
      return;
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault();
      if (items[acActiveIndex]) {
        acSelect(items[acActiveIndex].dataset.value);
      }
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      acHide();
      return;
    }
  }

  if (e.key !== 'Enter') return;
  if (isMobile) return; // on mobile, Enter always inserts newline (default behavior)
  if (e.shiftKey || e.altKey) return; // modifier+Enter inserts newline on desktop
  e.preventDefault();
  handleSend();
});

sendBtn.addEventListener('click', handleSend);

// Quick-reply chips
quickReplies.addEventListener('click', function (e) {
  var chip = e.target.closest('.chip');
  if (!chip || chip.disabled) return;

  var message = chip.dataset.message || '';
  if (!message) return;
  // Don't display bubble — wait for server broadcast (same as handleSend).
  pendingNotifyParent = true;
  freezeCurrentReplies(message);
  sendMessage(message);
  showLoading(); // hides quick replies via mutual exclusivity
});

// --- Voice mode ---

function playBeep(freq, duration, onDone) {
  var ctx = new (window.AudioContext || window.webkitAudioContext)();
  var osc = ctx.createOscillator();
  var gain = ctx.createGain();
  osc.connect(gain);
  gain.connect(ctx.destination);
  osc.frequency.value = freq;
  gain.gain.value = 0.15;
  osc.onended = onDone;
  osc.start();
  osc.stop(ctx.currentTime + duration);
}

function populateVoices() {
  var voices = speechSynthesis.getVoices();
  voiceSelect.innerHTML = '';
  var savedName = localStorage.getItem('tts-voice');
  var savedIndex = -1;
  for (var i = 0; i < voices.length; i++) {
    var opt = document.createElement('option');
    opt.value = i;
    opt.textContent = voices[i].name + ' (' + voices[i].lang + ')';
    voiceSelect.appendChild(opt);
    if (savedName && voices[i].name === savedName) {
      savedIndex = i;
    }
  }
  if (savedIndex >= 0) {
    voiceSelect.value = savedIndex;
    selectedVoice = voices[savedIndex];
  } else if (voices.length > 0 && !selectedVoice) {
    selectedVoice = voices[0];
  }
}

if (typeof speechSynthesis !== 'undefined') {
  speechSynthesis.onvoiceschanged = populateVoices;
  populateVoices();
}

voiceSelect.addEventListener('change', function() {
  var voices = speechSynthesis.getVoices();
  var idx = parseInt(voiceSelect.value);
  if (voices[idx]) {
    selectedVoice = voices[idx];
    localStorage.setItem('tts-voice', selectedVoice.name);
    // Preview the selected voice so the user can hear it
    speechSynthesis.cancel();
    var preview = new SpeechSynthesisUtterance(selectedVoice.name + ', OK');
    preview.voice = selectedVoice;
    preview.rate = 1.0;
    speechSynthesis.speak(preview);
  }
});

// Split text into sentence-sized chunks for TTS.
// Keeps each chunk short enough to avoid the iOS/WebKit ~15-second truncation bug.
function splitIntoChunks(text) {
  // Split on sentence boundaries: period, exclamation, question mark followed by space or end.
  // Also split on semicolons. Periods after digits (e.g. "1.") are not sentence boundaries.
  // First, protect numbered-list periods by replacing "N." with a placeholder.
  var safeText = text.replace(/(\d)\./g, '$1\u2024');
  var sentences = safeText.match(/[^.!?;]+[.!?;]+[\s]?|[^.!?;]+$/g);
  if (sentences) {
    for (var k = 0; k < sentences.length; k++) {
      sentences[k] = sentences[k].replace(/\u2024/g, '.');
    }
  }
  if (!sentences) return [text];

  // Merge very short sentences together; split very long ones.
  var chunks = [];
  var current = '';
  var MAX_CHARS = 200; // ~10-12 seconds of speech at normal rate

  for (var i = 0; i < sentences.length; i++) {
    var s = sentences[i].trim();
    if (!s) continue;

    if (current.length + s.length + 1 <= MAX_CHARS) {
      current = current ? current + ' ' + s : s;
    } else {
      if (current) chunks.push(current);
      // If a single sentence exceeds MAX_CHARS, split on commas
      if (s.length > MAX_CHARS) {
        var parts = s.match(/[^,]+,?\s?/g) || [s];
        var sub = '';
        for (var j = 0; j < parts.length; j++) {
          var p = parts[j].trim();
          if (sub.length + p.length + 1 <= MAX_CHARS) {
            sub = sub ? sub + ' ' + p : p;
          } else {
            if (sub) chunks.push(sub);
            sub = p;
          }
        }
        if (sub) chunks.push(sub);
        current = '';
      } else {
        current = s;
      }
    }
  }
  if (current) chunks.push(current);
  return chunks.length > 0 ? chunks : [text];
}

function speakText(text, onDone) {
  if (typeof speechSynthesis === 'undefined') {
    addSystemBubble('TTS not supported in this browser');
    if (onDone) onDone();
    return;
  }
  var voices = speechSynthesis.getVoices();
  if (voices.length === 0) {
    addSystemBubble('TTS: no voices available — speech output disabled');
    if (onDone) onDone();
    return;
  }
  // Cancel any stuck/pending utterances before speaking new one.
  // Safari/WebKit bug: cancel() immediately followed by speak() causes the
  // new utterance to be silently dropped (no onend/onerror fires). We must
  // delay speak() to let the synthesis engine settle after cancel().
  speechSynthesis.cancel();
  if (ttsSafetyTimer) { clearTimeout(ttsSafetyTimer); ttsSafetyTimer = null; }
  isSpeaking = true;
  btnVoice.classList.add('speaking');
  addSystemBubble('Speaking...');
  var ttsStart = Date.now();
  var done = false;
  function finish(reason) {
    if (done) return;
    done = true;
    isSpeaking = false;
    btnVoice.classList.remove('speaking');
    if (ttsSafetyTimer) { clearTimeout(ttsSafetyTimer); ttsSafetyTimer = null; }
    console.log('[' + ts() + '] TTS finished (' + reason + ') after ' + (Date.now() - ttsStart) + 'ms');
    if (onDone) onDone();
  }

  var chunks = splitIntoChunks(text);
  console.log('[' + ts() + '] TTS splitting into ' + chunks.length + ' chunks');

  function speakChunk(index) {
    if (done) return;
    if (index >= chunks.length) {
      finish('all-chunks-done');
      return;
    }
    var chunk = chunks[index];
    var utterance = new SpeechSynthesisUtterance(chunk);
    if (selectedVoice) utterance.voice = selectedVoice;
    utterance.rate = 1.0;
    utterance.onend = function() {
      if (index === 0) {
        var elapsed = Date.now() - ttsStart;
        if (elapsed < 500 && text.length > 20 && /iP(hone|ad|od)/.test(navigator.userAgent)) {
          addSystemBubble('TTS may be muted — check your device silent/mute switch');
        }
      }
      if (ttsSafetyTimer) { clearTimeout(ttsSafetyTimer); ttsSafetyTimer = null; }
      speakChunk(index + 1);
    };
    utterance.onerror = function(e) {
      console.error('[' + ts() + '] TTS onerror on chunk ' + index + ':', e.error);
      addSystemBubble('TTS error: ' + (e.error || 'unknown'));
      finish('onerror: ' + (e.error || 'unknown'));
    };
    speechSynthesis.speak(utterance);
    console.log('[' + ts() + '] TTS speak() chunk ' + (index + 1) + '/' + chunks.length + ', length=' + chunk.length + ', voice=' + (utterance.voice ? utterance.voice.name : 'default'));
    // Safety timeout proportional to chunk length (~100ms per char, min 15s).
    // Previous fixed 15s was too short for longer chunks on slower iOS voices.
    var chunkTimeout = Math.max(15000, chunk.length * 100);
    ttsSafetyTimer = setTimeout(function() {
      if (!done) {
        console.warn('[' + ts() + '] TTS safety timeout on chunk ' + index + ' (after ' + chunkTimeout + 'ms) — speak() may have failed silently');
        addSystemBubble('TTS timed out — future replies will have a play button');
        ttsUnlocked = false;
        speechSynthesis.cancel();
        finish('safety-timeout');
      }
    }, chunkTimeout);
  }

  // Delay speak() after cancel() to work around Safari WebKit bug
  setTimeout(function() { speakChunk(0); }, 100);
}

function addSystemBubble(text) {
  var MAX_VISIBLE = 2;

  // Add the new bubble first
  addBubble('[system] ' + text, 'system');

  // Walk backwards to find the trailing run of system bubbles + counter
  var children = messages.children;
  var runBubbles = [];
  var counterEl = null;
  for (var i = children.length - 1; i >= 0; i--) {
    var ch = children[i];
    if (ch.id === 'loading-bubble' || ch === quickReplies) continue;
    if (ch.classList.contains('system-collapse-counter')) {
      counterEl = ch;
      continue;
    }
    if (ch.classList.contains('bubble') && ch.classList.contains('system')) {
      runBubbles.unshift(ch);
    } else {
      break;
    }
  }

  if (runBubbles.length <= MAX_VISIBLE) return;

  // Remove all but the last MAX_VISIBLE
  var toRemove = runBubbles.length - MAX_VISIBLE;
  for (var k = 0; k < toRemove; k++) {
    runBubbles[k].parentNode.removeChild(runBubbles[k]);
  }

  // Create or update counter
  var prevCount = counterEl ? parseInt(counterEl.dataset.count, 10) || 0 : 0;
  var totalHidden = prevCount + toRemove;
  if (!counterEl) {
    counterEl = document.createElement('div');
    counterEl.className = 'system-collapse-counter';
  }
  counterEl.dataset.count = totalHidden;
  counterEl.textContent = '\u00b7\u00b7\u00b7 ' + totalHidden + ' more \u00b7\u00b7\u00b7';
  // Ensure counter is placed right before the first visible system bubble
  var firstVisible = runBubbles[toRemove];
  if (counterEl.nextSibling !== firstVisible) {
    messages.insertBefore(counterEl, firstVisible);
  }
}

function pulseLastTtsButton(onDone) {
  // Find the last agent bubble's TTS button and pulse it to attract attention
  var bubbles = messages.querySelectorAll('.bubble.agent');
  if (bubbles.length === 0) { if (onDone) onDone(); return; }
  var last = bubbles[bubbles.length - 1];
  var btn = last.querySelector('.bubble-tts-btn');
  if (!btn) { if (onDone) onDone(); return; }
  // Temporarily boost opacity to draw attention
  btn.style.opacity = '1';
  btn.style.color = '#7c3aed';
  setTimeout(function() {
    btn.style.opacity = '';
    btn.style.color = '';
  }, 2000);
  // Attach one-time done callback when user taps it
  if (onDone) {
    var wrapper = function() {
      if (onDone) { onDone(); onDone = null; }
      btn.removeEventListener('click', wrapper);
    };
    btn.addEventListener('click', wrapper);
  }
}

function setupSpeechRecognition() {
  // Use webkitSpeechRecognition directly — matches working reference implementation
  if (!('webkitSpeechRecognition' in window)) {
    addSystemBubble('SpeechRecognition not supported in this browser');
    return;
  }
  voiceRecognition = new webkitSpeechRecognition();
  voiceRecognition.continuous = true;
  voiceRecognition.interimResults = true;
  voiceRecognition.lang = 'en-US';

  voiceRecognition.onstart = function() {
    isListening = true;
    micRetryCount = 0; // reset backoff on successful start
    btnVoice.classList.add('active');
    addSystemBubble('Listening...');
  };

  voiceRecognition.onaudiostart = function() {
    console.log('[' + ts() + '] Voice: audio capture started');
  };

  voiceRecognition.onspeechstart = function() {
    console.log('[' + ts() + '] Voice: speech detected');
    btnVoice.classList.add('hearing');
  };

  voiceRecognition.onspeechend = function() {
    console.log('[' + ts() + '] Voice: speech ended');
    btnVoice.classList.remove('hearing');
  };

  voiceRecognition.onresult = function(e) {
    // Build full transcript from all results (continuous mode accumulates)
    var finalTranscript = '';
    var interimTranscript = '';
    for (var i = 0; i < e.results.length; i++) {
      if (e.results[i].isFinal) {
        finalTranscript += e.results[i][0].transcript;
      } else {
        interimTranscript += e.results[i][0].transcript;
      }
    }

    // Only act on final results
    if (!finalTranscript) return;

    // Stop recognition so we don't keep accumulating while processing
    stopListening();

    var text = finalTranscript.trim();
    if (!text) return;

    // "stop stop stop" detection
    if (text.toLowerCase().replace(/[^a-z ]/g, '').trim() === 'stop stop stop') {
      disableVoiceMode();
      return;
    }

    // Detect interrupt phrases (stop, wait, cancel, hold on, etc.)
    var lowerText = text.toLowerCase().replace(/[^a-z ]/g, '').trim();
    var isInterrupt = interruptPhrases.some(function(phrase) {
      return lowerText === phrase || lowerText.indexOf(phrase + ' ') === 0;
    });

    // Don't display bubble yet — wait for server broadcast.
    pendingNotifyParent = true;
    if (isInterrupt) pendingInterrupt = true;
    sendMessage('\ud83c\udfa4 ' + text);
    showLoading();
  };

  voiceRecognition.onerror = function(e) {
    console.error('[' + ts() + '] Voice recognition error:', e.error);
    isListening = false;

    // Non-retryable errors — give up and disable voice mode
    var fatal = ['not-allowed', 'service-not-allowed', 'language-not-supported'];
    if (fatal.indexOf(e.error) !== -1) {
      addSystemBubble('Voice error: ' + e.error + ' (cannot retry)');
      disableVoiceMode();
      return;
    }

    // Retryable errors (no-speech, audio-capture, network, aborted, etc.)
    // Suppress "aborted" when we intentionally stopped recognition (e.g., for TTS playback)
    if (e.error === 'aborted' && intentionalStop) {
      intentionalStop = false;
      return;
    }
    intentionalStop = false;
    addSystemBubble('Voice error: ' + e.error);
    retryMic();
  };

  voiceRecognition.onend = function() {
    isListening = false;
    intentionalStop = false; // reset flag
    // Auto-restart if still in voice mode
    if (voiceMode && !isSpeaking) {
      retryMic();
    }
  };
}

function retryMic() {
  if (!voiceMode) return;
  if (isSpeaking) return; // don't restart mic during TTS playback
  if (micRetryTimer) return; // a retry is already scheduled
  if (micRetryCount >= micRetryMax) {
    addSystemBubble('Mic failed after ' + micRetryMax + ' retries — disabling voice mode');
    disableVoiceMode();
    return;
  }
  var delay = micRetryBaseDelay * Math.pow(2, micRetryCount);
  micRetryCount++;
  console.log('[' + ts() + '] Mic retry ' + micRetryCount + '/' + micRetryMax + ' in ' + delay + 'ms');
  // Recreate recognition instance to clear any bad state
  if (voiceRecognition) {
    try { voiceRecognition.abort(); } catch(e) {}
    voiceRecognition = null;
  }
  micRetryTimer = setTimeout(function() {
    micRetryTimer = null;
    if (!voiceMode || isSpeaking) return;
    startListening();
  }, delay);
}

function startListening() {
  if (isListening) return; // already listening, avoid "already started" error
  if (isSpeaking) return; // don't start mic during TTS
  // Always recreate recognition instance — reusing a stopped instance can
  // silently fail on mobile browsers (especially iOS Safari/Chrome).
  if (voiceRecognition) {
    try { voiceRecognition.abort(); } catch(e) {}
    voiceRecognition = null;
  }
  setupSpeechRecognition();
  if (!voiceRecognition) return;
  try {
    isListening = true;
    voiceRecognition.start();
  } catch(e) {
    console.error('[' + ts() + '] Failed to start recognition:', e);
    addSystemBubble('Failed to start mic: ' + e.message);
    isListening = false;
    retryMic();
  }
}

var intentionalStop = false; // suppress "aborted" error when we stop recognition for TTS

function stopListening() {
  if (micRetryTimer) { clearTimeout(micRetryTimer); micRetryTimer = null; }
  if (!voiceRecognition) return;
  intentionalStop = true;
  // Use abort() + destroy instead of stop() to immediately release the audio
  // session. On iOS, stop() keeps the input session alive until onend fires,
  // which blocks SpeechSynthesis from outputting audio.
  try { voiceRecognition.abort(); } catch(e) {}
  voiceRecognition = null;
  isListening = false;
  btnVoice.classList.remove('hearing');
}

function enableVoiceMode() {
  // Warm up speechSynthesis SYNCHRONOUSLY in the click handler, BEFORE the
  // async getUserMedia call. Some browsers lose user activation after the mic
  // permission dialog, causing speak() inside .then() to be silently blocked.
  // Doing it here guarantees we're in the direct user-gesture chain.
  if (typeof speechSynthesis !== 'undefined') {
    speechSynthesis.cancel(); // clear any stuck queue
    var warmup = new SpeechSynthesisUtterance('Ready');
    warmup.volume = 1.0;
    if (selectedVoice) warmup.voice = selectedVoice;
    warmup.onend = function() {
      console.log('[' + ts() + '] TTS warmup completed');
      ttsUnlocked = true;
    };
    warmup.onerror = function(e) {
      console.error('[' + ts() + '] TTS warmup error:', e.error);
    };
    speechSynthesis.speak(warmup);
    console.log('[' + ts() + '] TTS warmup speak() called (pre-getUserMedia)');
    // If warmup doesn't complete within 3s, TTS is likely blocked (iframe/iOS restriction)
    setTimeout(function() {
      if (!ttsUnlocked) {
        console.warn('[' + ts() + '] TTS warmup did not complete — TTS may be blocked');
        addSystemBubble('TTS may not work — replies will have a play button');
      }
    }, 3000);
  }

  // Request mic permission explicitly (getUserMedia triggers browser prompt).
  // This is required — without it, SpeechRecognition silently fails.
  navigator.mediaDevices.getUserMedia({ audio: true }).then(function(stream) {
    // Stop the stream — we just needed the permission grant.
    // SpeechRecognition manages its own audio capture.
    stream.getTracks().forEach(function(t) { t.stop(); });
    voiceMode = true;
    micRetryCount = 0;
    btnVoice.classList.add('active');
    voiceControls.classList.add('visible');
    // If TTS warmup already succeeded (from the synchronous call above), skip.
    // Otherwise try again inside .then() as a fallback (works when Promise.then
    // preserves user activation, e.g. iOS Safari).
    if (typeof speechSynthesis !== 'undefined' && !ttsUnlocked) {
      speechSynthesis.cancel();
      var fallbackWarmup = new SpeechSynthesisUtterance('Ready');
      fallbackWarmup.volume = 1.0;
      if (selectedVoice) fallbackWarmup.voice = selectedVoice;
      fallbackWarmup.onend = function() {
        console.log('[' + ts() + '] TTS fallback warmup completed');
        ttsUnlocked = true;
      };
      fallbackWarmup.onerror = function(e) {
        console.error('[' + ts() + '] TTS fallback warmup error:', e.error);
        addSystemBubble('TTS warmup failed: ' + (e.error || 'unknown'));
      };
      speechSynthesis.speak(fallbackWarmup);
      console.log('[' + ts() + '] TTS fallback warmup speak() called (post-getUserMedia)');
    }
    // Warn if TTS voices are unavailable
    if (typeof speechSynthesis !== 'undefined' && speechSynthesis.getVoices().length === 0) {
      addSystemBubble('Warning: no TTS voices found — agent replies will not be spoken aloud');
    }
    // Re-create recognition instance each time voice mode is enabled
    setupSpeechRecognition();
    setTimeout(startListening, 300);
  }).catch(function(err) {
    console.error('Microphone permission denied:', err);
    addSystemBubble('Mic permission denied: ' + err.message);
  });
}

function disableVoiceMode() {
  voiceMode = false;
  btnVoice.classList.remove('active', 'speaking');
  voiceControls.classList.remove('visible');
  if (micRetryTimer) { clearTimeout(micRetryTimer); micRetryTimer = null; }
  if (ttsSafetyTimer) { clearTimeout(ttsSafetyTimer); ttsSafetyTimer = null; }
  stopListening();
  if (voiceRecognition) {
    try { voiceRecognition.abort(); } catch(e) {}
    voiceRecognition = null;
  }
  speechSynthesis.cancel();
  isSpeaking = false;
  isListening = false;
  ttsUnlocked = false;
  ttsQueue = [];
}

btnVoice.addEventListener('click', function() {
  if (isSpeaking) {
    // Interrupt TTS — cancel speech, clear queue, go back to listening
    speechSynthesis.cancel();
    isSpeaking = false;
    btnVoice.classList.remove('speaking');
    if (ttsSafetyTimer) { clearTimeout(ttsSafetyTimer); ttsSafetyTimer = null; }
    ttsQueue = [];
    micRetryCount = 0;
    setTimeout(startListening, 200);
    return;
  }
  if (voiceMode) {
    disableVoiceMode();
  } else {
    enableVoiceMode();
  }
});

// --- TTS queue: speak verbal replies sequentially without interrupting ---

function speakVerbalReply(text, quickReplies) {
  // Stop mic before TTS to prevent conflict and runaway restart loop
  stopListening();
  var hasQuickReplies = quickReplies && quickReplies.length > 0;
  var onDone = function() {
    // Drain queue: if more replies arrived while speaking, play the next one
    if (ttsQueue.length > 0) {
      var next = ttsQueue.shift();
      console.log('[' + ts() + '] TTS queue: playing next (' + ttsQueue.length + ' remaining)');
      speakVerbalReply(next.text, next.quickReplies);
      return;
    }
    if (hasQuickReplies) enableInput(quickReplies);
    // Resume listening if voice mode is on
    if (voiceMode) {
      micRetryCount = 0;
      setTimeout(startListening, 200);
    }
  };
  // For progress messages (no quick replies), append "Be right back" so the
  // user knows the agent is still working. For replies, append "Let me know."
  var spokenText = (!hasQuickReplies && text) ? text + ' Be right back.' : (hasQuickReplies && text) ? text + ' Let me know.' : text;
  if (ttsUnlocked) {
    // TTS warmup succeeded — auto-play
    speakText(spokenText, onDone);
  } else {
    // TTS warmup did not succeed (likely iframe restriction on iOS).
    // Pulse the bubble's play button so user gesture unlocks TTS.
    pulseLastTtsButton(onDone);
  }
}

// --- Connection status (no-op, status conveyed via input disabled state) ---

function setStatus(state) {}

// --- WebSocket connection with exponential backoff ---

var BACKOFF_INITIAL = 1000;
var BACKOFF_MAX = 30000;
var backoffDelay = BACKOFF_INITIAL;
var reconnectTimer = null;
var hasConnectedBefore = false;
var connectQuickReplies = null; // deferred until historyEnd

// --- History replay for browser reconnect ---

function replayHistory(history) {
  console.log('[' + ts() + '] Replaying ' + history.length + ' history events');
  clearMessages();

  var pendingReplies = null; // quick_replies from the most recent agent/verbal/draw event

  for (var i = 0; i < history.length; i++) {
    var event = history[i];
    switch (event.type) {
      case 'agentMessage':
        if (event.text || (event.files && event.files.length > 0)) {
          addBubble(event.text, 'agent', event.files, null, event.ts);
        }
        pendingReplies = (event.quick_replies && event.quick_replies.length > 0) ? event.quick_replies : null;
        break;
      case 'userMessage':
        // Freeze unchosen quick replies from the preceding agent message
        if (pendingReplies) {
          var chosenText = event.text || '';
          var remaining = [];
          for (var j = 0; j < pendingReplies.length; j++) {
            if (pendingReplies[j] !== chosenText) {
              remaining.push(pendingReplies[j]);
            }
          }
          if (remaining.length > 0) {
            appendFrozenReplies(remaining);
          }
          pendingReplies = null;
        }
        if (event.text || (event.files && event.files.length > 0)) {
          var isVoiceMsg = event.text && event.text.indexOf('\ud83c\udfa4') === 0;
          var displayText = isVoiceMsg ? event.text.replace('\ud83c\udfa4 ', '') : event.text;
          addBubble(displayText, 'user', event.files, isVoiceMsg ? 'voice' : null, event.ts);
        }
        break;
      case 'draw':
        if (event.instructions) {
          addCanvasBubble(event.instructions, true, null);
        }
        pendingReplies = (event.quick_replies && event.quick_replies.length > 0) ? event.quick_replies : null;
        break;
      case 'verbalReply':
        if (event.text || (event.files && event.files.length > 0)) {
          var hasReplies = event.quick_replies && event.quick_replies.length > 0;
          addBubble(event.text, 'agent', event.files, hasReplies ? 'voice lmk' : 'voice brb', event.ts);
        }
        pendingReplies = (event.quick_replies && event.quick_replies.length > 0) ? event.quick_replies : null;
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
  var wsUrl = proto + '//' + location.host + basePath + '/ws?cursor=' + lastSeq;
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

    // Track cursor for reconnect — events carry a seq number.
    if (data.seq) {
      lastSeq = data.seq;
    }

    switch (data.type) {
      case 'connected':
        console.log('[' + ts() + '] Connected event received');
        setStatus('connected');
        var label = hasConnectedBefore ? 'Reconnected' : 'Connected';
        if (!hasConnectedBefore) {
          label += ' \u00b7 [agent-chat](https://github.com/choonkeat/agent-chat)';
        }
        if (data.version) {
          var pageVersion = typeof SERVER_VERSION !== 'undefined' ? SERVER_VERSION : '';
          if (pageVersion && pageVersion !== data.version) {
            label += ' \u00b7 server v' + data.version + ' \u00b7 page v' + pageVersion;
          } else {
            label += ' \u00b7 v' + data.version;
          }
        }
        addSystemBubble(label);
        hasConnectedBefore = true;
        // History is now streamed as individual events after connect — no replay needed.
        if (data.pendingAckId) {
          pendingAckId = data.pendingAckId;
        }
        // Defer quick replies until historyEnd — showing them now would
        // cause freezeCurrentReplies to freeze the wrong replies when
        // history events stream in.
        connectQuickReplies = data.quickReplies || null;
        enableInput();
        break;

      case 'historyEnd':
        // History replay complete — show deferred quick replies if the
        // event stream didn't already set them (e.g. reconnect with no
        // missed events, or last event was an agentMessage with replies).
        if (connectQuickReplies && connectQuickReplies.length > 0) {
          var existing = quickReplies.querySelectorAll('.chip');
          if (existing.length === 0) {
            enableInput(connectQuickReplies);
          }
        }
        connectQuickReplies = null;
        break;

      case 'agentMessage':
        console.log('[' + ts() + '] Agent message received: "' + data.text + '"');
        addAgentMessage(data.text || '', data.files, null, data.ts);
        // With quick_replies: agent is waiting for input — show replies, hide loading
        // Without quick_replies: progress update — loading stays visible
        if (data.quick_replies && data.quick_replies.length > 0) {
          enableInput(data.quick_replies);
        }
        break;

      case 'draw':
        console.log('[' + ts() + '] Draw event received (' + (data.instructions || []).length + ' instructions)');

        // Store ack_id so quick-reply/send resolves the draw ack
        if (data.ack_id) {
          pendingAckId = data.ack_id;
        }

        addCanvasBubble(data.instructions || [], false, function () {
          enableInput(data.quick_replies); // removes loading via mutual exclusivity
        });
        break;

      case 'verbalReply':
        console.log('[' + ts() + '] Verbal reply received: "' + data.text + '", ttsUnlocked=' + ttsUnlocked + ', isSpeaking=' + isSpeaking);
        var isProgress = !(data.quick_replies && data.quick_replies.length > 0);
        addAgentMessage(data.text || '', data.files, isProgress ? 'voice brb' : 'voice lmk', data.ts);
        if (isSpeaking) {
          console.log('[' + ts() + '] TTS busy — queuing reply');
          ttsQueue.push({ text: data.text || '', quickReplies: data.quick_replies });
        } else {
          speakVerbalReply(data.text || '', data.quick_replies);
        }
        break;

      case 'userMessage':
        // Server broadcast of a user message — display the bubble now.
        // Freeze any active quick replies (unchosen ones stay in log).
        freezeCurrentReplies(data.text);
        // Reset scroll flag before addBubble so scrollToBottom succeeds.
        isUserScrolledUp = false;
        if (data.text || (data.files && data.files.length > 0)) {
          var isVoiceMsg = data.text && data.text.indexOf('\ud83c\udfa4') === 0;
          var displayText = isVoiceMsg ? data.text.replace('\ud83c\udfa4 ', '') : data.text;
          addBubble(displayText, 'user', data.files, isVoiceMsg ? 'voice' : null, data.ts);
        }
        // Re-enable input and clear the text now that the message is confirmed
        chatInput.value = '';
        chatInput.style.height = 'auto';
        chatInput.readOnly = false;
        chatInput.classList.remove('sending');
        sendBtn.disabled = false;
        sendBtn.classList.remove('sending');
        updateSendButton(); // re-disable if uploads still pending
        // Show loading — agent is now processing the user's message.
        // Also ensures correct state after replay for new/reconnecting browsers.
        showLoading();
        scrollToBottom(true);
        break;

      case 'messageQueued':
        // Server confirmed the message is in the queue — now safe to
        // tell the parent frame so it can trigger check_messages.
        if (pendingNotifyParent) {
          var nudgeText = pendingInterrupt
            ? 'check_messages; ask me how to proceed'
            : 'check_messages; reply me with a send_message';
          if (window.parent !== window) {
            if (pendingInterrupt) {
              // Voice interrupt: send Esc-Esc to abort current tool, then
              // nudge agent to check_messages so it sees the stop request.
              window.parent.postMessage({ type: 'agent-chat-interrupt', text: nudgeText }, '*');
            } else {
              var msg = { type: 'agent-chat-first-user-message' };
              // First message includes hint text for the parent to type into the terminal.
              if (!firstMessageSent) {
                msg.text = nudgeText;
                firstMessageSent = true;
              }
              window.parent.postMessage(msg, '*');
            }
          } else if (!warningShown) {
            // Parent not connected — show warning bubble with instructions (once).
            warningShown = true;
            addBubble('Type `check_messages` in your agent terminal to connect this chat. You only need to do this once.', 'agent', null, 'warning');
          }
          pendingInterrupt = false;
          pendingNotifyParent = false;
        }
        break;
    }
  };

  ws.onclose = function () {
    if (ws !== activeWs) return;
    console.log('[' + ts() + '] WebSocket closed, reconnecting...');
    addSystemBubble('Disconnected');
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
  var children = messages.children;
  var items = [];

  for (var i = 0; i < children.length; i++) {
    var b = children[i];
    if (b.id === 'loading-bubble') continue;

    // Elapsed time labels
    if (b.classList.contains('elapsed-time')) {
      items.push('<div class="elapsed-time">' + b.textContent + '</div>');
      continue;
    }

    // Frozen quick-reply chips
    if (b.classList.contains('frozen-replies')) {
      items.push('<div class="frozen-replies">' + b.innerHTML + '</div>');
      continue;
    }

    if (!b.classList.contains('bubble')) continue;

    if (b.classList.contains('canvas-bubble')) {
      var img = b.querySelector('img');
      if (img) {
        items.push('<div class="bubble agent canvas-bubble"><img src="' + img.src + '" style="width:100%;height:auto;display:block;border-radius:8px;"></div>');
      }
    } else {
      var role = b.classList.contains('user') ? 'user' : b.classList.contains('system') ? 'system' : 'agent';
      var voice = b.classList.contains('voice') ? ' voice' : '';
      items.push('<div class="bubble ' + role + voice + '">' + b.innerHTML + '</div>');
    }
  }

  var html = '<!DOCTYPE html>\n<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Chat Export</title><style>'
    + 'body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#1a1a2e;color:#e0e0e0;margin:0;padding:2rem;display:flex;justify-content:center;}'
    + '.chat{max-width:800px;width:100%;display:flex;flex-direction:column;gap:0.4rem;}'
    + '.bubble{max-width:80%;padding:0.5rem 0.75rem;border-radius:12px;font-size:0.9rem;line-height:1.45;word-wrap:break-word;}'
    + '.bubble.agent{align-self:flex-start;background:#16213e;color:#e0e0e0;border-bottom-left-radius:3px;position:relative;}'
    + '.bubble.user{align-self:flex-end;background:#2563eb;color:#fff;border-bottom-right-radius:3px;}'
    + '.bubble.user.voice{background:#7c3aed;}'
    + '.bubble.agent.voice{background:#1e293b;border-left:3px solid #7c3aed;}'
    + '.bubble.system{align-self:center;color:#666;font-size:0.75rem;}'
    + '.system-collapse-counter{align-self:center;color:#666;font-size:0.65rem;padding:0.15rem 0.5rem;opacity:0.5;}'
    + '.bubble.canvas-bubble{padding:0;background:#0d1525;overflow:hidden;max-width:90%;}'
    + '.bubble code{background:rgba(255,255,255,0.1);padding:0.1rem 0.3rem;border-radius:3px;font-size:0.85em;}'
    + '.bubble pre{background:rgba(0,0,0,0.3);padding:0.5rem;border-radius:6px;overflow-x:auto;margin:0.3rem 0;}'
    + '.bubble pre code{background:none;padding:0;font-size:inherit;}'
    + '.bubble h1,.bubble h2,.bubble h3,.bubble h4,.bubble h5,.bubble h6{margin:0.4rem 0 0.2rem;line-height:1.3;}'
    + '.bubble h1{font-size:1.3em;}.bubble h2{font-size:1.15em;}.bubble h3{font-size:1.05em;}.bubble h4,.bubble h5,.bubble h6{font-size:0.95em;}'
    + '.bubble hr{border:none;border-top:1px solid rgba(255,255,255,0.15);margin:0.4rem 0;}'
    + '.bubble ul,.bubble ol{margin:0.2rem 0;padding-left:1.2em;}.bubble li{margin:0.1rem 0;}'
    + '.bubble a{color:#93c5fd;text-decoration:underline;}'
    + '.bubble.user a{color:#fff;}'
    + '.bubble table{border-collapse:collapse;margin:0.3rem 0;font-size:0.85em;}'
    + '.bubble th,.bubble td{border:1px solid rgba(255,255,255,0.15);padding:0.25rem 0.5rem;}'
    + '.bubble th{background:rgba(255,255,255,0.08);font-weight:600;}'
    + '.elapsed-time{align-self:center;color:#666;font-size:0.65rem;padding:0.15rem 0.5rem;opacity:0.6;}'
    + '.file-thumb{max-width:300px;max-height:200px;border-radius:6px;cursor:pointer;margin-top:0.3rem;}'
    + '.file-attachment-link{color:#93c5fd;text-decoration:underline;display:inline-block;margin-top:0.3rem;}'
    + '.hl-k{color:#c792ea;}.hl-s{color:#c3e88d;}.hl-c{color:#6a737d;font-style:italic;}.hl-n{color:#f78c6c;}'
    + '.frozen-replies{display:flex;flex-direction:row;justify-content:flex-end;gap:0.5rem;padding:0.2rem 0;flex-wrap:wrap;}'
    + '.frozen-replies .chip{padding:0.35rem 0.9rem;font-size:0.8rem;font-weight:500;border:1px solid rgba(255,255,255,0.15);border-radius:16px;background:transparent;color:#999;cursor:default;}'
    + '.bubble-tts-btn{position:absolute;right:-32px;bottom:4px;width:24px;height:24px;border:1.5px solid #666;border-radius:50%;background:transparent;color:#666;cursor:pointer;display:flex;align-items:center;justify-content:center;padding:0;opacity:0.4;transition:opacity 0.15s,color 0.15s,border-color 0.15s;}'
    + '.bubble-tts-btn:hover{opacity:1;color:#999;border-color:#999;}'
    + '.bubble-tts-btn.playing{opacity:1;color:#7c3aed;border-color:#7c3aed;animation:ttsPulse 1s ease-in-out infinite;}'
    + '@keyframes ttsPulse{0%,100%{opacity:0.7;}50%{opacity:1;}}'
    + '.bubble-tts-btn svg{width:14px;height:14px;fill:currentColor;}'
    + '</style></head><body><div class="chat">'
    + items.join('\n')
    + '</div><script>'
    + '(function(){'
    + 'function splitChunks(t){var c=[],s="",ss=t.split(/(?<=[.!?])\\s+/);for(var i=0;i<ss.length;i++){if((s+ss[i]).length>800&&s){c.push(s);s="";}s+=(s?" ":"")+ss[i];}if(s)c.push(s);return c.length?c:[t];}'
    + 'function speak(text,done){'
    + 'if(typeof speechSynthesis==="undefined"){done();return;}'
    + 'speechSynthesis.cancel();'
    + 'var chunks=splitChunks(text),d=false;'
    + 'function fin(){if(d)return;d=true;done();}'
    + 'function next(i){if(d||i>=chunks.length){fin();return;}var u=new SpeechSynthesisUtterance(chunks[i]);u.rate=1;u.onend=function(){next(i+1);};u.onerror=function(){fin();};speechSynthesis.speak(u);}'
    + 'setTimeout(function(){next(0);},100);'
    + '}'
    + 'var btns=document.querySelectorAll(".bubble-tts-btn");'
    + 'for(var i=0;i<btns.length;i++){(function(btn){'
    + 'btn.addEventListener("click",function(e){'
    + 'e.stopPropagation();'
    + 'if(btn.classList.contains("playing"))return;'
    + 'btn.classList.add("playing");'
    + 'speak(btn.parentElement.innerText,function(){btn.classList.remove("playing");});'
    + '});'
    + '})(btns[i]);}'
    + '})();'
    // IMPORTANT: keep the closing script tag literal SPLIT (see line below).
    // When this app.js is inlined into an outer script block by a host page
    // (e.g. swe-swe playback), the HTML parser scans for the literal byte
    // sequence "<" + "/" + "script" regardless of JS string or comment
    // context, and would prematurely terminate the outer script tag. The
    // split survives both external-script loading and inline embedding.
    // NOTE: this comment must NOT contain the literal closing-tag bytes
    // either, otherwise it self-defeats when inlined.
    + '</' + 'script></body></html>';

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
  // Hide interactive elements (but keep btn-download visible — recordings are
  // exportable too, and the export logic walks the same DOM that playback
  // populates).
  document.getElementById('input-bar').style.display = 'none';
  document.getElementById('quick-replies').style.display = 'none';
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
      if (events.length === 0) {
        addBubble('Chat history is empty.', 'system');
        return;
      }
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
