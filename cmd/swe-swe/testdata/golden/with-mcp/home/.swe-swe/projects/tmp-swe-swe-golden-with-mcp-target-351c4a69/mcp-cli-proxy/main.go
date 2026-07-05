// mcp-cli-proxy fronts exactly one stdio MCP server and exposes it over a unix
// socket, so a short-lived client (the `mcp` CLI) can call its tools without
// owning the server's stdio. It is the "host" half of swe-swe's MCP-less mode:
// one mcp-cli-proxy instance per MCP server, launched by the entrypoint with the
// same env the agent would get.
//
// Responsibilities:
//   - exec the `-- <argv>` command verbatim (no env expansion of its own; the
//     spec is a `sh -c "exec ..."` string so the shell expands $VARs).
//   - perform the MCP `initialize` handshake with the child once.
//   - listen on a unix socket; accept concurrent clients and multiplex them by
//     JSON-RPC id onto the child's single newline-delimited-JSON stdio.
//   - emit `notifications/cancelled` for a client's in-flight request when that
//     client disconnects.
//   - restart the child on exit (exponential backoff + crash-loop cap); the
//     socket never moves, so clients just retry.
//
// Wire protocol on the socket is identical to MCP stdio: newline-delimited
// JSON-RPC. The proxy has already `initialize`d the child, so clients send
// `tools/list` / `tools/call` requests directly. The proxy rewrites each
// request's id to a globally-unique internal id before forwarding, and maps the
// response id back to the client's original id on the way out.
//
// Usage:
//
//	mcp-cli-proxy --name swe-swe-agent-chat \
//	  --socket /workspace/.swe-swe/run/mcp/swe-swe-agent-chat.sock \
//	  -- sh -c 'exec npx -y @choonkeat/agent-chat ...'
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultProtocolVersion = "2024-11-05"
	defaultMaxRestarts     = 10
	maxLineBytes           = 16 * 1024 * 1024 // 16MB, matches large MCP payloads (images)
)

type config struct {
	name            string
	socketPath      string
	protocolVersion string
	maxRestarts     int
	command         []string
}

func parseArgs(args []string) (config, error) {
	cfg := config{
		protocolVersion: defaultProtocolVersion,
		maxRestarts:     defaultMaxRestarts,
	}
	var i int
	for i = 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--name requires a value")
			}
			cfg.name = args[i]
		case "--socket":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--socket requires a value")
			}
			cfg.socketPath = args[i]
		case "--protocol-version":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--protocol-version requires a value")
			}
			cfg.protocolVersion = args[i]
		case "--max-restarts":
			i++
			if i >= len(args) {
				return cfg, fmt.Errorf("--max-restarts requires a value")
			}
			if _, err := fmt.Sscanf(args[i], "%d", &cfg.maxRestarts); err != nil {
				return cfg, fmt.Errorf("--max-restarts must be an integer: %w", err)
			}
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

// pending is a client request awaiting the child's response.
type pending struct {
	conn   *clientConn
	origID json.RawMessage
}

// clientConn is one accepted socket connection.
type clientConn struct {
	net.Conn
	writeMu sync.Mutex
}

func (c *clientConn) writeLine(b []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.Conn.Write(b)
	c.Conn.Write([]byte("\n"))
}

// child is a running, initialized MCP server subprocess.
type child struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
}

func (c *child) writeLine(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(b); err != nil {
		return err
	}
	_, err := c.stdin.Write([]byte("\n"))
	return err
}

type proxy struct {
	cfg    config
	logger *log.Logger

	mu      sync.Mutex
	cur     *child           // current child, nil while (re)starting
	pending map[int64]pending // internalID -> waiting client
	nextID  int64

	dead atomic.Bool // set when the crash-loop cap is exceeded
}

func newProxy(cfg config, logger *log.Logger) *proxy {
	return &proxy{cfg: cfg, logger: logger, pending: map[int64]pending{}}
}

