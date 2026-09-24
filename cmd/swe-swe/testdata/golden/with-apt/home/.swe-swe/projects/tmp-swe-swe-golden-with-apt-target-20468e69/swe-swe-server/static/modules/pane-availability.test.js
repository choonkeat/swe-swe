/**
 * Unit tests for pane-availability.js
 * Run with: node --test pane-availability.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { agentViewKnown, agentViewLive, agentViewUnavailablePath, filesPaneKnown, previewRevealAction } from './pane-availability.js';

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

// When Agent View cannot work, the tab stays and explains why instead of
// vanishing -- people who skipped the browser at setup never learned what
// they gave up.
test('agentViewKnown: true when the server says why it is unavailable', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, agentViewReason: 'off', uuid: 'abc' }), true);
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, agentViewReason: 'missing', uuid: 'abc' }), true);
});

// Older servers send no reason: keep hiding the tab as before.
test('agentViewKnown: false when unavailable with no reason', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, agentViewReason: '', uuid: 'abc' }), false);
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, agentViewReason: 'bogus', uuid: 'abc' }), false);
});

test('agentViewKnown: a reason still needs the session uuid', () => {
    assert.strictEqual(agentViewKnown({ agentViewAvailable: false, agentViewReason: 'off' }), false);
});

// The live viewer URL must not be built for a tab that only explains.
test('agentViewLive: only when the backend says it is available', () => {
    assert.strictEqual(agentViewLive({ agentViewAvailable: true, uuid: 'abc' }), true);
    assert.strictEqual(agentViewLive({ agentViewAvailable: false, agentViewReason: 'off', uuid: 'abc' }), false);
    assert.strictEqual(agentViewLive({ agentViewAvailable: true }), false);
    assert.strictEqual(agentViewLive(), false);
});

test('agentViewUnavailablePath: switched off', () => {
    assert.strictEqual(agentViewUnavailablePath({ agentViewReason: 'off' }), 'agent-view-unavailable.html?reason=off');
});

test('agentViewUnavailablePath: missing programs are listed', () => {
    assert.strictEqual(
        agentViewUnavailablePath({ agentViewReason: 'missing', agentViewMissing: ['chromium', 'x11vnc'] }),
        'agent-view-unavailable.html?reason=missing&missing=chromium%2Cx11vnc',
    );
    assert.strictEqual(agentViewUnavailablePath({ agentViewReason: 'missing', agentViewMissing: [] }), 'agent-view-unavailable.html?reason=missing');
});

test('agentViewUnavailablePath: null when live or no reason', () => {
    assert.strictEqual(agentViewUnavailablePath({ agentViewReason: '' }), null);
    assert.strictEqual(agentViewUnavailablePath(), null);
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

/// The first time the user's app answers on $PORT, Preview comes to the front,
// the way Agent View appears when the agent's browser starts.
test('previewRevealAction: adds Preview when it is not in the layout', () => {
    assert.strictEqual(previewRevealAction({ appUp: true }), 'add');
});

test('previewRevealAction: switches to Preview when it sits behind another tab', () => {
    assert.strictEqual(previewRevealAction({ appUp: true, inLayout: true, showing: false }), 'switch');
});

test('previewRevealAction: nothing when Preview is already showing', () => {
    assert.strictEqual(previewRevealAction({ appUp: true, inLayout: true, showing: true }), null);
});

test('previewRevealAction: nothing while the app is down or unknown', () => {
    assert.strictEqual(previewRevealAction({ appUp: false }), null);
    assert.strictEqual(previewRevealAction({}), null);
});

// Once per page: a user who switches away from Preview keeps it that way,
// even when a dev server restarts and the app comes up again.
test('previewRevealAction: only once per page', () => {
    assert.strictEqual(previewRevealAction({ appUp: true, alreadyDone: true }), null);
    assert.strictEqual(previewRevealAction({ appUp: true, inLayout: true, alreadyDone: true }), null);
});

test('previewRevealAction: never inside the embedded view', () => {
    assert.strictEqual(previewRevealAction({ appUp: true, embedded: true }), null);
    assert.strictEqual(previewRevealAction({ appUp: true, inLayout: true, embedded: true }), null);
});
