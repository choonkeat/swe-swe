/**
 * Pure rules for the New Session dialog's branch cards: grouping, tags, what
 * typing into "+ New branch" picks, and the branch value Start sends.
 * Side-effect free; shared between the dialog and its unit tests.
 * Decision table: mockups/lo-fi/2026-09/24-new-session-branch-cards.html.
 * @module branch-cards
 */

/**
 * A pick is one of:
 *   {kind:'workspace'}                 -- the checkout as it is (sends no branch)
 *   {kind:'local', name}               -- a branch on this box
 *   {kind:'online', remote, name}      -- a branch only on `remote`
 *   {kind:'new', name}                 -- a brand-new branch
 *   {kind:'blocked', reason}           -- typed text that can't be used
 */

/**
 * Split a /api/repo/branches reply into the dialog's groups. Returns null when
 * the reply has no cards (an older server), so the caller falls back to the
 * plain branch box.
 */
export function groupCards(data) {
    if (!data || !Array.isArray(data.cards)) return null;
    const g = { workspace: null, local: [], leftovers: data.leftovers || [], online: [] };
    const byRemote = new Map();
    for (const c of data.cards) {
        if (c.kind === 'workspace') g.workspace = c;
        else if (c.kind === 'local') g.local.push(c);
        else if (c.kind === 'online') {
            if (!byRemote.has(c.remote)) byRemote.set(c.remote, []);
            byRemote.get(c.remote).push(c);
        }
    }
    // Remote order as `git remote` lists it; any remote not in that list last.
    const order = (data.remotes || []).filter((r) => byRemote.has(r));
    for (const r of byRemote.keys()) if (!order.includes(r)) order.push(r);
    g.online = order.map((remote) => ({ remote, cards: byRemote.get(remote) }));
    return g;
}

/** The small tags a card shows. */
export function cardTags(card, data) {
    const tags = [];
    const def = (data && data.defaultBranch) || 'main';
    if (card.kind === 'workspace') {
        tags.push('on: ' + (card.name || 'no branch'));
        if (card.notDefault) tags.push('not ' + def);
        return tags;
    }
    if (card.kind === 'online') return ['online only'];
    if (card.default) tags.push('default');
    if (card.inUse) tags.push('in use');
    if (card.oddName) tags.push('odd name');
    return tags;
}

/** How many local branches and leftover folders make the cleanup tip show. */
export const CLEANUP_TIP_MIN = 10;

/**
 * The tip under the workspace card when there is a lot to clean up: deleting
 * one card at a time is slow, and an agent can weigh them all at once.
 * `g` is groupCards() output; '' when there are fewer than CLEANUP_TIP_MIN.
 */
export function cleanupTip(g) {
    const n = g ? g.local.length + g.leftovers.length : 0;
    if (n < CLEANUP_TIP_MIN) return '';
    return n + ' branches and folders here. Instead of deleting them one at a time, it may be quicker to ask your agent: ' +
        '"Let\'s discuss what worktrees & branches we can clean up".';
}

/**
 * Whether picking this card should ask the server what deleting it would
 * lose. Only local branches that can be deleted: the rest have no Delete,
 * so checking them would be git work for nothing.
 */
export function needsPickCheck(card) {
    return !!card && card.kind === 'local' && !!card.deletable;
}

/** "1.2 GB" style size, for the picked card's line. */
export function formatBytes(n) {
    if (!(n >= 0)) return '';
    const units = ['bytes', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1000 && i < units.length - 1) {
        v /= 1000;
        i++;
    }
    return (i === 0 ? String(v) : v.toFixed(v < 10 ? 1 : 0)) + ' ' + units[i];
}

/**
 * The line a picked card shows once /api/repo/branch-check answers: what
 * deleting would lose, then the folder size when known. `risky` is true when
 * something would be lost or the check couldn't tell.
 */
export function checkSummary(check) {
    if (!check) return { text: '', risky: false };
    const parts = [];
    let risky = false;
    if (!check.canTell) {
        parts.push("Couldn't check what would be lost.");
        risky = true;
    } else {
        const n = check.unsavedCommits || 0;
        const e = check.folderEdits || 0;
        if (n > 0) parts.push(n + (n === 1 ? ' saved change exists' : ' saved changes exist') + ' only here.');
        if (e > 0) parts.push(e + (e === 1 ? ' unsaved edit' : ' unsaved edits') + ' in its folder.');
        if (n > 0 || e > 0) risky = true;
        else parts.push('Nothing to lose.');
    }
    if (typeof check.folderBytes === 'number') parts.push('Folder is ' + formatBytes(check.folderBytes) + '.');
    return { text: parts.join(' '), risky };
}

/**
 * What text typed into "+ New branch" picks (sketch screen D). The field only
 * ever makes something new: an existing name jumps to that card instead.
 * Returns {pick, message}; pick is null for empty text.
 */
