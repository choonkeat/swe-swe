// What's new: a header button that opens the latest CHANGELOG.md releases,
// with an unread dot until the user has opened it for the newest release.
// Looks and behaves like agent-chat's What's new (sparkle icon, red dot,
// Done button, no dot on a brand-new browser).
//
// Like update-check.js this is a browser-side fetch: raw.githubusercontent.com
// sends "access-control-allow-origin: *" and "cache-control: max-age=300", so
// no Go handler is needed. It fails silent: offline or unparseable leaves the
// button hidden.
//
// "Seen" is remembered in localStorage. Inside a cross-origin iframe some
// browsers partition that storage per embedding site, and some may refuse it
// outright; either way the worst case is the dot showing again, never an error.
import { parseChangelog, inlineToHtml, isUnread } from './modules/changelog.js';
import { compareVersions } from './modules/semver.js';

const CHANGELOG_RAW_URL = 'https://raw.githubusercontent.com/choonkeat/swe-swe/main/CHANGELOG.md';
const REPO_BLOB_BASE = 'https://github.com/choonkeat/swe-swe/blob/main/';
const CHANGELOG_URL = REPO_BLOB_BASE + 'CHANGELOG.md';
const SEEN_KEY = 'swe-swe:whats-new:last-seen';
const RELEASES_SHOWN = 3;

function readSeen() {
    try {
        return window.localStorage.getItem(SEEN_KEY);
    } catch (e) {
        return null;
    }
}

function writeSeen(version) {
    try {
        window.localStorage.setItem(SEEN_KEY, version);
    } catch (e) { /* storage blocked: the dot just comes back next load */ }
}

function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
}

function renderRelease(rel, current) {
    const item = el('section', 'whats-new__release');
    const head = el('div', 'whats-new__release-head');
    head.appendChild(el('span', 'whats-new__version', 'v' + rel.version));
    if (rel.title) head.appendChild(el('span', 'whats-new__title', rel.title));
    if (current && current !== 'dev') {
        const cmp = compareVersions(rel.version, current);
        if (cmp === 0) head.appendChild(el('span', 'whats-new__tag whats-new__tag--current', 'your version'));
        else if (cmp > 0) head.appendChild(el('span', 'whats-new__tag whats-new__tag--newer', 'not installed yet'));
    }
    item.appendChild(head);

    for (const sec of rel.sections) {
        if (sec.heading) item.appendChild(el('div', 'whats-new__section', sec.heading));
        const ul = el('ul', 'whats-new__list');
        for (const text of sec.items) {
            const li = document.createElement('li');
            // inlineToHtml escapes everything before adding its own tags
            li.innerHTML = inlineToHtml(text, REPO_BLOB_BASE);
            ul.appendChild(li);
        }
        item.appendChild(ul);
    }
    return item;
}

function mount(wrap, releases) {
    const current = wrap.dataset.currentVersion;
    const newest = releases[0].version;

    // Same sparkle icon + red dot as agent-chat's What's new button
    const button = el('button', 'whats-new-btn');
    button.type = 'button';
    button.title = "What's new in swe-swe";
    button.setAttribute('aria-label', "What's new");
    button.setAttribute('aria-haspopup', 'dialog');
    button.setAttribute('aria-expanded', 'false');
    button.innerHTML = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" ' +
        'stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M8 1.8l1.5 4.2 4.2 1.5-4.2 1.5L8 13.2 6.5 9 2.3 7.5 6.5 6z"/>' +
        '<path d="M13 1.8v2.4M11.8 3h2.4"/></svg>';
    const dot = el('span', 'whats-new-btn__dot');
    button.appendChild(dot);

    const panel = el('div', 'whats-new');
    panel.setAttribute('role', 'dialog');
    panel.setAttribute('aria-label', "What's new in swe-swe");
    panel.hidden = true;
    const title = el('div', 'whats-new__heading', "What's new ");
    if (current && current !== 'dev') title.appendChild(el('span', 'whats-new__scope', 'you are on v' + current));
    panel.appendChild(title);
    const body = el('div', 'whats-new__body');
    for (const rel of releases) body.appendChild(renderRelease(rel, current));
    panel.appendChild(body);
    const actions = el('div', 'whats-new__actions');
    const foot = el('a', 'whats-new__more');
    foot.href = CHANGELOG_URL;
    foot.target = '_blank';
    foot.rel = 'noopener';
    foot.innerHTML = 'Full changelog &rarr;';
    const done = el('button', 'whats-new__done', 'Done');
    done.type = 'button';
    actions.appendChild(foot);
    actions.appendChild(done);
    panel.appendChild(actions);

    function syncDot() {
        const unread = isUnread(newest, readSeen());
        dot.hidden = !unread;
        button.classList.toggle('whats-new-btn--unread', unread);
    }

    function close() {
        panel.hidden = true;
        button.setAttribute('aria-expanded', 'false');
        button.classList.remove('whats-new-btn--active');
    }

    function open() {
        panel.hidden = false;
        button.setAttribute('aria-expanded', 'true');
        button.classList.add('whats-new-btn--active');
        writeSeen(newest);
        syncDot();
    }

    done.addEventListener('click', function () {
        close();
        button.focus();
    });
    button.addEventListener('click', function (e) {
        e.stopPropagation();
        if (panel.hidden) open(); else close();
    });
    document.addEventListener('click', function (e) {
        if (!panel.hidden && !wrap.contains(e.target)) close();
    });
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && !panel.hidden) {
            close();
            button.focus();
        }
    });
    // another tab of this homepage opened the panel: clear the dot here too
    window.addEventListener('storage', function (e) {
        if (e.key === SEEN_KEY) syncDot();
    });

    // first visit in this browser: remember the current newest release
    // quietly, so only releases published after today light the dot up
    if (!readSeen()) writeSeen(newest);

    wrap.appendChild(button);
    wrap.appendChild(panel);
    syncDot();
}

function loadWhatsNew() {
    const wrap = document.getElementById('whats-new-wrap');
    if (!wrap) return;
    fetch(CHANGELOG_RAW_URL, { credentials: 'omit' })
        .then(function (res) { return res.ok ? res.text() : ''; })
        .then(function (text) {
            const releases = parseChangelog(text, RELEASES_SHOWN);
            if (releases.length > 0) mount(wrap, releases);
        })
        .catch(function () { /* offline or blocked: leave the button hidden */ });
}

// module scripts are deferred, so the header is already parsed by now
loadWhatsNew();
