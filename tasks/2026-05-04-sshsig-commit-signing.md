# SSH commit signing via the per-session credential broker

## Status

**Planned, not yet started.** Companion to ADR-0044. Extends the
broker's `get` op with a `sign-ssh` op so users can sign commits with
SSH keys held server-side in memory. GPG signing comes after as a
separate phase.

## Why this shape

The PAT delivery path proves the trust ladder: users already trust
swe-swe-server with HTTPS bearer tokens that live in memory and are
cleared on session end. An SSH signing private key is the same trust
shape -- a server-held secret, scoped to the session, never on disk,
revocable from the forge by the user out-of-band. Pasting an SSH
private key into the same Settings UI does not raise the trust
ceiling; it just adds a second secret type to the existing model.

Why SSH signing first, not GPG:

- **Ecosystem.** GitHub (Aug 2022), GitLab 15.7 (Dec 2022), Gitea, and
  Forgejo all accept SSH-signed commits and show a "Verified" badge.
  Bitbucket Cloud added it in 2023. New users gravitate to SSH signing
  because the keys overlap with auth keys.
- **Implementation simplicity.** SSHSIG is a small, well-specified
  format (`PROTOCOL.sshsig` in OpenSSH). The pure-Go
  `golang.org/x/crypto/ssh` already has the primitives; an ed25519
  signing path is ~150 LOC server-side. OpenPGP signature emission
  needs a much larger packet-format implementation or a deprecated
  stdlib package.
- **No `gnupg` in the container.** The container ships a Go binary;
  no apt install, no gpg-agent, no pinentry to plumb.
- **Validates the broker's `sign` op surface.** Once the protocol's in
  place, GPG becomes a second backend behind the same wrapper-binary
  shape, with a wider implementation but the same end-to-end story.

`commit.gpgsign=true` is the right knob for both. Since git 2.34
(Nov 2021) the config name has been format-agnostic; with
`gpg.format = ssh` git invokes `ssh-keygen -Y sign -n git -f <key>`
instead of `gpg -bsau`. The wrapper is wired in as
`gpg.ssh.program`, not `gpg.program`.

## End-to-end flow

```
Browser (Settings UI)                Go server                       Session shell
─────────────────────                ─────────                       ─────────────
[Settings: SSH private key,
 algo=ed25519, key id]
   │ localStorage
   │
   │ WS set_credentials  ─────►  sessionSigningKey[sid] = parsed signer
                                  (in-memory ssh.Signer)
                                                                     git commit -S ...
                                                                       │
                                                                       │ git invokes
                                                                       ▼
                                                                     git-sign-swe-swe
                                                                     -Y sign -n git -f keypath
                                                                       │ stdin: data to sign
                                                                       │
                                                                       │ dial @swe-swe-broker
                                                                       ▼
                              ◄─── {op:"sign-ssh", namespace:"git",
                                    hash:"sha512", data: <bytes>}
   peer pid -> ancestry walk -> sid
                               ───► {signature: <SSHSIG armored>}
                                                                       │
                                                                       │ stdout: SSHSIG blob
                                                                       │ exit 0
                                                                       ▼
                                                                     git completes commit
```

## Phases

### Phase 1 -- broker `sign-ssh` op

**Server.** Extend `cmd/swe-swe/templates/host/swe-swe-server/broker.go`
with a new switch arm in `handleBrokerConn`:

```go
case "sign-ssh":
    namespace, _ := req["namespace"].(string)
    if namespace == "" { namespace = "git" }
    dataB64, _ := req["data"].(string)
    data, err := base64.StdEncoding.DecodeString(dataB64)
    if err != nil { ...error... }

    signer, ok := getSigningKey(sid)
    if !ok {
        brokerWriteJSON(c, map[string]any{"error": "no signing key for session"})
        return
    }
    sig, err := sshsig.Sign(data, signer, namespace, "sha512")
    if err != nil { ...error... }
    brokerWriteJSON(c, map[string]any{"signature": string(sig)})
```

`sshsig.Sign` is small enough to write directly: build the
SSHSIG-spec preamble (`MAGIC_PREAMBLE` || namespace || reserved ||
hash_algo || H(data)), sign with `ssh.Signer.Sign(rand, msg)`, wrap
in the SSHSIG binary container, base64-encode with the
`-----BEGIN SSH SIGNATURE-----` / `END` armor. ~80 LOC.

