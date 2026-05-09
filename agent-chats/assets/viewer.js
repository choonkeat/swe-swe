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

async function loadChat(mdPath, container) {
  container = container || document.querySelector('.chat');
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
