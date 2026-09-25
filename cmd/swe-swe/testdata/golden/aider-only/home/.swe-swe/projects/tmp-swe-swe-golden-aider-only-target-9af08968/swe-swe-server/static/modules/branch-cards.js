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

/**
 * The small tags a card shows. `check` is this branch's entry from
 * /api/repo/branch-check-all (or null until it arrives).
 */
export function cardTags(card, check, data) {
    const tags = [];
    const def = (data && data.defaultBranch) || 'main';
    if (card.kind === 'workspace') {
        tags.push('on: ' + (card.name || 'no branch'));
        if (card.notDefault) tags.push('not ' + def);
        return tags;
    }
    if (card.kind === 'online') return ['online only'];
    if (card.default) tags.push('default');
    if (card.folder) tags.push('has folder');
    if (card.inUse) tags.push('in use');
    if (card.oddName) tags.push('odd name');
    if (check && check.canTell && check.unsavedCommits > 0) tags.push(check.unsavedCommits + ' unsaved');
    return tags;
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