**Store.** New file `cmd/swe-swe/templates/host/swe-swe-server/sign_store.go`:

```go
type SigningKey struct {
    Signer    ssh.Signer  // parsed from PEM
    PublicKey ssh.PublicKey
    KeyID     string      // user-visible label, e.g. "ed25519@laptop"
}

var (
    sessionSigningKey   = map[string]SigningKey{}
    sessionSigningKeyMu sync.RWMutex
)
```

Mutators: `setSigningKey(sid, key)`, `getSigningKey(sid) (SigningKey, bool)`,
`clearSigningKey(sid)`. Cleared by `clearSessionCredentials` (already
called on session end).

**WS handler.** Extend `set_credentials` in `main.go:4990` to also
accept `signing_private_key_pem` + `signing_key_label`, parse via
`ssh.ParseRawPrivateKey` (or `ParsePrivateKeyWithPassphrase` if the
key is encrypted -- keep the passphrase in the same WS message,
discard after parse), build an `ssh.Signer`, and stash via
`setSigningKey`. Reject anything that isn't ed25519 in v1 (smallest
implementation surface; covers the common case).

### Phase 2 -- `git-sign-swe-swe` wrapper binary

New binary at `cmd/swe-swe/templates/host/git-sign-swe-swe/main.go`,
mirroring `git-credential-swe-swe`'s shape.

**CLI surface git invokes.** When `gpg.format = ssh` and
`gpg.ssh.program = git-sign-swe-swe`, git invokes:
```
git-sign-swe-swe -Y sign -n git -f <signing_pubkey_or_path>
```
with the data to sign on stdin and the SSHSIG blob expected on stdout.
We parse `-Y`, `-n`, and `-f`; ignore `-f` value (the broker holds the
signer). Exit 0 on success.

There's a second invocation pattern for verify (`-Y verify ...`) that
git uses on receive-side configurations. Out of scope for v1; verify
remains git's job, not ours.

**Behavior.**
1. Same parent-comm gate as `git-credential-swe-swe`: refuse unless
   parent process is `git` or `ssh-keygen`. (Git invokes
   `gpg.ssh.program` directly, so parent comm is `git`. Mirror the
   `parentIsGit` helper.)
2. Read data from stdin.
3. Dial `@swe-swe-broker`, send `{op:"sign-ssh", namespace, data}`.
4. Write the returned SSHSIG armor to stdout. Exit 0.
5. On any error: write a non-zero exit and a stderr message; never
   leak partial state to stdout.

**Dockerfile.** Add a build stage + COPY for the new binary, mirroring
`git-credential-swe-swe`. Stdlib + `golang.org/x/crypto/ssh` only.

### Phase 3 -- per-session signing config injection

Extend `buildSessionEnv` (or the per-session GIT_CONFIG_GLOBAL file
written by `writeSessionGitconfig`) to add when a signing key exists:

```
[gpg]
    format = ssh
[gpg "ssh"]
    program = git-sign-swe-swe
    allowedSignersFile = /tmp/swe-swe-allowed-signers-<sid>
[user]
    signingkey = <path-to-pubkey-file>      ; or literal "ssh-ed25519 AAAA..."
[commit]
    gpgsign = true
[tag]
    gpgsign = true
```

The pubkey can be written to a per-session file at session start (it's
not a secret; the user has already published it to the forge). The
`allowedSignersFile` is only used for `git log --show-signature`
verification on commits the user themselves authored; we can populate
it with a single line of the user's own pubkey.

The `setSigningKey` path is gated: if the user hasn't configured a
signing key, we leave `gpg.format` unset and `commit.gpgsign` false.
Existing PAT-only users see no change.

### Phase 4 -- Settings UI

Extend the Session Settings modal (the same one shown in the
screenshots from 2026-05-04 chat) with an "SSH Signing" section
beneath the existing GIT HTTPS CREDENTIALS:

- **Signing private key** (multiline textarea, monospace, redacted on
  blur). Accepted formats: OpenSSH PEM, RFC4716. v1: ed25519 only;
  reject other algos with a UI message.
