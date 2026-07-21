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
    var startTerminalBtn = document.getElementById('new-session-start-terminal');
    var startChatBtn = document.getElementById('new-session-start-chat');
    var errorDiv = document.getElementById('new-session-error');
    var agentHint = document.getElementById('new-session-agent-hint');
    var loadingDiv = document.getElementById('new-session-loading');
    var loadingText = document.getElementById('new-session-loading-text');
    var repoHistoryList = document.getElementById('repo-history');
    var branchList = document.getElementById('branch-list');
    var newSessionColorInput = document.getElementById('new-session-color-input');
    var newSessionColorHex = document.getElementById('new-session-color-hex');
    var newSessionColorClear = document.getElementById('new-session-color-clear');
    var envHint = document.getElementById('new-session-env-hint');
    var devChannelsField = document.getElementById('dev-channels-field');
    var devChannelsCheckbox = document.getElementById('new-session-dev-channels');
    var whereCombo = document.getElementById('where-combo');
    var branchCombo = document.getElementById('branch-combo');
    var extraArgsInput = document.getElementById('new-session-extra-args');

    // Derive a short "org/repo" label from a git remote URL for the Where
    // dropdown's primary line (the full URL rides along as the detail line).
    // Handles scp-style (git@host:org/repo.git) and URL-style
    // (https://host/org/repo.git) remotes; falls back to `fallback` (dirName).
    function shortRepoName(url, fallback) {
        if (!url) return fallback || url;
        var s = url.trim().replace(/\.git$/, '');
        s = s.replace(/^[a-z][a-z0-9+.-]*:\/\//i, ''); // drop scheme://
        s = s.replace(/^[^@\/]+@/, '');                 // drop user@
        var path = s.replace(/^[^\/:]+[:\/]/, '');       // drop host and first separator
        var parts = path.split('/').filter(Boolean);
        if (parts.length >= 2) return parts.slice(-2).join('/');
        if (parts.length === 1) return parts[0];
        return fallback || url;
    }

    // The static "Leave blank for default branch" placeholder, captured before
    // any prefill overrides it so reset can restore it. A recording's "+ New"
    // swaps in a branch-specific hint (see applyPendingPrefill).
    var DEFAULT_BRANCH_PLACEHOLDER = branchInput ? branchInput.placeholder : '';
    function setBranchPlaceholder(text) {
        if (branchInput) branchInput.placeholder = text;
        // The combo-box mirrors its placeholder attribute onto the visible
        // input at runtime, so set it there too (the raw input is hidden once
        // the combo upgrades it).
        if (branchCombo) branchCombo.setAttribute('placeholder', text);
    }

    // Clone-credential UI (three-state: TRANSPARENT / FRESH / REJECTED).
    var cloneCredTransparent = document.getElementById('clone-cred-transparent');
    var cloneCredTransparentHost = document.getElementById('clone-cred-transparent-host');
    var cloneCredChange = document.getElementById('clone-cred-change');
    var cloneCredFields = document.getElementById('clone-cred-fields');
    var cloneCredNotice = document.getElementById('clone-cred-notice');
    var cloneCredHostInput = document.getElementById('clone-cred-host');
    var cloneCredUsernameInput = document.getElementById('clone-cred-username');
    var cloneCredTokenInput = document.getElementById('clone-cred-token');
    var cloneCredSubmit = document.getElementById('clone-cred-submit');

    // Development channels: a checkbox, offered only by the agents that
    // understand the flag, and off unless you ask for it. Previously this was
    // typed into Extra CLI flags on your behalf, so the field was never blank
    // and opting out meant deleting a flag you did not put there.
    var DEV_CHANNELS_FLAG = '--dangerously-load-development-channels server:swe-swe-agent-chat';
    var AGENT_DEV_CHANNELS = {
        claude: DEV_CHANNELS_FLAG
    };

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
        whereKey: '',
        extraArgs: '',
        // init_sha of the selected repo (from /api/repo/branches). Used to
        // locate this repo's env-vars blob in localStorage so it can ride the
        // creation POST and reach the new session's process before it spawns.
        initSha: '',
        // Settings carried over from a recording's "+ New" button, applied
        // once the prefilled repo finishes preparing. Cleared if the user
        // switches the Where selection before that happens.
        pendingPrefill: null,
        // Session display name carried over from a recording (recordings name
        // their session via ?name=...); rides the creation POST unchanged.
        prefillName: ''
    };

    // Show the development-channels checkbox only for agents that take the
    // flag. Switching to an agent that does not also clears the tick, so the
    // flag cannot ride along invisibly to an agent that would choke on it.
    function updateDevChannelsOption(agent) {
        var supported = !!AGENT_DEV_CHANNELS[agent];
        if (devChannelsField) {
            devChannelsField.classList.toggle('dialog__field--hidden', !supported);
        }
        if (!supported && devChannelsCheckbox) {
            devChannelsCheckbox.checked = false;
        }
    }

    // The flags actually handed to the agent: whatever is typed in Extra CLI
    // flags, plus the development-channels flag when its box is ticked.
    function effectiveExtraArgs() {
        var typed = (dialogState.extraArgs || '').trim();
        if (!devChannelsCheckbox || !devChannelsCheckbox.checked) {
            return typed;
        }
        var flag = AGENT_DEV_CHANNELS[dialogState.selectedAgent];
        if (!flag) {
            return typed;
        }
        // Typing it by hand as well should not pass it twice.
        if (typed.indexOf(flag) !== -1) {
            return typed;
        }
        return typed ? typed + ' ' + flag : flag;
    }

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

    // Per-device "last used" recency for the Where dropdown. A map of repo
    // path -> last-used epoch ms, kept in localStorage (per-device, not
    // synced). fetchAndPopulateRepos sorts the dynamic options by this so the
    // repo you touched most recently floats to the top; never-used repos keep
    // the server's alphabetical order.
    var REPO_RECENCY_KEY = 'swe-swe-repo-recency';
    var REPO_RECENCY_MAX = 50; // cap so the map can't grow unbounded

    function loadRepoRecency() {
        try {
            return JSON.parse(localStorage.getItem(REPO_RECENCY_KEY) || '{}') || {};
        } catch (e) {
            return {};
        }
    }

    function recordRepoUsage(repoPath) {
        if (!repoPath) return;
        try {
            var rec = loadRepoRecency();
            rec[repoPath] = Date.now();
            // Trim to the most-recent REPO_RECENCY_MAX entries so stale paths
            // (repos long since deleted) don't accumulate forever.
            var keys = Object.keys(rec);
            if (keys.length > REPO_RECENCY_MAX) {
                keys.sort(function(a, b) { return rec[b] - rec[a]; });
                var trimmed = {};
                keys.slice(0, REPO_RECENCY_MAX).forEach(function(k) { trimmed[k] = rec[k]; });
                rec = trimmed;
            }
            localStorage.setItem(REPO_RECENCY_KEY, JSON.stringify(rec));
        } catch (e) {}
    }

    // Sync current <select> options to the where combo-box (excluding disabled placeholder)
    function syncWhereComboOptions() {
        if (!whereCombo) return;
        var opts = [];
        for (var i = 0; i < modeSelect.options.length; i++) {
            var opt = modeSelect.options[i];
            if (opt.disabled || opt.value === '') continue;
            opts.push({ value: opt.value, label: opt.textContent, detail: opt.dataset.detail || '' });
        }
        whereCombo.setOptions(opts);
    }

    function resetDownstream() {
        // Hide post-prepare fields
        postPrepareFields.classList.add('dialog__field--hidden');
        if (agentHint) agentHint.style.display = 'none';

        // Hide any revealed clone-credential UI
        hideCloneCredUI();

        // Reset warning
        warningDiv.style.display = 'none';
        warningDiv.textContent = '';

        // Reset branch
        branchField.classList.remove('dialog__field--hidden');
        branchInput.value = '';
        branchInput.disabled = true;
        branchList.innerHTML = '';
        setBranchPlaceholder(DEFAULT_BRANCH_PLACEHOLDER);
        if (branchCombo) {
            branchCombo.value = '';
            branchCombo.setOptions([]);
            branchCombo.setAttribute('disabled', '');
        }

        // Reset error/loading
        errorDiv.textContent = '';
        loadingDiv.style.display = 'none';
        startTerminalBtn.disabled = true; startChatBtn.disabled = true;

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
        dialogState.extraArgs = '';
        dialogState.initSha = '';
        dialogState.prefillName = '';
        if (extraArgsInput) extraArgsInput.value = '';
        // Unchecked by default, every time -- an opt-in that remembered itself
        // would be indistinguishable from the auto-prefill this replaced.
        if (devChannelsCheckbox) devChannelsCheckbox.checked = false;
        if (devChannelsField) devChannelsField.classList.add('dialog__field--hidden');

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
        dialogState.pendingPrefill = null;
        if (whereCombo) whereCombo.value = '';

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

    // With exactly one agent available there is no choice to make, and leaving
    // it unselected just leaves both Start buttons dead on arrival.
    function autoSelectSoleAgent() {
        if (dialogState.selectedAgent) return;
        var labels = agentsContainer.querySelectorAll('.dialog__agent:not(.dialog__agent--disabled)');
        if (labels.length === 1) selectAgent(labels[0]);
    }

    // Say why the Start buttons are dead. A disabled button fires no click, so
    // the showError('Please select an agent') guard inside startSession() can
    // never run -- the user was left with two greyed-out buttons and no reason.
    function updateStartHint() {
        if (!agentHint) return;
        var waiting = !dialogState.selectedAgent &&
                      !postPrepareFields.classList.contains('dialog__field--hidden');
        agentHint.style.display = waiting ? 'block' : 'none';
    }

    function enableBranchAndAgent() {
        // Enable branch input
        branchInput.disabled = false;
        if (branchCombo) branchCombo.removeAttribute('disabled');

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
                    updateDevChannelsOption(dialogState.preSelectedAgent);
                    startTerminalBtn.disabled = false; startChatBtn.disabled = false;
                }
            }
        }

        autoSelectSoleAgent();
        updateStartHint();
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
                    updateDevChannelsOption(dialogState.preSelectedAgent);
                    startTerminalBtn.disabled = false; startChatBtn.disabled = false;
                }
            }
        }

        autoSelectSoleAgent();
        updateStartHint();
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

    // Fetch repos and populate dropdown with dynamic options.
    // Returns a promise that resolves once the options (and the where combo)
    // are in their final state, so callers can select a dynamic option.
    function fetchAndPopulateRepos() {
        // Remove stale dynamic options first
        var dynamicOptions = modeSelect.querySelectorAll('option[data-dynamic]');
        dynamicOptions.forEach(function(opt) { opt.remove(); });

        return fetch('/api/repos')
            .then(function(response) {
                if (!response.ok) return null;
                return response.json();
            })
            .then(function(data) {
                if (!data) return;

                // Give the static "Default workspace" option the same detail
                // line the cloned-repo options get (its git origin URL).
                // The .finally() below syncs the combo, so this shows even
                // when there are no cloned repos.
                if (data.workspaceRemoteURL) {
                    var workspaceOption = modeSelect.querySelector('option[value="workspace"]');
                    if (workspaceOption) {
                        workspaceOption.dataset.detail = data.workspaceRemoteURL;
                    }
                }

                if (!data.repos || data.repos.length === 0) return;

                // Insert dynamic options before "Clone external repository..."
                var cloneOption = modeSelect.querySelector('option[value="clone"]');

                // Order by per-device recency: most-recently-used first.
                // Array.prototype.sort is stable, so repos with no recorded
                // usage (recency 0) keep the server's alphabetical order.
                var recency = loadRepoRecency();
                var repos = data.repos.slice();
                repos.sort(function(a, b) {
                    return (recency[b.path] || 0) - (recency[a.path] || 0);
                });

                repos.forEach(function(repo) {
                    var option = document.createElement('option');
                    option.value = repo.path;
                    option.dataset.dynamic = 'true';
                    if (repo.remoteURL) {
                        // Primary line = short org/repo; detail line = full URL.
                        option.textContent = shortRepoName(repo.remoteURL, repo.dirName);
                        option.dataset.detail = repo.remoteURL;
                    } else {
                        option.textContent = repo.dirName;
                    }
                    modeSelect.insertBefore(option, cloneOption);
                });

                syncWhereComboOptions();
            })
            .catch(function(err) {
                console.error('Failed to fetch repos:', err);
            })
            .finally(function() {
                syncWhereComboOptions();
            });
    }

    // Fill the branch datalist/combo from a /api/repo/branches payload.
    // setOptions leaves the combo's typed value alone, so a later refresh
    // never clobbers what the user is entering.
    function populateBranches(branchData) {
        dialogState.initSha = branchData.init_sha || '';
        var branches = branchData.branches || [];
        branchList.innerHTML = '';
        branches.forEach(function(branch) {
            var option = document.createElement('option');
            option.value = branch;
            branchList.appendChild(option);
        });
        if (branchCombo) branchCombo.setOptions(branches);
        if (branchData.warning) {
            warningDiv.textContent = branchData.warning;
            warningDiv.style.display = 'block';
        }
    }

    // Freshen remote refs (git fetch) without blocking the dialog, then
    // update the branch list. Best-effort: errors are ignored, and a result
    // that arrives after the user switched repos is dropped.
    function refreshBranchesInBackground(repoPath) {
        fetch('/api/repo/branches?path=' + encodeURIComponent(repoPath) + '&fetch=1')
            .then(function(response) {
                return response.ok ? response.json() : null;
            })
            .then(function(branchData) {
                if (!branchData) return;
                if (dialogState.repoPath !== repoPath) return;
                populateBranches(branchData);
            })
            .catch(function() {});
    }

    // Apply the settings a recording's "+ New" button carried over, once the
    // prefilled repo has finished preparing. One-shot.
    function applyPendingPrefill() {
        var prefill = dialogState.pendingPrefill;
        if (!prefill) return;
        dialogState.pendingPrefill = null;
        if (prefill.branch) {
            branchInput.value = prefill.branch;
            if (branchCombo) branchCombo.value = prefill.branch;
            dialogState.selectedBranch = prefill.branch;
        } else if (prefill.branchHint) {
            // Plain shared-checkout recording: no worktree branch, so the field
            // stays blank (reproducing the shared checkout). Surface the branch
            // the checkout was on as a non-submitting placeholder so the user
            // sees it without it forcing a worktree on that branch.
            setBranchPlaceholder('Leave blank to reuse ' + prefill.branchHint);
        }
        if (prefill.extraArgs) {
            // A recording made before the checkbox existed carries the flag
            // inline. Lift it back into the checkbox rather than showing it as
            // typed text, so the "+ New" dialog reads the way a fresh one does
            // -- and so effectiveExtraArgs does not have to dedupe it.
            var lifted = prefill.extraArgs;
            if (lifted.indexOf(DEV_CHANNELS_FLAG) !== -1) {
                lifted = lifted.split(DEV_CHANNELS_FLAG).join(' ').replace(/\s+/g, ' ').trim();
                if (devChannelsCheckbox) devChannelsCheckbox.checked = true;
            }
            extraArgsInput.value = lifted;
            dialogState.extraArgs = lifted;
        }
        if (prefill.name) {
            dialogState.prefillName = prefill.name;
        }
    }

    // --- Clone credentials (three-state UX) ---
    // Rule: apply saved credentials silently; surface fields only when the
    // user asks (Change) or when auth fails. Same localStorage store the
    // Settings panel uses: swe-swe-creds:<host> -> {username, token, ...}.

    // Derive the credential host from the URL via the unit-tested pure module
    // (exposed on window by a module shim in selection.html). Fallback: "".
    function deriveCloneHost(url) {
        if (typeof window.parseCloneHost === 'function') {
            return window.parseCloneHost(url);
        }
        return '';
    }

    function credsKey(host) { return 'swe-swe-creds:' + host; }

    function readCloneCreds(host) {
        if (!host) return null;
        try {
            var raw = localStorage.getItem(credsKey(host));
            return raw ? JSON.parse(raw) : null;
        } catch (e) { return null; }
    }

    // Persist a PAT under the host, preserving any name/email the Settings
    // panel stored for the same host (we only own username+token here).
    function writeCloneCreds(host, username, token) {
        if (!host) return;
        try {
            var existing = readCloneCreds(host) || {};
            existing.username = username || 'x-access-token';
            existing.token = token || '';
            localStorage.setItem(credsKey(host), JSON.stringify(existing));
        } catch (e) {}
    }

    // Build the /api/repo/prepare body for a clone, attaching credentials only
    // when a token is present. Never embeds the token in the URL.
    function buildCloneBody(url, host, username, token) {
        var body = { mode: 'clone', url: url };
        if (token) {
            body.credHost = host;
            body.credUsername = username || 'x-access-token';
            body.credToken = token;
        }
        return body;
    }

    function hideCloneCredUI() {
        if (cloneCredTransparent) cloneCredTransparent.classList.add('dialog__field--hidden');
        if (cloneCredFields) cloneCredFields.classList.add('dialog__field--hidden');
        if (cloneCredNotice) cloneCredNotice.style.display = 'none';
    }

    // TRANSPARENT: saved PAT in use, no fields -- just an unobtrusive line.
    function showCloneCredTransparent(host) {
        if (!cloneCredTransparent) return;
        cloneCredTransparentHost.textContent = host;
        cloneCredTransparent.classList.remove('dialog__field--hidden');
        cloneCredFields.classList.add('dialog__field--hidden');
    }

    // FRESH / REJECTED / Change: reveal fields, host prefilled, token in a
    // masked (password) input; notice explains why they appeared.
    function revealCloneCredFields(host, username, token, notice) {
        if (!cloneCredFields) return;
        cloneCredHostInput.value = host || '';
        cloneCredUsernameInput.value = username || 'x-access-token';
        cloneCredTokenInput.value = token || '';
        if (notice) {
            cloneCredNotice.textContent = notice;
            cloneCredNotice.style.display = 'block';
        } else {
            cloneCredNotice.style.display = 'none';
        }
        cloneCredTransparent.classList.add('dialog__field--hidden');
        cloneCredFields.classList.remove('dialog__field--hidden');
        cloneCredTokenInput.focus();
    }

    // Pre-seed the (origin, initSha) autosync-trust entry -- the SAME key the
    // terminal-ui signing/creds auto-restore uses (_signingTrustKey). Its
    // presence is the user's consent to auto-send stored secrets for this
    // repo, so seeding it after a user-authenticated clone lets the new
    // session restore the PAT without a second "trust this device?" prompt.
    // Preserves any signing fingerprint already bound.
    function signingTrustKey(initSha) {
        return 'swe-swe:signing-trust:' + window.location.origin + '|' + initSha;
    }
    function preseedCredsTrust(initSha) {
        if (!initSha) return;
        try {
            var raw = localStorage.getItem(signingTrustKey(initSha));
            var existing = raw ? JSON.parse(raw) : {};
            localStorage.setItem(signingTrustKey(initSha), JSON.stringify({
                fingerprint: (existing && existing.fingerprint) || '',
                savedAt: Date.now()
            }));
        } catch (e) {}
    }

    // Handle a needsAuth response: REJECTED when a stored token was tried and
    // failed, FRESH otherwise.
    function handleCloneNeedsAuth(data) {
        var url = urlInput.value.trim();
        var host = (data && data.host) || deriveCloneHost(url);
        var stored = host ? readCloneCreds(host) : null;
        if (stored && stored.token) {
            revealCloneCredFields(host, stored.username, stored.token,
                'Saved ' + host + ' token was rejected - update it?');
        } else {
            revealCloneCredFields(host, 'x-access-token', '',
                'This repository requires authentication. Enter a token to clone.');
        }
    }

    // Prepare repo and show post-prepare fields
    function prepareRepo(body) {
        showLoading('Preparing repository...');
        modeSelect.disabled = true;
        if (whereCombo) whereCombo.setAttribute('disabled', '');

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
            // Auth needed: a private clone failed (or a stored token was
            // rejected). Reveal/rescue the credential fields; do not proceed.
            if (data && data.needsAuth) {
                hideLoading();
                handleCloneNeedsAuth(data);
                return;
            }
            // Phase 3: a fresh clone the user authenticated with a PAT -- seed
            // the autosync-trust entry so the new session auto-restores the PAT
            // silently (no second trust prompt).
            if (body.credToken && data.justCloned && data.initSha) {
                preseedCredsTrust(data.initSha);
            }
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

            // Canonical whereKey is the resolved local repo path so the
            // session page (which only knows WorkDir) can compute the same key.
            var whereKey = data.path || dialogState.mode;

            // Show env file hint
            if (data.hasEnvFile) {
                envHint.innerHTML = 'Loading environment from <code>.swe-swe/env</code>';
            } else {
                envHint.innerHTML = 'Tip: set custom env vars in <code>.swe-swe/env</code>';
            }
            envHint.style.display = 'block';

            // Show post-prepare fields
            postPrepareFields.classList.remove('dialog__field--hidden');
            loadWhereColor(whereKey);

            if (dialogState.isNewProject || data.nonGit) {
                hideLoading();
                enableAgentOnly();
                applyPendingPrefill();
            } else {
                // List branches from local refs (instant) so the dialog is
                // usable right away; freshen remote refs in the background.
                return fetch('/api/repo/branches?path=' + encodeURIComponent(data.path))
                    .then(function(response) {
                        if (!response.ok) {
                            throw new Error('Failed to fetch branches');
                        }
                        return response.json();
                    })
                    .then(function(branchData) {
                        hideLoading();
                        populateBranches(branchData);
                        enableBranchAndAgent();
                        applyPendingPrefill();
                        // A fresh clone already has all refs local, so the
                        // background &fetch=1 call is redundant (and would be a
                        // second credentialed remote call). Skip it.
                        if (data.hasRemote && !data.justCloned) {
                            refreshBranchesInBackground(data.path);
                        }
                    });
            }
        })
        .catch(function(err) {
            hideLoading();
            showError(err.message || 'Failed to prepare repository');
        })
        .finally(function() {
            modeSelect.disabled = false;
            if (whereCombo) whereCombo.removeAttribute('disabled');
        });
    }

    // Mode change handler
    modeSelect.addEventListener('change', function() {
        var value = modeSelect.value;
        dialogState.mode = value;

        // A prefill only applies to the Where value it targeted; picking
        // anything else means the user took over.
        if (dialogState.pendingPrefill && dialogState.pendingPrefill.whereValue !== value) {
            dialogState.pendingPrefill = null;
        }

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
    function handleCloneNext() {
        var url = urlInput.value.trim();
        if (!url) {
            showError('Please enter a repository URL');
            return;
        }
        hideCloneCredUI();
        var host = deriveCloneHost(url);
        var stored = host ? readCloneCreds(host) : null;
        if (stored && stored.token) {
            // TRANSPARENT: attach the saved PAT and clone silently.
            showCloneCredTransparent(host);
            prepareRepo(buildCloneBody(url, host, stored.username, stored.token));
        } else {
            // No stored PAT: try bare. Public repos succeed with no fields;
            // a private repo comes back needsAuth and reveals the fields.
            prepareRepo(buildCloneBody(url, host, '', ''));
        }
    }
    cloneNextBtn.addEventListener('click', handleCloneNext);
    urlInput.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') { e.preventDefault(); handleCloneNext(); }
    });

    // Credential submit: store the PAT and retry the clone with it.
    function handleCloneCredSubmit() {
        var url = urlInput.value.trim();
        if (!url) { showError('Please enter a repository URL'); return; }
        var host = (cloneCredHostInput.value || '').trim() || deriveCloneHost(url);
        var username = (cloneCredUsernameInput.value || '').trim() || 'x-access-token';
        var token = (cloneCredTokenInput.value || '').trim();
        if (!token) { showError('Please enter a token'); return; }
        writeCloneCreds(host, username, token);
        hideCloneCredUI();
        prepareRepo(buildCloneBody(url, host, username, token));
    }
    if (cloneCredSubmit) {
        cloneCredSubmit.addEventListener('click', handleCloneCredSubmit);
    }
    if (cloneCredTokenInput) {
        cloneCredTokenInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') { e.preventDefault(); handleCloneCredSubmit(); }
        });
    }

    // "Change" expands the prefilled (masked) fields for a one-off override.
    if (cloneCredChange) {
        cloneCredChange.addEventListener('click', function(e) {
            e.preventDefault();
            var url = urlInput.value.trim();
            var host = deriveCloneHost(url) || (cloneCredHostInput.value || '').trim();
            var stored = host ? readCloneCreds(host) : null;
            revealCloneCredFields(host,
                stored ? stored.username : 'x-access-token',
                stored ? stored.token : '',
                '');
        });
    }

    // Create inline Next button
    function handleCreateNext() {
        var name = nameInput.value.trim();
        if (!name) {
            showError('Please enter a project name');
            return;
        }
        prepareRepo({ mode: 'create', name: name });
    }
    createNextBtn.addEventListener('click', handleCreateNext);
    nameInput.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') { e.preventDefault(); handleCreateNext(); }
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

    // Focusing the branch combo used to collapse every field below it (Agent,
    // Extra CLI flags, env hint, Start buttons) so the user could not press
    // Start while the free-entry combo still held uncommitted text. That cure
    // was worse than the disease: removing those fields from layout shrank the
    // dialog mid-gesture, so the branch field slid ~150px out from under the
    // pointer between mousedown and mouseup, the mouseup landed on a different
    // control, and the document-click handler closed the listbox the same tap
    // had just opened. It also made Agent unreachable by Tab. The uncommitted
    // text is now handled where it belongs -- startSession() commits the combo
    // before reading the branch.

    // Agent selection
    function selectAgent(label) {
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
            updateDevChannelsOption(radio.value);
            startTerminalBtn.disabled = false; startChatBtn.disabled = false;
            updateStartHint();
        }
    }

    // Keep dialogState in step with what is typed; effectiveExtraArgs reads it.
    extraArgsInput.addEventListener('input', function() {
        dialogState.extraArgs = extraArgsInput.value;
    });

    agentsContainer.addEventListener('click', function(e) {
        selectAgent(e.target.closest('.dialog__agent'));
    });

    agentsContainer.addEventListener('keydown', function(e) {
        var label = e.target.closest('.dialog__agent');
        if (!label) return;
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            selectAgent(label);
        }
    });

    // Build the session params (shared by both start buttons).
    // Mirrors url-builder.js:buildSessionPageUrl -- keep param contract in sync.
    function buildSessionParams(sessionMode) {
        var p = new URLSearchParams();
        p.set('assistant', dialogState.selectedAgent);
        if (sessionMode && sessionMode !== 'terminal') p.set('session', sessionMode);
        if (dialogState.isNewProject && dialogState.projectName) {
            p.set('name', dialogState.projectName);
        } else if (dialogState.selectedBranch) {
            p.set('branch', dialogState.selectedBranch);
        }
        // Recording prefill: keep the session display name (independent of
        // the create-mode project name above).
        if (!dialogState.isNewProject && dialogState.prefillName) {
            p.set('name', dialogState.prefillName);
        }
        if (dialogState.repoPath && dialogState.repoPath !== '/workspace') {
            p.set('pwd', dialogState.repoPath);
        }
        if (dialogState.debug) p.set('debug', '1');
        var extraArgs = effectiveExtraArgs();
        if (extraArgs) p.set('extra_args', extraArgs);

        // color is CSS-only (not read by server), append after canonical params
        if (dialogState.sessionColor) {
            p.set('color', dialogState.sessionColor.replace('#', ''));
            if (dialogState.whereKey && window.sweSweTheme) {
                window.sweSweTheme.saveColorPreference(
                    window.sweSweTheme.COLOR_STORAGE_KEYS.REPO_TYPE_PREFIX + dialogState.whereKey,
                    dialogState.sessionColor
                );
            }
        }

        return p;
    }

    // Start a session by POSTing the params to /api/session/new. The server
    // mints the UUID, stages a "new" creation intent, and 302-redirects to
    // /session/{uuid}; a native form submit follows that redirect. Creation
    // MUST go through this POST -- a bare navigation to /session/{uuid} no
    // longer materializes a session (no-ghost-session invariant), so the
    // staged intent is what grants permission to create.
    function startSession(sessionMode) {
        // The branch combo is free-entry: typed text only becomes its value on
        // close. Commit it here so a user who types a branch and hits Start in
        // one motion still sends the branch they typed.
        if (branchCombo && typeof branchCombo.commit === 'function') {
            branchCombo.commit();
        }
        if (!dialogState.selectedAgent) { showError('Please select an agent'); return; }
        // Record this repo as most-recently-used so it sorts to the top of the
        // Where dropdown next time (per-device recency). repoPath is the
        // resolved local path, matching the dynamic option's value.
        recordRepoUsage(dialogState.repoPath);
        var params = buildSessionParams(sessionMode);
        var form = document.createElement('form');
        form.method = 'POST';
        form.action = '/api/session/new';
        params.forEach(function(value, key) {
            var input = document.createElement('input');
            input.type = 'hidden';
            input.name = key;
            input.value = value;
            form.appendChild(input);
        });
        // Attach this repo's env-vars blob (if the browser holds one under the
        // matching (origin, init_sha) key) so the server can inject it BEFORE
        // the new session spawns. Without this the vars would only arrive via
        // set_env after spawn -- too late for the process env. Same-origin,
        // cookie-authenticated POST over the page's TLS: same trust surface as
        // the set_env WS message. Silently skipped when init_sha is unknown
        // (non-git repo) or no blob is saved for this repo.
        var envBlob = readRepoEnvBlob(dialogState.initSha);
        // Chat-log archive opt-out: unchecking the dialog checkbox stages an
        // explicit empty AGENT_CHAT_EXPORT_DIR= into the env blob. The server's
        // presence-checked default (defaultChatExportEnv) then skips its
        // {workDir}/agent-chats append, so the streaming export stays off for
        // this session. Chat sessions only -- terminal sessions never launch
        // agent-chat, so the override would be inert noise there. No new param:
        // this is sugar over the same 'env' field the settings panel uses.
        if (sessionMode === 'chat') {
            var archiveBox = document.getElementById('new-session-chatlog-archive');
            if (archiveBox && !archiveBox.checked) {
                envBlob = (envBlob ? envBlob + '\n' : '') + 'AGENT_CHAT_EXPORT_DIR=';
            }
        }
        if (envBlob) {
            var envInput = document.createElement('input');
            envInput.type = 'hidden';
            envInput.name = 'env';
            envInput.value = envBlob;
            form.appendChild(envInput);
        }
        document.body.appendChild(form);
        form.submit();
    }

    // Read the repo env-vars blob saved by the terminal-ui settings panel,
    // keyed by (origin, init_sha) -- the same localStorage scheme the panel and
    // its auto-sync use. Returns '' when initSha is empty or nothing is stored.
    function readRepoEnvBlob(initSha) {
        if (!initSha) return '';
        try {
            return localStorage.getItem('swe-swe-env:' + window.location.origin + '|' + initSha) || '';
        } catch (e) {
            return '';
        }
    }

    // Start Agent Terminal session
    startTerminalBtn.addEventListener('click', function() {
        startSession('terminal');
    });

    // Start Agent Chat session
    startChatBtn.addEventListener('click', function() {
        startSession('chat');
    });

    // Dialog open. prefill (optional) carries a recording's settings:
    // {repoPath, branch, name, extraArgs} -- the Where selection is made
    // automatically and the rest is applied once the repo has prepared, so
    // the user can review/tweak everything before starting.
    window.openNewSessionDialog = function(preSelectedAgent, sessionUUID, debug, prefill) {
        dialogState.sessionUUID = sessionUUID;
        dialogState.debug = debug;
        dialogState.preSelectedAgent = preSelectedAgent || '';

        resetDialog();
        loadRepoHistory();
        var reposLoaded = fetchAndPopulateRepos();
        overlay.style.display = 'flex';

        if (prefill) {
            dialogState.pendingPrefill = prefill;
            // Dynamic repo options only exist after /api/repos returns.
            reposLoaded.then(function() {
                selectPrefillWhere(prefill);
            });
            return;
        }

        // Open the Where listbox either way -- picking one is the only thing
        // this dialog does until you have, and the rest of it stays hidden
        // until then, so there is nothing for the list to obscure.
        //
        // How differs by input. With a keyboard, focus the input: it opens the
        // listbox AND lets you type to filter straight away. On touch, focus
        // would bring the soft keyboard up over the dialog for no gain, so
        // call _open() directly -- it shows the list without taking focus.
        //
        // Wait for /api/repos either way: _open() renders the options it has,
        // so opening first meant the list visibly grew a moment later.
        const whereCombo = document.getElementById('where-combo');
        var hasPointer = !window.matchMedia || window.matchMedia('(hover: hover)').matches;
        reposLoaded.then(function() {
            if (overlay.style.display === 'none') { return; } // closed meanwhile
            if (dialogState.pendingPrefill) { return; }       // a prefill took over
            if (!whereCombo) { return; }
            if (hasPointer && whereCombo._input) {
                whereCombo._input.focus();
            } else if (typeof whereCombo._open === 'function') {
                whereCombo._open();
            }
        });
    };

    // Select the Where option a prefill targets and kick off prepare. If the
    // option no longer exists (repo deleted), the prefill is abandoned and
    // the user picks manually.
    function selectPrefillWhere(prefill) {
        if (overlay.style.display === 'none') return; // dialog closed meanwhile
        if (dialogState.pendingPrefill !== prefill) return; // superseded
        var whereValue = prefill.repoPath || 'workspace';
        prefill.whereValue = whereValue;
        modeSelect.value = whereValue;
        if (modeSelect.value !== whereValue) {
            dialogState.pendingPrefill = null;
            return;
        }
        if (whereCombo) whereCombo.value = whereValue;
        modeSelect.dispatchEvent(new Event('change'));
    }

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
