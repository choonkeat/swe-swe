/**
 * Unit tests for branch-cards.js
 * Run with: node --test branch-cards.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { groupCards, cardTags, resolveTyped, branchValueFor, samePick } from './branch-cards.js';

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
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'plain', deletable: true }, null, d), []);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'x', folder: '/w/x', deletable: true }, null, d), ['has folder']);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'x', folder: '/w/x', inUse: true }, null, d), ['has folder', 'in use']);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'origin/x', oddName: true, deletable: true }, null, d), ['odd name']);
    assert.deepStrictEqual(cardTags({ kind: 'local', name: 'main', default: true }, null, d), ['default']);
    assert.deepStrictEqual(cardTags({ kind: 'online', name: 'foo', remote: 'origin' }, null, d), ['online only']);
});

test('cardTags: "N unsaved" fills in from the check', () => {
    const d = reply();
    const c = { kind: 'local', name: 'x', deletable: true };
    assert.deepStrictEqual(cardTags(c, { unsavedCommits: 3, canTell: true }, d), ['3 unsaved']);
    assert.deepStrictEqual(cardTags(c, { unsavedCommits: 0, canTell: true }, d), []);
});

test('cardTags: workspace shows its branch, and "not main" when off the default', () => {
    const d = reply();
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: 'main' }, null, d), ['on: main']);
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: 'feat/x', notDefault: true }, null, d), ['on: feat/x', 'not main']);
    assert.deepStrictEqual(cardTags({ kind: 'workspace', name: '', notDefault: true }, null, d), ['on: no branch', 'not main']);
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
