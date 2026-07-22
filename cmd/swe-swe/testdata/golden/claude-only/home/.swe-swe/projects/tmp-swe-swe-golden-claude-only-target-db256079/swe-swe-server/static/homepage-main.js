// Homepage main functionality
// CSP-compliant external script for selection.html

// Detect iOS Safari and show warning for self-signed cert scenarios
(function() {
    var ua = navigator.userAgent;
    var isIOS = /iPad|iPhone|iPod/.test(ua);
    var isSafari = /Safari/.test(ua) && !/Chrome|CriOS|FxiOS|EdgiOS/.test(ua);
    var isHTTPS = location.protocol === 'https:';

    if (isIOS && isSafari && isHTTPS) {
        var warning = document.getElementById('ios-safari-warning');
        if (warning) {
            warning.style.display = 'block';
        }
    }
})();

// Use embedded rendering for recordings on mobile devices.
(function() {
    var isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent) ||
                   (navigator.maxTouchPoints > 0 && window.innerWidth < 1024);
    if (isMobile) {
        document.querySelectorAll('a.recording-card__play, a.btn-view').forEach(function(link) {
            if (link.href && link.href.indexOf('?') === -1) {
                link.href += '?render=embedded';
            }
        });
    }
})();

function setButtonLoading(button, loading) {
    if (!button) return;
    if (loading) {
        button.classList.add('is-loading');
        button.setAttribute('aria-busy', 'true');
        if ('disabled' in button) { button.disabled = true; }
    } else {
        button.classList.remove('is-loading');
        button.removeAttribute('aria-busy');
        if ('disabled' in button) { button.disabled = false; }
    }
}

// Ending a session throws away its chat log by default -- the file survives on
// disk but nothing prompts you to keep it, and once the agent is dead nothing
// can scrub or commit it either. So when there IS an uncommitted log, offer the
// choice; when there isn't, stay out of the way with the plain confirm.
function endSession(uuid, button) {
    fetchChatLogStatus(uuid).then(function(info) {
        if (info && info.enabled && info.exists && !info.committed) {
            openEndSessionDialog(uuid, button, info);
            return;
        }
        if (confirm('End this session?')) {
            doEndSessionFromCard(uuid, button, '');
        }
    });
}

function openEndSessionDialog(uuid, button, info) {
    var overlay = document.getElementById('end-session-dialog-overlay');
    if (!overlay) { // template without the dialog: fall back to the old flow
        if (confirm('End this session?')) { doEndSessionFromCard(uuid, button, ''); }
        return;
    }
    var name = document.getElementById('end-dialog-logname');
    if (name) {
        name.textContent = (info.path || '').split('/').pop() +
            (info.titled ? '' : '  (untitled)');
    }

    function close() {
        overlay.style.display = 'none';
        document.removeEventListener('keydown', onKey);
    }
    function onKey(e) {
        if (e.key === 'Escape') { close(); }
    }

    overlay.querySelectorAll('.end-dialog__option').forEach(function(btn) {
        btn.onclick = function() {
            close();
            doEndSessionFromCard(uuid, button, btn.dataset.chatlog || '');
        };
    });
    var closeBtn = document.getElementById('end-dialog-close');
    if (closeBtn) { closeBtn.onclick = close; }
    overlay.onclick = function(e) { if (e.target === overlay) { close(); } };
    document.addEventListener('keydown', onKey);

    overlay.style.display = 'flex';
}

function doEndSessionFromCard(uuid, button, chatlog) {
    var publicPort = parseInt(button.dataset.publicPort, 10) || 0;
    checkPublicPortAndEndSession({
        chatlog: chatlog,
        uuid: uuid,
        publicPort: publicPort,
        onStart: function() {
            setButtonLoading(button, true);
        },
        onSuccess: function(mode) {
            if (mode === 'commit') {
                // Nothing is ending yet: the agent is scrubbing and committing,
                // and will end the session itself when it lands. Saying
                // "Ending..." here would be a lie, and greying the card out
                // would hide a session the user may want to watch -- so the
                // card gets its own banner instead, keeping Join and dropping
                // only End.
                setButtonLoading(button, false);
                markCardCommitting(sessionCard(uuid));
                return;
            }
            // The end request is only accepted, not finished. Flip the card to
            // its inert "Ending..." state now and let the live poll remove it
            // once teardown actually completes -- a reload here would just
            // re-render the same card, still mid-teardown.
            markSessionCardEnding(uuid);
            pollLiveSessions();
        },
        onError: function(msg) {
            setButtonLoading(button, false);
            alert(msg);
        }
    });
}

