// New Session Dialog functionality
// Manages the "Where" dropdown, repo fetching, prepare flow, and session creation.
(function() {
    var overlay = document.getElementById('new-session-dialog-overlay');
    var closeBtn = document.getElementById('new-session-close');
    var modeSelect = document.getElementById('new-session-mode');
    var newSessionFields = document.getElementById('new-session-fields');
    var cloneUrlField = document.getElementById('clone-url-field');
    var createNameField = document.getElementById('create-name-field');
    var urlInput = document.getElementById('new-session-url');
    var nameInput = document.getElementById('new-session-name');
    var cloneNextBtn = document.getElementById('clone-next-btn');
    var createNextBtn = document.getElementById('create-next-btn');
    var warningDiv = document.getElementById('new-session-warning');
    var postPrepareFields = document.getElementById('post-prepare-fields');
    var branchField = document.getElementById('branch-field');
    var branchInput = document.getElementById('new-session-branch');
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
        mode: '',
        repoPath: '',
        selectedBranch: '',
        selectedAgent: '',
        preSelectedAgent: '',
        isNewProject: false,
        projectName: '',
        sessionColor: '',
        whereKey: ''
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

    function resetDownstream() {
        // Hide post-prepare fields
        postPrepareFields.classList.add('dialog__field--hidden');

        // Reset warning
        warningDiv.style.display = 'none';
        warningDiv.textContent = '';

        // Reset branch
        branchField.classList.remove('dialog__field--hidden');
        branchInput.value = '';
        branchInput.disabled = true;
        branchList.innerHTML = '';

        // Reset error/loading
        errorDiv.textContent = '';
        loadingDiv.style.display = 'none';
        startBtn.disabled = true;

        // Reset agents
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

        // Reset state
        dialogState.repoPath = '';
        dialogState.selectedBranch = '';
        dialogState.selectedAgent = '';
        dialogState.isNewProject = false;
        dialogState.projectName = '';
        dialogState.sessionColor = '';
        dialogState.whereKey = '';

        // Reset color picker
        if (window.sweSweTheme) {
            newSessionColorInput.value = window.sweSweTheme.getCurrentColor();
        }
        newSessionColorHex.value = '';
        newSessionColorHex.placeholder = 'Use server default';
    }

    function resetDialog() {
        modeSelect.value = '';
        dialogState.mode = '';

        // Hide everything below dropdown
        newSessionFields.classList.add('dialog__field--hidden');
        cloneUrlField.classList.add('dialog__field--hidden');
        createNameField.classList.add('dialog__field--hidden');
        urlInput.value = '';
        nameInput.value = '';

        resetDownstream();
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

    function enableBranchAndAgent() {
        // Enable branch input
        branchInput.disabled = false;

        // Enable agent selection
        var agentLabels = agentsContainer.querySelectorAll('.dialog__agent');
        agentLabels.forEach(function(label) {
            label.classList.remove('dialog__agent--disabled');
            var radio = label.querySelector('input[type="radio"]');
            if (radio) {
                radio.disabled = false;
            }
        });

        // Auto-select preselected agent
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

    function enableAgentOnly() {
        // For create mode: no branch, just agent
        branchField.classList.add('dialog__field--hidden');

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

    // Load sticky color for the current "where" key
    function loadWhereColor(whereKey) {
        dialogState.whereKey = whereKey;
        if (!window.sweSweTheme) return;

        var savedColor = localStorage.getItem(window.sweSweTheme.COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + whereKey);
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

    // Fetch repos and populate dropdown with dynamic options
    function fetchAndPopulateRepos() {
        // Remove stale dynamic options first
        var dynamicOptions = modeSelect.querySelectorAll('option[data-dynamic]');
        dynamicOptions.forEach(function(opt) { opt.remove(); });

        fetch('/api/repos')
            .then(function(response) {
                if (!response.ok) return null;
                return response.json();
            })
            .then(function(data) {
                if (!data || !data.repos || data.repos.length === 0) return;

                // Insert dynamic options before "Clone external repository..."
                var cloneOption = modeSelect.querySelector('option[value="clone"]');

                data.repos.forEach(function(repo) {
                    var option = document.createElement('option');
                    option.value = repo.path;
                    option.dataset.dynamic = 'true';
                    option.textContent = repo.remoteURL || repo.dirName;
                    modeSelect.insertBefore(option, cloneOption);
                });
            })
            .catch(function(err) {
                console.error('Failed to fetch repos:', err);
            });
    }

    // Prepare repo and show post-prepare fields
    function prepareRepo(body) {
        showLoading('Preparing repository...');
        modeSelect.disabled = true;

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
            if (body.mode === 'create' && body.name) {
                dialogState.projectName = body.name;
            }

            // Save to history for clone mode
            if (body.mode === 'clone' && body.url) {
                saveToRepoHistory(body.url);
            }

            // Show warning if present
            if (data.warning) {
                warningDiv.textContent = data.warning;
                warningDiv.style.display = 'block';
            } else {
                warningDiv.style.display = 'none';
            }

            // Compute whereKey for sticky color
            var whereKey;
            if (body.mode === 'workspace') {
                whereKey = body.path ? body.path : 'workspace';
            } else if (body.mode === 'clone') {
                whereKey = body.url || 'clone';
            } else if (body.mode === 'create') {
                whereKey = body.name || 'create';
            } else {
                whereKey = dialogState.mode;
            }

            // Show post-prepare fields
            postPrepareFields.classList.remove('dialog__field--hidden');
            loadWhereColor(whereKey);

            if (dialogState.isNewProject || data.nonGit) {
                hideLoading();
                enableAgentOnly();
            } else {
                // Fetch branches
                return fetch('/api/repo/branches?path=' + encodeURIComponent(data.path))
                    .then(function(response) {
                        if (!response.ok) {
                            throw new Error('Failed to fetch branches');
                        }
                        return response.json();
                    })
                    .then(function(branchData) {
                        hideLoading();
                        branchList.innerHTML = '';
                        (branchData.branches || []).forEach(function(branch) {
                            var option = document.createElement('option');
                            option.value = branch;
                            branchList.appendChild(option);
                        });
                        enableBranchAndAgent();
                    });
            }
        })
        .catch(function(err) {
            hideLoading();
            showError(err.message || 'Failed to prepare repository');
        })
        .finally(function() {
            modeSelect.disabled = false;
        });
    }

    // Mode change handler
    modeSelect.addEventListener('change', function() {
        var value = modeSelect.value;
        dialogState.mode = value;

        // Empty placeholder: hide everything
        if (!value) {
            newSessionFields.classList.add('dialog__field--hidden');
            return;
        }

        // Show the fields container
        newSessionFields.classList.remove('dialog__field--hidden');

        // Reset downstream
        resetDownstream();

        // Show/hide mode-specific fields
        var isClone = value === 'clone';
        var isCreate = value === 'create';
        cloneUrlField.classList.toggle('dialog__field--hidden', !isClone);
        createNameField.classList.toggle('dialog__field--hidden', !isCreate);

        // For workspace or existing repo paths: auto-trigger prepare
        if (!isClone && !isCreate) {
            var body = { mode: 'workspace' };
            if (value !== 'workspace') {
                // It's an existing repo path
                body.path = value;
            }
            prepareRepo(body);
        }
    });

    // Clone inline Next button
    cloneNextBtn.addEventListener('click', function() {
        var url = urlInput.value.trim();
        if (!url) {
            showError('Please enter a repository URL');
            return;
        }
        prepareRepo({ mode: 'clone', url: url });
    });

    // Create inline Next button
    createNextBtn.addEventListener('click', function() {
        var name = nameInput.value.trim();
        if (!name) {
            showError('Please enter a project name');
            return;
        }
        prepareRepo({ mode: 'create', name: name });
    });

    // Color picker handlers
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

    // Branch change: update selectedBranch
    branchInput.addEventListener('change', function() {
        dialogState.selectedBranch = branchInput.value.trim();
    });

    // Agent selection
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

    // Start session
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

        // For new projects, name is the display name (no branch/worktree needed)
        if (dialogState.isNewProject && dialogState.projectName) {
            url += '&name=' + encodeURIComponent(dialogState.projectName);
        } else if (dialogState.selectedBranch) {
            // For workspace/existing repos, branch is used for worktree creation
            url += '&branch=' + encodeURIComponent(dialogState.selectedBranch);
        }

        // Save and pass color
        if (dialogState.sessionColor) {
            url += '&color=' + encodeURIComponent(dialogState.sessionColor.replace('#', ''));
            if (dialogState.whereKey && window.sweSweTheme) {
                window.sweSweTheme.saveColorPreference(
                    window.sweSweTheme.COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + dialogState.whereKey,
                    dialogState.sessionColor
                );
            }
        }

        window.location.href = url;
    });

    // Dialog open
    window.openNewSessionDialog = function(preSelectedAgent, sessionUUID, debug) {
        dialogState.sessionUUID = sessionUUID;
        dialogState.debug = debug;
        dialogState.preSelectedAgent = preSelectedAgent || '';

        resetDialog();
        loadRepoHistory();
        fetchAndPopulateRepos();
        overlay.style.display = 'flex';

        setTimeout(function() { modeSelect.focus(); }, 100);
    };

    // Dialog close
    function closeDialog() {
        overlay.style.display = 'none';
        resetDialog();
    }

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

    // Reset downstream when clone/create inputs change
    urlInput.addEventListener('input', function() {
        // Hide post-prepare if user modifies URL after prepare
        postPrepareFields.classList.add('dialog__field--hidden');
        resetDownstream();
    });
    nameInput.addEventListener('input', function() {
        postPrepareFields.classList.add('dialog__field--hidden');
        resetDownstream();
    });
})();
