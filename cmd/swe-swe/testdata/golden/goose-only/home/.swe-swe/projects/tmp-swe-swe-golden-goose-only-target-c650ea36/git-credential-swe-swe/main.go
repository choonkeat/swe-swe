// git-credential-swe-swe is the per-session credential helper that asks
// the swe-swe broker for HTTPS credentials over @swe-swe-broker.
//
// Wired into git via env-injected GIT_CONFIG_*: see buildSessionEnv.
// Git invokes us as `git-credential-swe-swe <action>` where action is
// fill, store, or erase. v1 implements only fill; store/erase are
// silent no-ops because the broker is the source of truth.
//
// Stdin (for fill): key=value lines terminated by blank line. Standard
// keys: protocol, host, path, username, password.
// Stdout: same format. We emit username + password (and echo back the
// host/protocol). Spec: https://git-scm.com/docs/git-credential
//
// Fail-open: if the broker is unreachable or returns no cred for the
// host, we emit nothing. Git then falls back to the next configured
// helper (or prompts the user).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

const brokerSocketName = "@swe-swe-broker"

func main() {
	if len(os.Args) < 2 || os.Args[1] != "fill" {
		return
	}

	in := readKVPairs(os.Stdin)
	host := in["host"]
	if host == "" {
		return
	}

	conn, err := net.Dial("unix", brokerSocketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-credential-swe-swe: dial %s failed: %v\n", brokerSocketName, err)
		return
	}
	defer conn.Close()

	req := map[string]string{
		"op":       "get",
		"host":     host,
		"protocol": in["protocol"],
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fmt.Fprintf(os.Stderr, "git-credential-swe-swe: send failed: %v\n", err)
		return
	}

	var resp map[string]string
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(os.Stderr, "git-credential-swe-swe: recv failed: %v\n", err)
		return
	}
	if resp["error"] != "" {
		// No cred stored for this host -- silent fall-through.
		return
	}

	if p := in["protocol"]; p != "" {
		fmt.Printf("protocol=%s\n", p)
	}
	fmt.Printf("host=%s\n", host)
	if u := resp["username"]; u != "" {
		fmt.Printf("username=%s\n", u)
	}
	if p := resp["password"]; p != "" {
		fmt.Printf("password=%s\n", p)
	}
}

func readKVPairs(r *os.File) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			out[line[:eq]] = line[eq+1:]
		}
	}
	return out
}
