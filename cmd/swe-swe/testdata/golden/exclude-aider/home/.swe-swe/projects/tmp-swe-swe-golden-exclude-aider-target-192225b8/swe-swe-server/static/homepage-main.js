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

function endSession(uuid, button) {
    if (!confirm('End this session?')) {
        return;
    }
    var publicPort = parseInt(button.dataset.publicPort, 10) || 0;
    checkPublicPortAndEndSession({
        uuid: uuid,
        publicPort: publicPort,
        onStart: function() {
            setButtonLoading(button, true);
        },
        onSuccess: function() {
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

// Reconcile the rendered session cards against the server's live set: flag the
// ones being torn down, drop the ones that are gone. The homepage is otherwise
// server-rendered with no polling, so without this an ended session's card
// would linger until the user reloaded by hand.
var liveSessionsPollTimer = null;

function pollLiveSessions() {
    return fetch('/api/sessions/live', { headers: { 'Accept': 'application/json' } })
        .then(function(response) {
            if (!response.ok) { return null; }
            return response.json();
        })
        .then(function(body) {
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
                }
            }
        })
        .catch(function() { /* transient: the next tick retries */ });
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
// session then exits; the page goes unreachable, so on success we replace the
// UI with a terminal notice instead of pretending anything is still live.
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
                document.body.innerHTML =
                    '<div style="display:flex; align-items:center; justify-content:center; height:100vh; ' +
                    'font-family:inherit; color:var(--text-secondary); font-size:16px;">' +
                    'swe-swe is shutting down.</div>';
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
