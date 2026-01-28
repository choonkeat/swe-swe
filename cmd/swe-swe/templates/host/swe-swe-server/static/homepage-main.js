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
    if (!confirm('End this session?')) {
        return;
    }
    fetch('/api/session/' + uuid + '/end', {
        method: 'POST'
    }).then(function(response) {
        if (response.ok) {
            var card = button.closest('.session-card');
            if (card) {
                card.remove();
            }
            // Update count
            var countEl = document.querySelector('.section-header__count');
            if (countEl) {
                var currentCount = parseInt(countEl.textContent) || 0;
                if (currentCount > 1) {
                    countEl.textContent = currentCount - 1;
                } else {
                    countEl.remove();
                }
            }
        } else {
            alert('Failed to end session');
        }
    }).catch(function(err) {
        alert('Error: ' + err.message);
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

// New Session Dialog functionality
(function() {
    var overlay = document.getElementById('new-session-dialog-overlay');
    var closeBtn = document.getElementById('new-session-close');
    var modeSelect = document.getElementById('new-session-mode');
    var cloneUrlField = document.getElementById('clone-url-field');
    var createNameField = document.getElementById('create-name-field');
    var urlInput = document.getElementById('new-session-url');
    var nameInput = document.getElementById('new-session-name');
    var prepareBtn = document.getElementById('new-session-prepare');
    var warningDiv = document.getElementById('new-session-warning');
    var branchField = document.getElementById('branch-field');
    var branchInput = document.getElementById('new-session-branch');
    var branchNextBtn = document.getElementById('new-session-branch-next');
    var agentsContainer = document.getElementById('new-session-agents');
    var startBtn = document.getElementById('new-session-start');
    var errorDiv = document.getElementById('new-session-error');
    var loadingDiv = document.getElementById('new-session-loading');
    var loadingText = document.getElementById('new-session-loading-text');
    var repoHistoryList = document.getElementById('repo-history');
    var branchList = document.getElementById('branch-list');
    var newSessionColorInput = document.getElementById('new-session-color-input');
    var newSessionColorHex = document.getElementById('new-session-color-hex');
    var newSessionColorClear = document.getElementById('new-session-color-clear');

    var dialogState = {
        sessionUUID: '',
        debug: false,
        mode: 'workspace',
        repoPath: '',
        selectedBranch: '',
        selectedAgent: '',
        preSelectedAgent: '',
        isNewProject: false,
        projectName: '',
        sessionColor: ''
    };

    var REPO_HISTORY_KEY = 'swe-swe-repo-history';

    function loadRepoHistory() {
        try {
            var history = JSON.parse(localStorage.getItem(REPO_HISTORY_KEY) || '[]');
            repoHistoryList.innerHTML = '';
            history.forEach(function(url) {
                var option = document.createElement('option');
                option.value = url;
                repoHistoryList.appendChild(option);
            });
        } catch (e) {
            console.error('Failed to load repo history:', e);
        }
    }

    function saveToRepoHistory(url) {
        try {
            var history = JSON.parse(localStorage.getItem(REPO_HISTORY_KEY) || '[]');
            history = history.filter(function(u) { return u !== url; });
            history.unshift(url);
            history = history.slice(0, 10);
            localStorage.setItem(REPO_HISTORY_KEY, JSON.stringify(history));
        } catch (e) {
            console.error('Failed to save repo history:', e);
        }
    }

    function resetDialog() {
        modeSelect.value = 'workspace';
        dialogState.mode = 'workspace';
        cloneUrlField.classList.add('dialog__field--hidden');
        createNameField.classList.add('dialog__field--hidden');
        urlInput.value = '';
        nameInput.value = '';
        warningDiv.style.display = 'none';
        warningDiv.textContent = '';
        branchField.classList.remove('dialog__field--hidden');
        branchInput.value = '';
        branchInput.disabled = true;
        branchNextBtn.disabled = true;
        branchList.innerHTML = '';
        errorDiv.textContent = '';
        loadingDiv.style.display = 'none';
        startBtn.disabled = true;

        var agentLabels = agentsContainer.querySelectorAll('.dialog__agent');
        agentLabels.forEach(function(label) {
            label.classList.add('dialog__agent--disabled');
            label.classList.remove('dialog__agent--selected');
            var radio = label.querySelector('input[type="radio"]');
            if (radio) {
                radio.disabled = true;
                radio.checked = false;
            }
        });

        dialogState.repoPath = '';
        dialogState.selectedBranch = '';
        dialogState.selectedAgent = '';
        dialogState.isNewProject = false;
        dialogState.projectName = '';
        dialogState.sessionColor = '';

        // Reset color picker
        if (window.sweSweTheme) {
            newSessionColorInput.value = window.sweSweTheme.getCurrentColor();
        }
        newSessionColorHex.value = '';
        newSessionColorHex.placeholder = 'Use server default';
    }

    function resetBranchAndAgent() {
        warningDiv.style.display = 'none';
        warningDiv.textContent = '';
        branchField.classList.remove('dialog__field--hidden');
        branchInput.value = '';
        branchInput.disabled = true;
        branchNextBtn.disabled = true;
        branchList.innerHTML = '';
        startBtn.disabled = true;

        var agentLabels = agentsContainer.querySelectorAll('.dialog__agent');
        agentLabels.forEach(function(label) {
            label.classList.add('dialog__agent--disabled');
            label.classList.remove('dialog__agent--selected');
            var radio = label.querySelector('input[type="radio"]');
            if (radio) {
                radio.disabled = true;
                radio.checked = false;
            }
        });

        dialogState.repoPath = '';
        dialogState.selectedBranch = '';
        dialogState.selectedAgent = '';
        dialogState.isNewProject = false;
        dialogState.projectName = '';
    }

    window.openNewSessionDialog = function(preSelectedAgent, sessionUUID, debug) {
        dialogState.sessionUUID = sessionUUID;
        dialogState.debug = debug;
        dialogState.preSelectedAgent = preSelectedAgent || '';

        resetDialog();
        loadRepoHistory();
        overlay.style.display = 'flex';

        setTimeout(function() { modeSelect.focus(); }, 100);
    };

    function closeDialog() {
        overlay.style.display = 'none';
        resetDialog();
    }

    function showError(msg) {
        errorDiv.textContent = msg;
        loadingDiv.style.display = 'none';
    }

    function showLoading(msg) {
        loadingText.textContent = msg || 'Loading...';
        loadingDiv.style.display = 'flex';
        errorDiv.textContent = '';
    }

    function hideLoading() {
        loadingDiv.style.display = 'none';
    }

    function enableBranchField() {
        branchInput.disabled = false;
        branchNextBtn.disabled = false;
    }

    function enableAgentSelection() {
        var agentLabels = agentsContainer.querySelectorAll('.dialog__agent');
        agentLabels.forEach(function(label) {
            label.classList.remove('dialog__agent--disabled');
            var radio = label.querySelector('input[type="radio"]');
            if (radio) {
                radio.disabled = false;
            }
        });

        if (dialogState.preSelectedAgent) {
            var preSelectedLabel = agentsContainer.querySelector('[data-agent="' + dialogState.preSelectedAgent + '"]');
            if (preSelectedLabel) {
                var radio = preSelectedLabel.querySelector('input[type="radio"]');
                if (radio) {
                    radio.checked = true;
                    preSelectedLabel.classList.add('dialog__agent--selected');
                    dialogState.selectedAgent = dialogState.preSelectedAgent;
                    startBtn.disabled = false;
                }
            }
        }
    }

    // Color picker for new session
    function loadRepoTypeColor() {
        if (!window.sweSweTheme) return;
        var repoTypeKey = dialogState.mode;
        var savedColor = localStorage.getItem(window.sweSweTheme.COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + repoTypeKey);
        if (savedColor) {
            newSessionColorInput.value = savedColor;
            newSessionColorHex.value = savedColor;
            dialogState.sessionColor = savedColor;
        } else {
            var serverDefault = window.sweSweTheme.getCurrentColor();
            newSessionColorInput.value = serverDefault;
            newSessionColorHex.value = '';
            newSessionColorHex.placeholder = 'Use server default';
            dialogState.sessionColor = '';
        }
    }

    // Mode change handler
    modeSelect.addEventListener('change', function() {
        dialogState.mode = modeSelect.value;

        // Show/hide relevant fields
        cloneUrlField.classList.toggle('dialog__field--hidden', dialogState.mode !== 'clone');
        createNameField.classList.toggle('dialog__field--hidden', dialogState.mode !== 'create');

        // Load saved color for this repo type
        loadRepoTypeColor();

        // Reset downstream fields
        resetBranchAndAgent();
    });

    newSessionColorInput.addEventListener('input', function() {
        var color = newSessionColorInput.value;
        newSessionColorHex.value = color;
        dialogState.sessionColor = color;
    });

    newSessionColorHex.addEventListener('change', function() {
        var val = newSessionColorHex.value.trim();
        if (!val) {
            dialogState.sessionColor = '';
            return;
        }
        if (!val.startsWith('#')) val = '#' + val;
        if (/^#[0-9a-fA-F]{6}$/.test(val)) {
            newSessionColorInput.value = val;
            dialogState.sessionColor = val;
        }
    });

    newSessionColorClear.addEventListener('click', function() {
        newSessionColorHex.value = '';
        dialogState.sessionColor = '';
        if (window.sweSweTheme) {
            newSessionColorInput.value = window.sweSweTheme.getCurrentColor();
        }
    });

    // Prepare button handler
    prepareBtn.addEventListener('click', function() {
        var body = { mode: dialogState.mode };

        if (dialogState.mode === 'clone') {
            body.url = urlInput.value.trim();
            if (!body.url) {
                showError('Please enter a repository URL');
                return;
            }
        } else if (dialogState.mode === 'create') {
            body.name = nameInput.value.trim();
            if (!body.name) {
                showError('Please enter a project name');
                return;
            }
        }

        showLoading('Preparing repository...');

        fetch('/api/repo/prepare', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
        .then(function(response) {
            if (!response.ok) {
                return response.json().then(function(data) {
                    throw new Error(data.error || 'Failed to prepare repository');
                });
            }
            return response.json();
        })
        .then(function(data) {
            dialogState.repoPath = data.path;
            dialogState.isNewProject = data.isNew || false;
            if (dialogState.mode === 'create' && body.name) {
                dialogState.projectName = body.name;
            }

            // Save to history for clone mode
            if (dialogState.mode === 'clone' && body.url) {
                saveToRepoHistory(body.url);
            }

            // Show warning if present (soft fail for workspace mode)
            if (data.warning) {
                warningDiv.textContent = data.warning;
                warningDiv.style.display = 'block';
            } else {
                warningDiv.style.display = 'none';
            }

            if (dialogState.isNewProject) {
                // Skip branch selection for new projects
                hideLoading();
                branchField.classList.add('dialog__field--hidden');
                enableAgentSelection();
            } else {
                // Fetch branches
                return fetch('/api/repo/branches?path=' + encodeURIComponent(data.path));
            }
        })
        .then(function(response) {
            if (!response) return; // Already handled new project case
            if (!response.ok) {
                throw new Error('Failed to fetch branches');
            }
            return response.json();
        })
        .then(function(data) {
            if (!data) return; // Already handled new project case
            hideLoading();
            branchList.innerHTML = '';
            (data.branches || []).forEach(function(branch) {
                var option = document.createElement('option');
                option.value = branch;
                branchList.appendChild(option);
            });
            enableBranchField();
            branchInput.focus();
        })
        .catch(function(err) {
            hideLoading();
            showError(err.message || 'Failed to prepare repository');
        });
    });

    branchNextBtn.addEventListener('click', function() {
        dialogState.selectedBranch = branchInput.value.trim();
        enableAgentSelection();
    });

    branchInput.addEventListener('change', function() {
        if (branchInput.value) {
            dialogState.selectedBranch = branchInput.value.trim();
            enableAgentSelection();
        }
    });

    agentsContainer.addEventListener('click', function(e) {
        var label = e.target.closest('.dialog__agent');
        if (!label || label.classList.contains('dialog__agent--disabled')) {
            return;
        }

        var allLabels = agentsContainer.querySelectorAll('.dialog__agent');
        allLabels.forEach(function(l) {
            l.classList.remove('dialog__agent--selected');
        });

        label.classList.add('dialog__agent--selected');
        var radio = label.querySelector('input[type="radio"]');
        if (radio) {
            radio.checked = true;
            dialogState.selectedAgent = radio.value;
            startBtn.disabled = false;
        }
    });

    startBtn.addEventListener('click', function() {
        if (!dialogState.selectedAgent) {
            showError('Please select an agent');
            return;
        }

        var url = '/session/' + dialogState.sessionUUID + '?assistant=' + encodeURIComponent(dialogState.selectedAgent);
        if (dialogState.debug) {
            url += '&debug=1';
        }
        if (dialogState.repoPath && dialogState.repoPath !== '/workspace') {
            url += '&pwd=' + encodeURIComponent(dialogState.repoPath);
        }
        // Use project name for new projects, branch for existing repos
        var sessionName = dialogState.isNewProject ? dialogState.projectName : dialogState.selectedBranch;
        if (sessionName) {
            url += '&name=' + encodeURIComponent(sessionName);
        }

        // Add color if set
        if (dialogState.sessionColor) {
            url += '&color=' + encodeURIComponent(dialogState.sessionColor.replace('#', ''));
            // Save color for this repo type
            var repoTypeKey = dialogState.mode === 'clone' ? urlInput.value.trim() : dialogState.mode;
            if (repoTypeKey && window.sweSweTheme) {
                window.sweSweTheme.saveColorPreference(
                    window.sweSweTheme.COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + repoTypeKey,
                    dialogState.sessionColor
                );
            }
        }

        window.location.href = url;
    });

    closeBtn.addEventListener('click', closeDialog);

    overlay.addEventListener('click', function(e) {
        if (e.target === overlay) {
            closeDialog();
        }
    });

    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape' && overlay.style.display === 'flex') {
            closeDialog();
        }
    });

    // Reset downstream when mode-specific inputs change
    urlInput.addEventListener('input', resetBranchAndAgent);
    nameInput.addEventListener('input', resetBranchAndAgent);
})();

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
        }
    });
});