export function resolveTyped(text, data) {
    const name = (text || '').trim();
    if (!name) return { pick: null, message: '' };
    const cards = (data && data.cards) || [];
    const remotes = (data && data.remotes) || [];
    const workspace = cards.find((c) => c.kind === 'workspace');
    const local = (n) => cards.find((c) => c.kind === 'local' && c.name === n);
    const online = (n, remote) => cards.find((c) => c.kind === 'online' && c.name === n && (!remote || c.remote === remote));

    const onBox = (n) => {
        if (workspace && workspace.name === n) {
            return { pick: { kind: 'workspace' }, message: '"' + n + '" is the workspace as it is. Picked that card for you.' };
        }
        if (local(n)) {
            return { pick: { kind: 'local', name: n }, message: '"' + n + '" is already on this box. Picked that card for you.' };
        }
        return null;
    };

    // Exact local names first, so an odd name like "origin/typo" finds its card.
    const exact = onBox(name);
    if (exact) return exact;

    const remote = remotes.find((r) => name.startsWith(r + '/'));
    if (remote) {
        const rest = name.slice(remote.length + 1);
        const hit = onBox(rest);
        if (hit) return hit;
        if (online(rest, remote)) {
            return { pick: { kind: 'online', remote, name: rest }, message: '"' + rest + '" is already online. Picked it for you.' };
        }
        const list = remotes.map((r) => r + '/').join(' or ');
        return {
            pick: { kind: 'blocked', reason: 'Names can\'t start with ' + list + ' (this repo\'s online copies).' },
            message: '',
        };
    }

    const onlineHit = online(name, 'origin') || online(name);
    if (onlineHit) {
        return { pick: { kind: 'online', remote: onlineHit.remote, name }, message: '"' + name + '" is already online. Picked it for you.' };
    }
    return { pick: { kind: 'new', name }, message: '' };
}

/**
 * The ?branch= value Start sends for a pick -- the same value the old branch
 * dropdown sent. An online branch on a remote other than origin is sent as
 * "<remote>/<name>" so the server tracks that remote's copy.
 */
export function branchValueFor(pick) {
    if (!pick) return '';
    switch (pick.kind) {
        case 'local':
        case 'new':
            return pick.name;
        case 'online':
            return pick.remote === 'origin' ? pick.name : pick.remote + '/' + pick.name;
        default:
            return '';
    }
}

/** Whether two picks name the same card. */
export function samePick(a, b) {
    if (!a || !b) return false;
    return a.kind === b.kind && (a.name || '') === (b.name || '') && (a.remote || '') === (b.remote || '');
}

/**
 * Delete flow for one card (sketch screens B and C). States:
 *   ready     -- nothing going on
 *   why       -- a greyed [x] was tapped; `reason` says why it can't go
 *   checking  -- Delete (or a leftover's [x]) tapped; the server re-checks
 *                and deletes if nothing would be lost. Shown as "deleting...",
 *                since that is what takes the time
 *   confirm   -- something would be lost; `reason`, `undoable` from server
 *   deleting  -- "Delete anyway" tapped
 *   deleted   -- gone; `undo` note and `undoable` for the Undo strip
 *   undoing   -- Undo tapped
 *   failed    -- the server said no; `reason`
 * Events: tap, why, server (a branch-delete / leftover-remove reply),
 * refused, keep, confirm, undo, undone.
 * Busy states ignore every tap, which is the double-tap guard.
 */
export function isBusy(s) {
    return !!s && (s.state === 'checking' || s.state === 'deleting' || s.state === 'undoing');
}

export function cardStateAfter(prev, event) {
    const s = prev || { state: 'ready' };
    if (isBusy(s) && event.type !== 'server' && event.type !== 'refused' && event.type !== 'undone') return s;
    switch (event.type) {
        case 'tap':
            return s.state === 'confirm' || s.state === 'deleted' ? s : { state: 'checking' };
        case 'why':
            return s.state === 'why' ? { state: 'ready' } : { state: 'why', reason: event.reason };
        case 'server': {
            if (s.state !== 'checking' && s.state !== 'deleting') return s;
            const r = event.res || {};
            if (r.deleted) return { state: 'deleted', undo: r.undo || null, undoable: !!r.undoable && !!r.undo };
            if (r.needsConfirm) return { state: 'confirm', reason: r.reason || '', undoable: !!r.undoable };
            return { state: 'failed', reason: 'Unexpected reply from the server.' };
        }
        case 'refused':
            if (s.state === 'undoing') return Object.assign({}, s, { state: 'deleted', reason: event.reason });
            return { state: 'failed', reason: event.reason };
        case 'keep':
            return s.state === 'confirm' ? { state: 'ready' } : s;
        case 'confirm':
            return s.state === 'confirm' ? { state: 'deleting' } : s;
        case 'undo':
            return s.state === 'deleted' && s.undoable ? Object.assign({}, s, { state: 'undoing', reason: '' }) : s;
        case 'undone':
            return s.state === 'undoing' ? { state: 'ready' } : s;
        default:
            return s;
    }
}

/**
 * The reply with deleted cards taken out, so typing a deleted branch's name
 * into "+ New" makes it new again instead of picking a card that is gone.
 * `states` maps cardKey -> state.
 */
export function withoutDeleted(data, states) {
    if (!data || !Array.isArray(data.cards)) return data;
    const gone = (c) => c.kind === 'local' && states[localKey(c.name)] &&
        (states[localKey(c.name)].state === 'deleted' || states[localKey(c.name)].state === 'undoing');
    return Object.assign({}, data, { cards: data.cards.filter((c) => !gone(c)) });
}

export function localKey(name) {
    return 'local:' + name;
}

export function leftoverKey(folder) {
    return 'leftover:' + folder;
}
