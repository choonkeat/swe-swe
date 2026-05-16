// git-sign-swe-swe is the per-session SSH commit/tag signing helper
// for git's gpg.ssh.program hook.
//
// Wiring (per-session GIT_CONFIG_GLOBAL):
//
//	[gpg]
//	    format = ssh
//	[gpg "ssh"]
//	    program = git-sign-swe-swe
//	    allowedSignersFile = <path>
//	[user]
//	    signingkey = <pubkey path or literal "ssh-ed25519 AAAA...">
//	[commit]
//	    gpgsign = true
//
// Git invokes us as if we were ssh-keygen, for both directions:
//
//	-Y sign -n git -f <keyfile> [<datafile>]         (for commit/tag)
//	-Y check-novalidate -n git -s <sigfile>          (for verify, no allowed_signers)
//	-Y verify -n git -f <allowed_signers> -I <principal> -s <sigfile>
//
// For -Y sign we dial @swe-swe-broker, which holds the session's
// signing key in memory, and write the armored SSHSIG it returns.
// For -Y verify and -Y check-novalidate we re-exec the real
// ssh-keygen with the same argv: verification is pure-pubkey work
// (no key material needed) so there is no reason to route it
// through the broker.
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
	"os/exec"
	"strings"
)

// var (not const) so tests can spin up a fake listener under a
// different abstract socket name without rebuilding the binary.
var brokerSocketName = "@swe-swe-broker"

// var so tests can swap in a fake ssh-keygen-like binary.
var verifyExecBinary = "ssh-keygen"

func main() {
	if !parentIsGit() {
		fmt.Fprintln(os.Stderr, "git-sign-swe-swe: refusing to serve - not invoked by git")
		os.Exit(1)
	}

	// Pre-scan for -Y to decide route before any flag parsing. ssh-keygen
	// accepts short-form clustered flags like `-Overify-time=now` that the
	// Go flag package does not, so for verify-side actions we delegate to
	// ssh-keygen with the original argv and skip parsing entirely.
	action := extractYAction(os.Args[1:])
	switch action {
	case "sign":
		runSign(os.Args[1:])
	case "check-novalidate", "verify", "find-principals", "match-principals":
		os.Exit(runVerify(verifyExecBinary, os.Args[1:]))
	default:
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: -Y must be sign|check-novalidate|verify|find-principals|match-principals (got %q)\n", action)
		os.Exit(2)
	}
}

// extractYAction returns the value of the `-Y` flag, supporting both
// `-Y verify` (space-separated) and `-Y=verify` (= form). Returns the
// empty string if `-Y` is absent.
func extractYAction(argv []string) string {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "-Y" {
			if i+1 < len(argv) {
				return argv[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, "-Y=") {
			return strings.TrimPrefix(a, "-Y=")
		}
	}
	return ""
}

func runSign(argv []string) {
	fs := flag.NewFlagSet("git-sign-swe-swe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	_ = fs.String("Y", "", "action (sign)")
	namespace := fs.String("n", "git", "signature namespace (git for commits/tags)")
	keyFile := fs.String("f", "", "signing key file or literal (ignored; broker holds the signer)")
	options := newRepeatedString()
	fs.Var(options, "O", "ssh-keygen -O option=value (accepted and ignored)")
	_ = keyFile

	if err := fs.Parse(argv); err != nil {
		os.Exit(2)
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

// runVerify forwards the verification request to the real ssh-keygen.
// Verification needs only the public key (already in -f's allowed_signers
// or skipped under -Y check-novalidate) and the signature blob, so no
// broker round-trip is needed. Returns ssh-keygen's exit code.
func runVerify(binary string, argv []string) int {
	path, err := exec.LookPath(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: %s not found: %v\n", binary, err)
		return 1
	}
	cmd := exec.Command(path, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "git-sign-swe-swe: %s: %v\n", binary, err)
		return 1
	}
	return 0
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
