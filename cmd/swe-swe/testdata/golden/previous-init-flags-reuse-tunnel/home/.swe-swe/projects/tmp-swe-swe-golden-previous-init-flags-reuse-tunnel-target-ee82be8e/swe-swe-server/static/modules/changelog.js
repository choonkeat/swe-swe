/**
 * CHANGELOG.md parsing for the homepage "What's new" panel.
 * Pure, side-effect free, no dependencies beyond semver.
 * @module changelog
 */
import { isNewer } from './semver.js';

const RELEASE_RE = /^##\s+v?(\d+\.\d+\.\d+[^\s]*)\s*(?:-\s*(.*))?$/;

/**
 * Parse CHANGELOG.md text into releases, newest first (file order).
 * Only "## vX.Y.Z - Title" headings start a release; anything else at that
 * level (e.g. "## Unreleased") is skipped along with its body.
 * @param {string} text
 * @param {number} [limit] stop after this many releases
 * @returns {{version: string, title: string, sections: {heading: string, items: string[]}[]}[]}
 */
export function parseChangelog(text, limit) {
    const releases = [];
    let release = null;
    let section = null;
    const lines = String(text || '').split(/\r?\n/);
    for (const raw of lines) {
        const line = raw.trimEnd();
        if (/^##\s/.test(line)) {
            const m = line.match(RELEASE_RE);
            if (release) releases.push(release);
            release = null;
            section = null;
            if (limit && releases.length >= limit) break;
            if (m) release = { version: m[1], title: (m[2] || '').trim(), sections: [] };
            continue;
        }
        if (!release) continue;
        const h3 = line.match(/^###\s+(.*)$/);
        if (h3) {
            section = { heading: h3[1].trim(), items: [] };
            release.sections.push(section);
            continue;
        }
        const item = line.match(/^[-*]\s+(.*)$/);
        if (item) {
            if (!section) {
                section = { heading: '', items: [] };
                release.sections.push(section);
            }
            section.items.push(item[1]);
            continue;
        }
        // an indented continuation line belongs to the previous bullet
        if (/^\s+\S/.test(raw) && section && section.items.length > 0) {
            section.items[section.items.length - 1] += ' ' + line.trim();
        }
    }
    if (release && !(limit && releases.length >= limit)) releases.push(release);
    return releases;
}

function escapeHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

/**
 * Render the small markdown subset used in CHANGELOG bullets (**bold**,
 * `code`, [text](url)) to HTML. Everything is escaped first, so the source
 * text can never inject markup. Relative link targets resolve against
 * `baseUrl`; only http(s) targets are kept as links.
 * @param {string} text
 * @param {string} baseUrl e.g. "https://github.com/owner/repo/blob/main/"
 * @returns {string}
 */
export function inlineToHtml(text, baseUrl) {
    const codes = [];
    // pull code spans out first so their contents are not treated as markup
    let s = String(text).replace(/`([^`]+)`/g, function (_, c) {
        codes.push(c);
        return '\u0000' + (codes.length - 1) + '\u0000';
    });
    s = escapeHtml(s);
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, function (whole, label, href) {
        let url;
        try {
            url = new URL(href.replace(/&amp;/g, '&'), baseUrl);
        } catch (e) {
            return label;
        }
        if (url.protocol !== 'https:' && url.protocol !== 'http:') return label;
        return '<a href="' + escapeHtml(url.href) + '" target="_blank" rel="noopener">' + label + '</a>';
    });
    return s.replace(/\u0000(\d+)\u0000/g, function (_, i) {
        return '<code>' + escapeHtml(codes[Number(i)]) + '</code>';
    });
}

/**
 * Whether the panel should show an unread dot: the newest release in the
 * changelog is newer than the newest one the user has already seen.
 * Never opened (no lastSeen) is NOT unread: a brand-new browser should not be
 * nagged about releases from before it ever saw swe-swe (same rule as
 * agent-chat's What's new). The caller seeds lastSeen on that first visit.
 * @param {string|null|undefined} newest
 * @param {string|null|undefined} lastSeen
 * @returns {boolean}
 */
export function isUnread(newest, lastSeen) {
    if (!newest) return false;
    if (!lastSeen) return false;
    return isNewer(lastSeen, newest);
}
