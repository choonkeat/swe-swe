// Update check: ask the npm registry whether a newer swe-swe has been published
// and, if so, render a badge next to the version stamp in the header.
//
// This is a browser-side fetch, not a server-side one: registry.npmjs.org sends
// "access-control-allow-origin: *" and "cache-control: max-age=300", so the
// browser does the request and the caching for us -- no Go handler, no cache to
// invalidate. The check fails silent: no network, offline box, or an
// unparseable response simply leaves the badge hidden.
import { isNewer } from './modules/semver.js';

const NPM_LATEST_URL = 'https://registry.npmjs.org/swe-swe/latest';
const RELEASE_NOTES_URL = 'https://github.com/choonkeat/swe-swe/blob/main/CHANGELOG.md';

function renderBadge(wrap, current, latest) {
    const badge = document.createElement('a');
    badge.className = 'update-badge';
    badge.href = RELEASE_NOTES_URL;
    badge.target = '_blank';
    badge.rel = 'noopener';
    badge.innerHTML = '<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">' +
        '<path d="M12 19V5M12 5L6 11M12 5l6 6" stroke="currentColor" stroke-width="3" ' +
        'stroke-linecap="round" stroke-linejoin="round"/></svg>';
    badge.appendChild(document.createTextNode(latest + ' available'));

    const tip = document.createElement('span');
    tip.className = 'update-tip';
    const line = document.createElement('span');
    line.className = 'update-tip__line';
    line.appendChild(document.createTextNode('swe-swe '));
    const strong = document.createElement('strong');
    strong.textContent = latest;
    line.appendChild(strong);
    line.appendChild(document.createTextNode(' is out. You are on ' + current + '.'));
    const cmd = document.createElement('span');
    cmd.className = 'update-tip__cmd';
    cmd.textContent = 'npx swe-swe@latest up';
    const link = document.createElement('a');
    link.className = 'update-tip__link';
    link.href = RELEASE_NOTES_URL;
    link.target = '_blank';
    link.rel = 'noopener';
    link.innerHTML = 'Release notes &rarr;';
    tip.appendChild(line);
    tip.appendChild(cmd);
    tip.appendChild(link);

    wrap.appendChild(badge);
    wrap.appendChild(tip);
}

function checkForUpdate() {
    const wrap = document.getElementById('update-badge-wrap');
    if (!wrap) return;
    const current = wrap.dataset.currentVersion;
    // "dev" builds are not on npm, so there is nothing meaningful to compare
    if (!current || current === 'dev') return;

    fetch(NPM_LATEST_URL, { credentials: 'omit' })
        .then(function (res) { return res.ok ? res.json() : null; })
        .then(function (data) {
            if (!data || !data.version) return;
            if (!isNewer(current, data.version)) return;
            renderBadge(wrap, current, data.version);
        })
        .catch(function () { /* offline or blocked: leave the badge hidden */ });
}

// module scripts are deferred, so the header is already parsed by now
checkForUpdate();