function sessionCard(uuid) {
    return document.querySelector('.session-card[data-session-uuid="' + uuid + '"]');
}

function markSessionCardEnding(uuid) {
    var card = sessionCard(uuid);
    if (card) { card.classList.add('session-card--ending'); }
}

// "Commit the log, then end" was chosen: the session is still live and
// joinable, so both buttons stay and the only change is the "(Ending)" suffix
// on the Join label. Once the agent actually ends itself the live poll adds
// --ending on top, which hides Join entirely and shows "Ending...".
// Takes the element, not a uuid: the poll already has it in hand.
var COMMITTING_HINT = "The agent is scrubbing and committing this session's chat log; it ends itself when that lands.";
function markCardCommitting(card) {
    if (!card) { return; }
    card.classList.add('session-card--committing');
    // Server-rendered cards carry this already; ones flipped by the poll or by
    // this tab's own End press do not.
    var join = card.querySelector('.btn-join');
    if (join && !join.title) { join.title = COMMITTING_HINT; }
}

// Reconcile the rendered session cards against the server's live set: flag the
// ones being torn down, drop the ones that are gone. The homepage is otherwise
// server-rendered with no polling, so without this an ended session's card
// would linger until the user reloaded by hand.
var liveSessionsPollTimer = null;

// Replace the whole page with a "waiting for the server" screen and poll until
// it answers again, then reload. Two-phase on purpose: we only reload after we
// have first SEEN the server go away (sawDown), so a graceful shutdown that
// keeps answering 200 for a moment does not bounce us straight back into a
// server that is on its way out. Used by the Shut-down button and, after a run
// of failed live polls, by a reboot triggered from elsewhere (MCP tool, CLI).
var serverDownScreenActive = false;
function showServerDownAndPoll(message) {
    if (serverDownScreenActive) { return; }
    serverDownScreenActive = true;
    if (liveSessionsPollTimer) { clearInterval(liveSessionsPollTimer); liveSessionsPollTimer = null; }
    document.body.innerHTML =
        '<div style="display:flex; flex-direction:column; gap:10px; align-items:center; ' +
        'justify-content:center; height:100vh; font-family:inherit; color:var(--text-secondary); ' +
        'font-size:16px; text-align:center; padding:24px;">' +
        '<div id="server-down-msg">' + message + '</div>' +
        '<div style="font-size:13px; opacity:0.65;">This page reloads itself once the server is back.</div>' +
        '</div>';
    var sawDown = false;
    function tick() {
        // fetch rejects only on a network-level failure; any HTTP status
        // (200/302/401) means the server is answering again.
        fetch('/', { cache: 'no-store' })
            .then(function() {
                if (sawDown) { window.location.reload(); return; }
                setTimeout(tick, 1000);
            })
            .catch(function() {
                if (!sawDown) {
                    sawDown = true;
                    var m = document.getElementById('server-down-msg');
                    if (m) { m.textContent = 'Waiting for the server to come back...'; }
                }
                setTimeout(tick, 2000);
            });
    }
    setTimeout(tick, 1000);
}

// Consecutive failed live-session polls. A reboot from outside the browser (the
// reboot_server MCP tool, a CLI compose down) shows up here as the poll simply
// failing; after a few in a row we assume the server is down and switch to the
// self-healing wait screen rather than leaving a stale homepage up.
var liveSessionsFailStreak = 0;
var LIVE_POLL_FAIL_THRESHOLD = 3;

function pollLiveSessions() {
    return fetch('/api/sessions/live', { headers: { 'Accept': 'application/json' } })
        .then(function(response) {
            if (!response.ok) { return null; }
            return response.json();
        })
        .then(function(body) {
            liveSessionsFailStreak = 0;
            if (!body || !body.sessions) { return; }

            var live = {};
            body.sessions.forEach(function(s) { live[s.uuid] = s; });

            var cards = document.querySelectorAll('.session-card[data-session-uuid]');
            for (var i = 0; i < cards.length; i++) {
                var card = cards[i];
                var entry = live[card.dataset.sessionUuid];
                if (!entry) {
                    card.remove();
                } else if (entry.ending) {
                    card.classList.add('session-card--ending');
                } else if (entry.endRequested) {
                    // Set on every tab, not just the one that pressed End, and
                    // re-applied after a reload -- the server owns this state.
                    markCardCommitting(card);
                }
            }
        })
        .catch(function() {
            // A single miss is transient (the next tick retries); a run of them
            // means the server is actually gone -- most likely a reboot -- so
            // hand off to the self-healing wait screen.
            if (++liveSessionsFailStreak >= LIVE_POLL_FAIL_THRESHOLD) {
                showServerDownAndPoll('The server went away -- rebooting?');
            }
        });
}

