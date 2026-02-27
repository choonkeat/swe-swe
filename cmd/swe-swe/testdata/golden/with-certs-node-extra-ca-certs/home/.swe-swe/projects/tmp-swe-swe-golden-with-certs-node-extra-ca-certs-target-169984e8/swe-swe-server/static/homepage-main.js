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

function endSession(uuid, button) {
    var publicPort = parseInt(button.dataset.publicPort, 10) || 0;
    var publicProxyPort = parseInt(button.dataset.publicProxyPort, 10) || 0;
    checkPublicPortAndEndSession({
        uuid: uuid,
        publicPort: publicPort,
        publicProxyPort: publicProxyPort,
        onSuccess: function() {
            window.location.reload();
        }
    });
}

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
        }
    });
});
