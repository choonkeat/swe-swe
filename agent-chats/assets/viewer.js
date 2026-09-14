// Tiny markdown→bubbles renderer. Splits on `**Role**` or `## Role` markers
// and renders each turn as a chat bubble. Uses marked.js (CDN) for the body
// markdown.

const ROLE_MAP = {
  user: 'user', you: 'user',
  agent: 'agent', claude: 'agent',
  system: 'system',
};
// Turn markers and elapsed-time markers live OUTSIDE the `> ` blockquote that
// wraps every user/agent body. The `(?!>)` lookahead makes that contract
// explicit: even if the markdown were corrupted to put a marker-like literal
// inside a blockquoted body, the regex won't false-match. Combined with
// `^…$` anchoring this also protects against marker text inside fenced code
// blocks (which gets blockquoted alongside the surrounding body).
const TURN_RE = /^(?!>)(?:## ([A-Za-z]+)|\*\*([A-Za-z]+)\*\*)\s*$/;
const ELAPSED_RE = /^(?!>)<small>\s*(?:took\s+)?([^<]+?)\s*<\/small><br\s*\/?>?\s*$/i;

function unblockquote(s) {
  return s.split('\n').map(line => line.replace(/^> ?/, '')).join('\n');
}

function stripFrontmatter(md) {
  if (!md.startsWith('---\n')) return { meta: {}, body: md };
  const end = md.indexOf('\n---\n', 4);
  if (end < 0) return { meta: {}, body: md };
  const fm = md.slice(4, end);
  const body = md.slice(end + 5);
  const meta = {};
  for (const line of fm.split('\n')) {
    const m = line.match(/^([a-zA-Z0-9_-]+):\s*(.*)$/);
    if (m) meta[m[1]] = m[2];
  }
  return { meta, body };
}

// Legacy variant-C parser — each turn is a 2-cell <table> (left/right empty
// indicates the role). Kept so old `agent-chats/*.md` still render.
function splitTurnsFromTables(body) {
  const tableRE = /<table[^>]*>[\s\S]*?<\/table>/g;
  const tdRE = /<td[^>]*>([\s\S]*?)<\/td>/g;
  const isEmpty = s => {
    const t = s.replace(/&nbsp;/g, '').replace(/\s+/g, '');
    return t === '';
  };
  const turns = [];
  let lastEnd = 0;
  let preamble = '';
  let m;
  while ((m = tableRE.exec(body)) !== null) {
    const between = body.slice(lastEnd, m.index).trim();
    if (between) {
      if (turns.length === 0) preamble += (preamble ? '\n\n' : '') + between;
      else turns.push({ role: 'system', body: between });
    }
    const tableInner = m[0];
    const cells = [];
    let cm;
    tdRE.lastIndex = 0;
    while ((cm = tdRE.exec(tableInner)) !== null) cells.push(cm[1]);
    if (cells.length === 2) {
      const [left, right] = cells.map(s => s.trim());
      let role, content;
      if (isEmpty(left) && !isEmpty(right))      { role = 'user';   content = right; }
      else if (isEmpty(right) && !isEmpty(left)) { role = 'agent';  content = left;  }
      else                                       { role = 'system'; content = left || right; }
      content = content.replace(/^\s*\*\*(You|Claude|Agent|User|System)\*\*\s*\n*/, '');
      turns.push({ role, body: content.trim() });
    } else if (cells.length === 1) {
      turns.push({ role: 'system', body: cells[0].trim() });
    }
    lastEnd = m.index + m[0].length;
  }
  const tail = body.slice(lastEnd).trim();
  if (tail) {
    if (turns.length === 0) preamble += (preamble ? '\n\n' : '') + tail;
    else turns.push({ role: 'system', body: tail });
  }
  return { preamble: preamble.trim(), turns };
}

// Heading / bold-line parser. A turn starts on a line matching TURN_RE
// (`## Role` or `**Role**`). Two pieces of metadata may appear adjacent to
// turn markers and are extracted into structured fields rather than left in
// the bubble body:
//   - Pre-marker `<small>took NN.Ns</small><br>` line  → turn.elapsed
//   - Trailing `[Quick replies]\n- A\n- B\n` block      → turn.replies
function splitTurnsFromHeadings(body) {
  const lines = body.split('\n');
  const turns = [];
  const preamble = [];
  let current = null;
  let pendingElapsed = null;

  for (const line of lines) {
    const turnMatch = line.match(TURN_RE);
    const rawRole = turnMatch && (turnMatch[1] || turnMatch[2]);
    const role = rawRole && ROLE_MAP[rawRole.toLowerCase()];
    if (role) {
      if (current) turns.push(current);
      current = { role, lines: [], elapsed: pendingElapsed };
      pendingElapsed = null;
      continue;
    }
    const elapsedMatch = line.match(ELAPSED_RE);
    if (elapsedMatch) {
      // Buffer it — applies to the *next* turn, not the current one.
      pendingElapsed = elapsedMatch[1].trim();
      continue;
    }
    if (current) {
      current.lines.push(line);
    } else {
      preamble.push(line);
    }
  }
  if (current) turns.push(current);

  return {
    preamble: preamble.join('\n').trim(),
    turns: turns.map(t => {
      let bodyText = t.lines.join('\n').trim();
      bodyText = unblockquote(bodyText);
      const { body: stripped, replies } = extractQuickReplies(bodyText);
      return { role: t.role, body: stripped.trim(), elapsed: t.elapsed, replies };
    }),
  };
}

// Pull a trailing `[Quick replies]\n- A\n- B\n…` block off the end of body.
// The block lives outside the `> ` blockquote, so a body line `> [Quick
// replies]` (a literal one inside the speech bubble) won't false-trigger:
// after `unblockquote` strips the `> ` we see `[Quick replies]` at column 0,
// but only at the *very end* of the body — and any text *after* the bullets
// (e.g. continued conversation prose) breaks the match.
function extractQuickReplies(body) {
  const m = body.match(/(?:^|\n)\[Quick replies\]\s*\n((?:[ \t]*-[^\n]*\n?)+)\s*$/);
  if (!m) return { body, replies: [] };
  const replies = m[1].split('\n')
    .map(line => line.replace(/^[ \t]*-\s*/, '').trim())
    .filter(Boolean);
  return { body: body.slice(0, m.index), replies };
}

// Blockquote-prefix parser (variants E/F). Lines like `> **You:** …` or
// `> **Claude:** …` start a new turn; bare content between them is the OTHER
// role. Kept for backward-compat with that variant.
function splitTurnsFromPrefix(body) {
  const lines = body.split('\n');
  const PREFIX_RE = /^> \*\*(You|Claude|Agent|User):\*\*\s?(.*)$/;
  const QUOTE_CONT_RE = /^> ?(.*)$/;
  const turns = [];
  let preamble = [];
  let current = null;
  let blockquoteRole = null;

  const flush = () => { if (current) { current.body = current.body.trim(); turns.push(current); current = null; } };

  for (const line of lines) {
    const pm = line.match(PREFIX_RE);
    if (pm) {
      flush();
      const role = ROLE_MAP[pm[1].toLowerCase()];
      blockquoteRole = blockquoteRole || role;
      current = { role, body: pm[2] + '\n', source: 'quote' };
      continue;
    }
    if (current && current.source === 'quote') {
      const qm = line.match(QUOTE_CONT_RE);
      if (qm) { current.body += qm[1] + '\n'; continue; }
      flush();
    }
    if (!current) {
      if (blockquoteRole === null) { preamble.push(line); continue; }
      const otherRole = blockquoteRole === 'user' ? 'agent' : 'user';
      current = { role: otherRole, body: '', source: 'bare' };
    }
    current.body += line + '\n';
  }
  flush();
  return {
    preamble: preamble.join('\n').trim(),
    turns: turns.map(t => ({ role: t.role, body: t.body.trim(), elapsed: null, replies: [] })),
  };
}

function splitTurns(body) {
  // Order matters. The new script-style format and old table format both
  // include `**Role**` markers (the table parser strips them from inside
  // cells, so seeing `**USER**` at top level means new format). Detect the
  // table format first by its distinctive `<table>` shape.
  if (/<table[^>]*>[\s\S]*?<\/table>/.test(body)) {
    return splitTurnsFromTables(body);
  }
  if (TURN_RE.test(body) || body.split('\n').some(l => TURN_RE.test(l))) {
    return splitTurnsFromHeadings(body);
  }
  if (/^> \*\*(You|Claude|Agent|User):\*\*/m.test(body)) {
    return splitTurnsFromPrefix(body);
  }
  return { preamble: body.trim(), turns: [] };
}

// Relative links inside a chat live relative to the .md file, but the parsed
// HTML is injected into index.html's document — so the browser would resolve
// `./assets/x.png` against index.html's URL, not the chat's. That was the same
// directory until exports moved into per-month subdirectories; now it is one
// level off. Rebase every relative src/href against the .md's own URL so the
// viewer agrees with how GitHub renders the very same markdown.
function rebaseRelativeURLs(root, mdPath) {
  const base = new URL(mdPath, location.href);
  for (const [sel, attr] of [['img[src]', 'src'], ['a[href]', 'href']]) {
    for (const el of root.querySelectorAll(sel)) {
      const raw = el.getAttribute(attr);
      // Absolute URLs, protocol-relative, in-page anchors and root-relative
      // paths already mean what they say.
      if (!raw || /^[a-z][a-z0-9+.-]*:/i.test(raw) || raw.startsWith('//') || raw.startsWith('#') || raw.startsWith('/')) continue;
      el.setAttribute(attr, new URL(raw, base).href);
    }
  }
}

// --- "Copy as markdown" ---
// Each bubble keeps the markdown it was rendered from, and a "⋯" button hands
// that source back to the clipboard. Two routes, because neither covers every
// place this page is opened:
//
//   1. navigator.clipboard.writeText — only exists in a secure context (https
//      or localhost), and inside a cross-origin iframe it additionally needs
//      the host page to grant `clipboard-write` via Permissions Policy. The
//      swe-swe Files tab does NOT grant it today, so writeText() rejects there.
//   2. document.execCommand('copy') on an off-screen textarea — deprecated but
//      universally implemented, works on plain http, and is NOT gated by
//      Permissions Policy. It only needs a user gesture, which a menu click is.
//
// Feature-detecting (1) is no help: its presence says nothing about whether the
// embedding page permits it. So we always offer the action and let the attempt
// decide.
function copyTextToClipboard(text, onResult) {
  function fallback() {
    let ok = false;
    const ta = document.createElement('textarea');
    ta.value = text;
    // Off-screen but focusable — display:none would make the copy a no-op.
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.top = '-1000px';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    try {
      ta.select();
      ta.setSelectionRange(0, ta.value.length);
      ok = document.execCommand('copy');
    } catch (e) {
      ok = false;
    }
    ta.remove();
    if (onResult) onResult(ok);
  }
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => { if (onResult) onResult(true); }, fallback);
    return;
  }
  fallback();
}

