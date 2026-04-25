// swe-swe-broker-probe is a measurement tool for the per-session credential
// broker fd-passing technique. See research/2026-04-25-per-session-git-credentials.md.
//
// It reads SWE_SWE_BROKER_FD from the environment, opens that fd, sends a
// JSON ping, prints the response, and exits. Always exits 0 -- it's a probe,
// not a guard.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	fdStr := os.Getenv("SWE_SWE_BROKER_FD")
	if fdStr == "" {
		fmt.Println("SWE_SWE_BROKER_FD not set")
		return
	}
	fdNum, err := strconv.Atoi(fdStr)
	if err != nil {
		fmt.Printf("SWE_SWE_BROKER_FD invalid (%q): %v\n", fdStr, err)
		return
	}

	f := os.NewFile(uintptr(fdNum), "broker")
	if f == nil {
		fmt.Printf("fd %d not available\n", fdNum)
		return
	}
	defer f.Close()

	req := map[string]any{
		"op":  "ping",
		"pid": os.Getpid(),
		"now": time.Now().Format(time.RFC3339Nano),
	}
	if err := json.NewEncoder(f).Encode(req); err != nil {
		fmt.Printf("write to fd %d failed: %v\n", fdNum, err)
		return
	}

	// Read one line of response. Use a deadline-equivalent: just bufio with
	// a single ReadString. If the broker is dead, the read returns error.
	resp, err := bufio.NewReader(f).ReadString('\n')
	if err != nil {
		fmt.Printf("read from fd %d failed: %v\n", fdNum, err)
		return
	}
	fmt.Print(resp)
}
