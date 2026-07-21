// Shared end-session logic with server-side PUBLIC_PORT safety check.
// Included as a regular <script> so both homepage-main.js and terminal-ui.js can call it.

/**
 * End a session. Callers are responsible for obtaining user consent before
 * invoking this helper -- it performs no consent prompt of its own.
 *
 * The server probes the public port and returns 409 if something is listening;
 * in that case the helper prompts the user to type the port number to confirm
 * the disruption (a port-safety check, not a general "are you sure" gate).
 *
 * @param {Object} opts
 * The server answers 202 as soon as the session is latched as ending and runs
 * the teardown in the background, so onSuccess means "accepted, session is
 * closed to new joins" -- NOT "teardown finished". Callers must not wait for
 * cleanup: it takes seconds normally and tens of seconds when a remote browser
 * backend is unreachable. Poll /api/sessions/live to see it actually go away.
 *
 * @param {Object} opts
 * @param {string} opts.uuid - Session UUID
 * @param {function} opts.onSuccess - Called once the end request was accepted
 * @param {function} [opts.onError] - Called on error (defaults to alert)
 * @param {function} [opts.onStart] - Called once the API request is in flight
 * @param {string} [opts.chatlog] - Chat-log disposition: 'discard' deletes the
 *   log before teardown; 'commit' hands the whole job (scrub, commit, end) to
 *   the agent and leaves the session running; omitted leaves the log alone.
 */
function checkPublicPortAndEndSession(opts) {
    var uuid = opts.uuid;
    var onSuccess = opts.onSuccess;
    var onError = opts.onError || function(msg) { alert(msg); };
    var onStart = opts.onStart;

    if (!uuid) {
        onSuccess();
        return;
    }

    if (onStart) { onStart(); }
    doEndSession(uuid, null, onSuccess, onError, opts.chatlog);
}

/**
 * Fetch this session's chat-log status so the caller can offer discard/commit
 * only when there is a log to act on. Never rejects -- a session with no
 * agent-chat, or an unreachable one, resolves to {enabled:false} so the End
 * button keeps working.
 */
function fetchChatLogStatus(uuid) {
    return fetch('/api/session/' + uuid + '/chatlog', { headers: { 'Accept': 'application/json' } })
        .then(function(r) { return r.ok ? r.json() : { enabled: false }; })
        .catch(function() { return { enabled: false }; });
}

/**
 * Call the end session API with optional public port confirmation.
 * @param {string} uuid
 * @param {number|null} confirmedPort - If set, sends X-Confirm-Public-Port header
 * @param {function} onSuccess
 * @param {function} onError
 * @param {string} [chatlog] - Chat-log disposition, forwarded as a query param
 */
function doEndSession(uuid, confirmedPort, onSuccess, onError, chatlog) {
    var headers = {};
    if (confirmedPort) {
        headers['X-Confirm-Public-Port'] = String(confirmedPort);
    }
    var url = '/api/session/' + uuid + '/end';
    if (chatlog) {
        url += '?chatlog=' + encodeURIComponent(chatlog);
    }

    fetch(url, { method: 'POST', headers: headers })
        .then(function(response) {
            if (response.ok) {
                // 'commit' does not end anything yet -- the agent is now doing
                // the work and will end the session itself. Tell the caller so
                // it can say that rather than claiming the session is gone.
                onSuccess(chatlog === 'commit' ? 'commit' : 'ended');
                return;
            }

            // 409 = server detected something listening on public port
            if (response.status === 409) {
                response.json().then(function(body) {
                    var port = body.publicPort;
                    var message = 'Something is running on PUBLIC_PORT ' + port + '.';
                    if (body.pageTitle) {
                        message += '\nPage title: "' + body.pageTitle + '"';
                    }
                    message += '\n\nEnding this session would disrupt any public users accessing it.';
                    message += '\n\nType ' + port + ' to confirm:';

                    var input = prompt(message);
                    if (input === null) {
                        return; // User cancelled
                    }
                    if (input.trim() !== String(port)) {
                        alert('Port number did not match. Session was NOT ended.');
                        return;
                    }

                    // Retry with confirmation header
                    doEndSession(uuid, port, onSuccess, onError, chatlog);
                });
                return;
            }

            onError('Failed to end session');
        })
        .catch(function(err) {
            onError('Error: ' + err.message);
        });
}
