// git-sign-swe-swe is the per-session SSH commit/tag signing helper
// for git's gpg.ssh.program hook.
//
// Wiring (per-session GIT_CONFIG_GLOBAL):
//
//	[gpg]
//	    format = ssh
//	[gpg "ssh"]
//	    program = git-sign-swe-swe
//	[user]
//	    signingkey = <pubkey path or literal "ssh-ed25519 AAAA...">
//	[commit]
//	    gpgsign = true
//
// On `git commit -S`, git invokes us as if we were ssh-keygen:
//
//	git-sign-swe-swe -Y sign -n git -f <keyfile> [<datafile>]
//
// We dial @swe-swe-broker, send {op:"sign-ssh", namespace, data},
// and write the armored SSHSIG returned by the broker. With a data
// file path on argv we write to <datafile>.sig (matching ssh-keygen
// -Y sign behavior); with no positional arg we read stdin and emit
// the armor on stdout.
//
// All real signing happens in swe-swe-server (the broker), which
// holds the session's SSH signing key in memory. The wrapper holds
// no key material -- anything that can run this binary AND lives
// in a registered session's process tree can produce a signature.
// Same SO_PEERCRED + ancestry-walk identity as the existing
// git-credential-swe-swe helper.
//
// Parent-comm gate: refuses to serve unless invoked by git. Same
// anti-leak posture as git-credential-swe-swe (commit 8a4fa87fb).
// Not a hard security boundary, but stops accidental
// signing-oracle output from showing up in chat transcripts,
// shell history, or agent tool buffers.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// var (not const) so tests can spin up a fake listener under a
// different abstract socket name without rebuilding the binary.
var brokerSocketName = "@swe-swe-broker"

func main() {
	fs := flag.NewFlagSet("git-sign-swe-swe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	action := fs.String("Y", "", "action: sign (only sign is supported)")
	namespace := fs.String("n", "git", "signature namespace (git for commits/tags)")
	keyFile := fs.String("f", "", "signing key file or literal (ignored; broker holds the signer)")
	options := newRepeatedString()
	fs.Var(options, "O", "ssh-keygen -O option=value (accepted and ignored)")
	_ = keyFile

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *action != "sign" {
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: only -Y sign is supported (got %q)\n", *action)
		os.Exit(2)
	}
	if !parentIsGit() {
		fmt.Fprintln(os.Stderr, "git-sign-swe-swe: refusing to serve - not invoked by git")
		os.Exit(1)
	}

	args := fs.Args()
	var (
		data    []byte
		sigPath string
		err     error
	)
	switch len(args) {
	case 0:
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-sign-swe-swe: read stdin: %v\n", err)
			os.Exit(1)
		}
	case 1:
		inputPath := args[0]
		data, err = os.ReadFile(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-sign-swe-swe: read %s: %v\n", inputPath, err)
			os.Exit(1)
		}
		sigPath = inputPath + ".sig"
	default:
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: expected zero or one input path; got %d\n", len(args))
		os.Exit(2)
	}

	armor, err := dialBrokerSign(*namespace, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: %v\n", err)
		os.Exit(1)
	}

	if sigPath != "" {
		if err := os.WriteFile(sigPath, []byte(armor), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "git-sign-swe-swe: write %s: %v\n", sigPath, err)
			os.Exit(1)
		}
		return
	}
	if _, err := os.Stdout.WriteString(armor); err != nil {
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: write stdout: %v\n", err)
		os.Exit(1)
	}
}

func dialBrokerSign(namespace string, data []byte) (string, error) {
	conn, err := net.Dial("unix", brokerSocketName)
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", brokerSocketName, err)
	}
	defer conn.Close()

	req := map[string]string{
		"op":        "sign-ssh",
		"namespace": namespace,
		"data":      base64.StdEncoding.EncodeToString(data),
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	var resp struct {
		Signature string `json:"signature"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("recv: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("broker: %s", resp.Error)
	}
	if resp.Signature == "" {
		return "", fmt.Errorf("broker returned empty signature")
	}
	return resp.Signature, nil
}

// parentIsGit returns true if the parent process's comm is "git".
// Same gate the credential helper uses: not a hard security
// boundary, but eliminates accidental-leak vectors.
func parentIsGit() bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", os.Getppid()))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "git"
}

// repeatedString is a flag.Value that accepts -O multiple times and
// silently records each value. Used to absorb ssh-keygen-compatible
// -O option=value flags that we don't act on.
type repeatedString struct{ vals []string }

func newRepeatedString() *repeatedString { return &repeatedString{} }

func (r *repeatedString) String() string     { return strings.Join(r.vals, ",") }
func (r *repeatedString) Set(s string) error { r.vals = append(r.vals, s); return nil }
