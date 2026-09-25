/**
 * Unit tests for branch-cards.js
 * Run with: node --test branch-cards.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { groupCards, cardTags, needsPickCheck, formatBytes, checkSummary, resolveTyped, branchValueFor, samePick } from './branch-cards.js';

// A /api/repo/branches reply shaped like screen A of the sketch.
function reply(overrides) {
    return Object.assign({
        branches: [],
        remotes: ['origin', 'upstream'],
        defaultBranch: 'main',
        defaultGuessed: false,
        cards: [
            { kind: 'workspace', name: 'main', deletable: false },
            { kind: 'local', name: 'browser-backend', folder: '/worktrees/browser-backend', deletable: true },
            { kind: 'local', name: 'feat/cross-box', folder: '/worktrees/feat--cross-box', inUse: true, deletable: false },
            { kind: 'local', name: 'origin/typo-fix', oddName: true, deletable: true },
            { kind: 'online', name: 'foo', remote: 'origin', deletable: false },
            { kind: 'online', name: 'shared', remote: 'origin', deletable: false },
            { kind: 'online', name: 'shared', remote: 'upstream', deletable: false },
        ],
        leftovers: [{ folder: '/worktrees/preview-per-session' }],
    }, overrides || {});
}

test('groupCards: workspace, on this box, leftovers, one online group per remote', () => {
    const g = groupCards(reply());
    assert.strictEqual(g.workspace.name, 'main');
    assert.deepStrictEqual(g.local.map((c) => c.name), ['browser-backend', 'feat/cross-box', 'origin/typo-fix']);
    assert.deepStrictEqual(g.leftovers.map((l) => l.folder), ['/worktrees/preview-per-session']);
    assert.deepStrictEqual(g.online.map((o) => [o.remote, o.cards.map((c) => c.name)]),
        [['origin', ['foo', 'shared']], ['upstream', ['shared']]]);
});

test('groupCards: a remote with no online-only branches gets no group', () => {
    const g = groupCards(reply({ cards: reply().cards.filter((c) => c.remote !== 'upstream') }));
    assert.deepStrictEqual(g.online.map((o) => o.remote), ['origin']);
});

test('groupCards: no cards in the reply (older server) returns null', () => {
    assert.strictEqual(groupCards({ branches: ['main'] }), null);
});

test('cardTags: one tag per decision-table row', () => {
    const d = reply();
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'plain', deletable: true }, d), []);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'x', folder: '/w/x', deletable: true }, d), []);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'x', folder: '/w/x', inUse: true }, d), ['in use']);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'origin/x', oddName: true, deletable: true }, d), ['odd name']);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'main', default: true }, d), ['default']);
    assert.deepStrictEqual(cardTags({ kind: 'online', name: 'foo', remote: 'origin' }, d), ['online only']);
});

test('needsPickCheck: only deletable local branches cost a check', () => {
    assert.strictEqual(needsPickCheck({ kind: 'local', name: 'x', deletable: true }), true);
    assert.strictEqual(needsPickCheck({ kind: 'local', name: 'main', default: true, deletable: false }), false);
    assert.strictEqual(needsPickCheck({ kind: 'online', name: 'x', remote: 'origin' }), false);
    assert.strictEqual(needsPickCheck({ kind: 'workspace', name: 'main' }), false);
    assert.strictEqual(needsPickCheck(null), false);
});

test('formatBytes: short sizes for the picked card', () => {
    assert.strictEqual(formatBytes(0), '0 bytes');
    assert.strictEqual(formatBytes(999), '999 bytes');
    assert.strictEqual(formatBytes(1500), '1.5 KB');
    assert.strictEqual(formatBytes(1200000000), '1.2 GB');
    assert.strictEqual(formatBytes(250000000), '250 MB');
    assert.strictEqual(formatBytes(undefined), '');
});

test('checkSummary: what deleting would lose, then the folder size', () => {
    assert.deepStrictEqual(checkSummary({ canTell: true, unsavedCommits: 0, folderEdits: 0 }),
        { text: 'Nothing to lose.', risky: false });
    assert.deepStrictEqual(checkSummary({ canTell: true, unsavedCommits: 3, folderEdits: 0, folderBytes: 1200000000 }),
        { text: '3 saved changes exist only here. Folder is 1.2 GB.', risky: true });
    assert.deepStrictEqual(checkSummary({ canTell: true, unsavedCommits: 1, folderEdits: 1 }),
        { text: '1 saved change exists only here. 1 unsaved edit in its folder.', risky: true });
    assert.deepStrictEqual(checkSummary({ canTell: true, unsavedCommits: 0, folderEdits: 2, folderBytes: 0 }),
        { text: '2 unsaved edits in its folder. Folder is 0 bytes.', risky: true });
    // Can't tell counts as risky, whatever the numbers say.
    assert.deepStrictEqual(checkSummary({ canTell: false, unsavedCommits: 0, folderEdits: 0 }),
        { text: "Couldn't check what would be lost.", risky: true });
    assert.deepStrictEqual(checkSummary(null), { text: '', risky: false });
});

test('cardTags: workspace shows its branch, and "not main" when off the default', () => {
    const d = reply();
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: 'main' }, d), ['on: main']);
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: 'feat/x', notDefault: true }, d), ['on: feat/x', 'not main']);
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: '', notDefault: true }, d), ['on: no branch', 'not main']);
});

// Screen D: "+ New branch" only makes something new.
test('resolveTyped: empty text changes nothing', () => {
    assert.strictEqual(resolveTyped('  ', reply()).pick, null);
});

test('resolveTyped: the workspace branch picks the workspace card', () => {
    const r = resolveTyped('main', reply());
    assert.deepStrictEqual(r.pick, { kind: 'workspace' });
    assert.match(r.message, /workspace as it is/);
});

test('resolveTyped: an existing branch on this box picks its card', () => {
    const r = resolveTyped('browser-backend', reply());
    assert.deepStrictEqual(r.pick, { kind: 'local', name: 'browser-backend' });
    assert.match(r.message, /already on this box/);
});

test('resolveTyped: an odd-name local branch typed in full picks its card, not the remote rule', () => {
    assert.deepStrictEqual(resolveTyped('origin/typo-fix', reply()).pick, { kind: 'local', name: 'origin/typo-fix' });
});

test('resolveTyped: <remote>/<existing> picks the online card', () => {
    const r = resolveTyped('origin/foo', reply());
    assert.deepStrictEqual(r.pick, { kind: 'online', remote: 'origin', name: 'foo' });
    assert.match(r.message, /already online/);
    assert.deepStrictEqual(resolveTyped('upstream/shared', reply()).pick, { kind: 'online', remote: 'upstream', name: 'shared' });
});

test('resolveTyped: <remote>/<name on this box> picks the local card', () => {
    assert.deepStrictEqual(resolveTyped('origin/browser-backend', reply()).pick, { kind: 'local', name: 'browser-backend' });
    assert.deepStrictEqual(resolveTyped('origin/main', reply()).pick, { kind: 'workspace' });
});

test('resolveTyped: <remote>/<missing> is blocked with a reason', () => {
    const r = resolveTyped('origin/bar', reply());
    assert.strictEqual(r.pick.kind, 'blocked');
    assert.match(r.pick.reason, /origin\/ or upstream\//);
});

test('resolveTyped: a bare online-only name picks its online card, origin first', () => {
    assert.deepStrictEqual(resolveTyped('foo', reply()).pick, { kind: 'online', remote: 'origin', name: 'foo' });
    assert.deepStrictEqual(resolveTyped('shared', reply()).pick, { kind: 'online', remote: 'origin', name: 'shared' });
});

test('resolveTyped: anything else is a new branch', () => {
    const r = resolveTyped(' fresh-idea ', reply());
    assert.deepStrictEqual(r.pick, { kind: 'new', name: 'fresh-idea' });
    assert.strictEqual(r.message, '');
});

// Start sends exactly what the old dropdown sent.
test('branchValueFor: the value Start sends', () => {
    assert.strictEqual(branchValueFor({ kind: 'workspace' }), '');
    assert.strictEqual(branchValueFor(null), '');
    assert.strictEqual(branchValueFor({ kind: 'local', name: 'x' }), 'x');
    assert.strictEqual(branchValueFor({ kind: 'online', remote: 'origin', name: 'foo' }), 'foo');
    assert.strictEqual(branchValueFor({ kind: 'online', remote: 'upstream', name: 'shared' }), 'upstream/shared');
    assert.strictEqual(branchValueFor({ kind: 'new', name: 'fresh' }), 'fresh');
    assert.strictEqual(branchValueFor({ kind: 'blocked', reason: 'no' }), '');
});

test('samePick compares kind, remote and name', () => {
    assert.ok(samePick({ kind: 'workspace' }, { kind: 'workspace' }));
    assert.ok(samePick({ kind: 'local', name: 'x' }, { kind: 'local', name: 'x' }));
    assert.ok(!samePick({ kind: 'online', remote: 'origin', name: 'x' }, { kind: 'online', remote: 'upstream', name: 'x' }));
    assert.ok(!samePick(null, { kind: 'workspace' }));
});

import { cardStateAfter, isBusy, withoutDeleted, localKey } from './branch-cards.js';

test('delete flow: nothing to lose goes straight to deleted with Undo', () => {
    let s = cardStateAfter(null, { type: 'tap' });
    assert.strictEqual(s.state, 'checking');
    s = cardStateAfter(s, { type: 'server', res: { deleted: true, undoable: true, undo: { branch: 'x', sha: 'a' } } });
    assert.deepStrictEqual(s, { state: 'deleted', undo: { branch: 'x', sha: 'a' }, undoable: true });
    s = cardStateAfter(s, { type: 'undo' });
    assert.strictEqual(s.state, 'undoing');
    assert.strictEqual(cardStateAfter(s, { type: 'undone' }).state, 'ready');
});

test('delete flow: something to lose asks inside the card; Keep goes back', () => {
    let s = cardStateAfter({ state: 'checking' }, { type: 'server', res: { needsConfirm: true, reason: '3 saved', undoable: true } });
    assert.deepStrictEqual(s, { state: 'confirm', reason: '3 saved', undoable: true });
    assert.strictEqual(cardStateAfter(s, { type: 'keep' }).state, 'ready');
    s = cardStateAfter(s, { type: 'confirm' });
    assert.strictEqual(s.state, 'deleting');
    s = cardStateAfter(s, { type: 'server', res: { deleted: true, undoable: false, undo: { branch: 'x', sha: 'a' } } });
    assert.strictEqual(s.state, 'deleted');
    assert.strictEqual(s.undoable, false);
    assert.strictEqual(cardStateAfter(s, { type: 'undo' }).state, 'deleted', 'no Undo when it can\'t be undone');
});

test('delete flow: a server refusal shows its reason; tapping again retries', () => {
    const s = cardStateAfter({ state: 'checking' }, { type: 'refused', reason: 'A live session is using this branch.' });
    assert.deepStrictEqual(s, { state: 'failed', reason: 'A live session is using this branch.' });
    assert.strictEqual(cardStateAfter(s, { type: 'tap' }).state, 'checking');
});

test('delete flow: double-tap guard while checking, deleting or undoing', () => {
    for (const state of ['checking', 'deleting', 'undoing']) {
        const s = { state };
        assert.ok(isBusy(s));
        for (const type of ['tap', 'why', 'keep', 'confirm', 'undo']) {
            assert.strictEqual(cardStateAfter(s, { type }), s, state + ' ignores ' + type);
        }
    }
    assert.ok(!isBusy({ state: 'ready' }));
    assert.ok(!isBusy(null));
});

test('delete flow: greyed [x] toggles its one-line reason', () => {
    const s = cardStateAfter(null, { type: 'why', reason: 'This is the default branch.' });
    assert.deepStrictEqual(s, { state: 'why', reason: 'This is the default branch.' });
    assert.strictEqual(cardStateAfter(s, { type: 'why', reason: 'x' }).state, 'ready');
});

test('delete flow: a refused Undo stays deleted and says why', () => {
    const s = cardStateAfter({ state: 'undoing', undo: { branch: 'x' }, undoable: true }, { type: 'refused', reason: 'A branch with that name exists again.' });
    assert.strictEqual(s.state, 'deleted');
    assert.strictEqual(s.reason, 'A branch with that name exists again.');
});

test('withoutDeleted: a deleted branch no longer counts as existing', () => {
    const data = { cards: [{ kind: 'local', name: 'a' }, { kind: 'local', name: 'b' }] };
    const out = withoutDeleted(data, { [localKey('a')]: { state: 'deleted' } });
    assert.deepStrictEqual(out.cards.map((c) => c.name), ['b']);
    assert.strictEqual(resolveTyped('a', out).pick.kind, 'new');
});
