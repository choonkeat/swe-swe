#!/bin/sh
# Optional corporate root CA: mount a PEM at /corp-ca.crt (e.g.
# -v "$NODE_EXTRA_CA_CERTS:/corp-ca.crt:ro") and it is trusted before the
# server starts. Chromium on Linux does not read the system CA bundle for
# locally-added anchors -- it needs the cert in its NSS store -- so import
# into both: update-ca-certificates for curl/tools, certutil for chromium.
set -e
if [ -f /corp-ca.crt ]; then
    cp /corp-ca.crt /usr/local/share/ca-certificates/corp-ca.crt
    update-ca-certificates
    NSSDB="${HOME:-/root}/.pki/nssdb"
    mkdir -p "$NSSDB"
    certutil -d sql:"$NSSDB" -N --empty-password 2>/dev/null || true
    certutil -d sql:"$NSSDB" -A -t "C,," -n corp-ca -i /corp-ca.crt
    echo "browser-backend: imported /corp-ca.crt into system bundle and NSS store"
fi

# Stale X state from a previous life of this container. `docker stop` +
# `docker start` preserves the filesystem, so /tmp/.X<n>-lock and the
# /tmp/.X11-unix sockets outlive the Xvfb processes that owned them. Xvfb
# then refuses the display ("Server is already active"), exits 1, chromium
# dies with it -- yet allocation still reports a healthy session and every
# CDP connect fails with "connection refused" on the internal port. Nothing
# in this container is expected to survive a restart, so clear it all.
rm -f /tmp/.X*-lock 2>/dev/null || true
rm -rf /tmp/.X11-unix/* 2>/dev/null || true
rm -rf /tmp/chromium-session-* 2>/dev/null || true
rm -rf /tmp/org.chromium.Chromium.* 2>/dev/null || true

exec /usr/local/bin/swe-swe-server -mode browser-backend "$@"