// rpcError writes a JSON-RPC error response for origID to conn.
func writeRPCError(conn *clientConn, origID json.RawMessage, code int, msg string) {
	if len(origID) == 0 {
		origID = json.RawMessage("null")
	}
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`, origID, code, msg)
	conn.writeLine([]byte(resp))
}

// idOnly is used to peek at the id field of an arbitrary JSON-RPC message.
type idOnly struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

// handleConn reads requests from one client and forwards them to the child,
// remapping ids. On disconnect it cancels the client's in-flight requests.
func (p *proxy) handleConn(nc net.Conn) {
	conn := &clientConn{Conn: nc}
	defer nc.Close()

	// internalIDs issued for this connection (for cancellation on disconnect).
	var mine []int64
	defer func() {
		p.mu.Lock()
		child := p.cur
		var cancelIDs []int64
		for _, id := range mine {
			if _, ok := p.pending[id]; ok {
				delete(p.pending, id)
				cancelIDs = append(cancelIDs, id)
			}
		}
		p.mu.Unlock()
		if child != nil {
			for _, id := range cancelIDs {
				notif := fmt.Sprintf(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":%d,"reason":"client disconnected"}}`, id)
				if err := child.writeLine([]byte(notif)); err != nil {
					p.logger.Printf("cancel forward failed for id %d: %v", id, err)
				}
			}
		}
	}()

	scanner := bufio.NewScanner(nc)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var peek idOnly
		if err := json.Unmarshal(line, &peek); err != nil {
			writeRPCError(conn, nil, -32700, "parse error: "+err.Error())
			continue
		}
		// Notifications (no id) are forwarded to the child as-is.
		if len(peek.ID) == 0 {
			p.mu.Lock()
			child := p.cur
			p.mu.Unlock()
			if child != nil {
				child.writeLine(line)
			}
			continue
		}

		internalID := atomic.AddInt64(&p.nextID, 1)
		remapped, err := replaceID(line, internalID)
		if err != nil {
			writeRPCError(conn, peek.ID, -32603, "internal: "+err.Error())
			continue
		}

		p.mu.Lock()
		child := p.cur
		if child == nil {
			p.mu.Unlock()
			if p.dead.Load() {
				writeRPCError(conn, peek.ID, -32000, "mcp server unavailable: "+p.cfg.name)
			} else {
				writeRPCError(conn, peek.ID, -32000, "mcp server restarting: "+p.cfg.name)
			}
			continue
		}
		p.pending[internalID] = pending{conn: conn, origID: peek.ID}
		mine = append(mine, internalID)
		p.mu.Unlock()

		if err := child.writeLine(remapped); err != nil {
			p.mu.Lock()
			delete(p.pending, internalID)
			p.mu.Unlock()
			writeRPCError(conn, peek.ID, -32000, "forward to mcp server failed: "+err.Error())
		}
	}
}

// replaceID rewrites the top-level "id" of a JSON-RPC message to newID.
func replaceID(line []byte, newID int64) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	m["id"] = json.RawMessage(fmt.Sprintf("%d", newID))
	return json.Marshal(m)
}

// routeResponse handles one line from the child's stdout: if it carries an id
// matching a pending request, translate the id back and deliver to that client.
func (p *proxy) routeResponse(line []byte) {
	var peek idOnly
	if err := json.Unmarshal(line, &peek); err != nil {
		p.logger.Printf("unparseable line from child: %v", err)
		return
	}
	if len(peek.ID) == 0 {
		// Server-initiated notification (progress/logging). Not tied to a
		// client request in this tools-only proxy; drop.
		return
	}
	var internalID int64
	if _, err := fmt.Sscanf(string(peek.ID), "%d", &internalID); err != nil {
		p.logger.Printf("non-integer id from child (%s); dropping", peek.ID)
		return
	}
	p.mu.Lock()
	pend, ok := p.pending[internalID]
	if ok {
		delete(p.pending, internalID)
	}
	p.mu.Unlock()
	if !ok {
		return // response to a cancelled/expired request
	}
	out, err := replaceIDRaw(line, pend.origID)
	if err != nil {
		p.logger.Printf("failed to restore client id: %v", err)
		return
	}
	pend.conn.writeLine(out)
}

// replaceIDRaw rewrites the top-level "id" with a raw JSON value.
func replaceIDRaw(line []byte, id json.RawMessage) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	m["id"] = id
	return json.Marshal(m)
}

// failAllPending responds to every outstanding request with an error, used when
// the child dies.
func (p *proxy) failAllPending(msg string) {
	p.mu.Lock()
	pend := p.pending
	p.pending = map[int64]pending{}
	p.mu.Unlock()
	for _, pr := range pend {
		writeRPCError(pr.conn, pr.origID, -32000, msg)
	}
}