(function() {
    if (!document.querySelector('.session-card[data-session-uuid]')) { return; }
    liveSessionsPollTimer = setInterval(pollLiveSessions, 3000);
    // Catch up immediately on return to the tab rather than waiting a tick.
    document.addEventListener('visibilitychange', function() {
        if (!document.hidden) { pollLiveSessions(); }
    });
})();

// Shut down the whole server from the settings dialog. The server ends every
// session then exits; the page goes unreachable, so on success we hand off to
// the self-healing wait screen. On a plain shutdown it will sit on "shutting
// down" forever (nothing comes back); on a reboot it reloads by itself once
// the stack is up again.
(function() {
    var btn = document.getElementById('server-shutdown-btn');
    if (!btn) return;
    btn.onclick = function() {
        if (!confirm('Shut down swe-swe? All active sessions will end.')) {
            return;
        }
        btn.disabled = true;
        btn.textContent = 'Shutting down...';
        fetch('/api/server/shutdown', { method: 'POST' })
            .then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(t) { throw new Error(t || resp.status); });
                }
                showServerDownAndPoll('swe-swe is shutting down...');
            })
            .catch(function(err) {
                btn.disabled = false;
                btn.textContent = 'Shut down server';
                alert('Shutdown failed: ' + err.message);
            });
    };
})();

function deleteRecording(uuid, button) {
    if (!confirm('Delete this recording?')) {
        return;
    }
    fetch('/api/recording/' + uuid, {
        method: 'DELETE'
    }).then(function(response) {
        if (response.ok) {
            var card = button.closest('.recording-card');
            if (card) {
                card.remove();
            }
        } else {
            alert('Failed to delete recording');
        }
    }).catch(function(err) {
        alert('Error: ' + err.message);
    });
}

function keepRecording(uuid, button) {
    fetch('/api/recording/' + uuid + '/keep', {
        method: 'POST'
    }).then(function(response) {
        if (response.ok) {
            var card = button.closest('.recording-card');
            if (card) {
                // Update status text
                var statusEl = card.querySelector('.recording-card__status');
                if (statusEl) {
                    statusEl.textContent = 'Saved';
                    statusEl.classList.remove('recording-card__status--expires');
                    statusEl.classList.add('recording-card__status--saved');
                }
                // Update keep button to show filled bookmark
                var keepBtn = card.querySelector('.btn-icon--keep');
                if (keepBtn) {
                    keepBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M19 21L12 16L5 21V5C5 3.89543 5.89543 3 7 3H17C18.1046 3 19 3.89543 19 5V21Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" fill="currentColor"/></svg>';
                    keepBtn.title = 'Kept indefinitely';
                    keepBtn.onclick = null;
                }
            }
        } else {
            alert('Failed to keep recording');
        }
    }).catch(function(err) {
        alert('Error: ' + err.message);
    });
}

function renameRecording(uuid, button) {
    var card = button.closest('.recording-card');
    var titleEl = card ? card.querySelector('.recording-card__title') : null;
    var currentName = titleEl ? titleEl.textContent.trim() : '';

    var newName = prompt('Rename recording:', currentName);
    if (newName === null) {
        return; // User cancelled
    }

    newName = newName.trim();

    // Validate: max 256 chars
    if (newName.length > 256) {
        alert('Name too long (max 256 characters)');
        return;
    }

    fetch('/api/recording/' + uuid + '/rename', {
        method: 'PATCH',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ name: newName })
    }).then(function(response) {
        if (response.ok) {
            if (titleEl) {
                titleEl.textContent = newName || 'session-' + uuid.substring(0, 8);
            }
        } else {
            response.text().then(function(text) {
                alert('Failed to rename recording: ' + text);
            });
        }
    }).catch(function(err) {
        alert('Error: ' + err.message);
    });
}

// Open the New Session dialog pre-filled with a recording's settings
// (assistant, repo, branch, name, extra args) so the user can tweak any of
// them before starting, instead of creating the session immediately.
function newSessionFromRecording(btn) {
    var headerBtn = document.getElementById('btn-new-session');
    openNewSessionDialog(
        btn.dataset.assistant || '',
        headerBtn ? headerBtn.dataset.uuid : '',
        btn.dataset.debug === '1',
        {
            repoPath: btn.dataset.pwd || '',
            branch: btn.dataset.branch || '',
            branchHint: btn.dataset.branchHint || '',
            name: btn.dataset.name || '',
            extraArgs: btn.dataset.extraArgs || ''
        }
    );
}

