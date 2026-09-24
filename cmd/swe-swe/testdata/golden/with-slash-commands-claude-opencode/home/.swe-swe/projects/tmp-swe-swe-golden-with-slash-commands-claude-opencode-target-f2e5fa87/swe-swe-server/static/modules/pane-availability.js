/**
 * Does this session HAVE a given pane at all?
 *
 * Distinct from "can this browser reach the pane's fast cross-origin form",
 * which is proxy-base.js's job. The two were tangled: Agent View and Files
 * both read their EXISTENCE off their proxy port, because until every pane had
 * a same-origin path form the port was the only address they had. In
 * single-port mode no proxy port is advertised at all (see the server's
 * single_port.go), so a pane gated that way vanishes from the tab bar instead
 * of falling back to its path form.
 *
 * Existence therefore comes from a signal of its own -- agentViewAvailable for
 * Agent View, the real md-serve port for Files -- and reachability stays with
 * the probe.
 *
 * @module pane-availability
 */

/**
 * Agent View exists when the backend says a browser can be started for this
 * session (agentViewAvailable, broadcast on every status frame) and we know
 * the session uuid, which the same-origin path form /proxy/{uuid}/vnc/ needs.
 *
 * Undefined means no status frame has arrived yet: unknown, not present.
 * Announcing a tab and then withdrawing it reads as a glitch.
 *
 * @param {object} [state]
 * @param {boolean} [state.agentViewAvailable]
 * @param {string} [state.uuid]
 * @returns {boolean}
 */
export function agentViewKnown({ agentViewAvailable, uuid } = {}) {
    return agentViewAvailable === true && !!uuid;
}

/**
 * The Files pane exists when this session has an md-serve of its own
 * (filesPort, the real target port -- not its proxy) and we know the uuid for
 * the same-origin path form /proxy/{uuid}/files/.
 *
 * @param {object} [state]
 * @param {number} [state.filesPort]
 * @param {string} [state.uuid]
 * @returns {boolean}
 */
export function filesPaneKnown({ filesPort, uuid } = {}) {
    return !!filesPort && !!uuid;
}

/**
 * What to do with the Preview tab when the user's app answers on $PORT.
 *
 * The first time the app answers, Preview is brought to the front: added to
 * its home slot if it is not in the layout, or switched to if it sits behind
 * another tab -- the same spirit as Agent View appearing when the agent's
 * browser starts. Only once per page: if the user switches away after that,
 * it stays that way. Never inside the embedded (iframe-in-iframe) view.
 *
 * @param {object} [state]
 * @param {boolean} [state.appUp] the app answered on $PORT
 * @param {boolean} [state.inLayout] Preview already sits in some slot
 * @param {boolean} [state.showing] Preview is the active tab of its slot
 * @param {boolean} [state.alreadyDone] this page already did it once
 * @param {boolean} [state.embedded] page is embedded in another page's iframe
 * @returns {'add'|'switch'|null}
 */
export function previewRevealAction({ appUp, inLayout, showing, alreadyDone, embedded } = {}) {
    if (appUp !== true || alreadyDone || embedded) return null;
    if (!inLayout) return 'add';
    return showing ? null : 'switch';
}