function showCopyToast(ok) {
  let t = document.getElementById('copy-toast');
  if (!t) {
    t = document.createElement('div');
    t.id = 'copy-toast';
    document.body.appendChild(t);
  }
  t.textContent = ok ? 'Copied as markdown' : 'Copy failed';
  t.classList.toggle('failed', !ok);
  t.classList.add('show');
  clearTimeout(t.hideTimer);
  t.hideTimer = setTimeout(() => t.classList.remove('show'), 1600);
}

let openBubbleMenu = null;

function closeBubbleMenu() {
  if (openBubbleMenu) {
    openBubbleMenu.remove();
    openBubbleMenu = null;
  }
}

// Fixed, just below its button, clamped to the viewport; flipped above when it
// would run off the bottom.
function positionMenuBelow(menu, btn) {
  const r = btn.getBoundingClientRect();
  const mr = menu.getBoundingClientRect();
  let top = r.bottom + 6;
  let left = r.left;
  if (left + mr.width > window.innerWidth - 8) left = window.innerWidth - 8 - mr.width;
  if (left < 8) left = 8;
  if (top + mr.height > window.innerHeight - 8) top = r.top - 6 - mr.height;
  if (top < 8) top = 8;
  menu.style.top = top + 'px';
  menu.style.left = left + 'px';
}