// Event listeners for buttons (CSP-compliant - no inline handlers)
document.addEventListener('DOMContentLoaded', function() {
    // New Session button
    var newSessionBtn = document.getElementById('btn-new-session');
    if (newSessionBtn) {
        newSessionBtn.addEventListener('click', function() {
            var uuid = this.dataset.uuid;
            var debug = this.dataset.debug === 'true';
            openNewSessionDialog('', uuid, debug);
        });
    }

    // Join Session anchor: show loading spinner while navigation begins
    document.addEventListener('click', function(e) {
        var join = e.target.closest('.btn-join');
        if (join && !join.classList.contains('is-loading')) {
            setButtonLoading(join, true);
        }
    });

    // Event delegation for session and recording actions
    document.addEventListener('click', function(e) {
        var btn = e.target.closest('[data-action]');
        if (!btn) return;

        var action = btn.dataset.action;
        var uuid = btn.dataset.uuid;

        if (action === 'end-session') {
            endSession(uuid, btn);
        } else if (action === 'delete-recording') {
            deleteRecording(uuid, btn);
        } else if (action === 'keep-recording') {
            keepRecording(uuid, btn);
        } else if (action === 'rename-recording') {
            renameRecording(uuid, btn);
        } else if (action === 'new-from-recording') {
            newSessionFromRecording(btn);
        }
    });
});

// Update check: ask the npm registry whether a newer swe-swe has been published
// and, if so, render a badge next to the version stamp in the header.
//
// This is a browser-side fetch, not a server-side one: registry.npmjs.org sends
// "access-control-allow-origin: *" and "cache-control: max-age=300", so the
// browser does the request and the caching for us -- no Go handler, no cache to
// invalidate. The check fails silent: no network, offline box, or an
// unparseable response simply leaves the badge hidden.
(function() {
    var NPM_LATEST_URL = 'https://registry.npmjs.org/swe-swe/latest';
    var RELEASE_NOTES_URL = 'https://github.com/choonkeat/swe-swe/blob/main/CHANGELOG.md';

    // Compare dotted numeric versions. Returns true when b is newer than a.
    // A "-rc1"-style suffix is dropped before comparing, so a prerelease never
    // reads as newer than the release it precedes.
    function isNewer(a, b) {
        var pa = String(a).split('-')[0].split('.');
        var pb = String(b).split('-')[0].split('.');
        for (var i = 0; i < Math.max(pa.length, pb.length); i++) {
            var na = parseInt(pa[i], 10) || 0;
            var nb = parseInt(pb[i], 10) || 0;
            if (nb > na) return true;
            if (nb < na) return false;
        }
        return false;
    }

    function renderBadge(wrap, current, latest) {
        var badge = document.createElement('a');
        badge.className = 'update-badge';
        badge.href = RELEASE_NOTES_URL;
        badge.target = '_blank';
        badge.rel = 'noopener';
        badge.innerHTML = '<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">' +
            '<path d="M12 19V5M12 5L6 11M12 5l6 6" stroke="currentColor" stroke-width="3" ' +
            'stroke-linecap="round" stroke-linejoin="round"/></svg>';
        badge.appendChild(document.createTextNode(latest + ' available'));

        var tip = document.createElement('span');
        tip.className = 'update-tip';
        var line = document.createElement('span');
        line.className = 'update-tip__line';
        line.appendChild(document.createTextNode('swe-swe '));
        var strong = document.createElement('strong');
        strong.textContent = latest;
        line.appendChild(strong);
        line.appendChild(document.createTextNode(' is out. You are on ' + current + '.'));
        var cmd = document.createElement('span');
        cmd.className = 'update-tip__cmd';
        cmd.textContent = 'npx swe-swe@latest up';
        var link = document.createElement('a');
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

    document.addEventListener('DOMContentLoaded', function() {
        var wrap = document.getElementById('update-badge-wrap');
        if (!wrap) return;
        var current = wrap.dataset.currentVersion;
        // "dev" builds are not on npm, so there is nothing meaningful to compare
        if (!current || current === 'dev') return;

        fetch(NPM_LATEST_URL, { credentials: 'omit' })
            .then(function(res) { return res.ok ? res.json() : null; })
            .then(function(data) {
                if (!data || !data.version) return;
                if (!isNewer(current, data.version)) return;
                renderBadge(wrap, current, data.version);
            })
            .catch(function() { /* offline or blocked: leave the badge hidden */ });
    });
})();
