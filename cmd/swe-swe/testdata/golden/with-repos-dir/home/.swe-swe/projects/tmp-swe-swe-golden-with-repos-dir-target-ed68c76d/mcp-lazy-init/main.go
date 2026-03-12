// mcp-lazy-init is a generic lazy-init MCP proxy.
//
// It wraps any stdio MCP server. Before the first tools/call reaches the
// wrapped server, it makes a configurable HTTP request. After that, it's a
// transparent relay.
//
// Usage:
//
//	mcp-lazy-init \
//	  --init-method POST \
//	  --init-url http://localhost:9898/api/session/$UUID/browser/start \
//	  -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$PORT
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	initMethod      string
	initURL         string
	initHeaders     []string // "Key: Value" pairs
	initRequestBody string
	command         []string // wrapped MCP server command + args
}

func parseArgs(args []string) (config, error) {
	var cfg config
	var i int
	for i = 0; i < len(args); i++ {
		switch args[i] {
		case "--init-method":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--init-method requires a value")
			}
			cfg.initMethod = args[i]
		case "--init-url":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--init-url requires a value")
			}
			cfg.initURL = args[i]
		case "--init-header":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--init-header requires a value")
			}
			cfg.initHeaders = append(cfg.initHeaders, args[i])
		case "--init-request-body":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--init-request-body requires a value")
			}
			cfg.initRequestBody = args[i]
		case "--":
			cfg.command = args[i+1:]
			if len(cfg.command) == 0 {
				return cfg, fmt.Errorf("no command specified after --")
			}
			return cfg, nil
		default:
			return cfg, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return cfg, fmt.Errorf("no command specified (missing -- separator)")
}

// jsonRPCMethod extracts the "method" field from a JSON-RPC message.
// Returns empty string if not found or not parseable.
func jsonRPCMethod(line []byte) string {
	var msg struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}
	return msg.Method
}

// doInit makes the configured HTTP request for lazy initialization.
func doInit(cfg config) error {
	var body io.Reader
	if cfg.initRequestBody != "" {
		body = strings.NewReader(cfg.initRequestBody)
	}

	req, err := http.NewRequest(cfg.initMethod, cfg.initURL, body)
	if err != nil {
		return fmt.Errorf("creating init request: %w", err)
	}

	for _, h := range cfg.initHeaders {
		parts := strings.SplitN(h, ": ", 2)
		if len(parts) == 2 {
			req.Header.Set(parts[0], parts[1])
		}
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("init request failed (%v): %w", elapsed, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[mcp-lazy-init] init %s %s → %d (%v): %s", cfg.initMethod, cfg.initURL, resp.StatusCode, elapsed, string(respBody))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("init request returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func run(cfg config, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	logger := log.New(stderr, "", log.LstdFlags)

	cmd := exec.Command(cfg.command[0], cfg.command[1:]...)
	cmd.Stderr = stderr

	subStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	subStdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting subprocess: %w", err)
	}
	logger.Printf("[mcp-lazy-init] started subprocess: %v (PID %d)", cfg.command, cmd.Process.Pid)

	var wg sync.WaitGroup

	// stdout relay: subprocess → our stdout (transparent)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(subStdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
		for scanner.Scan() {
			line := scanner.Bytes()
			stdout.Write(line)
			stdout.Write([]byte("\n"))
		}
	}()

	// stdin relay: our stdin → subprocess (with init interception)
	initDone := false
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		line := scanner.Bytes()

		if !initDone {
			method := jsonRPCMethod(line)
			if method == "tools/call" {
				logger.Printf("[mcp-lazy-init] intercepted tools/call, triggering init")
				if err := doInit(cfg); err != nil {
					logger.Printf("[mcp-lazy-init] init failed: %v (forwarding tools/call anyway)", err)
				}
				initDone = true
			}
		}

		subStdin.Write(line)
		subStdin.Write([]byte("\n"))
	}

	// stdin closed — kill subprocess
	subStdin.Close()

	// Wait for stdout relay to finish
	wg.Wait()

	// Wait for process to exit
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			logger.Printf("[mcp-lazy-init] subprocess exited with code %d", exitErr.ExitCode())
			return exitErr
		}
		return err
	}
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags)

	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-lazy-init: %v\n", err)
		fmt.Fprintf(os.Stderr, "Usage: mcp-lazy-init --init-method METHOD --init-url URL [--init-header 'Key: Value'] [--init-request-body BODY] -- COMMAND [ARGS...]\n")
		os.Exit(1)
	}

	if cfg.initMethod == "" || cfg.initURL == "" {
		fmt.Fprintf(os.Stderr, "mcp-lazy-init: --init-method and --init-url are required\n")
		os.Exit(1)
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("[mcp-lazy-init] received signal: %v, exiting", sig)
		os.Exit(0)
	}()

	if err := run(cfg, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		log.Fatalf("[mcp-lazy-init] %v", err)
	}
}
