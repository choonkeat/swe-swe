// Shared end-session logic with PUBLIC_PORT safety check.
// Included as a regular <script> so both homepage-main.js and terminal-ui.js can call it.

/**
 * End a session, warning the user if something is listening on the public port.
 *
 * @param {Object} opts
 * @param {string} opts.uuid - Session UUID
 * @param {number|null} opts.publicPort - PUBLIC_PORT env var value (e.g. 5000)
 * @param {number|null} opts.publicProxyPort - Traefik proxy port (e.g. 25000)
 * @param {function} opts.onSuccess - Called after session is ended successfully
 * @param {function} [opts.onError] - Called on error (defaults to alert)
 */
function checkPublicPortAndEndSession(opts) {
    var uuid = opts.uuid;
    var publicPort = opts.publicPort;
    var publicProxyPort = opts.publicProxyPort;
    var onSuccess = opts.onSuccess;
    var onError = opts.onError || function(msg) { alert(msg); };

    if (!uuid) {
        onSuccess();
        return;
    }

    // If no public port configured, skip the check
    if (!publicPort || !publicProxyPort) {
        doEndSession(uuid, onSuccess, onError);
        return;
    }

    // Try to fetch the public proxy URL to see if something is listening
    var publicUrl = location.protocol + '//' + location.hostname + ':' + publicProxyPort + '/';

    fetch(publicUrl, { method: 'GET', mode: 'cors' })
        .then(function(response) {
            return response.text();
        })
        .then(function(html) {
            // Something is listening — extract <title> if present
            var title = '';
            var match = html.match(/<title[^>]*>([^<]+)<\/title>/i);
            if (match) {
                title = match[1].trim();
            }

            var message = 'Something is running on PUBLIC_PORT ' + publicPort + '.';
            if (title) {
                message += '\nPage title: "' + title + '"';
            }
            message += '\n\nEnding this session would disrupt any public users accessing it.';
            message += '\n\nType ' + publicPort + ' to confirm:';

            var input = prompt(message);
            if (input === null) {
                return; // User cancelled
            }
            if (input.trim() !== String(publicPort)) {
                alert('Port number did not match. Session was NOT ended.');
                return;
            }

            doEndSession(uuid, onSuccess, onError);
        })
        .catch(function() {
            // Nothing listening on public port — safe to end
            doEndSession(uuid, onSuccess, onError);
        });
}

/**
 * Call the end session API.
 */
function doEndSession(uuid, onSuccess, onError) {
    fetch('/api/session/' + uuid + '/end', { method: 'POST' })
        .then(function(response) {
            if (response.ok) {
                onSuccess();
            } else {
                onError('Failed to end session');
            }
        })
        .catch(function(err) {
            onError('Error: ' + err.message);
        });
}