- **Passphrase** (optional, password-input). Used once at parse time
  on the server, then discarded.
- **Key label** (optional, single line). Cosmetic.
- **Save Signing Key** button -> WS message
  `set_credentials { signing_private_key_pem, signing_passphrase, signing_key_label }`.
- After save: redact the textarea and show a "Signing key set
  (label, fingerprint)" indicator. Server replies with the SHA256
  fingerprint so the user can verify out-of-band against the pubkey
  registered on GitHub/GitLab.

**localStorage.** Same model as the PAT. Encrypted-at-rest in the
browser is out of scope for v1 (matches PAT handling); device-flow /
hardware-backed-key alternatives are deferred to a later phase
alongside the equivalent PAT improvements.

### Phase 5 -- e2e test

Mirror `42688cd1d` (the existing credentials-UI WS round-trip test)
for the signing key. Plus an in-container test that:

1. Sets a known ed25519 private key via WS.
2. Runs `echo "test" | git-sign-swe-swe -Y sign -n git -f /dev/null`.
3. Captures the SSHSIG output.
4. Verifies it with `ssh-keygen -Y verify -n git -s <sig> -I principal -f allowed_signers`.
5. Asserts the signature validates.

Plus a real-git smoke: `git commit -S --allow-empty -m test` in a
session, `git log --show-signature` returns "Good signature."

## Open questions

- **RSA / ECDSA keys.** v1 ed25519-only keeps the parser minimal;
  RSA adds ~30 LOC and a key-size check. Worth doing if we see
  user demand; default-eds for the first ship.
- **Tag signing.** `tag.gpgsign = true` lights up automatically once
  the wrapper is in place. Worth a smoke test in Phase 5; no separate
  code path.
- **`ssh-keygen` standalone signing.** Some users run
  `ssh-keygen -Y sign` outside git context. Out of scope -- they can
  use a client-side ssh-agent for that case; we are not in the
  general-purpose ssh-agent business.
- **Sibling-pane creds gap (ADR-0044 v1.1 follow-up #1) hits this
  too.** A free Terminal pane committing won't see the chat
  session's signing key. Should be fixed once, alongside the cred
  storage scope decision, not separately for signing.
- **Sign-op approval gate.** Same shape as the deferred per-push
  approval gate. Round-trip a `confirm` to the UI on every sign-ssh,
  optionally with auto-approve windows. Defer to v2 unless we see
  agent commits we wish we hadn't signed.

## Effort estimate

| Piece | Size | Notes |
|---|---|---|
| Broker `sign-ssh` op + SSHSIG armor builder | M | ~150 LOC; pure stdlib + `x/crypto/ssh` |
| `sign_store.go` + WS handler ext | S | ~80 LOC, mirrors `cred_store.go` |
| `git-sign-swe-swe` binary | S | ~120 LOC, mirrors `git-credential-swe-swe` |
| Dockerfile build stage | S | Same shape as existing helper |
| `GIT_CONFIG_GLOBAL` ext for `gpg.format=ssh` | S | One block in `writeSessionGitconfig` |
| Settings UI section | M | Mostly copy-paste from PAT section + ed25519 parsing on submit |
| E2E test (WS round-trip + sign-verify) | M | Reuses the harness from `42688cd1d` |
| **Total** | **S/M** | One PR, ~1-2 day implementation, no new external deps |

## References

- ADR-0044: per-session credential broker (the protocol we're extending)
- Research doc: `research/2026-04-25-per-session-git-credentials.md`
- Existing helper for shape: `cmd/swe-swe/templates/host/git-credential-swe-swe/main.go`
- Existing WS handler for shape: `cmd/swe-swe/templates/host/swe-swe-server/main.go:4990`
- SSHSIG format: `PROTOCOL.sshsig` in OpenSSH source tree
- Git config knobs: `gpg.format`, `gpg.ssh.program`, `gpg.ssh.allowedSignersFile`,
  `commit.gpgsign`, `tag.gpgsign`
- GitLab SSH commit signing: 15.7 release notes (Dec 2022); profile -> SSH Keys -> usage="Signing"
- GitHub SSH commit signing: announced 2022-08; profile -> SSH and GPG keys -> "Signing key" type
