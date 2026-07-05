// sign_store.go -- in-memory per-session SSH signing-key store.
//
// Populated by the WebSocket handler when the browser sends a
// "set_credentials" message that includes a signing private key.
// Read by the broker's "sign-ssh" op when git invokes
// `gpg.ssh.program` (which is git-sign-swe-swe in tunnel/container
// sessions).
//
// Lifecycle: cleared by clearSessionCredentials when the session
// ends. Survival across server restarts is out of scope; users
// re-paste from the Settings UI.
//
// Browser-write-only: there is no API to read the private key back
// out. The fingerprint and label may be returned via WS so the UI
// can show "key registered" without re-rendering the secret.
package main

import (
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SigningKey is a parsed in-memory SSH signing key for a session.
// Only the Signer is needed for the sign-ssh op; Fingerprint and
// Label are user-facing labels for the Settings UI ack.
type SigningKey struct {
	Signer      ssh.Signer
	Fingerprint string // SHA256:... per ssh.FingerprintSHA256
	Label       string // user-supplied, optional
}

var (
	sessionSigningKey   = map[string]SigningKey{}
	sessionSigningKeyMu sync.RWMutex
)

func setSigningKey(sid string, key SigningKey) {
	if sid == "" || key.Signer == nil {
		return
	}
	sessionSigningKeyMu.Lock()
	sessionSigningKey[sid] = key
	sessionSigningKeyMu.Unlock()
}

func getSigningKey(sid string) (SigningKey, bool) {
	sessionSigningKeyMu.RLock()
	defer sessionSigningKeyMu.RUnlock()
	k, ok := sessionSigningKey[sid]
	return k, ok
}

func clearSigningKey(sid string) {
	if sid == "" {
		return
	}
	sessionSigningKeyMu.Lock()
	delete(sessionSigningKey, sid)
	sessionSigningKeyMu.Unlock()
}

// parseSigningKey parses an OpenSSH PEM-encoded private key into a
// SigningKey. v1 only supports ed25519: anything else returns an
// error that is surfaced to the user via the WS ack.
//
// passphrase may be empty; if the key is encrypted and no passphrase
// is given, the parse fails with a friendly message. The passphrase
// is consumed once and not retained anywhere -- the SigningKey holds
// only the parsed Signer.
func parseSigningKey(pem []byte, passphrase, label string) (SigningKey, error) {
	var (
		raw any
		err error
	)
	if passphrase != "" {
		raw, err = ssh.ParseRawPrivateKeyWithPassphrase(pem, []byte(passphrase))
	} else {
		raw, err = ssh.ParseRawPrivateKey(pem)
	}
	if err != nil {
		if _, missing := err.(*ssh.PassphraseMissingError); missing {
			return SigningKey{}, fmt.Errorf("key is encrypted; passphrase required")
		}
		return SigningKey{}, fmt.Errorf("parse private key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return SigningKey{}, fmt.Errorf("build signer: %w", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return SigningKey{}, fmt.Errorf("unsupported key type %q (v1 supports ed25519 only)", signer.PublicKey().Type())
	}
	return SigningKey{
		Signer:      signer,
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		Label:       label,
	}, nil
}

// signingKeyPublicAuthorized returns the OpenSSH-authorized-key form
// (e.g. "ssh-ed25519 AAAA...") of the session's signing public key,
// without trailing newline. Empty string if no signing key is set.
// Used to populate user.signingkey in the per-session gitconfig.
func signingKeyPublicAuthorized(sid string) string {
	k, ok := getSigningKey(sid)
	if !ok || k.Signer == nil {
		return ""
	}
	authorized := ssh.MarshalAuthorizedKey(k.Signer.PublicKey())
	if len(authorized) > 0 && authorized[len(authorized)-1] == '\n' {
		authorized = authorized[:len(authorized)-1]
	}
	return string(authorized)
}
