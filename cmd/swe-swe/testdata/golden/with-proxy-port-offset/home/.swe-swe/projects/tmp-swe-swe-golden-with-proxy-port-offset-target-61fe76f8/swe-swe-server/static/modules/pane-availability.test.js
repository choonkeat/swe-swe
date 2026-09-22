/**
 * Unit tests for pane-availability.js
 * Run with: node --test pane-availability.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { agentViewKnown, filesPaneKnown } from './pane-availability.js';

// Single-port mode advertises no proxy ports at all, so a pane whose EXISTENCE
// is read off its proxy port disappears instead of falling back to its
// same-origin path form. These two helpers are the separation: existence comes
// from a signal of its own, reachability from the probe.

test('agentViewKnown: available with no proxy port in sight', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: true, uuid: 'abc' }), true);
});

test('agentViewKnown: false when the backend says the pane does not exist', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, uuid: 'abc' }), false);
});

// Before the first status frame the flag is undefined. The pane is unknown
// then, not assumed present: showing the tab and taking it away again is worse
// than showing it a beat later.
test('agentViewKnown: false before the first status frame', () => {
    assert.strictEqual(agentViewKnown({ uuid: 'abc' }), false);
});

// The path form is /proxy/{uuid}/vnc/, so without a uuid there is no URL to
// give the iframe.
test('agentViewKnown: false with no session uuid', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: true, uuid: '' }), false);
    assert.strictEqual(agentViewKnown({ agentViewAvailable: true }), false);
});

test('agentViewKnown: tolerates no argument', () => {
    assert.strictEqual(agentViewKnown(), false);
});

// Files is the other pane whose tab was gated on a proxy port. Its existence
// signal is filesPort -- the real md-serve port, which single-port mode keeps
// advertising because md-serve really is running there.
test('filesPaneKnown: true on the real files port alone', () => {
    assert.strictEqual(filesPaneKnown({ filesPort: 9000, uuid: 'abc' }), true);
});

test('filesPaneKnown: false with no files port', () => {
    assert.strictEqual(filesPaneKnown({ filesPort: 0, uuid: 'abc' }), false);
    assert.strictEqual(filesPaneKnown({ uuid: 'abc' }), false);
});

test('filesPaneKnown: false with no session uuid', () => {
    assert.strictEqual(filesPaneKnown({ filesPort: 9000, uuid: '' }), false);
});

test('filesPaneKnown: tolerates no argument', () => {
    assert.strictEqual(filesPaneKnown(), false);
});