const ICON_DOTS = '<svg viewBox="0 0 24 24"><circle cx="5" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/></svg>';
const ICON_COPY = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M6 15H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h9a1 1 0 0 1 1 1v1"/></svg>';

// Give a rendered bubble its "⋯" menu button. `md` is the markdown it came
// from — what Copy hands back, rather than a reconstruction of the rendered
// HTML. The button is markup only: the single delegated listener below drives
// every button and every row in the log, so a long chat costs one listener, not
// one per bubble.
function addBubbleMenu(bubble, md) {
  bubble.dataset.md = md;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'bubble-menu-btn';
  btn.title = 'More actions';
  btn.setAttribute('aria-haspopup', 'true');
  btn.innerHTML = ICON_DOTS;
  bubble.appendChild(btn);
}

function openBubbleMenuFor(btn) {
  const bubble = btn.closest('.bubble');
  if (!bubble) return;
  const menu = document.createElement('div');
  menu.className = 'bubble-menu';
  menu.ownerBtn = btn;
  const item = document.createElement('button');
  item.type = 'button';
  item.dataset.action = 'copy';
  item.innerHTML = '<span class="bubble-menu-ic">' + ICON_COPY + '</span><span>Copy as markdown</span>';
  menu.appendChild(item);
  document.body.appendChild(menu);
  positionMenuBelow(menu, btn);
  openBubbleMenu = menu;
}

