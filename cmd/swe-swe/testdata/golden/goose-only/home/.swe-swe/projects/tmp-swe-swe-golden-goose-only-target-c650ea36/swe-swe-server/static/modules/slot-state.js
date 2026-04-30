/**
 * Slot-state utilities. Pure functions that operate on the activeBySlot
 * map (slotId -> { tabs: string[], active: string|null }).
 * @module slot-state
 */

/**
 * Enforce the "one pane, one slot" rule across all slots. Walks slotIds in
 * order; the first slot containing a given pane keeps it, every later
 * slot has the duplicate pane removed from its tabs. If the removed pane
 * was a slot's active tab, falls back to the first remaining tab (or null).
 *
 * Mutates activeBySlot in place. Heals stale localStorage written before
 * the dedup rule existed -- without this, a bad saved state stays bad
 * across every reload because the user-driven addPaneToSlot dedup only
 * runs on user-initiated add events.
 *
 * @param {Object<string, {tabs: string[], active: string|null}>} activeBySlot
 * @param {string[]} slotIds - Order in which to walk; earliest wins.
 */
export function dedupePanesAcrossSlots(activeBySlot, slotIds) {
    const seen = new Set();
    slotIds.forEach(slotId => {
        const state = activeBySlot[slotId];
        if (!state || !state.tabs) return;
        const kept = [];
        state.tabs.forEach(p => {
            if (seen.has(p)) return;
            seen.add(p);
            kept.push(p);
        });
        if (kept.length === state.tabs.length) return;
        state.tabs = kept;
        if (!kept.includes(state.active)) {
            state.active = kept[0] || null;
        }
    });
}
