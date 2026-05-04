package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// genTestEd25519Signer returns a fresh ed25519 ssh.Signer for tests.
func genTestEd25519Signer(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey: %v", err)
	}
	return signer
}

// TestSignSSH_RoundTrip verifies that signSSH produces an armor block
// whose inner SSH signature validates against the signer's public key
// over the SSHSIG signed-data structure. This is exactly the check
// that ssh-keygen -Y verify would do.
func TestSignSSH_RoundTrip(t *testing.T) {
	signer := genTestEd25519Signer(t)
	message := []byte("hello sshsig")

	armor, err := signSSH(message, signer, "git")
	if err != nil {
		t.Fatalf("signSSH: %v", err)
	}
	if !strings.HasPrefix(armor, "-----BEGIN SSH SIGNATURE-----\n") {
		t.Errorf("missing armor header in:\n%s", armor)
	}
	if !strings.Contains(armor, "\n-----END SSH SIGNATURE-----\n") {
		t.Errorf("missing armor footer in:\n%s", armor)
	}

	pub, namespace, hashAlgo, sig, err := parseSSHSigArmor(armor)
	if err != nil {
		t.Fatalf("parseSSHSigArmor: %v", err)
	}
	if namespace != "git" {
		t.Errorf("namespace: got %q, want %q", namespace, "git")
	}
	if hashAlgo != "sha512" {
		t.Errorf("hashAlgo: got %q, want sha512", hashAlgo)
	}
	if pub.Type() != ssh.KeyAlgoED25519 {
		t.Errorf("pub type: got %q, want %q", pub.Type(), ssh.KeyAlgoED25519)
	}

	hashed, err := hashForSSHSig(hashAlgo, message)
	if err != nil {
		t.Fatalf("hashForSSHSig: %v", err)
	}
	signed := encodeSSHSigSignedData(namespace, hashAlgo, hashed)
	if err := pub.Verify(signed, sig); err != nil {
		t.Errorf("pub.Verify failed: %v", err)
	}
}

// TestSignSSH_WrongMessage_Rejected confirms the signature does not
// validate against tampered data.
func TestSignSSH_WrongMessage_Rejected(t *testing.T) {
	signer := genTestEd25519Signer(t)
	armor, err := signSSH([]byte("original"), signer, "git")
	if err != nil {
		t.Fatalf("signSSH: %v", err)
	}
	pub, ns, algo, sig, err := parseSSHSigArmor(armor)
	if err != nil {
		t.Fatalf("parseSSHSigArmor: %v", err)
	}
	hashed, _ := hashForSSHSig(algo, []byte("tampered"))
	signed := encodeSSHSigSignedData(ns, algo, hashed)
	if err := pub.Verify(signed, sig); err == nil {
		t.Error("expected Verify to fail on tampered message; got nil")
	}
}

// TestSignSSH_NamespaceDefault confirms an empty namespace becomes
// "git" inside the signed structure.
func TestSignSSH_NamespaceDefault(t *testing.T) {
	signer := genTestEd25519Signer(t)
	armor, err := signSSH([]byte("data"), signer, "")
	if err != nil {
		t.Fatalf("signSSH: %v", err)
	}
	_, ns, _, _, err := parseSSHSigArmor(armor)
	if err != nil {
		t.Fatalf("parseSSHSigArmor: %v", err)
	}
	if ns != "git" {
		t.Errorf("namespace: got %q, want git", ns)
	}
}

// parseSSHSigArmor unwraps an SSHSIG ASCII armor block into its
// constituent fields. Test-only helper that mirrors what
// ssh-keygen -Y verify does internally.
func parseSSHSigArmor(armor string) (ssh.PublicKey, string, string, *ssh.Signature, error) {
	const begin = "-----BEGIN SSH SIGNATURE-----"
	const end = "-----END SSH SIGNATURE-----"
	body := armor
	if i := strings.Index(body, begin); i >= 0 {
		body = body[i+len(begin):]
	}
	if i := strings.Index(body, end); i >= 0 {
		body = body[:i]
	}
	body = strings.ReplaceAll(body, "\n", "")
	body = strings.ReplaceAll(body, "\r", "")
	body = strings.ReplaceAll(body, " ", "")
	blob, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, "", "", nil, err
	}
	r := newSSHReader(blob)
	if got := r.fixed(6); string(got) != "SSHSIG" {
		return nil, "", "", nil, errAtPos("magic preamble mismatch", r.pos)
	}
	if r.uint32() != 1 {
		return nil, "", "", nil, errAtPos("unsupported version", r.pos)
	}
	pubBytes := r.string()
	namespace := string(r.string())
	_ = r.string() // reserved
	hashAlgo := string(r.string())
	sigBytes := r.string()
	if r.err != nil {
		return nil, "", "", nil, r.err
	}
	pub, err := ssh.ParsePublicKey(pubBytes)
	if err != nil {
		return nil, "", "", nil, err
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(sigBytes, &sig); err != nil {
		return nil, "", "", nil, err
	}
	return pub, namespace, hashAlgo, &sig, nil
}