// ONE click listener for every bubble menu in the log: a chosen row, a "⋯"
// button (toggle), or anywhere else (dismiss).
document.addEventListener('click', (e) => {
  const t = e.target;
  if (!t || !t.closest) { closeBubbleMenu(); return; }

  const row = t.closest('.bubble-menu button[data-action]');
  if (row) {
    const owner = openBubbleMenu && openBubbleMenu.ownerBtn;
    const bubble = owner && owner.closest('.bubble');
    closeBubbleMenu();
    if (bubble && row.dataset.action === 'copy') {
      copyTextToClipboard(bubble.dataset.md || bubble.innerText, showCopyToast);
    }
    return;
  }
  // A click on the menu's own chrome should neither act nor dismiss.
  if (t.closest('.bubble-menu')) return;

  const btn = t.closest('.bubble-menu-btn');
  if (btn) {
    const wasOpenForThis = openBubbleMenu && openBubbleMenu.ownerBtn === btn;
    closeBubbleMenu();
    if (!wasOpenForThis) openBubbleMenuFor(btn);
    return;
  }

  closeBubbleMenu();
});

document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeBubbleMenu(); });

async function loadChat(mdPath, container) {
  container = container || document.querySelector('.chat');
  closeBubbleMenu();
  container.innerHTML = '';
  try {
    // Ask for raw markdown via Accept content-negotiation. md-serve
    // (>= the Accept-header release) sees no `text/html` in the list and
    // returns the source bytes; vanilla static servers ignore Accept and
    // serve the file as-is, which is also fine.
    const resp = await fetch(mdPath, {
      cache: 'no-cache',
      headers: { 'Accept': 'text/markdown, text/plain' },
    });
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    const md = await resp.text();
    const { meta, body } = stripFrontmatter(md);
    const { preamble, turns } = splitTurns(body);

    // Strip HTML comments (e.g. `<!-- agent-chat export … -->`) so they don't
    // create an empty system bubble.
    const preambleClean = preamble.replace(/<!--[\s\S]*?-->/g, '').trim();
    if (preambleClean) {
      const pre = document.createElement('div');
      pre.className = 'bubble system';
      pre.innerHTML = marked.parse(preambleClean);
      rebaseRelativeURLs(pre, mdPath);
      container.appendChild(pre);
    }
    for (const turn of turns) {
      if (turn.elapsed) {
        const el = document.createElement('div');
        el.className = 'elapsed-time';
        el.textContent = turn.elapsed;
        container.appendChild(el);
      }
      const bubble = document.createElement('div');
      bubble.className = 'bubble ' + turn.role;
      bubble.innerHTML = marked.parse(turn.body);
      rebaseRelativeURLs(bubble, mdPath);
      addBubbleMenu(bubble, turn.body);
      container.appendChild(bubble);
      if (turn.replies && turn.replies.length) {
        const fr = document.createElement('div');
        fr.className = 'frozen-replies';
        for (const r of turn.replies) {
          const chip = document.createElement('span');
          chip.className = 'chip frozen';
          chip.textContent = r;
          fr.appendChild(chip);
        }
        container.appendChild(fr);
      }
    }
    return meta;
  } catch (e) {
    const err = document.createElement('div');
    err.className = 'error';
    err.textContent = 'Failed to load ' + mdPath + ': ' + e.message;
    container.appendChild(err);
    return {};
  }
}

// Legacy dropdown viewer (viewer.html). Looks for #chat-select; no-op if absent.
function init(manifest) {
  const select = document.querySelector('#chat-select');
  if (!select) return;
  for (const entry of manifest) {
    const opt = document.createElement('option');
    opt.value = entry.md;
    opt.textContent = entry.label;
    select.appendChild(opt);
  }
  const update = async () => {
    const meta = await loadChat(select.value);
    const tb = document.querySelector('.toolbar h1');
    if (tb && meta) {
      tb.textContent = (meta.date || '') + (meta.index ? '-' + meta.index : '') + ' · ' + (meta.title || select.value);
    }
    if (meta && meta.title) document.title = meta.title + ' — chat log';
  };
  select.addEventListener('change', update);
  if (manifest.length) update();
}

window.viewer = { init, load: loadChat };