// startChild execs the command, performs the initialize handshake, and starts
// the stdout router. It returns the child and a channel closed when the child
// exits (carrying the wait error).
func (p *proxy) startChild() (*child, error) {
	cmd := exec.Command(p.cfg.command[0], p.cfg.command[1:]...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	p.logger.Printf("started child %v (PID %d)", p.cfg.command, cmd.Process.Pid)

	c := &child{cmd: cmd, stdin: stdin}
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	// Handshake: initialize request, await response, then initialized notice.
	initReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"mcp-cli-proxy","version":"1"}}}`, p.cfg.protocolVersion)
	if err := c.writeLine([]byte(initReq)); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("send initialize: %w", err)
	}
	if !reader.Scan() {
		cmd.Process.Kill()
		cmd.Wait()
		if err := reader.Err(); err != nil {
			return nil, fmt.Errorf("read initialize response: %w", err)
		}
		return nil, fmt.Errorf("child closed stdout before initialize response")
	}
	// (We don't need the init result's contents; a successful line is enough.)
	if err := c.writeLine([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("send initialized: %w", err)
	}

	// stdout router
	go func() {
		for reader.Scan() {
			line := append([]byte(nil), reader.Bytes()...)
			p.routeResponse(line)
		}
	}()

	return c, nil
}

// supervise keeps the child alive, restarting with exponential backoff up to the
// crash-loop cap. It never returns while restarts remain.
func (p *proxy) supervise(stop <-chan struct{}) {
	backoff := 200 * time.Millisecond
	restarts := 0
	for {
		select {
		case <-stop:
			return
		default:
		}

		c, err := p.startChild()
		if err != nil {
			p.logger.Printf("child start failed: %v", err)
		} else {
			backoff = 200 * time.Millisecond // healthy start resets backoff
			p.mu.Lock()
			p.cur = c
			p.mu.Unlock()

			waitErr := c.cmd.Wait()
			p.mu.Lock()
			p.cur = nil
			p.mu.Unlock()
			p.failAllPending("mcp server exited: " + p.cfg.name)
			p.logChildExit(c, waitErr)
		}

		restarts++
		if restarts > p.cfg.maxRestarts {
			p.dead.Store(true)
			p.logger.Printf("crash-loop cap (%d) exceeded; marking %s unavailable", p.cfg.maxRestarts, p.cfg.name)
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func (p *proxy) logChildExit(c *child, waitErr error) {
	pid := -1
	if c.cmd.Process != nil {
		pid = c.cmd.Process.Pid
	}
	if waitErr == nil {
		p.logger.Printf("child %s (PID %d) exited cleanly", p.cfg.name, pid)
		return
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		p.logger.Printf("child %s (PID %d) exited: %s", p.cfg.name, pid, exitErr.String())
		return
	}
	p.logger.Printf("child %s (PID %d) wait error: %v", p.cfg.name, pid, waitErr)
}

func (p *proxy) serve(ln net.Listener, stop <-chan struct{}) {
	go func() {
		<-stop
		ln.Close()
	}()
	for {
		nc, err := ln.Accept()
		if err != nil {
			select {
			case <-stop:
				return
			default:
				p.logger.Printf("accept error: %v", err)
				return
			}
		}
		go p.handleConn(nc)
	}
}

func run(cfg config, logger *log.Logger) error {
	// Remove a stale socket from a previous run.
	if err := os.Remove(cfg.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing stale socket: %w", err)
	}
	ln, err := net.Listen("unix", cfg.socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.socketPath, err)
	}

	p := newProxy(cfg, logger)
	stop := make(chan struct{})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logger.Printf("received %v, shutting down", sig)
		close(stop)
		ln.Close()
		p.mu.Lock()
		c := p.cur
		p.mu.Unlock()
		if c != nil && c.cmd.Process != nil {
			c.cmd.Process.Signal(syscall.SIGTERM)
		}
		os.Remove(cfg.socketPath)
	}()

	go p.supervise(stop)
	p.serve(ln, stop)
	os.Remove(cfg.socketPath)
	return nil
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-cli-proxy: %v\n", err)
		fmt.Fprintf(os.Stderr, "Usage: mcp-cli-proxy --name NAME --socket PATH [--protocol-version V] [--max-restarts N] -- COMMAND [ARGS...]\n")
		os.Exit(1)
	}
	if cfg.name == "" || cfg.socketPath == "" {
		fmt.Fprintf(os.Stderr, "mcp-cli-proxy: --name and --socket are required\n")
		os.Exit(1)
	}
	logger := log.New(os.Stderr, fmt.Sprintf("[mcp-cli-proxy:%s] ", cfg.name), log.LstdFlags)
	if err := run(cfg, logger); err != nil {
		logger.Fatalf("%v", err)
	}
}
