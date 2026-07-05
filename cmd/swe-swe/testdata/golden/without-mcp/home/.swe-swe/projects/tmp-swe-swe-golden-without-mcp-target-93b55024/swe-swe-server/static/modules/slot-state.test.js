/**
 * Unit tests for slot-state.js
 * Run with: node --test slot-state.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { dedupePanesAcrossSlots } from './slot-state.js';

test('no-op when each pane appears in exactly one slot', () => {
    const state = {
        a: { tabs: ['agent-terminal'], active: 'agent-terminal' },
        b: { tabs: ['preview'], active: 'preview' },
        c: { tabs: ['agent-chat'], active: 'agent-chat' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b', 'c']);
    assert.deepStrictEqual(state.a.tabs, ['agent-terminal']);
    assert.deepStrictEqual(state.b.tabs, ['preview']);
    assert.deepStrictEqual(state.c.tabs, ['agent-chat']);
});

test('first slot wins when a pane is duplicated', () => {
    const state = {
        a: { tabs: ['agent-terminal', 'preview'], active: 'agent-terminal' },
        b: { tabs: ['preview', 'agent-chat'], active: 'preview' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.a.tabs, ['agent-terminal', 'preview']);
    assert.deepStrictEqual(state.b.tabs, ['agent-chat']);
});

test('falls back to first remaining tab when removed pane was active', () => {
    const state = {
        a: { tabs: ['preview'], active: 'preview' },
        b: { tabs: ['preview', 'agent-chat'], active: 'preview' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.b.tabs, ['agent-chat']);
    assert.strictEqual(state.b.active, 'agent-chat',
        'active should fall back when its pane got deduped away');
});

test('falls back to null when slot tabs become empty', () => {
    const state = {
        a: { tabs: ['preview'], active: 'preview' },
        b: { tabs: ['preview'], active: 'preview' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.b.tabs, []);
    assert.strictEqual(state.b.active, null);
});

test('preserves active when active is not the duplicate', () => {
    const state = {
        a: { tabs: ['preview'], active: 'preview' },
        b: { tabs: ['agent-chat', 'preview'], active: 'agent-chat' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.b.tabs, ['agent-chat']);
    assert.strictEqual(state.b.active, 'agent-chat');
});

test('honors slot order -- earlier in slotIds wins regardless of map order', () => {
    const state = {
        b: { tabs: ['preview'], active: 'preview' },
        a: { tabs: ['preview'], active: 'preview' },
    };
    // Walk a before b -- a wins.
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.a.tabs, ['preview']);
    assert.deepStrictEqual(state.b.tabs, []);
});

test('handles missing slot state gracefully', () => {
    const state = {
        a: { tabs: ['preview'], active: 'preview' },
        // b is missing entirely (preset declared a slot the saved state didn't have)
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.a.tabs, ['preview']);
});

test('handles slot with missing tabs property', () => {
    const state = {
        a: { tabs: ['preview'], active: 'preview' },
        b: { active: null }, // no tabs array
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    // No throw, no mutation of b.
    assert.deepStrictEqual(state.a.tabs, ['preview']);
});

test('multiple panes deduped from a single later slot', () => {
    const state = {
        a: { tabs: ['agent-terminal', 'preview', 'agent-chat'], active: 'agent-terminal' },
        b: { tabs: ['preview', 'agent-chat', 'shell'], active: 'preview' },
    };
    dedupePanesAcrossSlots(state, ['a', 'b']);
    assert.deepStrictEqual(state.b.tabs, ['shell']);
    assert.strictEqual(state.b.active, 'shell');
});
