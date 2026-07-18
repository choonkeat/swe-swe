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
exec /usr/local/bin/swe-swe-server -mode browser-backend "$@"
