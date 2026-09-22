/**
 * Semantic version comparison, per https://semver.org precedence rules.
 * Pure, side-effect free, no dependencies.
 * @module semver
 */

/**
 * Split "1.2.3-rc.1+build.5" into its core numbers and prerelease identifiers.
 * Build metadata is discarded: semver says it is ignored for precedence.
 * @param {string} version
 * @returns {{core: number[], pre: string[]}}
 */
function parse(version) {
    const text = String(version).trim().replace(/^v/, '').split('+')[0];
    const dash = text.indexOf('-');
    const coreText = dash === -1 ? text : text.slice(0, dash);
    const preText = dash === -1 ? '' : text.slice(dash + 1);
    const core = coreText.split('.').map(function (n) {
        const v = parseInt(n, 10);
        return isNaN(v) ? 0 : v;
    });
    return { core: core, pre: preText === '' ? [] : preText.split('.') };
}

/**
 * Compare two prerelease identifier lists per semver rule 11.4.
 * An empty list means "not a prerelease", which outranks any prerelease.
 * @param {string[]} a
 * @param {string[]} b
 * @returns {number} -1, 0 or 1
 */
function comparePrerelease(a, b) {
    // 1.0.0 > 1.0.0-rc1: a release outranks its own prereleases
    if (a.length === 0 && b.length === 0) return 0;
    if (a.length === 0) return 1;
    if (b.length === 0) return -1;

    for (let i = 0; i < Math.max(a.length, b.length); i++) {
        // a longer set of identifiers wins when all the earlier ones are equal
        if (i >= a.length) return -1;
        if (i >= b.length) return 1;

        const ia = a[i];
        const ib = b[i];
        const numA = /^\d+$/.test(ia);
        const numB = /^\d+$/.test(ib);
        if (numA && numB) {
            const na = parseInt(ia, 10);
            const nb = parseInt(ib, 10);
            if (na !== nb) return na < nb ? -1 : 1;
        } else if (numA !== numB) {
            // numeric identifiers always have lower precedence than alphanumeric
            return numA ? -1 : 1;
        } else if (ia !== ib) {
            return ia < ib ? -1 : 1;
        }
    }
    return 0;
}

/**
 * Compare two versions by semver precedence.
 * @param {string} a
 * @param {string} b
 * @returns {number} -1 when a < b, 0 when equal, 1 when a > b
 */
export function compareVersions(a, b) {
    const pa = parse(a);
    const pb = parse(b);
    for (let i = 0; i < Math.max(pa.core.length, pb.core.length); i++) {
        const na = pa.core[i] || 0;
        const nb = pb.core[i] || 0;
        if (na !== nb) return na < nb ? -1 : 1;
    }
    return comparePrerelease(pa.pre, pb.pre);
}

/**
 * Whether `latest` is a newer version than `current`.
 * @param {string} current
 * @param {string} latest
 * @returns {boolean}
 */
export function isNewer(current, latest) {
    return compareVersions(latest, current) > 0;
}
