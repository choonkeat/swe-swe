// Shared end-session logic with server-side PUBLIC_PORT safety check.
// Included as a regular <script> so both homepage-main.js and terminal-ui.js can call it.

/**
 * End a session. The server probes the public port and returns 409 if
 * something is listening, requiring the user to confirm before proceeding.
 *
 * @param {Object} opts
 * @param {string} opts.uuid - Session UUID
 * @param {function} opts.onSuccess - Called after session is ended successfully
 * @param {function} [opts.onError] - Called on error (defaults to alert)
 */
function checkPublicPortAndEndSession(opts) {
    var uuid = opts.uuid;
    var onSuccess = opts.onSuccess;
    var onError = opts.onError || function(msg) { alert(msg); };

    if (!uuid) {
        onSuccess();
        return;
    }

    if (!confirm('End this session?')) {
        return;
    }

    doEndSession(uuid, null, onSuccess, onError);
}

/**
 * Call the end session API with optional public port confirmation.
 * @param {string} uuid
 * @param {number|null} confirmedPort - If set, sends X-Confirm-Public-Port header
 * @param {function} onSuccess
 * @param {function} onError
 */
function doEndSession(uuid, confirmedPort, onSuccess, onError) {
    var headers = {};
    if (confirmedPort) {
        headers['X-Confirm-Public-Port'] = String(confirmedPort);
    }

    fetch('/api/session/' + uuid + '/end', { method: 'POST', headers: headers })
        .then(function(response) {
            if (response.ok) {
                onSuccess();
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
                    doEndSession(uuid, port, onSuccess, onError);
                });
                return;
            }

            onError('Failed to end session');
        })
        .catch(function(err) {
            onError('Error: ' + err.message);
        });
}
