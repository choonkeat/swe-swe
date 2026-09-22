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
    function markDown() {
        if (sawDown) { return; }
        sawDown = true;
        var m = document.getElementById('server-down-msg');
        if (m) { m.textContent = 'Waiting for the server to come back...'; }
    }
    function tick() {
        // fetch rejects only on a network-level failure, which is what a
        // direct connection to a dead server looks like. Behind a proxy or
        // tunnel the edge stays up and answers 502/503/504 instead -- that is
        // the server being DOWN, not answering, so counting it as alive left
        // the screen polling forever and never reloading once it came back.
        // Any other status (200/302/401) does mean swe-swe itself replied.
        fetch('/', { cache: 'no-store' })
            .then(function(resp) {
                if (resp.status === 502 || resp.status === 503 || resp.status === 504) {
                    markDown();
                    setTimeout(tick, 2000);
                    return;
                }
                if (sawDown) { window.location.reload(); return; }
                setTimeout(tick, 1000);
            })
            .catch(function() {
                markDown();
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

// Homepage Settings: "Tunnel secrets" and "Session environment" panes, plus
// the tunnel status strip. Both panes hold a KEY=VALUE blob; "Remember on
// this device" keeps it in localStorage (per origin) and the page re-applies
// it on load, so an ephemeral host needs no re-paste across reloads. The
// tunnel blob goes to /api/server/tunnel (server process only), the env blob
// to /api/server/env (inherited by new sessions); neither is ever rendered
// once saved -- the pane collapses to a count until Edit.
(function() {
    function parseKV(raw) {
        var out = {};
        String(raw || '').split('\n').forEach(function(line) {
            line = line.trim();
            if (!line || line.charAt(0) === '#') return;
            var i = line.indexOf('=');
            if (i <= 0) return;
            out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
        });
        return out;
    }
    function countLines(raw) {
        return Object.keys(parseKV(raw)).length;
    }
    function postJSON(url, body) {
        return fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        }).then(function(resp) {
            return resp.json().catch(function() { return {}; }).then(function(data) {
                if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
                return data;
            });
        });
    }
    var KINDS = {
        tunnel: {
            key: 'swe-swe-server-tunnel:' + window.location.origin,
            apply: function(raw) {
                var kv = parseKV(raw);
                return postJSON('/api/server/tunnel', {
                    serverUrl: kv.SWE_TUNNEL_SERVER_URL || '',
                    unique: kv.SWE_TUNNEL_UNIQUE || '',
                    identityKey: kv.SWE_TUNNEL_IDENTITY_KEY || '',
                }).then(function(data) {
                    return 'Applied. Tunnel connecting to ' + data.serverUrl + ' as ' + data.unique + '; watch the colour of the settings gear.';
                });
            },
        },
        env: {
            key: 'swe-swe-server-env:' + window.location.origin,
            apply: function(raw) {
                return postJSON('/api/server/env', { raw: raw }).then(function(data) {
                    var msg = data.count + ' variable' + (data.count === 1 ? '' : 's') + ' will be given to new sessions.';
                    if (data.dropped && data.dropped.length) {
                        msg += ' Refused (reserved): ' + data.dropped.join(', ') + '.';
                    }
                    return msg;
                });
            },
        },
    };
    function readSaved(key) {
        try { return localStorage.getItem(key) || ''; } catch (e) { return ''; }
    }
    function writeSaved(key, raw) {
        try {
            if (raw && raw.trim()) localStorage.setItem(key, raw);
            else localStorage.removeItem(key);
        } catch (e) {}
    }

    function wirePane(pane) {
        var kind = KINDS[pane.dataset.kind];
        if (!kind) return;
        var saved = pane.querySelector('.settings-env__saved');
        var savedText = pane.querySelector('.settings-env__saved-text');
        var textarea = pane.querySelector('.settings-env__textarea');
        var remember = pane.querySelector('.settings-env__remember-box');
        var rememberLabel = pane.querySelector('.settings-env__remember');
        var apply = pane.querySelector('.settings-env__apply');
        var status = pane.querySelector('.settings-env__status');

        function setStatus(text, isError) {
            status.textContent = text || '';
            status.classList.toggle('settings-env__status--error', !!isError);
        }
        function showCollapsed(raw) {
            savedText.textContent = countLines(raw) + ' line' + (countLines(raw) === 1 ? '' : 's') + ' saved on this device; applied on page load.';
            saved.hidden = false;
            textarea.hidden = true;
            rememberLabel.hidden = true;
            apply.hidden = true;
        }
        function showEditor(raw) {
            textarea.value = raw || '';
            saved.hidden = true;
            textarea.hidden = false;
            rememberLabel.hidden = false;
            apply.hidden = false;
        }

        var stored = readSaved(kind.key);
        remember.checked = !!stored;
        if (stored) showCollapsed(stored); else showEditor('');

        pane.querySelector('.settings-env__edit').onclick = function() {
            showEditor(readSaved(kind.key));
            textarea.focus();
        };
        pane.querySelector('.settings-env__forget').onclick = function() {
            writeSaved(kind.key, '');
            remember.checked = false;
            showEditor('');
            setStatus('Forgotten on this device. The server keeps what was last applied until you apply again or it restarts.');
        };
        apply.onclick = function() {
            var raw = textarea.value;
            apply.disabled = true;
            setStatus('Applying...');
            kind.apply(raw).then(function(msg) {
                if (remember.checked) {
                    writeSaved(kind.key, raw);
                    showCollapsed(raw);
                } else {
                    writeSaved(kind.key, '');
                }
                setStatus(msg);
            }).catch(function(err) {
                setStatus('Not applied: ' + err.message, true);
            }).then(function() {
                apply.disabled = false;
            });
        };

        // Auto-apply what this device remembers, tunnel first (it is the
        // thing that makes the rest reachable).
        if (stored) {
            setStatus('Applying saved values...');
            kind.apply(stored).then(function(msg) {
                setStatus(msg);
            }).catch(function(err) {
                setStatus('Saved values were not accepted: ' + err.message, true);
            });
        }
    }
    var panes = Array.prototype.slice.call(document.querySelectorAll('.settings-env'));
    panes.sort(function(a) { return a.dataset.kind === 'tunnel' ? -1 : 1; }).forEach(wirePane);

    // Tunnel status in the Settings tunnel pane. The settings gear itself is
    // <settings-gear> (static/settings-gear.js): it carries the tunnel's colour
    // and owns the only poll on this page, so the pane just listens.
    var lastKey = '';

    function renderTunnelPane(st, description) {
        var row = document.getElementById('server-tunnel-url');
        if (!row) return;
        if (!st || !st.configured) { row.hidden = true; return; }
        row.hidden = false;
        var stateEl = document.getElementById('server-tunnel-state');
        var link = document.getElementById('server-tunnel-link');
        var copy = document.getElementById('server-tunnel-copy');
        var detail = document.getElementById('server-tunnel-detail');
        var connected = (st.state === 'connected') && !!st.url;
        stateEl.textContent = connected ? 'Connected:' : description;
        link.hidden = !connected;
        copy.hidden = !connected;
        detail.hidden = connected;
        if (connected) {
            link.href = st.url;
            link.textContent = st.url;
            copy.dataset.url = st.url;
        } else {
            detail.textContent = 'The public address appears here once the tunnel connects.';
        }
    }

    // navigator.clipboard exists only in a secure context, and a box reached
    // over plain http on anything but localhost is not one -- which is exactly
    // how you reach it BEFORE the tunnel is up, so the async API alone would
    // leave Copy dead on the page that needs it most.
    function copyText(text) {
        if (window.isSecureContext && navigator.clipboard) {
            return navigator.clipboard.writeText(text);
        }
        return new Promise(function(resolve, reject) {
            var ta = document.createElement('textarea');
            ta.value = text;
            ta.setAttribute('readonly', '');
            ta.style.position = 'fixed';
            ta.style.top = '-1000px';
            document.body.appendChild(ta);
            ta.select();
            var ok = false;
            try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
            document.body.removeChild(ta);
            if (ok) resolve(); else reject(new Error('copy unavailable'));
        });
    }

    var copyBtn = document.getElementById('server-tunnel-copy');
    if (copyBtn) {
        copyBtn.addEventListener('click', function() {
            var url = copyBtn.dataset.url || '';
            if (!url) return;
            function flash(word) {
                copyBtn.textContent = word;
                setTimeout(function() { copyBtn.textContent = 'Copy'; }, 1500);
            }
            copyText(url).then(function() {
                flash('Copied');
            }).catch(function() {
                // Neither route worked. Select the address so the keyboard
                // still gets it, and say so rather than doing nothing.
                var link = document.getElementById('server-tunnel-link');
                if (link && window.getSelection) {
                    var range = document.createRange();
                    range.selectNodeContents(link);
                    var sel = window.getSelection();
                    sel.removeAllRanges();
                    sel.addRange(range);
                }
                flash('Selected - press Ctrl+C');
            });
        });
    }

    // The poll repeats every few seconds whether or not anything moved; the
    // key skips the repaint when nothing did.
    document.addEventListener('swe-tunnel-status', function(e) {
        var st = e.detail && e.detail.status;
        var key = st ? [st.configured, st.state, st.url, st.reason, st.retryAfterMs, st.serverUrl].join('|') : '';
        if (key === lastKey) return;
        lastKey = key;
        renderTunnelPane(st, (e.detail && e.detail.description) || '');
    });
})();