type sshReader struct {
	buf []byte
	pos int
	err error
}

func newSSHReader(b []byte) *sshReader { return &sshReader{buf: b} }

func (r *sshReader) fixed(n int) []byte {
	if r.err != nil {
		return nil
	}
	if r.pos+n > len(r.buf) {
		r.err = errAtPos("short read", r.pos)
		return nil
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *sshReader) uint32() uint32 {
	b := r.fixed(4)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *sshReader) string() []byte {
	n := r.uint32()
	if r.err != nil {
		return nil
	}
	return r.fixed(int(n))
}

type posErr struct {
	msg string
	pos int
}

func (e *posErr) Error() string { return e.msg }
func errAtPos(msg string, pos int) *posErr {
	return &posErr{msg: msg, pos: pos}
}

// TestParseSigningKey_RoundTrip exercises the WS-handler key parse
// path: take an in-memory ed25519 PEM, run it through
// parseSigningKey, and confirm the resulting Signer signs the same
// way the original key would.
func TestParseSigningKey_RoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("ssh.MarshalPrivateKey: %v", err)
	}
	var pemBytes []byte
	pemBytes = append(pemBytes, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n")...)
	encoded := base64.StdEncoding.EncodeToString(pemBlock.Bytes)
	for i := 0; i < len(encoded); i += 70 {
		end := i + 70
		if end > len(encoded) {
			end = len(encoded)
		}
		pemBytes = append(pemBytes, []byte(encoded[i:end])...)
		pemBytes = append(pemBytes, '\n')
	}
	pemBytes = append(pemBytes, []byte("-----END OPENSSH PRIVATE KEY-----\n")...)

	key, err := parseSigningKey(pemBytes, "", "test-label")
	if err != nil {
		t.Fatalf("parseSigningKey: %v", err)
	}
	if key.Label != "test-label" {
		t.Errorf("label: got %q, want test-label", key.Label)
	}
	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint: got %q, want SHA256: prefix", key.Fingerprint)
	}
	if key.Signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("signer type: got %q, want ed25519", key.Signer.PublicKey().Type())
	}

	// Sanity-check that key.Signer's pubkey matches the original.
	wantPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey: %v", err)
	}
	if string(key.Signer.PublicKey().Marshal()) != string(wantPub.Marshal()) {
		t.Errorf("public key mismatch between parsed and generated")
	}
}

// TestParseSigningKey_RejectsECDSA confirms v1 rejects non-ed25519
// keys. ECDSA P-256 generates much faster than RSA, so we use that
// for the rejection-path coverage.
func TestParseSigningKey_RejectsECDSA(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pemBytes := marshalOpenSSHPEM(t, priv)
	_, err = parseSigningKey(pemBytes, "", "ecdsa-test")
	if err == nil {
		t.Fatal("expected ECDSA key to be rejected; got nil")
	}
	if !strings.Contains(err.Error(), "unsupported key type") {
		t.Errorf("expected unsupported-key-type error; got: %v", err)
	}
}

func marshalOpenSSHPEM(t *testing.T, key any) []byte {
	t.Helper()
	pemBlock, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatalf("ssh.MarshalPrivateKey: %v", err)
	}
	var out []byte
	out = append(out, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n")...)
	encoded := base64.StdEncoding.EncodeToString(pemBlock.Bytes)
	for i := 0; i < len(encoded); i += 70 {
		end := i + 70
		if end > len(encoded) {
			end = len(encoded)
		}
		out = append(out, []byte(encoded[i:end])...)
		out = append(out, '\n')
	}
	out = append(out, []byte("-----END OPENSSH PRIVATE KEY-----\n")...)
	return out
}

// TestSetGetClearSigningKey covers the in-memory store lifecycle
// and the integration with clearSessionCredentials.
func TestSetGetClearSigningKey(t *testing.T) {
	sid := "test-sid-sign-store"
	defer clearSessionCredentials(sid)

	if _, ok := getSigningKey(sid); ok {
		t.Fatal("expected no signing key initially")
	}

	signer := genTestEd25519Signer(t)
	setSigningKey(sid, SigningKey{
		Signer:      signer,
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		Label:       "lab",
	})

	got, ok := getSigningKey(sid)
	if !ok {
		t.Fatal("expected signing key after set")
	}
	if got.Label != "lab" {
		t.Errorf("label: got %q, want lab", got.Label)
	}

	authorized := signingKeyPublicAuthorized(sid)
	if !strings.HasPrefix(authorized, "ssh-ed25519 ") {
		t.Errorf("authorized: expected ssh-ed25519 prefix, got %q", authorized)
	}

	// clearSessionCredentials should sweep the signing key too.
	clearSessionCredentials(sid)
	if _, ok := getSigningKey(sid); ok {
		t.Error("expected signing key cleared after clearSessionCredentials")
	}
}
