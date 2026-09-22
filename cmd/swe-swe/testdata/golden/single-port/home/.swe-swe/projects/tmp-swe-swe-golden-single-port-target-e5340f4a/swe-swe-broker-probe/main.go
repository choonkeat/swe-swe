// swe-swe-broker-probe is a measurement tool for the per-session credential
// broker (Option B: SO_PEERCRED on @swe-swe-broker abstract unix socket).
// See research/2026-04-25-per-session-git-credentials.md (addendum).
//
// It dials @swe-swe-broker, sends a JSON ping, prints the response, and
// exits. Always exits 0 -- it is a probe, not a guard.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

const brokerSocketName = "@swe-swe-broker"

func main() {
	conn, err := net.Dial("unix", brokerSocketName)
	if err != nil {
		fmt.Printf("dial %s failed: %v\n", brokerSocketName, err)
		return
	}
	defer conn.Close()

	req := map[string]any{
		"op":  "ping",
		"pid": os.Getpid(),
		"now": time.Now().Format(time.RFC3339Nano),
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fmt.Printf("write to broker failed: %v\n", err)
		return
	}

	var resp map[string]any
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Printf("read from broker failed: %v\n", err)
		return
	}

	out, err := json.Marshal(resp)
	if err != nil {
		fmt.Printf("marshal response failed: %v\n", err)
		return
	}
	fmt.Println(string(out))
}
