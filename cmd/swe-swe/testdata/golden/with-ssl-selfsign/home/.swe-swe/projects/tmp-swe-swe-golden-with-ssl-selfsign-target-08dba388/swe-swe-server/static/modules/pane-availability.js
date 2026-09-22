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
