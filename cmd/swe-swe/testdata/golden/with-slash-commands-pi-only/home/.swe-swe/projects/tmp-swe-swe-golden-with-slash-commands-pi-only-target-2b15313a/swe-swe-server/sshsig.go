// sshsig.go -- SSHSIG signature builder per PROTOCOL.sshsig.
//
// Produces armored SSH signatures over arbitrary data using an
// in-memory ssh.Signer. Used by the broker's "sign-ssh" op so that
// git's `gpg.ssh.program = git-sign-swe-swe` invocation can sign
// commits / tags without ever exposing the private key to the
// session shell or the calling process.
//
// Wire format reference: OpenSSH PROTOCOL.sshsig
// https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.sshsig
package main

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	sshsigMagicPreamble = "SSHSIG"
	sshsigVersion       = 1
	sshsigDefaultHash   = "sha512"
	sshsigArmorLineLen  = 70
)

// signSSH produces an armored SSH signature blob over data using
// signer. namespace ("git" for git commit/tag signing) is included in
// both the inner signed payload and the outer blob. Hash algorithm
// defaults to sha512.
func signSSH(data []byte, signer ssh.Signer, namespace string) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("nil signer")
	}
	if namespace == "" {
		namespace = "git"
	}
	hashAlgo := sshsigDefaultHash
	hashed, err := hashForSSHSig(hashAlgo, data)
	if err != nil {
		return "", err
	}

	signed := encodeSSHSigSignedData(namespace, hashAlgo, hashed)
	sig, err := signer.Sign(cryptorand.Reader, signed)
	if err != nil {
		return "", fmt.Errorf("ssh.Signer.Sign: %w", err)
	}

	blob := encodeSSHSigBlob(signer.PublicKey(), namespace, hashAlgo, sig)
	return sshsigArmor(blob), nil
}

func hashForSSHSig(algo string, data []byte) ([]byte, error) {
	switch algo {
	case "sha512":
		h := sha512.Sum512(data)
		return h[:], nil
	case "sha256":
		h := sha256.Sum256(data)
		return h[:], nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm: %q", algo)
	}
}

// encodeSSHSigSignedData builds the bytes covered by the inner SSH
// signature (PROTOCOL.sshsig "Signed Data"):
//
//	byte[6]  MAGIC_PREAMBLE = "SSHSIG"
//	string   namespace
//	string   reserved (empty)
//	string   hash_algorithm
//	string   H(message)
func encodeSSHSigSignedData(namespace, hashAlgo string, hashedMessage []byte) []byte {
	var buf []byte
	buf = append(buf, []byte(sshsigMagicPreamble)...)
	buf = sshsigAppendString(buf, []byte(namespace))
	buf = sshsigAppendString(buf, nil)
	buf = sshsigAppendString(buf, []byte(hashAlgo))
	buf = sshsigAppendString(buf, hashedMessage)
	return buf
}

// encodeSSHSigBlob builds the outer SSHSIG blob (the bytes that get
// ASCII-armored):
//
//	byte[6]  MAGIC_PREAMBLE
//	uint32   SIG_VERSION = 1
//	string   publickey         (SSH wire-format public key)
//	string   namespace
//	string   reserved (empty)
//	string   hash_algorithm
//	string   signature         (SSH wire-format ssh.Signature)
func encodeSSHSigBlob(pubKey ssh.PublicKey, namespace, hashAlgo string, sig *ssh.Signature) []byte {
	var buf []byte
	buf = append(buf, []byte(sshsigMagicPreamble)...)

	verBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(verBuf, sshsigVersion)
	buf = append(buf, verBuf...)

	buf = sshsigAppendString(buf, pubKey.Marshal())
	buf = sshsigAppendString(buf, []byte(namespace))
	buf = sshsigAppendString(buf, nil)
	buf = sshsigAppendString(buf, []byte(hashAlgo))
	buf = sshsigAppendString(buf, ssh.Marshal(sig))
	return buf
}

// sshsigAppendString writes an SSH "string" (uint32 length + bytes)
// to buf and returns the new buf.
func sshsigAppendString(buf, s []byte) []byte {
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(s)))
	buf = append(buf, lenBuf...)
	buf = append(buf, s...)
	return buf
}

// sshsigArmor wraps blob in the SSHSIG ASCII armor that ssh-keygen
// -Y sign emits and ssh-keygen -Y verify accepts.
func sshsigArmor(blob []byte) string {
	encoded := base64.StdEncoding.EncodeToString(blob)
	var sb strings.Builder
	sb.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for i := 0; i < len(encoded); i += sshsigArmorLineLen {
		end := i + sshsigArmorLineLen
		if end > len(encoded) {
			end = len(encoded)
		}
		sb.WriteString(encoded[i:end])
		sb.WriteByte('\n')
	}
	sb.WriteString("-----END SSH SIGNATURE-----\n")
	return sb.String()
}
