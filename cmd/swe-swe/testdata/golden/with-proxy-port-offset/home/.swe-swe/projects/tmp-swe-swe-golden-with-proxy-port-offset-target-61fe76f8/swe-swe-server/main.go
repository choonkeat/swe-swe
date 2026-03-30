package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	crypto_rand "crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	agentproxy "github.com/choonkeat/agent-reverse-proxy"
	recordtui "github.com/choonkeat/record-tui/playback"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hinshun/vt10x"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

//go:embed all:static
var staticFS embed.FS

//go:embed all:page-templates
var pageTemplatesFS embed.FS

//go:embed all:container-templates
var containerTemplatesFS embed.FS

//go:embed agent-chat-dist
var agentChatDistFS embed.FS

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "GOLDEN_TEST"
)

// recoverGoroutine logs panics from goroutines without crashing the server.
// Usage: defer recoverGoroutine("description")
func recoverGoroutine(where string) {
	if r := recover(); r != nil {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		log.Printf("PANIC recovered in %s: %v\n%s", where, r, buf[:n])
	}
}

var indexTemplate *template.Template
var selectionTemplate *template.Template

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// Chunked WebSocket constants for iOS Safari compatibility
// See: research/2026-01-04-ios-safari-websocket-chunking.md
const (
	// ChunkMarker identifies a chunked binary message (0x02)
	ChunkMarker = 0x02
	// DefaultChunkSize is 8KB - safe for iOS Safari WebSocket
	DefaultChunkSize = 8192
	// MinChunkSize prevents excessively small chunks
	MinChunkSize = 512
	// RingBufferSize is the size of the terminal scrollback ring buffer (512KB)
	RingBufferSize = 512 * 1024
)

var (
	previewPortStart   = 3000
	previewPortEnd     = 3019
	agentChatPortStart = 4000
	agentChatPortEnd   = 4019
	publicPortStart    = 5000
	publicPortEnd      = 5019
	cdpPortStart       = 6000
	cdpPortEnd         = 6019
	vncPortStart       = 7000
	vncPortEnd         = 7019
	proxyPortOffset    = 50000
)

func previewProxyPort(port int) int    { return proxyPortOffset + port }
func agentChatProxyPort(port int) int  { return proxyPortOffset + port }
func cdpProxyPort(port int) int        { return proxyPortOffset + port }
func vncProxyPort(port int) int        { return proxyPortOffset + port }

// agentChatClient is a shared HTTP client for the agent chat reverse proxy.
// Using a single client avoids leaking http.Transport instances (each with its
// own TLS state and connection pool) on every proxied request. See OOM crash
// investigation 2026-03-07.
var agentChatClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
	// Don't follow redirects automatically - let the browser handle them
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// ANSI escape sequence helpers for terminal formatting
func ansiCyan(s string) string   { return "\033[0;36m" + s + "\033[0m" }
func ansiDim(s string) string    { return "\033[2m" + s + "\033[0m" }
func ansiYellow(s string) string { return "\033[0;33m" + s + "\033[0m" }

// MOTDGracePeriod is how long to buffer input after displaying MOTD
// This gives users time to read the MOTD before the shell starts receiving input
const MOTDGracePeriod = 3 * time.Second

// dsrQuery is the Device Status Report escape sequence that terminals
// use to query cursor position. Applications (like Codex CLI via crossterm)
// send this and expect a response of the form \x1b[{row};{col}R.
var dsrQuery = []byte("\x1b[6n")

// TermSize represents terminal dimensions
type TermSize struct {
	Rows uint16
	Cols uint16
}

// SafeConn wraps a websocket.Conn with a mutex for thread-safe writes.
// gorilla/websocket doesn't support concurrent writes, so all writes
// must be serialized. This wrapper makes it impossible to forget the lock.
type SafeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// NewSafeConn wraps a websocket connection for thread-safe writes
func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{conn: conn}
}

// WriteMessage sends a message with the given type and payload (thread-safe)
func (sc *SafeConn) WriteMessage(messageType int, data []byte) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.WriteMessage(messageType, data)
}

// WriteJSON sends a JSON-encoded message (thread-safe)
func (sc *SafeConn) WriteJSON(v interface{}) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.WriteJSON(v)
}

// ReadMessage reads the next message (no lock needed - reads are already safe)
func (sc *SafeConn) ReadMessage() (messageType int, p []byte, err error) {
	return sc.conn.ReadMessage()
}

// Close closes the underlying connection
func (sc *SafeConn) Close() error {
	return sc.conn.Close()
}

// SlashCommandFormat represents the slash command format supported by an assistant
type SlashCommandFormat string

const (
	SlashCmdMD   SlashCommandFormat = "md"   // Markdown format (Claude, Codex, OpenCode)
	SlashCmdTOML SlashCommandFormat = "toml" // TOML format (Gemini)
	SlashCmdFile SlashCommandFormat = "file" // File mention (Goose, Aider)
	SlashCmdNone SlashCommandFormat = ""     // No commands (Shell)
)

// AssistantConfig holds the configuration for an AI coding assistant
type AssistantConfig struct {
	Name            string             // Display name
	ShellCmd        string             // Command to start the assistant
	ShellRestartCmd string             // Command to restart (resume) the assistant
	YoloShellCmd    string             // Command to start in YOLO mode (empty = not supported)
	YoloRestartCmd  string             // Command to restart in YOLO mode (empty = not supported)
	Binary          string             // Binary name to check with exec.LookPath
	Homepage        bool               // Whether to show on homepage (false = hidden, e.g., shell)
	SlashCmdFormat  SlashCommandFormat // Slash command format ("md", "toml", or "" for none)
}

// SessionInfo holds session data for template rendering
// SessionPageQuery holds the query parameters for a session page URL.
// Encode() produces the canonical query string used by both the Go
// templates and (mirrored in) the JS buildSessionPageUrl function.
type SessionPageQuery struct {
	Assistant   string // required -- agent binary name
	SessionMode string // "chat" or "terminal"; omit if terminal (default)
	Name        string // display name (optional)
	BranchName  string // git branch / worktree (optional)
	WorkDir     string // working directory; omit if "/workspace" (default)
	ParentUUID  string // parent session UUID (shell sub-sessions)
	Debug       bool   // debug UI flag
}

// Encode returns a URL-encoded query string (without leading "?").
// Returns template.URL so html/template won't double-escape the & and = characters
// when used inside href attributes.
func (q SessionPageQuery) Encode() template.URL {
	v := url.Values{}
	if q.Assistant != "" {
		v.Set("assistant", q.Assistant)
	}
	if q.SessionMode != "" && q.SessionMode != "terminal" {
		v.Set("session", q.SessionMode)
	}
	if q.Name != "" {
		v.Set("name", q.Name)
	}
	if q.BranchName != "" {
		v.Set("branch", q.BranchName)
	}
	if q.WorkDir != "" && q.WorkDir != "/workspace" {
		v.Set("pwd", q.WorkDir)
	}
	if q.ParentUUID != "" {
		v.Set("parent", q.ParentUUID)
	}
	if q.Debug {
		v.Set("debug", "1")
	}
	return template.URL(v.Encode())
}

type SessionInfo struct {
	UUID        string
	UUIDShort   string
	ClientCount int
	CreatedAt   time.Time
	DurationStr string // human-readable duration (e.g., "5m", "1h 23m")
	PublicPort  int    // PUBLIC_PORT env var value (e.g. 5000)
	Query       SessionPageQuery
	SummaryLine   string // One-line summary: "{who}: {message}" from last chat event or terminal
	SummaryStatus string // "green" (waiting for user) or "red" (agent busy) or "" (unknown)
	MemoryUsage   string // Human-readable RSS of session process tree (e.g. "1.2 GB")
}

// formatDuration returns a human-readable duration string
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)
	if d < time.Minute {
		return "<1m"
	}
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	if h > 0 {
		if m > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// AgentWithSessions groups an assistant with its active sessions
type AgentWithSessions struct {
	Assistant        AssistantConfig
	Sessions         []SessionInfo   // sorted by CreatedAt desc (most recent first)
	Recordings       []RecordingInfo // ended recordings for this agent (deprecated, use Recent/Kept)
	RecentRecordings []RecordingInfo // recent recordings (auto-deletable, not kept)
	KeptRecordings   []RecordingInfo // kept recordings (user explicitly kept)
}

// RecordingMetadata stores information about a terminal recording session
type RecordingMetadata struct {
	UUID          string     `json:"uuid"`
	Name          string     `json:"name,omitempty"`
	Agent         string     `json:"agent"`
	AgentBinary   string     `json:"agent_binary,omitempty"`   // binary name for URLs (e.g. "claude"); empty in old recordings
	RecordingType string     `json:"recording_type,omitempty"` // "agent", "chat", "terminal"
	SessionMode   string     `json:"session_mode,omitempty"`   // "terminal" or "chat"
	BranchName    string     `json:"branch_name,omitempty"`    // git branch / worktree name
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	KeptAt       *time.Time `json:"kept_at,omitempty"` // When user marked this recording to keep (nil = recent, auto-deletable)
	Command      []string   `json:"command"`
	Visitors     []Visitor  `json:"visitors,omitempty"`
	MaxCols      uint16     `json:"max_cols,omitempty"`      // Max terminal columns during recording
	MaxRows      uint16     `json:"max_rows,omitempty"`      // Max terminal rows during recording
	PlaybackCols uint16     `json:"playback_cols,omitempty"` // Content-based cols for playback (calculated at session end)
	PlaybackRows uint32     `json:"playback_rows,omitempty"` // Content-based rows for playback (calculated at session end)
	WorkDir      string     `json:"work_dir,omitempty"`      // Working directory for VS Code links in playback
}

// Visitor represents a client that joined the session
type Visitor struct {
	JoinedAt time.Time `json:"joined_at"`
	IP       string    `json:"ip"`
}

// Predefined assistant configurations (ordered for consistent display)
var assistantConfigs = []AssistantConfig{
	{
		Name:            "Claude",
		ShellCmd:        "claude",
		ShellRestartCmd: "claude --continue",
		YoloShellCmd:    "claude --dangerously-skip-permissions",
		YoloRestartCmd:  "claude --dangerously-skip-permissions --continue",
		Binary:          "claude",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdMD,
	},
	{
		Name:            "Gemini",
		ShellCmd:        "gemini",
		ShellRestartCmd: "gemini --resume",
		YoloShellCmd:    "gemini --approval-mode=yolo",
		YoloRestartCmd:  "gemini --resume --approval-mode=yolo",
		Binary:          "gemini",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdTOML,
	},
	{
		Name:            "Codex",
		ShellCmd:        "codex",
		ShellRestartCmd: "codex resume --last",
		YoloShellCmd:    "codex --yolo",
		YoloRestartCmd:  "codex --yolo resume --last",
		Binary:          "codex",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdMD,
	},
	{
		Name:            "Goose",
		ShellCmd:        "goose session",
		ShellRestartCmd: "goose session -r",
		YoloShellCmd:    "GOOSE_MODE=auto goose session",
		YoloRestartCmd:  "GOOSE_MODE=auto goose session -r",
		Binary:          "goose",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdFile,
	},
	{
		Name:            "Aider",
		ShellCmd:        "aider",
		ShellRestartCmd: "aider --restore-chat-history",
		YoloShellCmd:    "aider --yes-always",
		YoloRestartCmd:  "aider --yes-always --restore-chat-history",
		Binary:          "aider",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdFile,
	},
	{
		Name:            "OpenCode",
		ShellCmd:        "opencode",
		ShellRestartCmd: "opencode --continue",
		YoloShellCmd:    "", // YOLO mode not supported
		YoloRestartCmd:  "", // YOLO mode not supported
		Binary:          "opencode",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdMD,
	},
	{
		Name:     "Shell",
		Binary:   "shell",
		Homepage: false, // Hidden from homepage, accessed via status bar link
	},
}

// Session represents a terminal session with multiple clients
type Session struct {
	UUID            string
	Name            string // User-assigned session name (optional)
	BranchName      string // Git branch name for this session's worktree (derived from Name)
	WorkDir         string // Working directory for the session (empty = server cwd)
	Assistant       string // The assistant key (e.g., "claude", "gemini", "custom")
	AssistantConfig AssistantConfig
	Cmd             *exec.Cmd
	PTY             *os.File
	wsClients       map[*SafeConn]bool     // WebSocket clients (SafeConn for thread-safe writes)
	wsClientSizes   map[*SafeConn]TermSize // WebSocket client terminal sizes
	ptySize         TermSize               // Current PTY dimensions (for dedup)
	mu              sync.RWMutex
	CreatedAt       time.Time // when the session was created
	lastActive      time.Time
	vt              vt10x.Terminal // virtual terminal for screen state tracking
	vtMu            sync.Mutex     // separate mutex for VT operations
	// Ring buffer for terminal scrollback history
	ringBuf  []byte // circular buffer storage
	ringHead int    // write position (where next byte goes)
	ringLen  int    // current bytes stored (0 to RingBufferSize)
	// Recording
	RecordingUUID   string             // UUID for recording files (separate from session UUID for restarts)
	RecordingPrefix string             // Filename prefix: "session-{uuid}" or "session-{parent}-{child}"
	Metadata        *RecordingMetadata // Recording metadata (saved on name change or visitor join)
	// Parent session relationship
	ParentUUID  string // UUID of parent session (for shell sessions opened from agent sessions)
	PreviewPort   int // App preview target port for this session
	AgentChatPort int // Agent chat MCP server port for this session
	PublicPort    int // Public (no-auth) port for this session
	CDPPort       int // Chrome DevTools Protocol port for this session
	VNCPort       int // VNC port for browser view for this session
	BrowserPIDs    []int  // PIDs of browser processes (Xvfb, Chromium, x11vnc, noVNC)
	BrowserDataDir string // Per-session Chromium user data directory
	BrowserStarted bool   // Whether browser processes have been started
	// Input buffering during MOTD grace period
	inputBuffer   [][]byte // buffered input during grace period
	inputBufferMu sync.Mutex
	graceUntil    time.Time // buffer input until this time
	// YOLO mode state
	yoloMode           bool   // Whether YOLO mode is active
	pendingReplacement string // If set, replace process with this command instead of ending session
	// UI theme at session creation (for COLORFGBG env var)
	Theme string // "light" or "dark"
	// Agent Chat sidecar (nil for terminal-only sessions)
	AgentChatCmd    *exec.Cmd
	agentChatCancel context.CancelFunc // cancels sessionCtx (stops sidecar watcher)
	SessionMode     string             // "terminal" or "chat"
	// Per-session preview proxy (hosted in swe-swe-server, not a separate process)
	PreviewProxy          *agentproxy.Proxy // Per-session preview proxy instance
	SessionMux            http.Handler      // Handles /proxy/{uuid}/preview/ AND /proxy/{uuid}/agentchat/
	PreviewProxyServer    *http.Server      // Per-port listener for preview proxy (port-based mode)
	AgentChatProxyServer *http.Server // Per-port listener for agent chat proxy (port-based mode)
}

// computeRestartCommand returns the appropriate restart command based on YOLO mode.
// If yoloMode is true and the agent supports YOLO, returns YoloRestartCmd.
// Otherwise returns ShellRestartCmd.
func (s *Session) computeRestartCommand(yoloMode bool) string {
	if yoloMode && s.AssistantConfig.YoloRestartCmd != "" {
		return s.AssistantConfig.YoloRestartCmd
	}
	return s.AssistantConfig.ShellRestartCmd
}

// detectYoloMode checks if the given command contains YOLO mode flags.
// Returns true if any known YOLO flag is present.
func detectYoloMode(cmd string) bool {
	yoloPatterns := []string{
		"--dangerously-skip-permissions", // Claude
		"--approval-mode=yolo",           // Gemini
		"--yolo",                         // Codex
		"--yes-always",                   // Aider
		"GOOSE_MODE=auto",                // Goose
	}
	for _, pattern := range yoloPatterns {
		if strings.Contains(cmd, pattern) {
			return true
		}
	}
	return false
}

// SessionEnvParams holds the parameters for building a session's environment variables.
type SessionEnvParams struct {
	PreviewPort   int
	AgentChatPort int
	PublicPort    int
	CDPPort       int
	VNCPort       int
	Theme         string
	WorkDir       string
	SessionMode   string
}

func buildSessionEnv(p SessionEnvParams) []string {
	env := filterEnv(os.Environ(), "TERM", "PORT", "BROWSER", "PATH", "COLORFGBG", "AGENT_CHAT_PORT", "AGENT_CHAT_DISABLE", "PUBLIC_PORT", "BROWSER_CDP_PORT", "BROWSER_VNC_PORT")
	env = append(env,
		"TERM=xterm-256color",
		fmt.Sprintf("PORT=%d", p.PreviewPort),
		fmt.Sprintf("AGENT_CHAT_PORT=%d", p.AgentChatPort),
		fmt.Sprintf("PUBLIC_PORT=%d", p.PublicPort),
		fmt.Sprintf("BROWSER_CDP_PORT=%d", p.CDPPort),
		fmt.Sprintf("BROWSER_VNC_PORT=%d", p.VNCPort),
		"BROWSER=/home/app/.swe-swe/bin/swe-swe-open",
		"PATH=/home/app/.swe-swe/bin:"+os.Getenv("PATH"),
	)
	// Disable agent chat sidecar for non-chat sessions
	if p.SessionMode != "chat" {
		env = append(env, "AGENT_CHAT_DISABLE=1")
	}
	// Set COLORFGBG so CLI tools (vim, bat, ls --color, etc.) adapt to background
	if p.Theme == "light" {
		env = append(env, "COLORFGBG=0;15") // dark-on-light
	} else {
		env = append(env, "COLORFGBG=15;0") // light-on-dark
	}
	// Append user-defined vars from swe-swe/env (last so they take precedence)
	if p.WorkDir != "" {
		env = append(env, loadEnvFile(filepath.Join(p.WorkDir, "swe-swe", "env"))...)
	}
	return env
}

func loadEnvFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []string
	local := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok {
			val = os.Expand(val, func(k string) string {
				if v, ok := local[k]; ok {
					return v
				}
				return os.Getenv(k)
			})
			local[key] = val
			entries = append(entries, key+"="+val)
		}
	}
	return entries
}

func filterEnv(env []string, keys ...string) []string {
	if len(keys) == 0 {
		return env
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	filtered := env[:0]
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 0 {
			continue
		}
		if _, drop := keySet[parts[0]]; drop {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// AddClient adds a WebSocket client to the session
func (s *Session) AddClient(conn *SafeConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsClients[conn] = true
	s.lastActive = time.Now()
	log.Printf("Client added to session %s (total: %d)", s.UUID, len(s.wsClients))

	// Broadcast status after lock is released
	go s.BroadcastStatus()
}

// RemoveClient removes a WebSocket client from the session
func (s *Session) RemoveClient(conn *SafeConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.wsClients, conn)
	delete(s.wsClientSizes, conn)
	s.lastActive = time.Now()
	log.Printf("Client removed from session %s (total: %d)", s.UUID, len(s.wsClients))

	// Recalculate PTY size based on remaining clients
	if len(s.wsClientSizes) > 0 && s.PTY != nil {
		minRows, minCols := s.calculateMinSize()

		// Only resize if session's min size actually changed
		if s.ptySize.Rows != minRows || s.ptySize.Cols != minCols {
			s.ptySize = TermSize{Rows: minRows, Cols: minCols}
			pty.Setsize(s.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
			log.Printf("Session %s: resized PTY to %dx%d (from %d clients)", s.UUID, minCols, minRows, len(s.wsClientSizes))

			// Also resize the virtual terminal for accurate snapshots
			s.vtMu.Lock()
			s.vt.Resize(int(minCols), int(minRows))
			s.vtMu.Unlock()
		}
	}

	// Broadcast status after lock is released
	go s.BroadcastStatus()
}

// WriteInputOrBuffer writes data to PTY immediately, or buffers it during grace period
// Returns true if data was written/buffered successfully
func (s *Session) WriteInputOrBuffer(data []byte) error {
	s.inputBufferMu.Lock()
	defer s.inputBufferMu.Unlock()

	// During grace period, buffer the input
	if time.Now().Before(s.graceUntil) {
		// Make a copy since data slice may be reused
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		s.inputBuffer = append(s.inputBuffer, dataCopy)
		return nil
	}

	// Grace period over - flush any buffered input first
	if len(s.inputBuffer) > 0 {
		for _, buffered := range s.inputBuffer {
			if _, err := s.PTY.Write(buffered); err != nil {
				return err
			}
		}
		s.inputBuffer = nil
	}

	// Write current input
	_, err := s.PTY.Write(data)
	return err
}

// SetGracePeriod sets the input buffering grace period
func (s *Session) SetGracePeriod(d time.Duration) {
	s.inputBufferMu.Lock()
	defer s.inputBufferMu.Unlock()
	s.graceUntil = time.Now().Add(d)
}

// UpdateClientSize updates a client's terminal size and recalculates the PTY size
func (s *Session) UpdateClientSize(conn *SafeConn, rows, cols uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wsClientSizes[conn] = TermSize{Rows: rows, Cols: cols}
	s.lastActive = time.Now()

	// Calculate minimum size across all clients
	minRows, minCols := s.calculateMinSize()

	// Only resize if session's min size actually changed
	if s.ptySize.Rows == minRows && s.ptySize.Cols == minCols {
		return // No change to session size, skip resize to prevent flicker
	}

	s.ptySize = TermSize{Rows: minRows, Cols: minCols}

	// Track max dimensions for recording playback
	if s.Metadata != nil {
		if minCols > s.Metadata.MaxCols {
			s.Metadata.MaxCols = minCols
		}
		if minRows > s.Metadata.MaxRows {
			s.Metadata.MaxRows = minRows
		}
	}

	// Apply to PTY
	if s.PTY != nil {
		pty.Setsize(s.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
		log.Printf("Session %s: resized PTY to %dx%d (from %d clients)", s.UUID, minCols, minRows, len(s.wsClientSizes))
	}

	// Also resize the virtual terminal for accurate snapshots
	s.vtMu.Lock()
	s.vt.Resize(int(minCols), int(minRows))
	s.vtMu.Unlock()

	// Broadcast status after lock is released
	go s.BroadcastStatus()
}

// calculateMinSize returns the minimum rows and cols across all clients
// Must be called with lock held
func (s *Session) calculateMinSize() (uint16, uint16) {
	// Return default if no clients at all
	if len(s.wsClientSizes) == 0 {
		return 24, 80 // default size
	}

	var minRows, minCols uint16 = 0xFFFF, 0xFFFF

	// Include WebSocket client sizes
	for _, size := range s.wsClientSizes {
		if size.Rows < minRows {
			minRows = size.Rows
		}
		if size.Cols < minCols {
			minCols = size.Cols
		}
	}

	// Handle edge case where minRows/minCols were never set (shouldn't happen with above check)
	if minRows == 0xFFFF {
		minRows = 24
	}
	if minCols == 0xFFFF {
		minCols = 80
	}

	// Ensure minimum reasonable size
	if minRows < 1 {
		minRows = 1
	}
	if minCols < 1 {
		minCols = 1
	}

	return minRows, minCols
}

// ClientCount returns the number of connected WebSocket clients
func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wsClients)
}

// LastActive returns the last activity time
func (s *Session) LastActive() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActive
}

// Broadcast sends data to all connected clients
func (s *Session) Broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			log.Printf("Broadcast write error: %v", err)
		}
	}
}

// BroadcastStatus sends current session status (viewers, PTY size, assistant) to all clients
func (s *Session) BroadcastStatus() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, cols := s.calculateMinSize()
	uuidShort := s.UUID
	if len(s.UUID) >= 5 {
		uuidShort = s.UUID[:5]
	}
	workDir := s.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	// Only expose agentChatPort for chat sessions; terminal sessions
	// should never probe or show the Agent Chat tab.
	var agentChatPort int
	if s.SessionMode == "chat" {
		agentChatPort = s.AgentChatPort
	}
	status := map[string]interface{}{
		"type":               "status",
		"sessionUUID":        s.UUID,
		"viewers":            len(s.wsClients),
		"cols":               cols,
		"rows":               rows,
		"assistant":          s.AssistantConfig.Name,
		"sessionName":        s.Name,
		"uuidShort":          uuidShort,
		"workDir":            workDir,
		"previewPort":        s.PreviewPort,
		"agentChatPort":      agentChatPort,
		"previewProxyPort":   previewProxyPort(s.PreviewPort),
		"publicPort":         s.PublicPort,
		"cdpPort":            s.CDPPort,
		"vncPort":            s.VNCPort,
		"vncProxyPort":       vncProxyPort(s.VNCPort),
		"yoloMode":           s.yoloMode,
		"yoloSupported":      s.AssistantConfig.YoloRestartCmd != "",
		"browserStarted":     s.BrowserStarted,
	}
	if agentChatPort != 0 {
		status["agentChatProxyPort"] = agentChatProxyPort(agentChatPort)
	}

	data, err := json.Marshal(status)
	if err != nil {
		log.Printf("BroadcastStatus marshal error: %v", err)
		return
	}

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastStatus write error: %v", err)
		}
	}
	log.Printf("Session %s: broadcast status (viewers=%d, size=%dx%d)", s.UUID, len(s.wsClients), cols, rows)
}

// BroadcastChatMessage broadcasts a chat message to all connected clients
// without storing history
func (s *Session) BroadcastChatMessage(userName, text string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chatJSON := map[string]interface{}{
		"type":      "chat",
		"userName":  userName,
		"text":      text,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(chatJSON)
	if err != nil {
		log.Printf("BroadcastChatMessage marshal error: %v", err)
		return
	}

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastChatMessage write error: %v", err)
		}
	}
}

// buildExitMessage creates the exit message payload for a session.
// Includes worktree info if the session is running in a worktree.
func buildExitMessage(s *Session, exitCode int) map[string]interface{} {
	msg := map[string]interface{}{
		"type":     "exit",
		"exitCode": exitCode,
	}

	// Include worktree info if session is in a worktree
	if strings.HasPrefix(s.WorkDir, worktreeDir) && s.BranchName != "" {
		msg["worktree"] = map[string]string{
			"path":         s.WorkDir,
			"branch":       s.BranchName,
			"targetBranch": getMainRepoBranch(),
		}
	}

	return msg
}

// BroadcastExit sends a process exit notification to all connected clients
func (s *Session) BroadcastExit(exitCode int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exitJSON := buildExitMessage(s, exitCode)

	data, err := json.Marshal(exitJSON)
	if err != nil {
		log.Printf("BroadcastExit marshal error: %v", err)
		return
	}

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastExit write error: %v", err)
		}
	}
	log.Printf("Session %s: broadcast exit (code=%d)", s.UUID, exitCode)
}

// writeToRing writes data to the ring buffer, wrapping around when full.
// Must be called with vtMu held (shares lock with VT operations).
func (s *Session) writeToRing(data []byte) {
	for _, b := range data {
		s.ringBuf[s.ringHead] = b
		s.ringHead = (s.ringHead + 1) % RingBufferSize
		if s.ringLen < RingBufferSize {
			s.ringLen++
		}
	}
}

// readRing returns a copy of the ring buffer contents in correct order (oldest to newest).
// Must be called with vtMu held (shares lock with VT operations).
func (s *Session) readRing() []byte {
	if s.ringLen == 0 {
		return nil
	}

	result := make([]byte, s.ringLen)
	if s.ringLen < RingBufferSize {
		// Buffer not full yet, data starts at 0
		copy(result, s.ringBuf[:s.ringLen])
	} else {
		// Buffer is full, data starts at ringHead (oldest)
		start := s.ringHead
		copy(result, s.ringBuf[start:])
		copy(result[RingBufferSize-start:], s.ringBuf[:start])
	}
	return result
}

// Close terminates the session
func (s *Session) Close() {
	// Set EndedAt and save metadata before closing (must be done before acquiring lock
	// since saveMetadata also uses the lock)
	s.mu.Lock()
	if s.Metadata != nil && s.Metadata.EndedAt == nil {
		now := time.Now()
		s.Metadata.EndedAt = &now
	}
	s.mu.Unlock()
	if err := s.saveMetadata(); err != nil {
		log.Printf("Failed to save metadata on close: %v", err)
	}

	s.mu.Lock()

	// Cancel session context (used for coordinating shutdown of chat sessions)
	if s.agentChatCancel != nil {
		s.agentChatCancel()
	}

	// Shut down per-port proxy servers
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if s.PreviewProxyServer != nil {
		s.PreviewProxyServer.Shutdown(shutdownCtx)
	}
	if s.AgentChatProxyServer != nil {
		s.AgentChatProxyServer.Shutdown(shutdownCtx)
	}

	// Stop per-session browser processes (Xvfb, Chromium, x11vnc, noVNC)
	stopSessionBrowser(s)

	// Close all WebSocket client connections
	for conn := range s.wsClients {
		conn.Close()
	}
	s.wsClients = make(map[*SafeConn]bool)

	// Kill the process group and close PTY
	if s.Cmd != nil && s.Cmd.Process != nil {
		pid := s.Cmd.Process.Pid
		// Kill entire process group to clean up child processes
		log.Printf("[KILL] Session.Close: sending SIGKILL to process group -%d (server pid=%d)", pid, os.Getpid())
		syscall.Kill(-pid, syscall.SIGKILL)
		// Wait to reap the zombie process
		s.Cmd.Wait()
	}
	if s.PTY != nil {
		s.PTY.Close()
	}

	s.mu.Unlock()
	return
}

// compressSnapshot compresses data using gzip for efficient WebSocket transmission
func compressSnapshot(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sendChunked sends compressed data as multiple chunks for iOS Safari compatibility.
// Each chunk: [0x02, chunkIndex, totalChunks, ...data]
// Returns the number of chunks sent, or error.
func sendChunked(conn *SafeConn, data []byte, chunkSize int) (int, error) {
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
	}

	totalChunks := (len(data) + chunkSize - 1) / chunkSize
	if totalChunks > 255 {
		// Cap at 255 chunks (protocol limit with single byte for count)
		totalChunks = 255
		chunkSize = (len(data) + 254) / 255
	}
	if totalChunks == 0 {
		totalChunks = 1 // At least one chunk even for empty data
	}

	for i := 0; i < totalChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}

		// Build chunk: [marker, index, total, ...payload]
		chunk := make([]byte, 3+end-start)
		chunk[0] = ChunkMarker
		chunk[1] = byte(i)
		chunk[2] = byte(totalChunks)
		copy(chunk[3:], data[start:end])

		if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
			return i, err
		}
		log.Printf("Sent chunk %d/%d (%d bytes)", i+1, totalChunks, len(chunk)-3)
	}

	return totalChunks, nil
}

// GenerateSnapshot creates ANSI escape sequences to recreate the current screen state
// Returns gzip-compressed data for efficient transmission
func (s *Session) GenerateSnapshot() []byte {
	s.vtMu.Lock()
	defer s.vtMu.Unlock()

	var buf bytes.Buffer

	cols, rows := s.vt.Size()

	// Clear screen and move cursor to home
	buf.WriteString("\x1b[2J") // clear entire screen
	buf.WriteString("\x1b[H")  // cursor to home (1,1)

	// Track current attributes to minimize escape sequences
	var lastFG, lastBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG

	// Render each cell
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := s.vt.Cell(col, row)

			// Handle color changes
			if cell.FG != lastFG || cell.BG != lastBG {
				buf.WriteString("\x1b[0m") // reset attributes

				// Set foreground color
				if cell.FG != vt10x.DefaultFG && cell.FG < 256 {
					fmt.Fprintf(&buf, "\x1b[38;5;%dm", cell.FG)
				}
				// Set background color
				if cell.BG != vt10x.DefaultBG && cell.BG < 256 {
					fmt.Fprintf(&buf, "\x1b[48;5;%dm", cell.BG)
				}
				lastFG, lastBG = cell.FG, cell.BG
			}

			// Write character (or space if null)
			if cell.Char == 0 {
				buf.WriteRune(' ')
			} else {
				buf.WriteRune(cell.Char)
			}
		}
		// Move to next line (except for last row)
		if row < rows-1 {
			buf.WriteString("\r\n")
		}
	}

	// Reset attributes
	buf.WriteString("\x1b[0m")

	// Position cursor
	cursor := s.vt.Cursor()
	fmt.Fprintf(&buf, "\x1b[%d;%dH", cursor.Y+1, cursor.X+1)

	rawData := buf.Bytes()

	// Compress the snapshot
	compressed, err := compressSnapshot(rawData)
	if err != nil {
		log.Printf("Failed to compress snapshot, sending uncompressed: %v", err)
		return rawData
	}

	ratio := float64(len(compressed)) * 100 / float64(len(rawData))
	log.Printf("Snapshot compressed: %d -> %d bytes (%.1f%%)", len(rawData), len(compressed), ratio)

	return compressed
}

// RestartProcess restarts the shell process for this session
func (s *Session) RestartProcess(cmdStr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Wait on old process to reap zombie
	if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Wait()
	}

	// Close old PTY
	if s.PTY != nil {
		s.PTY.Close()
	}

	// Create new command and PTY
	cmdName, cmdArgs := parseCommand(cmdStr)

	// Wrap with script for recording (reuse existing recording prefix)
	cmdName, cmdArgs = wrapWithScript(cmdName, cmdArgs, s.RecordingPrefix)

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = buildSessionEnv(SessionEnvParams{
		PreviewPort:   s.PreviewPort,
		AgentChatPort: s.AgentChatPort,
		PublicPort:    s.PublicPort,
		CDPPort:       s.CDPPort,
		VNCPort:       s.VNCPort,
		Theme:         s.Theme,
		WorkDir:       s.WorkDir,
		SessionMode:   s.SessionMode,
	})
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}

	// Note: pty.Start sets Setsid=true which creates a new session AND process group,
	// so kill(-pid, sig) still works for process group cleanup. Don't set Setpgid here
	// because Setpgid + Setsid conflict (setpgid makes process a group leader, then
	// setsid fails with EPERM).
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	s.Cmd = cmd
	s.PTY = ptmx
	s.lastActive = time.Now()

	log.Printf("Restarted process for session %s (pid=%d, recording=%s)", s.UUID, cmd.Process.Pid, s.RecordingUUID)
	return nil
}

// startPTYReader starts the goroutine that reads from PTY and broadcasts to clients
// It restarts the process if it exits with a non-zero exit code (error)
// If the process exits with code 0 (success), it does not restart
func (s *Session) startPTYReader() {
	go func() {
		defer recoverGoroutine(fmt.Sprintf("PTY reader for session %s", s.UUID))
		buf := make([]byte, 4096)
		for {
			s.mu.RLock()
			ptyFile := s.PTY
			s.mu.RUnlock()

			n, err := ptyFile.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("PTY read error: %v", err)
				}

				// Process has died - check if we should restart
				s.mu.RLock()
				cmd := s.Cmd
				clientCount := len(s.wsClients)
				s.mu.RUnlock()

				// PTY can be broken while process is still alive (e.g., I/O error).
				// Kill the process group if still running, otherwise cmd.Wait() blocks forever.
				if cmd != nil && cmd.Process != nil {
					if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
						log.Printf("Session %s: PTY broken but process (pid %d) still alive, killing process group",
							s.UUID, cmd.Process.Pid)
						log.Printf("[KILL] PTY broken: sending SIGKILL to process group -%d (server pid=%d)", cmd.Process.Pid, os.Getpid())
						syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					}
				}

				// Wait on the process to reap the zombie and get exit status
				exitCode := 0
				if cmd != nil {
					if err := cmd.Wait(); err != nil {
						// Wait returns error for non-zero exit
						if exitErr, ok := err.(*exec.ExitError); ok {
							exitCode = exitErr.ExitCode()
						}
					}
				}

				// Check for pending replacement (e.g., from YOLO toggle)
				s.mu.Lock()
				replacementCmd := s.pendingReplacement
				s.pendingReplacement = "" // Clear after reading
				s.mu.Unlock()

				if replacementCmd != "" {
					log.Printf("Session %s: replacing process with command: %s", s.UUID, replacementCmd)
					if err := s.RestartProcess(replacementCmd); err != nil {
						log.Printf("Session %s: failed to replace process: %v", s.UUID, err)
						errMsg := []byte("\r\n[Failed to replace process: " + err.Error() + "]\r\n")
						s.vtMu.Lock()
						s.vt.Write(errMsg)
						s.writeToRing(errMsg)
						s.vtMu.Unlock()
						s.Broadcast(errMsg)
						// Fall through to end session
					} else {
						continue // Process replaced successfully, continue reading
					}
				}

				if clientCount == 0 {
					log.Printf("Session %s: process exited with no clients", s.UUID)
					// Save ended_at in metadata
					s.mu.Lock()
					if s.Metadata != nil {
						now := time.Now()
						s.Metadata.EndedAt = &now
					}
					s.mu.Unlock()
					if err := s.saveMetadata(); err != nil {
						log.Printf("Failed to save metadata on exit: %v", err)
					}
					return
				}

				// Session ends - save metadata and notify clients
				log.Printf("Session %s: process exited (code %d)", s.UUID, exitCode)
				s.mu.Lock()
				if s.Metadata != nil {
					now := time.Now()
					s.Metadata.EndedAt = &now
				}
				s.mu.Unlock()
				if err := s.saveMetadata(); err != nil {
					log.Printf("Failed to save metadata on exit: %v", err)
				}

				exitMsg := []byte(fmt.Sprintf("\r\n[Process exited (code %d)]\r\n", exitCode))
				s.vtMu.Lock()
				s.vt.Write(exitMsg)
				s.writeToRing(exitMsg)
				s.vtMu.Unlock()
				s.Broadcast(exitMsg)

				// Send structured exit message so browser can prompt user
				s.BroadcastExit(exitCode)
				return
			}

			data := buf[:n]

			// Intercept DSR (Device Status Report) cursor position queries.
			// Applications like Codex CLI send \x1b[6n and expect a response
			// \x1b[{row};{col}R. In a web terminal, the browser round-trip
			// is too slow for crossterm's timeout. Respond immediately from
			// the server using the current PTY size as the cursor position.
			if bytes.Contains(data, dsrQuery) {
				s.mu.RLock()
				rows := s.ptySize.Rows
				s.mu.RUnlock()
				if rows == 0 {
					rows = 24
				}
				response := []byte(fmt.Sprintf("\x1b[%d;1R", rows))
				s.PTY.Write(response)
			}

			// Update virtual terminal state and ring buffer
			s.vtMu.Lock()
			s.vt.Write(data)
			s.writeToRing(data)
			s.vtMu.Unlock()

			// Broadcast to all clients
			s.Broadcast(data)
		}
	}()
}

var (
	sessions            = make(map[string]*Session)
	sessionsMu          sync.RWMutex
	shellCmd            string
	shellRestartCmd     string
	workingDir          string
	availableAssistants []AssistantConfig // Populated at startup by detectAvailableAssistants

	// SSL certificate download endpoint
	tlsCertPath string // Path to TLS certificate file

	// serverCtx is cancelled on SIGINT/SIGTERM for graceful shutdown.
	// Session contexts derive from this so all processes are cleaned up.
	serverCtx context.Context

	// mcpAuthKey authenticates requests to the global orchestration MCP server.
	// Generated at boot, injected into sessions as MCP_AUTH_KEY env var.
	mcpAuthKey string
)

// detectAvailableAssistants checks which AI assistants are installed and populates availableAssistants.
// If -shell flag is provided, it adds a "custom" assistant.
// Returns an error if no assistants are available.
func detectAvailableAssistants() error {
	availableAssistants = nil

	// Check each predefined assistant
	for _, cfg := range assistantConfigs {
		// Non-homepage assistants (like shell) are always available
		if !cfg.Homepage {
			availableAssistants = append(availableAssistants, cfg)
			continue
		}
		// Homepage assistants need their binary to be installed
		if _, err := exec.LookPath(cfg.Binary); err == nil {
			log.Printf("Detected assistant: %s (%s)", cfg.Name, cfg.Binary)
			availableAssistants = append(availableAssistants, cfg)
		}
	}

	// Add custom assistant if -shell flag was provided (non-default)
	if shellCmd != "claude" || shellRestartCmd != "claude --continue" {
		log.Printf("Detected assistant: Custom (shell=%q, shell-restart=%q)", shellCmd, shellRestartCmd)
		availableAssistants = append(availableAssistants, AssistantConfig{
			Name:            "Custom",
			ShellCmd:        shellCmd,
			ShellRestartCmd: shellRestartCmd,
			Binary:          "custom",
		})
	}

	if len(availableAssistants) == 0 {
		return fmt.Errorf("no AI assistants available: install claude, gemini, codex, goose, or aider; or provide -shell flag")
	}

	return nil
}

// agentChatWaitingPage is shown by the agent chat proxy when the MCP sidecar
// hasn't started yet. It auto-polls and reloads once the backend is up.
// Note: %% is used to escape % characters in CSS for fmt.Fprintf
const agentChatWaitingPage = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Agent Chat</title>
    <script>
        (function(){var m=document.cookie.match(/(?:^|;\s*)swe-swe-theme=([^;]+)/);
        if(m)document.documentElement.setAttribute('data-theme',m[1]);})();
    </script>
    <style>
        :root {
            --pp-bg: #1e1e1e;
            --pp-text: #9ca3af;
            --pp-status: #6b7280;
        }
        [data-theme="light"] {
            --pp-bg: #ffffff;
            --pp-text: #64748b;
            --pp-status: #94a3b8;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 100vh;
            margin: 0;
            background: var(--pp-bg);
            color: var(--pp-text);
        }
        .status {
            font-size: 0.9rem;
            color: var(--pp-status);
        }
        .status-dot {
            display: inline-block;
            width: 6px;
            height: 6px;
            background: var(--pp-status);
            border-radius: 50%%;
            margin-right: 6px;
            animation: pulse 2s ease-in-out infinite;
        }
        @keyframes pulse {
            0%%, 100%% { opacity: 0.4; }
            50%% { opacity: 1; }
        }
    </style>
</head>
<body>
    <div class="status">
        <span class="status-dot"></span>
        <span>Waiting for Agent Chat...</span>
    </div>
    <script>
        async function checkApp() {
            try {
                const response = await fetch(window.location.href, { method: 'HEAD' });
                if (response.ok) {
                    window.location.reload();
                }
            } catch (e) {}
        }
        setInterval(checkApp, 3000);
    </script>
</body>
</html>`

// corsWrapper wraps an HTTP handler with CORS headers for cross-origin per-port
// proxy access. The browser on :1977 probes per-port listeners (e.g., :23000)
// which are cross-origin, so preflight and response headers are required.
//
// The /__probe__ path is handled inline: it returns CORS headers and
// X-Agent-Reverse-Proxy without proxying. This path bypasses ForwardAuth
// in Traefik (higher-priority router) so Safari's stricter cross-port
// CORS+credentials handling doesn't cause a fallback to path-based mode.
func corsWrapper(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Agent-Reverse-Proxy")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Lightweight reachability probe -- no auth, no proxy, just CORS + marker header.
		if r.URL.Path == "/__probe__" {
			w.Header().Set("X-Agent-Reverse-Proxy", "1")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// agentChatProxyHandler returns an HTTP handler that reverse-proxies requests
// to the agent chat app running on the given target URL.
func agentChatProxyHandler(target *url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mark responses so browser probes can detect our proxy.
		w.Header().Set("X-Agent-Reverse-Proxy", "1")

		// WebSocket upgrade detection: relay raw bytes instead of HTTP proxy
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			handleWebSocketRelay(w, r, target)
			return
		}

		// Build the target URL with the request path
		targetURL := *target
		targetURL.Path = singleJoiningSlash(target.Path, r.URL.Path)
		targetURL.RawQuery = r.URL.RawQuery

		// Create outgoing request
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
		if err != nil {
			log.Printf("Agent chat proxy: failed to create request: %v", err)
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			return
		}

		// Copy headers from incoming request
		for key, values := range r.Header {
			if isHopByHopHeader(key) {
				continue
			}
			for _, value := range values {
				outReq.Header.Add(key, value)
			}
		}

		// Set Host header to target host
		outReq.Host = target.Host

		// Make the request
		resp, err := agentChatClient.Do(outReq)
		if err != nil {
			log.Printf("Agent chat proxy error: %v", err)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, agentChatWaitingPage)
			return
		}
		defer resp.Body.Close()

		// Process response (copy headers, strip cookie Domain/Secure, stream body)
		processProxyResponse(w, resp, target)
	})
}

// processProxyResponse copies headers (stripping Domain from cookies), status, and body.
func processProxyResponse(w http.ResponseWriter, resp *http.Response, target *url.URL) {
	// Copy response headers, handling cookies specially
	for key, values := range resp.Header {
		if isHopByHopHeader(key) {
			continue
		}
		// Strip X-Frame-Options -- agent chat content is displayed in an iframe
		if strings.EqualFold(key, "X-Frame-Options") {
			continue
		}
		// Handle Set-Cookie specially to strip Domain attribute
		if strings.EqualFold(key, "Set-Cookie") {
			for _, cookie := range resp.Cookies() {
				// Strip Domain so cookie applies to proxy domain
				cookie.Domain = ""
				// Also strip Secure flag if we're proxying (allows cookies over non-HTTPS proxy)
				cookie.Secure = false
				http.SetCookie(w, cookie)
			}
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleWebSocketRelay upgrades both sides to WebSocket and relays frames
// between client and backend. Using gorilla/websocket on both sides gives
// each proxy hop a proper WebSocket handshake, which is required for
// multi-hop proxy chains (e.g. Cloudflare -> cloudflared -> Traefik).
func handleWebSocketRelay(w http.ResponseWriter, r *http.Request, target *url.URL) {
	// Dial backend FIRST -- if it's down we can still return HTTP 502
	backendWsURL := fmt.Sprintf("ws://%s%s", target.Host, r.URL.RequestURI())
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	backendConn, _, err := dialer.Dial(backendWsURL, nil)
	if err != nil {
		log.Printf("Agent chat proxy: WebSocket backend dial error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// Upgrade client connection
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Agent chat proxy: WebSocket client upgrade error: %v", err)
		return
	}
	defer clientConn.Close()

	// Bidirectional frame relay
	errc := make(chan error, 2)
	// backend -> client
	go func() {
		defer recoverGoroutine("WebSocket relay backend->client")
		for {
			mt, msg, err := backendConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				errc <- err
				return
			}
		}
	}()
	// client -> backend
	go func() {
		defer recoverGoroutine("WebSocket relay client->backend")
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := backendConn.WriteMessage(mt, msg); err != nil {
				errc <- err
				return
			}
		}
	}()
	<-errc // first error closes both via defers
}

// isHopByHopHeader returns true if the header is a hop-by-hop header
func isHopByHopHeader(header string) bool {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}
	return hopByHop[http.CanonicalHeaderKey(header)]
}

// singleJoiningSlash joins two URL paths properly
func singleJoiningSlash(a, b string) string {
	aSlash := strings.HasSuffix(a, "/")
	bSlash := strings.HasPrefix(b, "/")
	switch {
	case aSlash && bSlash:
		return a + b[1:]
	case !aSlash && !bSlash:
		return a + "/" + b
	}
	return a + b
}

func main() {
	addr := flag.String("addr", ":9898", "Listen address")
	version := flag.Bool("version", false, "Show version and exit")
	dumpTemplates := flag.String("dump-container-templates", "", "Dump all container templates to directory and exit")
	flag.StringVar(&shellCmd, "shell", "claude", "Command to execute")
	flag.StringVar(&shellRestartCmd, "shell-restart", "claude --continue", "Command to restart on process death")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	flag.Parse()

	// Override preview port range from environment (set by docker-compose)
	if portRange := os.Getenv("SWE_PREVIEW_PORTS"); portRange != "" {
		if parts := strings.SplitN(portRange, "-", 2); len(parts) == 2 {
			if start, err := strconv.Atoi(parts[0]); err == nil {
				if end, err := strconv.Atoi(parts[1]); err == nil {
					previewPortStart = start
					previewPortEnd = end
				}
			}
		}
	}

	// Override agent chat port range from environment (set by docker-compose)
	if portRange := os.Getenv("SWE_AGENT_CHAT_PORTS"); portRange != "" {
		if parts := strings.SplitN(portRange, "-", 2); len(parts) == 2 {
			if start, err := strconv.Atoi(parts[0]); err == nil {
				if end, err := strconv.Atoi(parts[1]); err == nil {
					agentChatPortStart = start
					agentChatPortEnd = end
				}
			}
		}
	}

	// Override public port range from environment (set by docker-compose)
	if portRange := os.Getenv("SWE_PUBLIC_PORTS"); portRange != "" {
		if parts := strings.SplitN(portRange, "-", 2); len(parts) == 2 {
			if start, err := strconv.Atoi(parts[0]); err == nil {
				if end, err := strconv.Atoi(parts[1]); err == nil {
					publicPortStart = start
					publicPortEnd = end
				}
			}
		}
	}

	// Override CDP port range from environment (set by docker-compose)
	if portRange := os.Getenv("SWE_CDP_PORTS"); portRange != "" {
		if parts := strings.SplitN(portRange, "-", 2); len(parts) == 2 {
			if start, err := strconv.Atoi(parts[0]); err == nil {
				if end, err := strconv.Atoi(parts[1]); err == nil {
					cdpPortStart = start
					cdpPortEnd = end
				}
			}
		}
	}

	// Override VNC port range from environment (set by docker-compose)
	if portRange := os.Getenv("SWE_VNC_PORTS"); portRange != "" {
		if parts := strings.SplitN(portRange, "-", 2); len(parts) == 2 {
			if start, err := strconv.Atoi(parts[0]); err == nil {
				if end, err := strconv.Atoi(parts[1]); err == nil {
					vncPortStart = start
					vncPortEnd = end
				}
			}
		}
	}

	// Override proxy port offset from environment (set by docker-compose / .env)
	if offsetStr := os.Getenv("SWE_PROXY_PORT_OFFSET"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			proxyPortOffset = v
		}
	}

	// Generate auth key for global MCP orchestration endpoint
	authKeyBytes := make([]byte, 32)
	crypto_rand.Read(authKeyBytes)
	mcpAuthKey = hex.EncodeToString(authKeyBytes)

	// Handle --version flag
	if *version {
		fmt.Printf("swe-swe-server %s (%s)\n", Version, GitCommit)
		os.Exit(0)
	}

	// Handle --dump-container-templates flag
	if *dumpTemplates != "" {
		if err := dumpContainerTemplates(*dumpTemplates); err != nil {
			log.Fatalf("Failed to dump container templates: %v", err)
		}
		os.Exit(0)
	}

	log.Printf("swe-swe-server %s (%s)", Version, GitCommit)

	// Change to working directory if specified
	if workingDir != "" {
		if err := os.Chdir(workingDir); err != nil {
			log.Fatalf("Failed to change to working directory %q: %v", workingDir, err)
		}
		log.Printf("Changed to working directory: %s", workingDir)
	}

	// Detect available AI assistants
	if err := detectAvailableAssistants(); err != nil {
		log.Fatal(err)
	}
	log.Printf("Available assistants: %d", len(availableAssistants))

	// Initialize SSL certificate download endpoint
	tlsCertPath = os.Getenv("TLS_CERT_PATH")
	if tlsCertPath == "" {
		tlsCertPath = "/etc/traefik/tls/server.crt"
	}
	if _, err := os.Stat(tlsCertPath); err == nil {
		log.Printf("SSL certificate available at: /ssl/ca.crt")
	}

	// Parse templates
	indexContent, err := pageTemplatesFS.ReadFile("page-templates/index.html")
	if err != nil {
		log.Fatal(err)
	}
	indexTemplate, err = template.New("index").Parse(string(indexContent))
	if err != nil {
		log.Fatal(err)
	}

	selectionContent, err := pageTemplatesFS.ReadFile("page-templates/selection.html")
	if err != nil {
		log.Fatal(err)
	}
	selectionTemplate, err = template.New("selection").Parse(string(selectionContent))
	if err != nil {
		log.Fatal(err)
	}

	// Serve static files from embedded filesystem
	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	staticHandler := http.FileServer(http.FS(staticContent))

	// Handler for terminal-ui.js with template substitution (dev mode compatibility)
	http.HandleFunc("/terminal-ui.js", func(w http.ResponseWriter, r *http.Request) {
		content, err := staticFS.ReadFile("static/terminal-ui.js")
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		// Replace template variables with defaults for dev mode
		result := strings.ReplaceAll(string(content), "{{TERMINAL_FONT_SIZE}}", "14")
		result = strings.ReplaceAll(result, "{{TERMINAL_FONT_FAMILY}}", "Monaco, Menlo, Consolas, monospace")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(result))
	})

	// Start session reaper
	go sessionReaper()

	// Global MCP orchestration server
	orchMCPSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "swe-swe",
		Version: Version,
	}, nil)
	if err := registerOrchestrationTools(orchMCPSrv); err != nil {
		log.Printf("WARNING: %v", err)
	}
	orchHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return orchMCPSrv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	http.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != mcpAuthKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		orchHandler.ServeHTTP(w, r)
	}))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Root path: show assistant selection page
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// No-cache for homepage to ensure latest version
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			// Prevent homepage from being embedded in iframes to avoid nested UI
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")

			// Check for debug query param
			debugParam := r.URL.Query().Get("debug")
			debugMode := debugParam == "true" || debugParam == "1"

			// Build agents with their sessions
			sessionsByAssistant := make(map[string][]SessionInfo)

			// Collect session info and data needed for summaries under the read lock
			type sessionSummaryInput struct {
				assistant    string
				index        int
				recordingUUID string
				sessionMode  string
				sess         *Session
				pid          int // root PID for memory tracking
			}
			var summaryInputs []sessionSummaryInput

			sessionsMu.RLock()
			for _, sess := range sessions {
				// Skip sessions where process has exited
				if sess.Cmd.ProcessState != nil {
					continue
				}

				uuidShort := sess.UUID
				if len(sess.UUID) >= 5 {
					uuidShort = sess.UUID[:5]
				}

				info := SessionInfo{
					UUID:        sess.UUID,
					UUIDShort:   uuidShort,
					ClientCount: sess.ClientCount(),
					CreatedAt:   sess.CreatedAt,
					DurationStr: formatDuration(time.Since(sess.CreatedAt)),
					PublicPort:  sess.PublicPort,
					Query: SessionPageQuery{
						Assistant:   sess.Assistant,
						SessionMode: sess.SessionMode,
						Name:        sess.Name,
						BranchName:  sess.BranchName,
						WorkDir:     sess.WorkDir,
						ParentUUID:  sess.ParentUUID,
						Debug:       debugMode,
					},
				}
				idx := len(sessionsByAssistant[sess.Assistant])
				sessionsByAssistant[sess.Assistant] = append(sessionsByAssistant[sess.Assistant], info)
				var pid int
				if sess.Cmd != nil && sess.Cmd.Process != nil {
					pid = sess.Cmd.Process.Pid
				}
				summaryInputs = append(summaryInputs, sessionSummaryInput{
					assistant:     sess.Assistant,
					index:         idx,
					recordingUUID: sess.RecordingUUID,
					sessionMode:   sess.SessionMode,
					sess:          sess,
					pid:           pid,
				})
			}
			sessionsMu.RUnlock()

			// Populate session summaries outside the lock (involves file I/O)
			for _, si := range summaryInputs {
				var summaryLine, status string
				if si.sessionMode == "chat" {
					summaryLine, status = getSessionSummaryFromChat(si.recordingUUID)
				}
				if summaryLine == "" {
					// Fallback to terminal output
					termLine := getSessionSummaryFromTerminal(si.sess)
					if termLine != "" {
						summaryLine = termLine
					}
				}
				sessionsByAssistant[si.assistant][si.index].SummaryLine = summaryLine
				sessionsByAssistant[si.assistant][si.index].SummaryStatus = status
				if si.pid > 0 {
					rss := getProcessTreeRSS(si.pid)
					if rss > 0 {
						sessionsByAssistant[si.assistant][si.index].MemoryUsage = formatBytes(rss)
					}
				}
			}

			// Sort sessions within each assistant by CreatedAt desc (most recent first)
			for assistant := range sessionsByAssistant {
				sort.Slice(sessionsByAssistant[assistant], func(i, j int) bool {
					return sessionsByAssistant[assistant][i].CreatedAt.After(sessionsByAssistant[assistant][j].CreatedAt)
				})
			}

			// Load recordings (sorted by timestamp) and group by agent for per-agent sections
			recordings := loadEndedRecordings()
			recordingsByAgent := loadEndedRecordingsByAgent(recordings)

			const defaultRecordingsPerPage = 10
			recordingsPerPage := defaultRecordingsPerPage
			if sizeStr := r.URL.Query().Get("recordings_page_size"); sizeStr != "" {
				if sizeVal, err := strconv.Atoi(sizeStr); err == nil {
					switch {
					case sizeVal < 1:
						recordingsPerPage = 1
					case sizeVal > 200:
						recordingsPerPage = 200
					default:
						recordingsPerPage = sizeVal
					}
				}
			}
			recordingsPage := 1
			if pageStr := r.URL.Query().Get("recordings_page"); pageStr != "" {
				if pageVal, err := strconv.Atoi(pageStr); err == nil && pageVal > 0 {
					recordingsPage = pageVal
				}
			}
			recordingsTotalPages := 0
			if len(recordings) > 0 {
				recordingsTotalPages = (len(recordings) + recordingsPerPage - 1) / recordingsPerPage
				if recordingsPage > recordingsTotalPages {
					recordingsPage = recordingsTotalPages
				}
				start := (recordingsPage - 1) * recordingsPerPage
				end := start + recordingsPerPage
				if end > len(recordings) {
					end = len(recordings)
				}
				recordings = recordings[start:end]
			}

			// Propagate debug flag to recording restart queries
			if debugMode {
				for i := range recordings {
					recordings[i].Query.Debug = true
				}
			}

			// Build AgentWithSessions for all available assistants (homepage only)
			agents := make([]AgentWithSessions, 0, len(availableAssistants))
			for _, assistant := range availableAssistants {
				// Skip non-homepage assistants (like shell)
				if !assistant.Homepage {
					continue
				}
				recordings := recordingsByAgent[assistant.Binary]
				// Split recordings into recent and kept
				var recentRecordings, keptRecordings []RecordingInfo
				for _, rec := range recordings {
					if rec.IsKept {
						keptRecordings = append(keptRecordings, rec)
					} else {
						recentRecordings = append(recentRecordings, rec)
					}
				}
				agents = append(agents, AgentWithSessions{
					Assistant:        assistant,
					Sessions:         sessionsByAssistant[assistant.Binary], // nil if no sessions
					Recordings:       recordings,                            // all recordings (for backward compat)
					RecentRecordings: recentRecordings,
					KeptRecordings:   keptRecordings,
				})
			}

			// Check if SSL certificate is available
			_, hasSSLCert := os.Stat(tlsCertPath)

			// Get the workspace origin URL for the default repo
			defaultRepoUrl, err := getWorkspaceOriginURL()
			if err != nil {
				// Fallback to /workspace if we can't get origin URL
				defaultRepoUrl = "/workspace"
			}

			prevPage := recordingsPage - 1
			if prevPage < 1 {
				prevPage = 1
			}
			nextPage := recordingsPage + 1
			if recordingsTotalPages > 0 && nextPage > recordingsTotalPages {
				nextPage = recordingsTotalPages
			}

			data := struct {
				Agents               []AgentWithSessions
				Recordings           []RecordingInfo
				RecordingsPage       int
				RecordingsTotalPages int
				RecordingsHasPrev    bool
				RecordingsHasNext    bool
				RecordingsPrevPage   int
				RecordingsNextPage   int
				NewUUID              string
				HasSSLCert           bool
				Debug                bool
				DefaultRepoUrl       string
				Version              string
			}{
				Agents:               agents,
				Recordings:           recordings,
				RecordingsPage:       recordingsPage,
				RecordingsTotalPages: recordingsTotalPages,
				RecordingsHasPrev:    recordingsPage > 1,
				RecordingsHasNext:    recordingsTotalPages > 0 && recordingsPage < recordingsTotalPages,
				RecordingsPrevPage:   prevPage,
				RecordingsNextPage:   nextPage,
				NewUUID:              uuid.New().String(),
				HasSSLCert:           hasSSLCert == nil,
				Debug:                debugMode,
				DefaultRepoUrl:       defaultRepoUrl,
				Version:              Version + " (" + GitCommit + ")",
			}
			if err := selectionTemplate.Execute(w, data); err != nil {
				log.Printf("Selection template error: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		// Session proxy: /proxy/{uuid}/{preview|agentchat}/...
		if strings.HasPrefix(r.URL.Path, "/proxy/") {
			handleProxyRoute(w, r)
			return
		}

		// WebSocket path: handle WebSocket connection
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			sessionUUID := strings.TrimPrefix(r.URL.Path, "/ws/")
			handleWebSocket(w, r, sessionUUID)
			return
		}

		// SSL certificate download: /ssl/ca.crt
		if r.URL.Path == "/ssl/ca.crt" {
			handleSSLCertDownload(w, r)
			return
		}

		// Worktrees API endpoint
		if r.URL.Path == "/api/worktrees" {
			handleWorktreesAPI(w, r)
			return
		}

		// Worktree check API endpoint
		if r.URL.Path == "/api/worktree/check" {
			handleWorktreeCheckAPI(w, r)
			return
		}

		// Repos list API endpoint
		if r.URL.Path == "/api/repos" {
			handleReposAPI(w, r)
			return
		}

		// Repo prepare API endpoint (clone/fetch)
		if r.URL.Path == "/api/repo/prepare" {
			handleRepoPrepareAPI(w, r)
			return
		}

		// Repo branches API endpoint
		if r.URL.Path == "/api/repo/branches" {
			handleRepoBranchesAPI(w, r)
			return
		}

		// Autocomplete API endpoint (for agent-chat slash command completion)
		if strings.HasPrefix(r.URL.Path, "/api/autocomplete/") {
			handleAutocompleteAPI(w, r)
			return
		}

		// Recording API endpoints
		if strings.HasPrefix(r.URL.Path, "/api/recording/") {
			handleRecordingAPI(w, r)
			return
		}

		// Session end API endpoint
		if strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/end") {
			handleSessionEndAPI(w, r)
			return
		}

		// Browser start API endpoint (on-demand browser)
		if strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/browser/start") {
			handleBrowserStartAPI(w, r)
			return
		}

		// VNC readiness probe (same-origin, avoids cross-origin opaque response issues)
		if strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/vnc-ready") {
			handleVNCReadyAPI(w, r)
			return
		}

		// Recording playback page and raw session data
		if strings.HasPrefix(r.URL.Path, "/recording/") {
			path := strings.TrimPrefix(r.URL.Path, "/recording/")
			if path == "" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			// Serve chat events JSONL file
			if strings.HasSuffix(path, "/chat.events.jsonl") {
				parentUUID := strings.TrimSuffix(path, "/chat.events.jsonl")
				handleChatEventsFile(w, r, parentUUID)
				return
			}

			// Serve chat playback page
			if strings.HasSuffix(path, "/chat") {
				parentUUID := strings.TrimSuffix(path, "/chat")
				handleChatPlaybackPage(w, r, parentUUID)
				return
			}

			// Serve raw session.log for streaming
			if strings.HasSuffix(path, "/session.log") {
				recordingUUID := strings.TrimSuffix(path, "/session.log")
				handleRecordingSessionLog(w, r, recordingUUID)
				return
			}

			// Serve streaming HTML page
			handleRecordingPage(w, r, path)
			return
		}

		// Session path: serve template with UUID and assistant
		if strings.HasPrefix(r.URL.Path, "/session/") {
			sessionUUID := strings.TrimPrefix(r.URL.Path, "/session/")
			if sessionUUID == "" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			// Get assistant from query param
			assistant := r.URL.Query().Get("assistant")
			if assistant == "" {
				// No assistant specified, redirect to selection
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			// Validate assistant exists in available list and get its display name
			var validAssistant bool
			var assistantName string
			for _, a := range availableAssistants {
				if a.Binary == assistant {
					validAssistant = true
					assistantName = a.Name
					break
				}
			}
			if !validAssistant {
				log.Printf("Invalid assistant requested: %s", assistant)
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}

			// If session already exists with different assistant, redirect to correct URL
			// preserving all other query params (pwd, branch, name, session, etc.)
			sessionsMu.RLock()
			existingSession, exists := sessions[sessionUUID]
			sessionsMu.RUnlock()
			if exists && existingSession.Assistant != assistant {
				q := r.URL.Query()
				q.Set("assistant", existingSession.Assistant)
				correctURL := fmt.Sprintf("/session/%s?%s", sessionUUID, q.Encode())
				log.Printf("Session %s exists with assistant=%s, redirecting from %s", sessionUUID, existingSession.Assistant, assistant)
				http.Redirect(w, r, correctURL, http.StatusFound)
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			uuidShort := sessionUUID
			if len(sessionUUID) >= 5 {
				uuidShort = sessionUUID[:5]
			}
			data := struct {
				UUID          string
				UUIDShort     string
				Assistant     string
				AssistantName string
				Version       string
			}{
				UUID:          sessionUUID,
				UUIDShort:     uuidShort,
				Assistant:     assistant,
				AssistantName: assistantName,
				Version:       Version + "-" + GitCommit,
			}
			if err := indexTemplate.Execute(w, data); err != nil {
				log.Printf("Template error: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		// All other paths: serve static files
		staticHandler.ServeHTTP(w, r)
	})

	log.Printf("swe-swe-server v%s", Version)
	log.Printf("Starting server on %s", *addr)
	log.Printf("  shell: %s", shellCmd)
	if shellRestartCmd != shellCmd {
		log.Printf("  shell-restart: %s", shellRestartCmd)
	}
	if workingDir != "" {
		log.Printf("  working-directory: %s", workingDir)
	}

	// Start signal monitor and heartbeat for crash forensics
	startSignalMonitor()
	startHeartbeat()

	// Signal-aware shutdown: cancel serverCtx on SIGINT/SIGTERM
	var serverCancel context.CancelFunc
	serverCtx, serverCancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer serverCancel()

	// Set up embedded auth if SWE_SWE_PASSWORD is set (dockerfile-only mode).
	// In compose mode, Traefik + auth service handle authentication externally.
	var handler http.Handler
	if authPassword := os.Getenv("SWE_SWE_PASSWORD"); authPassword != "" {
		handler = setupEmbeddedAuth(authPassword)
		log.Printf("Embedded auth enabled (SWE_SWE_PASSWORD set)")
	}

	srv := &http.Server{Addr: *addr, Handler: handler}
	go func() {
		defer recoverGoroutine("shutdown handler")
		<-serverCtx.Done()
		log.Println("Shutting down server...")
		// Close all sessions (kills processes)
		sessionsMu.Lock()
		for uuid, sess := range sessions {
			log.Printf("Closing session %s on shutdown", uuid)
			sess.Close()
			delete(sessions, uuid)
		}
		sessionsMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// deriveBranchName converts a session name to a valid git branch name
// - Lowercase
// - Unicode normalized (NFD), diacritics removed
// - Spaces, underscores preserved or converted to hyphens
// - Special chars replaced with hyphens
// - Multiple hyphens collapsed
// - Leading/trailing hyphens removed
func deriveBranchName(sessionName string) string {
	if sessionName == "" {
		return ""
	}

	// Normalize unicode and remove diacritics (e.g., e-acute -> e)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, sessionName)

	// Lowercase
	result = strings.ToLower(result)

	// Replace spaces with hyphens
	result = strings.ReplaceAll(result, " ", "-")

	// Remove any character that's not alphanumeric, hyphen, underscore, dot, or slash
	// These are the safe characters git allows in branch names
	// (dots have restrictions: no leading dot per component, no "..", no ".lock" suffix)
	re := regexp.MustCompile(`[^a-z0-9_./-]+`)
	result = re.ReplaceAllString(result, "-")

	// Collapse multiple hyphens
	re = regexp.MustCompile(`-+`)
	result = re.ReplaceAllString(result, "-")

	// Clean up slashes: collapse multiple slashes
	re = regexp.MustCompile(`/+`)
	result = re.ReplaceAllString(result, "/")

	// Clean up consecutive dots (git doesn't allow "..")
	re = regexp.MustCompile(`\.+`)
	result = re.ReplaceAllString(result, ".")

	// Clean up patterns like "/-", "-/", "/.", "./"
	for _, pattern := range []string{"/-", "-/", "/.", "./"} {
		for strings.Contains(result, pattern) {
			result = strings.ReplaceAll(result, pattern, "/")
		}
	}

	// Collapse multiple slashes again (pattern cleanup above can create them)
	re = regexp.MustCompile(`/+`)
	result = re.ReplaceAllString(result, "/")

	// Remove leading dots from each path component (git restriction)
	// e.g., ".hidden/foo" -> "hidden/foo", "foo/.bar" -> "foo/bar"
	re = regexp.MustCompile(`(^|/)\.+`)
	result = re.ReplaceAllString(result, "$1")

	// Remove .lock suffix if present (git restriction)
	result = strings.TrimSuffix(result, ".lock")

	// Trim leading/trailing hyphens, slashes, and dots
	result = strings.Trim(result, "-/.")

	return result
}

// worktreeDirName converts a branch name to a safe directory name.
// Replaces "/" with "--" to keep a flat directory structure.
func worktreeDirName(branchName string) string {
	return strings.ReplaceAll(branchName, "/", "--")
}

// branchNameFromDir converts a directory name back to a branch name.
// Replaces "--" with "/" to restore hierarchical branch names.
func branchNameFromDir(dirName string) string {
	return strings.ReplaceAll(dirName, "--", "/")
}

// worktreeDir is the base directory for git worktrees.
// This is intentionally at /worktrees (not under /workspace) to keep worktrees
// separate from the main workspace. The container Dockerfile/entrypoint must
// ensure this directory exists with proper permissions for the app user.
var worktreeDir = "/worktrees"

// excludeFromSymlink lists entries that should never be symlinked to worktrees
var excludeFromSymlink = []string{".git"}

// dumpContainerTemplates writes all embedded container templates to destDir,
// always overwriting existing files. Used by the update-swe-swe slash command
// to extract latest templates for three-way merge.
func dumpContainerTemplates(destDir string) error {
	return fs.WalkDir(containerTemplatesFS, "container-templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(path, "container-templates/")
		if relPath == "" || relPath == "." {
			return nil // skip root
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		content, err := containerTemplatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded %s: %w", path, err)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", destPath, err)
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}

		fmt.Printf("Dumped %s\n", destPath)
		return nil
	})
}

// setupSweSweFiles writes embedded container template files into destDir.
// Used to provision swe-swe files (.swe-swe/docs/*, swe-swe/setup)
// into cloned repos and new projects that weren't set up by swe-swe init.
// Idempotent: skips files that already exist.
// Also cleans up legacy .mcp.json files (MCP config moved to ~/.claude.json).
func setupSweSweFiles(destDir string) error {
	// Clean up legacy .mcp.json that only contains swe-swe servers
	mcpPath := filepath.Join(destDir, ".mcp.json")
	if existingContent, readErr := os.ReadFile(mcpPath); readErr == nil {
		var doc map[string]any
		if json.Unmarshal(existingContent, &doc) == nil {
			if servers, ok := doc["mcpServers"].(map[string]any); ok {
				// Remove swe-swe-* servers
				for name := range servers {
					if strings.HasPrefix(name, "swe-swe-") {
						delete(servers, name)
					}
				}
				if len(servers) == 0 {
					// Only had swe-swe servers -- delete the file
					os.Remove(mcpPath)
					// Also remove baseline if it exists
					baselinePath := filepath.Join(destDir, ".swe-swe", "baseline", ".mcp.json")
					os.Remove(baselinePath)
					log.Printf("Removed legacy .mcp.json (swe-swe servers moved to ~/.claude.json): %s", mcpPath)
				} else {
					// Has user-defined servers -- rewrite without swe-swe-* entries
					doc["mcpServers"] = servers
					cleaned, _ := json.MarshalIndent(doc, "", "  ")
					os.WriteFile(mcpPath, append(cleaned, '\n'), 0644)
					log.Printf("Removed swe-swe servers from .mcp.json (kept user servers): %s", mcpPath)
				}
			}
		}
	}

	return fs.WalkDir(containerTemplatesFS, "container-templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(path, "container-templates/")
		if relPath == "" || relPath == "." {
			return nil // skip root
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		content, err := containerTemplatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded %s: %w", path, err)
		}

		// Skip files that already exist
		if _, err := os.Stat(destPath); err == nil {
			return nil
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", destPath, err)
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}

		log.Printf("Wrote swe-swe file: %s", destPath)

		// Also write baseline snapshot for three-way merge during updates
		baselinePath := filepath.Join(destDir, ".swe-swe", "baseline", relPath)
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0755); err != nil {
			return fmt.Errorf("failed to create baseline directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(baselinePath, content, 0644); err != nil {
			return fmt.Errorf("failed to write baseline %s: %w", baselinePath, err)
		}

		return nil
	})
}

// generateMOTD creates the terminal MOTD displaying available swe-swe commands
func generateMOTD(workDir, branchName string, cfg AssistantConfig) string {
	// Shell sessions don't need MOTD - they're not AI agents
	if cfg.SlashCmdFormat == SlashCmdNone {
		return ""
	}

	// Determine the workspace directory
	wsDir := "/workspace"
	if workDir != "" && strings.HasPrefix(workDir, worktreeDir) {
		wsDir = workDir
	}

	// Determine command invocation syntax based on agent's slash command support
	var tipText, setupCmd string
	var isSlashCmd bool
	switch cfg.SlashCmdFormat {
	case SlashCmdMD, SlashCmdTOML:
		// Slash-command agents use /swe-swe:cmd
		tipText = "Tip: type /swe-swe to see commands available"
		setupCmd = "/swe-swe:setup"
		isSlashCmd = true
	case SlashCmdFile:
		// File-mention agents use @swe-swe/cmd
		tipText = "Tip: @swe-swe to see available commands"
		setupCmd = "@swe-swe/setup"
		isSlashCmd = false
	default:
		return "" // Safety: unknown format gets no MOTD
	}

	// For slash-command agents, we don't need the /workspace/swe-swe directory
	// since commands are installed in agent-specific directories
	sweSweDir := wsDir + "/swe-swe"

	// Check if swe-swe directory exists (required for file-mention agents)
	entries, err := os.ReadDir(sweSweDir)
	if err != nil && !isSlashCmd {
		return "" // No swe-swe directory and no slash command support - no MOTD
	}

	// For slash-command agents, always show MOTD (commands are in home directory)
	// For file-mention agents, check if command files exist
	hasCommands := isSlashCmd
	hasSetup := isSlashCmd // Slash-command agents always have bundled setup
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			hasCommands = true
			if entry.Name() == "setup" {
				hasSetup = true
			}
		}
	}

	if !hasCommands {
		return ""
	}

	// Check if setup has been completed (swe-swe snippet in CLAUDE.md or AGENTS.md)
	setupDone := false
	for _, filename := range []string{"CLAUDE.md", "AGENTS.md"} {
		content, err := os.ReadFile(wsDir + "/" + filename)
		if err == nil && strings.Contains(string(content), ".swe-swe/docs/AGENTS.md") {
			setupDone = true
			break
		}
	}

	// Format the MOTD (use \r\n for proper terminal line endings)
	var sb strings.Builder
	sb.WriteString("\r\n")
	sb.WriteString(ansiDim(tipText) + "\r\n")

	// Show "Try this" only if setup exists and hasn't been done
	if hasSetup && !setupDone {
		sb.WriteString(ansiDim("Try this:") + " " + ansiCyan(setupCmd) + "\r\n")
	}

	sb.WriteString("\r\n")
	return sb.String()
}

// isTrackedInGit checks if a file is tracked in git
// Returns true if the file is tracked, false otherwise
func isTrackedInGit(repoDir, relativePath string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", relativePath)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

// ensureSweSweFiles symlinks swe-swe files from the base repo into a worktree.
// Processes: dotfiles (except .git), CLAUDE.md, AGENTS.md, and swe-swe/ directory.
// Skips entries tracked in git (worktree already has them) and entries that already exist at destination.
// All entries (files and directories) are symlinked using absolute paths.
func ensureSweSweFiles(srcDir, destDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	var symlinked []string
	for _, entry := range entries {
		name := entry.Name()

		// Check if this entry matches our patterns
		shouldProcess := false
		if strings.HasPrefix(name, ".") {
			shouldProcess = true
		} else if name == "CLAUDE.md" || name == "AGENTS.md" || name == "swe-swe" {
			shouldProcess = true
		}
		if !shouldProcess {
			continue
		}

		// Check exclusion list
		excluded := false
		for _, exc := range excludeFromSymlink {
			if name == exc {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// Skip entries tracked in git (worktree already has them)
		if isTrackedInGit(srcDir, name) {
			continue
		}

		dstPath := destDir + "/" + name

		// Skip if destination already exists
		if _, err := os.Lstat(dstPath); err == nil {
			continue
		}

		// Create absolute symlink
		srcPath := srcDir + "/" + name
		if err := os.Symlink(srcPath, dstPath); err != nil {
			log.Printf("Warning: failed to symlink %s to worktree: %v", name, err)
			continue
		}
		symlinked = append(symlinked, name)
	}

	if len(symlinked) > 0 {
		log.Printf("Symlinked swe-swe files to worktree: %v", symlinked)
	}
	return nil
}

// getGitRoot returns the root directory of the git repository
func getGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getMainRepoBranch returns the current branch of the main repo (/workspace)
func getMainRepoBranch() string {
	cmd := exec.Command("git", "-C", "/workspace", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// worktreeExists checks if a worktree directory already exists for the given branch name
func worktreeExists(branchName string) bool {
	worktreePath := worktreeDir + "/" + worktreeDirName(branchName)
	_, err := os.Stat(worktreePath)
	return err == nil
}

// localBranchExists checks if a local git branch exists with the given name
func localBranchExists(branchName string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	return cmd.Run() == nil
}

// remoteBranchExists checks if a remote git branch exists with the given name
func remoteBranchExists(branchName string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "origin/"+branchName)
	return cmd.Run() == nil
}

// WorktreeSessionInfo contains information about an active session running in a worktree
type WorktreeSessionInfo struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Assistant   string `json:"assistant"`
	ClientCount int    `json:"clientCount"`
	DurationStr string `json:"durationStr"`
}

// WorktreeInfo contains information about an existing worktree
type WorktreeInfo struct {
	Name          string               `json:"name"`
	Path          string               `json:"path"`
	ActiveSession *WorktreeSessionInfo `json:"activeSession,omitempty"`
}

// listWorktrees returns a list of existing worktree directories
func listWorktrees() ([]WorktreeInfo, error) {
	// Check if worktree directory exists
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return []WorktreeInfo{}, nil // No worktrees yet
	}

	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read worktree directory: %w", err)
	}

	var worktrees []WorktreeInfo
	for _, entry := range entries {
		if entry.IsDir() {
			// Convert directory name back to branch name (e.g., "style--foo" -> "style/foo")
			worktrees = append(worktrees, WorktreeInfo{
				Name: branchNameFromDir(entry.Name()),
				Path: worktreeDir + "/" + entry.Name(),
			})
		}
	}

	// Return empty slice instead of nil for consistent JSON encoding
	if worktrees == nil {
		worktrees = []WorktreeInfo{}
	}

	return worktrees, nil
}

// handleWorktreesAPI handles GET /api/worktrees
func handleWorktreesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	worktrees, err := listWorktrees()
	if err != nil {
		log.Printf("Error listing worktrees: %v", err)
		http.Error(w, "Failed to list worktrees", http.StatusInternalServerError)
		return
	}

	// Build a map of branchName -> *Session for active sessions
	sessionsMu.RLock()
	branchToSession := make(map[string]*Session)
	for _, sess := range sessions {
		if sess.BranchName != "" {
			branchToSession[sess.BranchName] = sess
		}
	}
	sessionsMu.RUnlock()

	// Populate ActiveSession for worktrees with matching sessions
	for i := range worktrees {
		if sess, ok := branchToSession[worktrees[i].Name]; ok {
			sess.mu.RLock()
			worktrees[i].ActiveSession = &WorktreeSessionInfo{
				UUID:        sess.UUID,
				Name:        sess.Name,
				Assistant:   sess.Assistant,
				ClientCount: len(sess.wsClients),
				DurationStr: formatDuration(time.Since(sess.CreatedAt)),
			}
			sess.mu.RUnlock()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"worktrees": worktrees,
	})
}

// RepoInfo represents a previously-cloned repository found in /repos/
type RepoInfo struct {
	Path      string `json:"path"`
	RemoteURL string `json:"remoteURL"`
	DirName   string `json:"dirName"`
}

// handleReposAPI handles GET /api/repos
// Scans /repos/ for existing git repositories and returns their info.
// Returns: { "repos": [{"path": "/repos/foo/workspace", "remoteURL": "https://...", "dirName": "foo"}] }
func handleReposAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repos := []RepoInfo{}

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		// /repos/ doesn't exist or can't be read -- return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"repos": repos,
		})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		workspacePath := filepath.Join(reposDir, dirName, "workspace")

		// Check if workspace/.git exists
		if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err != nil {
			continue
		}

		info := RepoInfo{
			Path:    workspacePath,
			DirName: dirName,
		}

		// Try to get remote URL
		cmd := exec.Command("git", "-C", workspacePath, "remote", "get-url", "origin")
		if output, err := cmd.Output(); err == nil {
			info.RemoteURL = strings.TrimSpace(string(output))
		}

		repos = append(repos, info)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"repos": repos,
	})
}

// handleWorktreeCheckAPI handles GET /api/worktree/check?name={branch}
// Returns whether the branch/worktree exists and what type
func handleWorktreeCheckAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter required", http.StatusBadRequest)
		return
	}

	// Derive branch name from session name
	branchName := deriveBranchName(name)
	if branchName == "" {
		// No valid branch name derived
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exists": false,
		})
		return
	}

	// Check in priority order: worktree > local branch > remote branch
	var conflictType string
	if worktreeExists(branchName) {
		conflictType = "worktree"
	} else if localBranchExists(branchName) {
		conflictType = "local"
	} else if remoteBranchExists(branchName) {
		conflictType = "remote"
	}

	w.Header().Set("Content-Type", "application/json")
	if conflictType != "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exists": true,
			"type":   conflictType,
			"branch": branchName,
		})
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exists": false,
		})
	}
}

// reposDir is the base directory for external repository clones.
// External repos are cloned to /repos/{sanitized-url}/workspace
var reposDir = "/repos"

// sanitizeRepoURL converts a repository URL to a filesystem-safe directory name.
// Replaces invalid filesystem characters with "-".
func sanitizeRepoURL(repoURL string) string {
	// Remove protocol prefix
	sanitized := repoURL
	sanitized = strings.TrimPrefix(sanitized, "https://")
	sanitized = strings.TrimPrefix(sanitized, "http://")
	sanitized = strings.TrimPrefix(sanitized, "git@")
	sanitized = strings.TrimPrefix(sanitized, "ssh://")
	sanitized = strings.TrimSuffix(sanitized, ".git")

	// Replace invalid filesystem characters with "-"
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", "@", " "}
	for _, char := range invalidChars {
		sanitized = strings.ReplaceAll(sanitized, char, "-")
	}

	// Collapse multiple dashes
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	// Trim leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")

	return sanitized
}

// getWorkspaceOriginURL returns the origin remote URL of /workspace repo
func getWorkspaceOriginURL() (string, error) {
	cmd := exec.Command("git", "-C", "/workspace", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get origin URL: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getRepoOriginURL returns the origin remote URL for a given repo path
func getRepoOriginURL(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get origin URL: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getCurrentBranch returns the current branch name for a given repo path
func getCurrentBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// extractOwnerRepo extracts "owner/repo" from a git URL
// Handles: https://github.com/owner/repo.git, git@github.com:owner/repo.git, etc.
func extractOwnerRepo(gitURL string) string {
	gitURL = strings.TrimSpace(gitURL)
	gitURL = strings.TrimSuffix(gitURL, ".git")

	// Handle SSH format: git@github.com:owner/repo or git@gitlab.com:group/subgroup/owner/repo
	if strings.HasPrefix(gitURL, "git@") {
		if idx := strings.Index(gitURL, ":"); idx != -1 {
			pathPart := gitURL[idx+1:]
			parts := strings.Split(pathPart, "/")
			if len(parts) >= 2 {
				return parts[len(parts)-2] + "/" + parts[len(parts)-1]
			}
			return pathPart // fallback for edge cases
		}
	}

	// Handle HTTPS/HTTP format: https://github.com/owner/repo
	// Extract last two path segments
	gitURL = strings.TrimPrefix(gitURL, "https://")
	gitURL = strings.TrimPrefix(gitURL, "http://")
	gitURL = strings.TrimPrefix(gitURL, "ssh://")

	// Split by "/" and take last 2 segments
	parts := strings.Split(gitURL, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return ""
}

// deriveDefaultSessionName derives a default session name from repo and branch info
// Format: {owner}/{repo}@{branch} or {dirName}@{branch} for repos without origin
// Falls back to just {owner}/{repo} or {dirName} if branch cannot be determined (e.g. empty repo)
func deriveDefaultSessionName(repoPath string) string {
	branchName, err := getCurrentBranch(repoPath)
	if err != nil {
		log.Printf("Warning: could not get current branch for %s: %v", repoPath, err)
	}

	suffix := ""
	if branchName != "" {
		suffix = "@" + branchName
	}

	originURL, err := getRepoOriginURL(repoPath)
	if err == nil {
		ownerRepo := extractOwnerRepo(originURL)
		if ownerRepo != "" {
			return ownerRepo + suffix
		}
	}

	// Fallback: use directory name for /repos/{name}/workspace paths
	if strings.HasPrefix(repoPath, reposDir+"/") {
		rel := strings.TrimPrefix(repoPath, reposDir+"/")
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) > 0 && parts[0] != "" {
			return parts[0] + suffix
		}
	}

	return ""
}

// isWorkspaceRepo checks if the given URL matches the /workspace repo's origin
func isWorkspaceRepo(repoURL string) bool {
	// Normalize input
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return false
	}

	// Check if it's a local path that is /workspace
	if repoURL == "/workspace" || repoURL == "/workspace/" {
		return true
	}

	// Get workspace origin
	originURL, err := getWorkspaceOriginURL()
	if err != nil {
		return false
	}

	// Normalize both URLs for comparison
	normalizeGitURL := func(u string) string {
		u = strings.TrimSpace(u)
		u = strings.TrimSuffix(u, ".git")
		u = strings.TrimPrefix(u, "https://")
		u = strings.TrimPrefix(u, "http://")
		u = strings.TrimPrefix(u, "git@")
		u = strings.TrimPrefix(u, "ssh://")
		u = strings.ReplaceAll(u, ":", "/")
		return strings.ToLower(u)
	}

	return normalizeGitURL(repoURL) == normalizeGitURL(originURL)
}

// handleRepoPrepareAPI handles POST /api/repo/prepare
// Input: { "mode": "workspace|clone|create", "url": "...", "name": "..." }
// Modes:
//   - workspace: use /workspace, fetch (soft fail with warning)
//   - clone: clone external URL to /repos/{sanitized-url}/workspace (hard fail)
//   - create: create new project at /repos/{name}/workspace with git init
//
// Returns: { "path": "/repos/...", "isWorkspace": bool, "warning": "..." }
func handleRepoPrepareAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Mode string `json:"mode"` // "workspace", "clone", or "create"
		URL  string `json:"url"`  // for clone mode
		Name string `json:"name"` // for create mode
		Path string `json:"path"` // for workspace mode: optional existing repo path
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	req.Mode = strings.TrimSpace(req.Mode)
	req.URL = strings.TrimSpace(req.URL)
	req.Name = strings.TrimSpace(req.Name)

	// Default mode for backwards compatibility
	if req.Mode == "" {
		if req.URL == "" || isWorkspaceRepo(req.URL) {
			req.Mode = "workspace"
		} else {
			req.Mode = "clone"
		}
	}

	req.Path = strings.TrimSpace(req.Path)

	switch req.Mode {
	case "workspace":
		handleRepoPrepareWorkspace(w, req.Path)
	case "clone":
		handleRepoPrepareClone(w, req.URL)
	case "create":
		handleRepoPrepareCreate(w, req.Name)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid mode. Use 'workspace', 'clone', or 'create'"})
	}
}

// handleRepoPrepareWorkspace handles the workspace mode - use /workspace or an existing repo path with soft fetch
func handleRepoPrepareWorkspace(w http.ResponseWriter, repoPath string) {
	workDir := "/workspace"
	isWorkspace := true

	if repoPath != "" {
		// Validate path: must start with /repos/ and not contain traversal
		cleaned := filepath.Clean(repoPath)
		if !strings.HasPrefix(cleaned, reposDir+"/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid repository path"})
			return
		}
		workDir = cleaned
		isWorkspace = false
	}

	response := map[string]interface{}{
		"path":        workDir,
		"isWorkspace": isWorkspace,
	}

	// Check if swe-swe/env exists
	if _, err := os.Stat(filepath.Join(workDir, "swe-swe", "env")); err == nil {
		response["hasEnvFile"] = true
	}

	// Check if it's a git repository
	if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
		log.Printf("%s is not a git repository, skipping git operations", workDir)
		response["nonGit"] = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check if workspace has any remotes configured
	remoteCmd := exec.Command("git", "-C", workDir, "remote")
	remoteOutput, err := remoteCmd.Output()
	hasRemote := err == nil && len(strings.TrimSpace(string(remoteOutput))) > 0

	if hasRemote {
		log.Printf("Fetching all for %s", workDir)
		cmd := exec.Command("git", "-C", workDir, "fetch", "--all")
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Soft fail - return warning instead of error
			log.Printf("Git fetch failed (continuing with cached): %v, output: %s", err, string(output))
			response["warning"] = "Unable to fetch latest changes. Using cached branches."
		}
	} else {
		log.Printf("No remote configured for %s, skipping fetch", workDir)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRepoPrepareClone handles the clone mode - clone external URL (hard fail)
func handleRepoPrepareClone(w http.ResponseWriter, url string) {
	if url == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "URL is required for clone mode"})
		return
	}

	sanitizedURL := sanitizeRepoURL(url)
	if sanitizedURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid repository URL"})
		return
	}

	repoBase := filepath.Join(reposDir, sanitizedURL)
	repoPath := filepath.Join(repoBase, "workspace")

	// Check if already cloned
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		// Already cloned, fetch instead (but still hard fail for clone mode)
		log.Printf("Repository already exists at %s, fetching", repoPath)
		cmd := exec.Command("git", "-C", repoPath, "fetch", "--all")
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Git fetch failed: %v, output: %s", err, string(output))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Git fetch failed: %s", string(output))})
			return
		}
	} else {
		// Clone the repo
		log.Printf("Cloning %s to %s", url, repoPath)
		if err := os.MkdirAll(repoBase, 0755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to create directory: %v", err)})
			return
		}

		cmd := exec.Command("git", "clone", url, repoPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Git clone failed: %v, output: %s", err, string(output))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Git clone failed: %s", string(output))})
			return
		}
	}

	// Set up swe-swe files (.swe-swe/docs/*, swe-swe/setup) and clean up legacy .mcp.json
	if err := setupSweSweFiles(repoPath); err != nil {
		log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
	}

	resp := map[string]interface{}{
		"path":        repoPath,
		"isWorkspace": false,
	}
	if _, err := os.Stat(filepath.Join(repoPath, "swe-swe", "env")); err == nil {
		resp["hasEnvFile"] = true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// sanitizeProjectDirName converts a display name to a safe directory name.
// Replaces spaces and special chars with dashes, collapses runs, trims edges.
func sanitizeProjectDirName(name string) string {
	// Replace any character that isn't alphanumeric, dash, or underscore with a dash
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	sanitized := re.ReplaceAllString(name, "-")
	// Collapse multiple dashes
	sanitized = regexp.MustCompile(`-{2,}`).ReplaceAllString(sanitized, "-")
	// Trim leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")
	return sanitized
}

// handleRepoPrepareCreate handles the create mode - create new project with git init
func handleRepoPrepareCreate(w http.ResponseWriter, name string) {
	if name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Project name is required"})
		return
	}

	// Sanitize name for directory use (display name is passed separately via session URL)
	dirName := sanitizeProjectDirName(name)
	if dirName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Project name must contain at least one letter or number."})
		return
	}

	repoPath := filepath.Join(reposDir, dirName, "workspace")

	// Check if already exists
	if _, err := os.Stat(repoPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Project '%s' already exists", dirName)})
		return
	}

	// Create directory
	log.Printf("Creating new project at %s", repoPath)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to create directory: %v", err)})
		return
	}

	// Initialize git repo
	cmd := exec.Command("git", "-C", repoPath, "init")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Git init failed: %v, output: %s", err, string(output))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Git init failed: %s", string(output))})
		return
	}

	// Create initial empty commit so git operations (rev-parse, worktree, etc.) work
	commitCmd := exec.Command("git", "-C", repoPath,
		"-c", "user.name=swe-swe", "-c", "user.email=swe-swe@localhost",
		"commit", "--allow-empty", "-m", "initial")
	commitOutput, err := commitCmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: initial commit failed: %v, output: %s", err, string(commitOutput))
		// Non-fatal: the repo still works, just without an initial commit
	}

	// Set up swe-swe files (.swe-swe/docs/*, swe-swe/setup) and clean up legacy .mcp.json
	if err := setupSweSweFiles(repoPath); err != nil {
		log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
	}

	resp := map[string]interface{}{
		"path":        repoPath,
		"isWorkspace": false,
		"isNew":       true,
	}
	if _, err := os.Stat(filepath.Join(repoPath, "swe-swe", "env")); err == nil {
		resp["hasEnvFile"] = true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleRepoBranchesAPI handles GET /api/repo/branches?path=/workspace
// Returns: { "branches": ["main", "origin/feature-x", ...] }
func handleRepoBranchesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repoPath := r.URL.Query().Get("path")
	if repoPath == "" {
		repoPath = "/workspace"
	}

	// Security check: only allow /workspace or /repos/* paths
	if repoPath != "/workspace" && !strings.HasPrefix(repoPath, reposDir+"/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid repository path"})
		return
	}

	// Clean path to prevent traversal
	repoPath = filepath.Clean(repoPath)

	// Get all branches (local and remote)
	cmd := exec.Command("git", "-C", repoPath, "branch", "-a", "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Git branch list failed for %s: %v", repoPath, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to list branches"})
		return
	}

	// Parse branch names
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	branches := make([]string, 0, len(lines))
	seen := make(map[string]bool)

	for _, line := range lines {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		// Skip HEAD pointer
		if strings.Contains(branch, "HEAD") {
			continue
		}
		// Avoid duplicates
		if seen[branch] {
			continue
		}
		seen[branch] = true
		branches = append(branches, branch)
	}

	// Sort: local branches first, then remote
	sort.Slice(branches, func(i, j int) bool {
		iRemote := strings.HasPrefix(branches[i], "origin/")
		jRemote := strings.HasPrefix(branches[j], "origin/")
		if iRemote != jRemote {
			return !iRemote // local before remote
		}
		return branches[i] < branches[j]
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"branches": branches,
	})
}

// resolveWorkingDirectory calculates the working directory for a session
// based on repoPath and optional branchName.
// - Branch blank: return repoPath (no worktree)
// - /workspace + branch: /worktrees/{branch}
// - External + branch: /repos/{sanitized-url}/worktree/{branch}
func resolveWorkingDirectory(repoPath, branchName string) string {
	if branchName == "" {
		return repoPath
	}

	// For /workspace, use /worktrees directory
	if repoPath == "/workspace" {
		return filepath.Join(worktreeDir, worktreeDirName(branchName))
	}

	// For external repos, use worktrees subdirectory within the repo
	// e.g., /repos/github.com-user-repo/worktrees/feature-branch
	return filepath.Join(filepath.Dir(repoPath), "worktrees", worktreeDirName(branchName))
}

// createWorktreeInRepo creates a worktree for a specific repo
// Supports both /workspace and external repos at different paths
func createWorktreeInRepo(repoPath, branchName string) (string, error) {
	if branchName == "" {
		return repoPath, nil
	}

	worktreePath := resolveWorkingDirectory(repoPath, branchName)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		log.Printf("Re-entering existing worktree at %s", worktreePath)
		return worktreePath, nil
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(worktreePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create worktree directory: %w", err)
	}

	var cmd *exec.Cmd
	var output []byte
	var err error

	// Check if local branch exists in this repo
	localCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", branchName)
	localExists := localCmd.Run() == nil

	// Check if remote branch exists
	remoteCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "origin/"+branchName)
	remoteExists := remoteCmd.Run() == nil

	if localExists {
		log.Printf("Attaching worktree to existing local branch %s in %s", branchName, repoPath)
		cmd = exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, branchName)
	} else if remoteExists {
		log.Printf("Creating worktree tracking remote branch origin/%s in %s", branchName, repoPath)
		cmd = exec.Command("git", "-C", repoPath, "worktree", "add", "--track", "-b", branchName, worktreePath, "origin/"+branchName)
	} else {
		log.Printf("Creating new worktree with fresh branch %s in %s", branchName, repoPath)
		cmd = exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath)
	}

	output, err = cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create worktree: %w (output: %s)", err, string(output))
	}

	log.Printf("Created worktree at %s (branch: %s)", worktreePath, branchName)

	// Symlink swe-swe files from base repo into worktree (graceful degradation on failure)
	if err := ensureSweSweFiles(repoPath, worktreePath); err != nil {
		log.Printf("Warning: failed to symlink swe-swe files to worktree: %v", err)
	}

	return worktreePath, nil
}

// isValidWorktreePath checks if a path is a valid worktree path (under worktreeDir).
// This is a security check to prevent path traversal attacks.
func isValidWorktreePath(path string) bool {
	if path == "" {
		return false
	}

	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(path)

	// Must start with worktreeDir
	if !strings.HasPrefix(cleanPath, worktreeDir+"/") {
		return false
	}

	// Must not contain path traversal after cleaning
	if cleanPath != path && strings.Contains(path, "..") {
		return false
	}

	return true
}

// sessionReaper periodically cleans up sessions where the process has exited
// Sessions persist until the process exits (no TTL-based expiry)
func sessionReaper() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sessionsMu.Lock()
		for uuid, sess := range sessions {
			// Only clean up sessions where the process has exited
			if sess.Cmd != nil && sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
				log.Printf("Session cleaned up (process exited): %s", uuid)
				sess.Close()
				delete(sessions, uuid)
			}
		}
		sessionsMu.Unlock()

		// Clean up old recent recordings
		cleanupRecentRecordings()
	}
}

// Constants for recording cleanup
const (
	recentRecordingMaxAge = 14 * 24 * time.Hour
)

// cleanupRecentRecordings deletes old recent recordings (those without KeptAt set).
// Expiry is based on EndedAt from metadata -- recordings are only eligible for
// deletion recentRecordingMaxAge after the session ends. Active sessions (EndedAt
// unset) are never deleted.
func cleanupRecentRecordings() {
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		return
	}

	// Build map of active recording UUIDs
	activeRecordings := make(map[string]bool)
	sessionsMu.RLock()
	for _, sess := range sessions {
		if sess.RecordingUUID != "" && sess.Cmd != nil && sess.Cmd.ProcessState == nil {
			activeRecordings[sess.RecordingUUID] = true
		}
	}
	sessionsMu.RUnlock()

	now := time.Now()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".metadata.json") {
			continue
		}

		// Extract UUID stem
		stem := strings.TrimPrefix(name, "session-")
		stem = strings.TrimSuffix(stem, ".metadata.json")

		// Only process root recordings (skip children)
		parentUUID, childUUID, ok := parseRecordingFilename(stem)
		if !ok || childUUID != "" {
			continue
		}
		uuid := parentUUID

		// Skip active recordings
		if activeRecordings[uuid] {
			continue
		}

		// Read metadata
		metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"
		metaData, err := os.ReadFile(metadataPath)
		if err != nil {
			continue
		}

		var meta RecordingMetadata
		if err := json.Unmarshal(metaData, &meta); err != nil {
			continue
		}

		// Skip kept recordings
		if meta.KeptAt != nil {
			continue
		}

		// Skip recordings that haven't ended yet (active session without
		// a live process -- e.g. crash). Use log file mtime as fallback
		// so crashed sessions still get cleaned up eventually.
		var endTime time.Time
		if meta.EndedAt != nil {
			endTime = *meta.EndedAt
		} else {
			logPath := resolveLogPath("session-" + uuid)
			if logPath == "" {
				continue
			}
			logInfo, err := os.Stat(logPath)
			if err != nil {
				continue
			}
			endTime = logInfo.ModTime()
		}

		if now.Sub(endTime) > recentRecordingMaxAge {
			deleteRecordingFiles(uuid)
			log.Printf("Auto-deleted recent recording %s (agent=%s, age=%v)",
				uuid[:8], meta.Agent, now.Sub(endTime).Round(time.Minute))
		}
	}

	// Second pass: clean up orphaned files (no corresponding .log file).
	// This handles files left behind by past bugs (e.g. .input files not
	// deleted before Feb 23 fix) and edge cases like metadata-only ghosts.
	orphanCount := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || entry.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz") {
			continue // .log/.log.gz files are authoritative -- never orphan-delete them
		}
		if strings.HasSuffix(name, ".log.pipe") {
			continue // skip FIFO pipes used during active recording
		}

		// Extract stem by removing "session-" prefix and any known suffix
		stem := strings.TrimPrefix(name, "session-")
		for _, suffix := range []string{".timing", ".input", ".metadata.json", ".events.jsonl"} {
			stem = strings.TrimSuffix(stem, suffix)
		}

		parentUUID, _, ok := parseRecordingFilename(stem)
		if !ok {
			continue
		}
		if activeRecordings[parentUUID] {
			continue
		}
		if resolveLogPath("session-"+parentUUID) != "" {
			continue
		}

		os.Remove(recordingsDir + "/" + name)
		orphanCount++
	}
	if orphanCount > 0 {
		log.Printf("Cleaned up %d orphaned recording files (no corresponding .log)", orphanCount)
	}
}

// deleteRecordingFiles removes all files for a recording and its children.
func deleteRecordingFiles(recUUID string) {
	// Delete parent files
	suffixes := []string{".log", ".log.gz", ".log.pipe", ".timing", ".input", ".metadata.json"}
	for _, suffix := range suffixes {
		os.Remove(recordingsDir + "/session-" + recUUID + suffix)
	}
	// Delete child files (session-{uuid}-*)
	childMatches, _ := filepath.Glob(recordingsDir + "/session-" + recUUID + "-*")
	for _, path := range childMatches {
		os.Remove(path)
	}
}

func agentChatPortFromPreview(previewPort int) int {
	return previewPort + 1000
}

func publicPortFromPreview(previewPort int) int {
	return previewPort + 2000
}

func cdpPortFromPreview(previewPort int) int {
	return previewPort + 3000
}

func vncPortFromPreview(previewPort int) int {
	return previewPort + 4000
}

// displayNumberFromPreview derives a unique X11 display number from a preview port.
// Preview port 3000 -> DISPLAY=:1, 3001 -> :2, etc.
func displayNumberFromPreview(previewPort int) int {
	return (previewPort - previewPortStart) + 1
}

// startSessionBrowser starts per-session Xvfb, Chromium, x11vnc, and noVNC processes.
// The session gets its own isolated X11 display and browser instance.
func startSessionBrowser(sess *Session) error {
	display := displayNumberFromPreview(sess.PreviewPort)
	displayStr := fmt.Sprintf(":%d", display)

	// 1. Start Xvfb on a unique display with Unix socket only (no TCP)
	xvfbCmd := exec.Command("Xvfb", displayStr, "-screen", "0", "1024x768x24", "-nolisten", "tcp")
	if err := xvfbCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Xvfb on display %s: %w", displayStr, err)
	}
	sess.BrowserPIDs = append(sess.BrowserPIDs, xvfbCmd.Process.Pid)
	log.Printf("Started Xvfb on display %s (PID %d) for session %s", displayStr, xvfbCmd.Process.Pid, sess.UUID)
	go func() { xvfbCmd.Wait() }()

	// Wait briefly for Xvfb to initialize
	time.Sleep(500 * time.Millisecond)

	// 2. Start Chromium with remote debugging on the session's CDP port.
	// Each session gets its own --user-data-dir to avoid Chrome's singleton
	// profile lock, which would cause all but the first instance to delegate
	// to the first and immediately exit.
	chromiumBinary := "chromium"
	if _, err := exec.LookPath("chromium"); err != nil {
		chromiumBinary = "chromium-browser" // fallback name on some distros
	}
	userDataDir := fmt.Sprintf("/tmp/chromium-session-%s", sess.UUID)
	sess.BrowserDataDir = userDataDir
	chromeCmd := exec.Command(chromiumBinary,
		"--no-sandbox",
		"--test-type",
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--disable-dev-shm-usage",
		"--remote-debugging-address=0.0.0.0",
		fmt.Sprintf("--remote-debugging-port=%d", sess.CDPPort),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--remote-allow-origins=*",
		"--window-size=1024,768",
		"--start-maximized",
	)
	chromeCmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", displayStr))
	if err := chromeCmd.Start(); err != nil {
		stopSessionBrowser(sess)
		return fmt.Errorf("failed to start Chromium on CDP port %d: %w", sess.CDPPort, err)
	}
	sess.BrowserPIDs = append(sess.BrowserPIDs, chromeCmd.Process.Pid)
	chromePID := chromeCmd.Process.Pid
	log.Printf("Started Chromium on CDP port %d, display %s (PID %d) for session %s", sess.CDPPort, displayStr, chromePID, sess.UUID)
	go func() {
		err := chromeCmd.Wait()
		if err != nil {
			log.Printf("Chromium exited with error (PID %d, session %s): %v", chromePID, sess.UUID, err)
		} else {
			log.Printf("Chromium exited normally (PID %d, session %s)", chromePID, sess.UUID)
		}
	}()

	// Wait for Chrome to start
	time.Sleep(1 * time.Second)

	// 3. Start x11vnc on an internal port (raw VNC protocol, consumed by noVNC)
	// Offset by the range size so internal ports never collide with session VNC ports
	x11vncInternalPort := sess.VNCPort + (vncPortEnd - vncPortStart + 1)
	x11vncCmd := exec.Command("x11vnc",
		"-display", displayStr,
		"-forever",
		"-shared",
		"-nopw",
		"-rfbport", fmt.Sprintf("%d", x11vncInternalPort),
		"-xkb",
	)
	if err := x11vncCmd.Start(); err != nil {
		stopSessionBrowser(sess)
		return fmt.Errorf("failed to start x11vnc on port %d: %w", x11vncInternalPort, err)
	}
	sess.BrowserPIDs = append(sess.BrowserPIDs, x11vncCmd.Process.Pid)
	log.Printf("Started x11vnc on port %d, display %s (PID %d) for session %s", x11vncInternalPort, displayStr, x11vncCmd.Process.Pid, sess.UUID)
	go func() { x11vncCmd.Wait() }()

	// 4. Start noVNC (websockify) proxy on the session's VNC port
	// Bridges WebSocket (VNCPort) to raw VNC (x11vncInternalPort)
	noVNCCmd := exec.Command("websockify",
		"--web", "/usr/share/novnc",
		fmt.Sprintf("%d", sess.VNCPort),
		fmt.Sprintf("localhost:%d", x11vncInternalPort),
	)
	if err := noVNCCmd.Start(); err != nil {
		stopSessionBrowser(sess)
		return fmt.Errorf("failed to start noVNC proxy on port %d: %w", sess.VNCPort, err)
	}
	sess.BrowserPIDs = append(sess.BrowserPIDs, noVNCCmd.Process.Pid)
	log.Printf("Started noVNC proxy on port %d -> localhost:%d (PID %d) for session %s", sess.VNCPort, x11vncInternalPort, noVNCCmd.Process.Pid, sess.UUID)
	go func() { noVNCCmd.Wait() }()

	sess.BrowserStarted = true
	return nil
}

// stopSessionBrowser kills all browser processes for a session and cleans up
// the per-session Chromium user data directory.
func stopSessionBrowser(sess *Session) {
	for _, pid := range sess.BrowserPIDs {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			// Process may have already exited
			if !errors.Is(err, syscall.ESRCH) {
				log.Printf("Failed to kill browser process PID %d for session %s: %v", pid, sess.UUID, err)
			}
		} else {
			log.Printf("[KILL] Killed browser process PID %d for session %s (server PID %d)", pid, sess.UUID, os.Getpid())
		}
	}
	sess.BrowserPIDs = nil
	// Clean up per-session Chromium user data directory
	if sess.BrowserDataDir != "" {
		if err := os.RemoveAll(sess.BrowserDataDir); err != nil {
			log.Printf("Failed to clean up browser data dir %s for session %s: %v", sess.BrowserDataDir, sess.UUID, err)
		}
		sess.BrowserDataDir = ""
	}
}

// findAvailablePortQuintuple finds a preview port and its derived agent chat,
// public, CDP, and VNC ports that are not already allocated to an existing session.
// Must be called while holding sessionsMu.
// Returns (previewPort, agentChatPort, publicPort, cdpPort, vncPort, error).
func findAvailablePortQuintuple() (int, int, int, int, int, error) {
	// Collect ports already assigned to live sessions.
	usedPorts := make(map[int]bool)
	for _, sess := range sessions {
		if sess.PreviewPort != 0 {
			usedPorts[sess.PreviewPort] = true
		}
	}

	for port := previewPortStart; port <= previewPortEnd; port++ {
		if usedPorts[port] {
			continue
		}
		acPort := agentChatPortFromPreview(port)
		pubPort := publicPortFromPreview(port)
		cdpPort := cdpPortFromPreview(port)
		vncPort := vncPortFromPreview(port)
		return port, acPort, pubPort, cdpPort, vncPort, nil
	}
	return 0, 0, 0, 0, 0, fmt.Errorf("no available port quintuple in preview range %d-%d", previewPortStart, previewPortEnd)
}

// SessionParams holds the parameters for creating or retrieving a session.
// Using a struct avoids positional string parameter confusion.
type SessionParams struct {
	UUID                string // session UUID (required)
	Assistant           string // key from availableAssistants (e.g., "claude", "gemini", "custom")
	Name                string // display name (optional, can be empty)
	Branch              string // used for worktree creation (optional, separate from display name)
	WorkDir             string // working directory for the session (empty = use server cwd)
	RepoPath            string // base repo for worktree creation (empty = /workspace)
	ParentUUID          string // parent session UUID (for child sessions)
	ParentName          string // parent session name
	ParentRecordingUUID string // parent recording UUID
	Theme               string // terminal theme
	SessionMode         string // "terminal" or "chat"
}

// getOrCreateSession returns an existing session or creates a new one
func getOrCreateSession(p SessionParams) (*Session, bool, error) {
	// Memory guard: check before acquiring lock (avoid deadlock with getMaxSessionRSS)
	if p.ParentUUID == "" { // only guard top-level sessions, not child sessions
		if err := checkMemoryForNewSession(); err != nil {
			return nil, false, err
		}
	}

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if sess, ok := sessions[p.UUID]; ok {
		// Check if the session's process has exited - clean up and create fresh session
		if sess.Cmd != nil && sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
			log.Printf("Cleaning up dead session on reconnect: %s (exit code=%d)", p.UUID, sess.Cmd.ProcessState.ExitCode())
			sess.Close()
			delete(sessions, p.UUID)
			// Fall through to create a new session
		} else {
			return sess, false, nil // existing session
		}
	}

	// Find the assistant config
	var cfg AssistantConfig
	var found bool
	for _, a := range availableAssistants {
		if a.Binary == p.Assistant {
			cfg = a
			found = true
			break
		}
	}
	if !found {
		return nil, false, fmt.Errorf("unknown assistant: %s", p.Assistant)
	}

	// Ensure recordings directory exists
	if err := ensureRecordingsDir(); err != nil {
		log.Printf("Warning: failed to create recordings directory: %v", err)
	}

	// Determine working directory and create worktree if needed
	workDir := p.WorkDir
	if workDir == "" {
		// If repoPath provided, use it as base; otherwise default to /workspace
		baseRepo := p.RepoPath
		if baseRepo == "" {
			baseRepo = "/workspace"
		}

		// If branch is provided, create/use worktree
		if p.Branch != "" {
			// Use createWorktreeInRepo which supports both /workspace and external repos
			var err error
			workDir, err = createWorktreeInRepo(baseRepo, p.Branch)
			if err != nil {
				log.Printf("Warning: failed to create worktree for branch %s in %s: %v", p.Branch, baseRepo, err)
				// Fall back to base repo without worktree
				workDir = baseRepo
			}
		} else {
			// No branch specified, use base repo directly
			workDir = baseRepo
		}
	}

	var previewPort int
	var acPort int
	var pubPort int
	var cdpPort int
	var vncPort int
	if p.ParentUUID != "" {
		if parentSess, ok := sessions[p.ParentUUID]; ok {
			previewPort = parentSess.PreviewPort
			acPort = parentSess.AgentChatPort
			pubPort = parentSess.PublicPort
			cdpPort = parentSess.CDPPort
			vncPort = parentSess.VNCPort
		}
	}
	if previewPort == 0 {
		var err error
		previewPort, acPort, pubPort, cdpPort, vncPort, err = findAvailablePortQuintuple()
		if err != nil {
			return nil, false, err
		}
	}

	// Inherit name from parent session if this is a shell session with a parent
	name := p.Name
	if name == "" && p.ParentName != "" && p.Assistant == "shell" {
		name = p.ParentName + " (Terminal)"
		log.Printf("Shell session inheriting name from parent: %s", name)
	}

	// Derive default session name if none provided.
	// Format: {owner}/{repo}@{branch}
	if workDir != "" && name == "" {
		derived := deriveDefaultSessionName(workDir)
		if derived != "" {
			log.Printf("Derived default session name: %s", derived)
			name = derived
		}
	}

	// Generate recording UUID and compute filename prefix
	recordingUUID := uuid.New().String()
	recPrefix := recordingPrefix(p.ParentRecordingUUID, recordingUUID)

	// Determine recording type
	var recType string
	if p.ParentUUID != "" {
		recType = "terminal"
	} else {
		recType = "agent"
	}

	// For shell assistant, resolve $SHELL at runtime
	shellCmdToUse := cfg.ShellCmd
	if p.SessionMode == "chat" && cfg.YoloShellCmd != "" {
		shellCmdToUse = cfg.YoloShellCmd
	}
	if p.Assistant == "shell" {
		userShell := os.Getenv("SHELL")
		if userShell == "" {
			userShell = "bash"
		}
		shellCmdToUse = userShell + " -l"
		log.Printf("Shell session: using %s", shellCmdToUse)
	}

	// Create new session with PTY using assistant's shell command
	cmdName, cmdArgs := parseCommand(shellCmdToUse)

	// Wrap with script for recording
	cmdName, cmdArgs = wrapWithScript(cmdName, cmdArgs, recPrefix)
	log.Printf("Recording session to: %s/%s.{log,timing}", recordingsDir, recPrefix)

	env := buildSessionEnv(SessionEnvParams{
		PreviewPort:   previewPort,
		AgentChatPort: acPort,
		PublicPort:    pubPort,
		CDPPort:       cdpPort,
		VNCPort:       vncPort,
		Theme:         p.Theme,
		WorkDir:       workDir,
		SessionMode:   p.SessionMode,
	})
	env = append(env, fmt.Sprintf("SESSION_UUID=%s", p.UUID))
	env = append(env, fmt.Sprintf("MCP_AUTH_KEY=%s", mcpAuthKey))

	// Set up chat event log recording for chat sessions
	var chatRecordingUUID string
	if p.SessionMode == "chat" {
		chatRecordingUUID = uuid.New().String()
		chatPrefix := recordingPrefix(recordingUUID, chatRecordingUUID)
		chatLogPath := fmt.Sprintf("%s/%s.events.jsonl", recordingsDir, chatPrefix)
		env = append(env, fmt.Sprintf("AGENT_CHAT_EVENT_LOG=%s", chatLogPath))
		log.Printf("Chat event log: %s", chatLogPath)
	}

	// Agent-chat sidecar context for chat sessions.
	// The sidecar process itself is managed externally (e.g. baked into the container image),
	// but we keep the context/cancel infrastructure for graceful shutdown coordination.
	var agentChatCmd *exec.Cmd
	var sessionCancel context.CancelFunc
	if p.SessionMode == "chat" {
		_, sessionCancel = context.WithCancel(serverCtx)
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = env
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Note: pty.Start sets Setsid=true which creates a new session AND process group,
	// so kill(-pid, sig) still works for process group cleanup. Don't set Setpgid here
	// because Setpgid + Setsid conflict (setpgid makes process a group leader, then
	// setsid fails with EPERM).
	ptmx, err := pty.Start(cmd)
	if err != nil {
		if sessionCancel != nil {
			sessionCancel()
		}
		return nil, false, err
	}

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	now := time.Now()
	sess := &Session{
		UUID:            p.UUID,
		Name:            name,
		BranchName:      p.Branch,
		WorkDir:         workDir,
		Assistant:       p.Assistant,
		AssistantConfig: cfg,
		Cmd:             cmd,
		PTY:             ptmx,
		wsClients:       make(map[*SafeConn]bool),
		wsClientSizes:   make(map[*SafeConn]TermSize),
		ptySize:         TermSize{Rows: 24, Cols: 80},
		CreatedAt:       now,
		lastActive:      now,
		vt:              vt10x.New(vt10x.WithSize(80, 24)),
		ringBuf:         make([]byte, RingBufferSize),
		RecordingUUID:   recordingUUID,
		RecordingPrefix: recPrefix,
		ParentUUID:      p.ParentUUID,
		PreviewPort:     previewPort,
		AgentChatPort:   acPort,
		PublicPort:      pubPort,
		CDPPort:         cdpPort,
		VNCPort:         vncPort,
		Theme:           p.Theme,
		yoloMode:        detectYoloMode(shellCmdToUse), // Detect initial YOLO mode from startup command
		AgentChatCmd:    agentChatCmd,
		agentChatCancel: sessionCancel,
		SessionMode:     p.SessionMode,
		Metadata: &RecordingMetadata{
			UUID:          recordingUUID,
			Name:          name,
			Agent:         cfg.Name,
			AgentBinary:   cfg.Binary,
			RecordingType: recType,
			SessionMode:   p.SessionMode,
			BranchName:    p.Branch,
			StartedAt:     now,
			Command:       append([]string{cmdName}, cmdArgs...),
			MaxCols:       80, // Default starting size
			MaxRows:       24,
			WorkDir:       workDir,
		},
	}
	sessions[p.UUID] = sess

	// Create per-session preview proxy hosted inside swe-swe-server.
	// Two instances share a DebugHub: path-based (with basePath prefix for URL
	// rewriting) and port-based (empty basePath, no rewriting needed).
	previewTarget := &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", previewPort)}
	sharedHub := agentproxy.NewDebugHub()
	previewProxy, err := agentproxy.New(agentproxy.Config{
		BasePath:    "/proxy/" + sess.UUID + "/preview",
		Target:      previewTarget,
		ToolPrefix:  "preview",
		ThemeCookie: "swe-swe-theme",
		Hub:         sharedHub,
	})
	if err != nil {
		log.Printf("Warning: failed to create preview proxy for session %s: %v", sess.UUID, err)
	} else {
		mcpSrv := mcp.NewServer(&mcp.Implementation{
			Name:    "swe-swe-preview",
			Version: agentproxy.ProxyVersion,
		}, nil)
		previewProxy.RegisterTools(mcpSrv)
		previewProxy.RegisterResources(mcpSrv)

		sessMux := http.NewServeMux()
		sessMux.Handle("/proxy/"+sess.UUID+"/preview/mcp", previewProxy.MCPHandler(mcpSrv))
		sessMux.Handle("/proxy/"+sess.UUID+"/preview/", previewProxy)
		// Agent chat proxy route (same-origin, path-based)
		acTarget, _ := url.Parse(fmt.Sprintf("http://localhost:%d", acPort))
		sessMux.Handle("/proxy/"+sess.UUID+"/agentchat/", http.StripPrefix(
			"/proxy/"+sess.UUID+"/agentchat",
			agentChatProxyHandler(acTarget),
		))
		sess.PreviewProxy = previewProxy
		sess.SessionMux = sessMux

		// Start per-port listeners for port-based proxy mode.
		// Port-based proxy uses empty BasePath (no URL rewriting) and shares
		// the same DebugHub so MCP tools and debug WebSockets work in both modes.
		portPreviewProxy, _ := agentproxy.New(agentproxy.Config{
			Target:      previewTarget,
			ToolPrefix:  "preview",
			ThemeCookie: "swe-swe-theme",
			Hub:         sharedHub,
		})
		previewPP := previewProxyPort(previewPort)
		previewSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", previewPP),
			Handler: corsWrapper(portPreviewProxy),
		}
		go func() {
			defer recoverGoroutine(fmt.Sprintf("preview proxy for session %s", sess.UUID))
			ln, err := net.Listen("tcp", previewSrv.Addr)
			if err != nil {
				log.Printf("Session %s: preview proxy port %d unavailable: %v", sess.UUID, previewPP, err)
				return
			}
			log.Printf("Session %s: preview proxy listening on :%d", sess.UUID, previewPP)
			if err := previewSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Printf("Session %s: preview proxy server error: %v", sess.UUID, err)
			}
		}()
		sess.PreviewProxyServer = previewSrv

		acPP := agentChatProxyPort(acPort)
		acSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", acPP),
			Handler: corsWrapper(agentChatProxyHandler(acTarget)),
		}
		go func() {
			defer recoverGoroutine(fmt.Sprintf("agent chat proxy for session %s", sess.UUID))
			ln, err := net.Listen("tcp", acSrv.Addr)
			if err != nil {
				log.Printf("Session %s: agent chat proxy port %d unavailable: %v", sess.UUID, acPP, err)
				return
			}
			log.Printf("Session %s: agent chat proxy listening on :%d", sess.UUID, acPP)
			if err := acSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Printf("Session %s: agent chat proxy server error: %v", sess.UUID, err)
			}
		}()
		sess.AgentChatProxyServer = acSrv

		// Public port: Traefik routes directly to the app (no swe-swe-server proxy needed)
	}

	// Save metadata immediately so recordings are properly tracked even if session ends unexpectedly
	if err := sess.saveMetadata(); err != nil {
		log.Printf("Failed to save initial metadata: %v", err)
	}

	// Save chat sidecar metadata so grouped listings can discover it
	if chatRecordingUUID != "" {
		chatPrefix := recordingPrefix(recordingUUID, chatRecordingUUID)
		chatMeta := &RecordingMetadata{
			UUID:          chatRecordingUUID,
			Name:          name,
			Agent:         cfg.Name,
			RecordingType: "chat",
			StartedAt:     now,
			WorkDir:       workDir,
		}
		chatMetaPath := fmt.Sprintf("%s/%s.metadata.json", recordingsDir, chatPrefix)
		if data, err := json.MarshalIndent(chatMeta, "", "  "); err == nil {
			if err := os.WriteFile(chatMetaPath, data, 0644); err != nil {
				log.Printf("Failed to save chat metadata: %v", err)
			}
		}
	}

	// Browser processes are started on-demand via POST /api/session/{uuid}/browser/start
	// (triggered by mcp-lazy-init on first Playwright MCP tool call).
	// Child sessions share the parent's browser.

	log.Printf("Created new session: %s (assistant=%s, pid=%d, recording=%s)", sess.UUID, cfg.Name, cmd.Process.Pid, recordingUUID)
	return sess, true, nil // new session
}

func handleProxyRoute(w http.ResponseWriter, r *http.Request) {
	// Extract session UUID from /proxy/{uuid}/{preview|agentchat}/...
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/proxy/"), "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	sessionUUID := parts[0]

	sessionsMu.RLock()
	sess, ok := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !ok || sess.SessionMux == nil {
		http.NotFound(w, r)
		return
	}
	sess.SessionMux.ServeHTTP(w, r)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, sessionUUID string) {
	// Log client info for debugging
	userAgent := r.Header.Get("User-Agent")
	remoteAddr := r.RemoteAddr
	log.Printf("WebSocket upgrade request: session=%s remote=%s UA=%s", sessionUUID, remoteAddr, userAgent)

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v (remote=%s)", err, remoteAddr)
		return
	}
	defer rawConn.Close()

	// Wrap in SafeConn for thread-safe writes
	conn := NewSafeConn(rawConn)

	// Get assistant from query param
	assistant := r.URL.Query().Get("assistant")
	if assistant == "" {
		log.Printf("WebSocket error: no assistant specified (remote=%s)", remoteAddr)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: no assistant specified"))
		return
	}

	// Get optional session name (display name for new session dialog)
	sessionName := r.URL.Query().Get("name")

	// Get optional branch (for worktree creation, separate from display name)
	// Sanitize to ensure git-safe branch name (e.g. spaces become hyphens)
	branchParam := deriveBranchName(r.URL.Query().Get("branch"))

	// Get optional pwd (base repo path for new session dialog)
	pwd := r.URL.Query().Get("pwd")

	// Get optional parent session UUID to inherit workDir and name (for shell sessions)
	parentUUID := r.URL.Query().Get("parent")
	var workDir string
	var parentName string
	var parentRecordingUUID string
	if parentUUID != "" {
		sessionsMu.RLock()
		parentSess, parentFound := sessions[parentUUID]
		if parentFound {
			workDir = parentSess.WorkDir
			parentName = parentSess.Name
			parentRecordingUUID = parentSess.RecordingUUID
			log.Printf("Shell session inheriting workDir from parent %s: %s", parentUUID, workDir)
		}
		sessionsMu.RUnlock()

		// If parent was specified but not found (e.g., after server reboot),
		// reject the connection so the client retries -- the parent tab may
		// reconnect shortly and recreate the parent session.
		if !parentFound {
			log.Printf("Parent session %s not found for child %s, rejecting (client will retry)", parentUUID, sessionUUID)
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(4001, "parent session not found"))
			return
		}
	}

	// Get theme hint from client for COLORFGBG env var
	theme := r.URL.Query().Get("theme")

	// Get session mode (chat or terminal)
	sessionMode := r.URL.Query().Get("session")
	if sessionMode == "" {
		sessionMode = "terminal" // default
	}

	sess, isNew, err := getOrCreateSession(SessionParams{
		UUID:                sessionUUID,
		Assistant:           assistant,
		Name:                sessionName,
		Branch:              branchParam,
		WorkDir:             workDir,
		RepoPath:            pwd,
		ParentUUID:          parentUUID,
		ParentName:          parentName,
		ParentRecordingUUID: parentRecordingUUID,
		Theme:               theme,
		SessionMode:         sessionMode,
	})
	if err != nil {
		log.Printf("Session creation error: %v (remote=%s)", err, remoteAddr)
		conn.WriteMessage(websocket.TextMessage, []byte("Error creating session: "+err.Error()))
		return
	}

	// Add this client to the session
	sess.AddClient(conn)
	defer sess.RemoveClient(conn)

	// Track visitor in metadata (for non-first clients)
	if !isNew {
		sess.mu.Lock()
		if sess.Metadata != nil {
			sess.Metadata.Visitors = append(sess.Metadata.Visitors, Visitor{
				JoinedAt: time.Now(),
				IP:       remoteAddr,
			})
		}
		sess.mu.Unlock()
		if err := sess.saveMetadata(); err != nil {
			log.Printf("Failed to save metadata for visitor: %v", err)
		}
	}

	log.Printf("WebSocket connected: session=%s (new=%v, remote=%s)", sessionUUID, isNew, remoteAddr)

	// If this is a new session, start the PTY reader goroutine
	if isNew {
		// Display MOTD with available swe-swe commands (for human user discoverability)
		// Send directly to websocket (not PTY) so ANSI escape codes are properly interpreted
		motd := generateMOTD(sess.WorkDir, sess.BranchName, sess.AssistantConfig)
		if motd != "" {
			motdBytes := []byte(motd)
			// Write to VT and ring buffer so joining clients see it too
			sess.vtMu.Lock()
			sess.vt.Write(motdBytes)
			sess.writeToRing(motdBytes)
			sess.vtMu.Unlock()
			// Send to first client
			conn.WriteMessage(websocket.BinaryMessage, motdBytes)
			// Buffer input during grace period so MOTD stays visible
			sess.SetGracePeriod(MOTDGracePeriod)
		}
		sess.startPTYReader()
	} else {
		// Send ring buffer (scrollback history) first, then VT snapshot
		// Both are gzip-compressed and sent as chunked messages for iOS Safari compatibility
		log.Printf("Generating scrollback and snapshot for joining client (remote=%s)", remoteAddr)

		// Send ring buffer contents (scrollback history) if any
		sess.vtMu.Lock()
		ringData := sess.readRing()
		sess.vtMu.Unlock()

		if len(ringData) > 0 {
			compressed, err := compressSnapshot(ringData)
			if err != nil {
				log.Printf("Failed to compress scrollback: %v (remote=%s)", err, remoteAddr)
			} else {
				log.Printf("Sending %d bytes of scrollback history (compressed: %d bytes, remote=%s)", len(ringData), len(compressed), remoteAddr)
				numChunks, err := sendChunked(conn, compressed, DefaultChunkSize)
				if err != nil {
					log.Printf("Failed to send scrollback chunks: %v (remote=%s, sent %d chunks before error)", err, remoteAddr, numChunks)
				} else {
					log.Printf("Sent scrollback history (%d bytes raw, %d chunks, remote=%s)", len(ringData), numChunks, remoteAddr)
				}
			}
		}

		// Send VT snapshot (positions cursor correctly on current screen)
		snapshot := sess.GenerateSnapshot()
		log.Printf("Sending %d byte snapshot to client (remote=%s)", len(snapshot), remoteAddr)
		numChunks, err := sendChunked(conn, snapshot, DefaultChunkSize)
		if err != nil {
			log.Printf("Failed to send snapshot chunks: %v (remote=%s, sent %d chunks before error)", err, remoteAddr, numChunks)
		} else {
			log.Printf("Sent screen snapshot to new client (%d bytes in %d chunks, remote=%s)", len(snapshot), numChunks, remoteAddr)
		}
	}

	// Read from this WebSocket and write to PTY
	// Message protocol:
	// - Binary frames: terminal I/O
	//   - If starts with 0x00 and len >= 5: resize message [0x00, rows_hi, rows_lo, cols_hi, cols_lo]
	//   - Otherwise: terminal input
	// - Text frames: JSON control messages {"type": "...", ...}
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			// Provide more context on disconnect reason
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				log.Printf("WebSocket closed: %v (remote=%s)", err, remoteAddr)
			} else {
				log.Printf("WebSocket read error: %v (remote=%s)", err, remoteAddr)
			}
			break
		}

		// Handle text (JSON) messages
		if messageType == websocket.TextMessage {
			var msg struct {
				Type     string          `json:"type"`
				Data     json.RawMessage `json:"data,omitempty"`
				UserName string          `json:"userName,omitempty"`
				Text     string          `json:"text,omitempty"`
				Name     string          `json:"name,omitempty"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("Invalid JSON message: %v", err)
				continue
			}

			switch msg.Type {
			case "ping":
				response := map[string]interface{}{"type": "pong"}
				if msg.Data != nil {
					response["data"] = msg.Data
				}
				if err := conn.WriteJSON(response); err != nil {
					log.Printf("Failed to send pong: %v", err)
				}
			case "chat":
				// Handle incoming chat message
				if msg.UserName != "" && msg.Text != "" {
					sess.BroadcastChatMessage(msg.UserName, msg.Text)
					log.Printf("Chat message from %s: %s", msg.UserName, msg.Text)
				}
			case "rename_session":
				// Handle session rename request
				name := strings.TrimSpace(msg.Name)
				// Validate: max 256 chars, alphanumeric + spaces + hyphens + underscores + slashes + dots + @
				if len(name) > 256 {
					log.Printf("Session rename rejected: name too long (%d chars)", len(name))
					continue
				}
				valid := true
				for _, r := range name {
					if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_' || r == '/' || r == '.' || r == '@') {
						valid = false
						break
					}
				}
				if !valid {
					log.Printf("Session rename rejected: invalid characters in name %q", name)
					continue
				}
				sess.mu.Lock()
				sess.Name = name
				if sess.Metadata != nil {
					sess.Metadata.Name = name
				}
				sess.mu.Unlock()
				log.Printf("Session %s renamed to %q", sess.UUID, name)
				// Save metadata with new name
				if err := sess.saveMetadata(); err != nil {
					log.Printf("Failed to save metadata: %v", err)
				}
				sess.BroadcastStatus()

				// Propagate rename to child sessions (shell sessions opened from this agent session)
				sessionsMu.RLock()
				for _, childSess := range sessions {
					if childSess.ParentUUID == sess.UUID {
						childSess.mu.Lock()
						var childName string
						if name != "" {
							childName = name + " (Terminal)"
						}
						childSess.Name = childName
						if childSess.Metadata != nil {
							childSess.Metadata.Name = childName
						}
						childSess.mu.Unlock()
						log.Printf("Child session %s renamed to %q (parent %s renamed)", childSess.UUID, childName, sess.UUID)
						if err := childSess.saveMetadata(); err != nil {
							log.Printf("Failed to save child metadata: %v", err)
						}
						childSess.BroadcastStatus()
					}
				}
				sessionsMu.RUnlock()
			case "toggle_yolo":
				// Handle YOLO mode toggle request
				// Check if agent supports YOLO mode
				if sess.AssistantConfig.YoloRestartCmd == "" {
					log.Printf("Session %s: toggle_yolo ignored (agent %s doesn't support YOLO)", sess.UUID, sess.AssistantConfig.Name)
					continue
				}

				sess.mu.Lock()
				newYoloMode := !sess.yoloMode
				sess.yoloMode = newYoloMode
				sess.pendingReplacement = sess.computeRestartCommand(newYoloMode)
				cmd := sess.Cmd
				sess.mu.Unlock()

				log.Printf("Session %s: toggling YOLO mode to %v", sess.UUID, newYoloMode)

				// Broadcast status update with new YOLO state
				sess.BroadcastStatus()

				// Send visual feedback to terminal
				modeStr := "OFF"
				if newYoloMode {
					modeStr = "ON"
				}
				feedbackMsg := []byte(fmt.Sprintf("\r\n[Switching YOLO mode %s, restarting agent...]\r\n", modeStr))
				sess.vtMu.Lock()
				sess.vt.Write(feedbackMsg)
				sess.writeToRing(feedbackMsg)
				sess.vtMu.Unlock()
				sess.Broadcast(feedbackMsg)

				// Kill process group - pendingReplacement will cause process to be replaced
				if cmd != nil && cmd.Process != nil {
					log.Printf("[KILL] YOLO toggle: sending SIGTERM to process group -%d (server pid=%d)", cmd.Process.Pid, os.Getpid())
					syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				}
			default:
				log.Printf("Unknown message type: %s", msg.Type)
			}
			continue
		}

		// Handle binary messages (terminal I/O)
		// Check for resize message (0x00 prefix)
		if len(data) >= 5 && data[0] == 0x00 {
			rows := uint16(data[1])<<8 | uint16(data[2])
			cols := uint16(data[3])<<8 | uint16(data[4])
			sess.UpdateClientSize(conn, rows, cols)
			continue
		}

		// Check for file upload message (0x01 prefix)
		// Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
		if len(data) >= 3 && data[0] == 0x01 {
			nameLen := int(data[1])<<8 | int(data[2])
			if len(data) < 3+nameLen {
				log.Printf("Invalid file upload: data too short for filename")
				sendFileUploadResponse(conn, false, "", "Invalid upload format")
				continue
			}
			filename := string(data[3 : 3+nameLen])
			fileData := data[3+nameLen:]

			// Sanitize filename: only keep the base name, no path traversal
			filename = sanitizeFilename(filename)
			if filename == "" {
				sendFileUploadResponse(conn, false, "", "Invalid filename")
				continue
			}

			// Resolve uploads directory relative to session's working directory
			baseDir := sess.WorkDir
			if baseDir == "" {
				baseDir, _ = os.Getwd()
			}
			uploadsDir := filepath.Join(baseDir, ".swe-swe", "uploads")
			if err := os.MkdirAll(uploadsDir, 0755); err != nil {
				log.Printf("Failed to create uploads directory: %v", err)
				sendFileUploadResponse(conn, false, filename, "Failed to create uploads directory")
				continue
			}

			// Save the file to the uploads directory
			filePath := filepath.Join(uploadsDir, filename)
			if err := os.WriteFile(filePath, fileData, 0644); err != nil {
				log.Printf("File upload error: %v", err)
				sendFileUploadResponse(conn, false, filename, err.Error())
				continue
			}

			log.Printf("File uploaded: %s (%d bytes)", filePath, len(fileData))
			sendFileUploadResponse(conn, true, filename, "")

			// Send the file path to PTY - Claude Code will detect it and read from disk
			absFilePath := filePath
			if err := sess.WriteInputOrBuffer([]byte(absFilePath)); err != nil {
				log.Printf("PTY write error for uploaded file path: %v", err)
			}
			continue
		}

		// Regular terminal input (buffered during MOTD grace period)
		if err := sess.WriteInputOrBuffer(data); err != nil {
			log.Printf("PTY write error: %v", err)
			break
		}
	}

	log.Printf("WebSocket disconnected: session=%s (remote=%s)", sessionUUID, remoteAddr)
}

// parseCommand splits a command string into executable and arguments
func parseCommand(cmdStr string) (string, []string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return cmdStr, nil
	}
	return parts[0], parts[1:]
}

// recordingsDir is the directory where terminal recordings are stored
// This is a variable (not const) to allow override in tests
var recordingsDir = "/workspace/.swe-swe/recordings"

// ensureRecordingsDir creates the recordings directory if it doesn't exist
func ensureRecordingsDir() error {
	return os.MkdirAll(recordingsDir, 0755)
}

// saveMetadata writes the session's recording metadata to disk
// Only writes if metadata exists (name was set or visitor joined)
// When session has ended, also calculates playback dimensions from content.
func (s *Session) saveMetadata() error {
	s.mu.RLock()
	metadata := s.Metadata
	recPrefix := s.RecordingPrefix
	s.mu.RUnlock()

	if metadata == nil {
		return nil // Nothing to save
	}

	// Calculate playback dimensions when session ends (only once)
	if metadata.EndedAt != nil && metadata.PlaybackCols == 0 {
		logPath := resolveLogPath(recPrefix)
		if logPath != "" {
			dims := calculateTerminalDimensions(logPath)
			s.mu.Lock()
			s.Metadata.PlaybackCols = dims.Cols
			s.Metadata.PlaybackRows = dims.Rows
			s.mu.Unlock()
		}
	}

	path := fmt.Sprintf("%s/%s.metadata.json", recordingsDir, recPrefix)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// recordingPrefix returns the filename prefix for a recording.
// Child sessions use "session-{parentRecUUID}-{recUUID}", root sessions use "session-{recUUID}".
func recordingPrefix(parentRecUUID, recUUID string) string {
	if parentRecUUID != "" {
		return "session-" + parentRecUUID + "-" + recUUID
	}
	return "session-" + recUUID
}

// parseRecordingFilename extracts parent and child UUIDs from a recording filename stem.
// A stem is the filename minus the "session-" prefix and the extension(s).
// Root recordings have a single UUID (36 chars). Child recordings have parent-child (36+1+36=73 chars).
// Returns (parentUUID, childUUID, ok). For root recordings, childUUID is empty.
func parseRecordingFilename(stem string) (parentUUID, childUUID string, ok bool) {
	const uuidLen = 36
	switch len(stem) {
	case uuidLen:
		if _, err := uuid.Parse(stem); err != nil {
			return "", "", false
		}
		return stem, "", true
	case uuidLen + 1 + uuidLen:
		if stem[uuidLen] != '-' {
			return "", "", false
		}
		parent := stem[:uuidLen]
		child := stem[uuidLen+1:]
		if _, err := uuid.Parse(parent); err != nil {
			return "", "", false
		}
		if _, err := uuid.Parse(child); err != nil {
			return "", "", false
		}
		return parent, child, true
	default:
		return "", "", false
	}
}

// wrapWithScript wraps a command with the Linux script command for recording.
// Returns the new command name and arguments to record terminal output and timing.
// The prefix is the filename stem (e.g. "session-{uuid}" or "session-{parent}-{child}").
//
// Terminal output is compressed in real-time using a named pipe (FIFO) and gzip,
// producing a .log.gz file instead of a plain .log file. This reduces disk usage
// by ~100x since the Claude Code TUI generates highly repetitive screen redraws.
// The timing and input files remain uncompressed (they are small).
func wrapWithScript(cmdName string, cmdArgs []string, prefix string) (string, []string) {
	// Build the full command string for script -c
	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd += " " + strings.Join(cmdArgs, " ")
	}

	logGzPath := fmt.Sprintf("%s/%s.log.gz", recordingsDir, prefix)
	logPipePath := fmt.Sprintf("%s/%s.log.pipe", recordingsDir, prefix)
	timingPath := fmt.Sprintf("%s/%s.timing", recordingsDir, prefix)
	inputPath := fmt.Sprintf("%s/%s.input", recordingsDir, prefix)

	// Use a named pipe (FIFO) to compress terminal output in real-time:
	// 1. Create a FIFO at .log.pipe
	// 2. Start gzip in background reading from the FIFO, writing to .log.gz
	// 3. Run script with -O pointing to the FIFO
	// 4. After script exits, wait for gzip to finish and clean up the FIFO
	//
	// The trap ensures that when SIGTERM arrives (session end), we forward it
	// to the script process, then wait for gzip to flush. Without this, bash
	// exits immediately on SIGTERM and gzip never finishes writing.
	//
	// setsid isolates gzip from the session's process group so it survives
	// the group-wide SIGTERM sent by killSessionProcessGroup. Once script
	// exits (closing the FIFO write end), gzip reads the remaining data,
	// flushes, and exits naturally.
	// Run script in the foreground so it inherits stdin from the PTY.
	// If script is backgrounded (& + wait), bash owns the foreground and
	// stdin never reaches script, breaking interactive input for TUI apps
	// like Claude Code. gzip runs in the background via setsid and exits
	// naturally when script closes the FIFO write end.
	wrapperScript := fmt.Sprintf(
		`rm -f %[1]q; mkfifo %[1]q; setsid sh -c 'gzip > %[2]q < %[1]q' & GZ_PID=$!; trap 'rm -f %[1]q' EXIT; script -q -f -T %[3]q -I %[4]q -O %[1]q -c %[5]q; EXIT=$?; wait $GZ_PID 2>/dev/null; exit $EXIT`,
		logPipePath, logGzPath, timingPath, inputPath, fullCmd,
	)

	return "bash", []string{"-c", wrapperScript}
}

// resolveLogPath returns the path to a recording's log file, checking for both
// compressed (.log.gz) and uncompressed (.log) variants. Prefers .log.gz.
// Returns empty string if neither exists.
func resolveLogPath(prefix string) string {
	gzPath := fmt.Sprintf("%s/%s.log.gz", recordingsDir, prefix)
	if _, err := os.Stat(gzPath); err == nil {
		return gzPath
	}
	plainPath := fmt.Sprintf("%s/%s.log", recordingsDir, prefix)
	if _, err := os.Stat(plainPath); err == nil {
		return plainPath
	}
	return ""
}

// openLogReader opens a log file for reading, transparently decompressing gzip.
// Returns a ReadCloser that must be closed by the caller.
func openLogReader(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Check for gzip magic bytes
	header := make([]byte, 2)
	n, err := f.Read(header)
	if err != nil || n < 2 {
		f.Seek(0, io.SeekStart)
		return f, nil
	}
	f.Seek(0, io.SeekStart)

	if header[0] == 0x1f && header[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &gzipReadCloser{gz: gz, f: f}, nil
	}
	return f, nil
}

type gzipReadCloser struct {
	gz *gzip.Reader
	f  *os.File
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }
func (g *gzipReadCloser) Close() error {
	g.gz.Close()
	return g.f.Close()
}

// sanitizeFilename removes path components and validates the filename
func sanitizeFilename(name string) string {
	// Extract base name (remove any path components)
	name = strings.TrimSpace(name)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "\\"); idx >= 0 {
		name = name[idx+1:]
	}

	// Reject empty names, hidden files starting with .., or invalid names
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "..") {
		return ""
	}

	return name
}

// sendFileUploadResponse sends a JSON response for file upload
func sendFileUploadResponse(conn *SafeConn, success bool, filename, errMsg string) {
	response := map[string]interface{}{
		"type":    "file_upload",
		"success": success,
	}
	if filename != "" {
		response["filename"] = filename
	}
	if errMsg != "" {
		response["error"] = errMsg
	}
	if err := conn.WriteJSON(response); err != nil {
		log.Printf("Failed to send file upload response: %v", err)
	}
}

// handleSSLCertDownload serves the SSL certificate for mobile installation
// URL: /ssl/ca.crt (protected by forwardauth middleware)
func handleSSLCertDownload(w http.ResponseWriter, r *http.Request) {
	// Read certificate file
	certData, err := os.ReadFile(tlsCertPath)
	if err != nil {
		log.Printf("Failed to read SSL certificate: %v", err)
		http.Error(w, "Certificate not available", http.StatusNotFound)
		return
	}

	// Set headers for certificate download
	// iOS Safari recognizes application/x-x509-ca-cert and prompts to install
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", "attachment; filename=\"swe-swe-ca.crt\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(certData)))

	w.Write(certData)
	log.Printf("SSL certificate downloaded from %s", r.RemoteAddr)
}

// RecordingListItem represents a recording in the API response
type RecordingListItem struct {
	UUID        string     `json:"uuid"`
	Name        string     `json:"name,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	KeptAt      *time.Time `json:"kept_at,omitempty"`
	HasChat     bool       `json:"has_chat,omitempty"`
	HasTerminal bool       `json:"has_terminal,omitempty"`
	HasTiming bool       `json:"has_timing"`
	SizeBytes int64      `json:"size_bytes"`
	IsActive  bool       `json:"is_active,omitempty"`
}

// RecordingInfo holds recording data for template rendering
type RecordingInfo struct {
	UUID            string
	UUIDShort       string
	Name            string
	Agent           string
	AgentBadgeClass string
	EndedAgo        string     // "15m ago", "2h ago", "yesterday"
	EndedAt         time.Time  // actual timestamp for sorting
	KeptAt          *time.Time // When user marked this recording to keep (nil = recent, auto-deletable)
	IsKept          bool       // Convenience field for templates
	ExpiresIn       string     // "59m", "30m" - time until auto-deletion (only for non-kept)
	HasChat         bool       // has a chat .events.jsonl child recording
	HasTerminal     bool       // has a terminal .log child recording
	ChatUUID        string     // child UUID for chat playback URL
	TerminalUUIDs   []string   // child UUIDs for terminal playback
	SizeHuman       string           // human-readable total size ("2.4 GB")
	SummaryLine     string           // one-line summary from last chat event
	RestartUUID     string           // fresh UUID for "restart" link
	Query           SessionPageQuery // params to restart a similar session
}

func agentBadgeClass(agent string) string {
	if agent == "" {
		return "custom"
	}
	switch strings.ToLower(agent) {
	case "claude", "codex", "gemini", "goose", "aider":
		return strings.ToLower(agent)
	default:
		return "custom"
	}
}

// formatTimeAgo returns a human-readable relative time string
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	if d < 48*time.Hour {
		return "yesterday"
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%d days ago", days)
}

// formatSizeHuman returns a human-readable file size string
func formatSizeHuman(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// loadEndedRecordings returns a list of ended recordings for the homepage.
// Child recordings (terminal, chat) are grouped into their parent's RecordingInfo.
func loadEndedRecordings() []RecordingInfo {
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		return nil
	}

	// Build map of active recording UUIDs (only for sessions with running processes)
	activeRecordings := make(map[string]bool)
	sessionsMu.RLock()
	for _, sess := range sessions {
		// Only consider recording "active" if the process is still running
		if sess.RecordingUUID != "" && sess.Cmd != nil && sess.Cmd.ProcessState == nil {
			activeRecordings[sess.RecordingUUID] = true
		}
	}
	sessionsMu.RUnlock()

	// First pass: classify files into root and child sets.
	// Track which parent UUIDs have children, and what kind.
	type childInfo struct {
		chatUUID      string
		terminalUUIDs []string
	}
	children := make(map[string]*childInfo)  // parentUUID -> child info
	rootLogs := make(map[string]os.DirEntry) // uuid -> dir entry for .log files
	rootSizes := make(map[string]int64)      // parentUUID -> total bytes

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") {
			continue
		}

		// Strip "session-" prefix and extension to get the stem
		rest := strings.TrimPrefix(name, "session-")

		// Determine file type from extension
		var stem string
		var fileType string // "log", "events", "metadata", "timing", "input"
		switch {
		case strings.HasSuffix(rest, ".events.jsonl"):
			stem = strings.TrimSuffix(rest, ".events.jsonl")
			fileType = "events"
		case strings.HasSuffix(rest, ".metadata.json"):
			stem = strings.TrimSuffix(rest, ".metadata.json")
			fileType = "metadata"
		case strings.HasSuffix(rest, ".log.gz"):
			stem = strings.TrimSuffix(rest, ".log.gz")
			fileType = "log"
		case strings.HasSuffix(rest, ".log"):
			stem = strings.TrimSuffix(rest, ".log")
			fileType = "log"
		case strings.HasSuffix(rest, ".log.pipe"):
			continue // skip FIFO pipes used during recording
		case strings.HasSuffix(rest, ".timing"):
			stem = strings.TrimSuffix(rest, ".timing")
			fileType = "timing"
		case strings.HasSuffix(rest, ".input"):
			stem = strings.TrimSuffix(rest, ".input")
			fileType = "input"
		default:
			continue
		}

		parentUUID, childUUID, ok := parseRecordingFilename(stem)
		if !ok {
			continue
		}

		// Accumulate file size for the parent recording
		if fi, err := entry.Info(); err == nil {
			rootSizes[parentUUID] += fi.Size()
		}

		if childUUID == "" {
			// Root recording
			if fileType == "log" {
				rootLogs[parentUUID] = entry
			}
		} else {
			// Child recording -- classify by file type
			ci := children[parentUUID]
			if ci == nil {
				ci = &childInfo{}
				children[parentUUID] = ci
			}
			if fileType == "events" {
				if fi, err := entry.Info(); err == nil && fi.Size() > 0 {
					ci.chatUUID = childUUID
				}
			} else if fileType == "log" {
				ci.terminalUUIDs = append(ci.terminalUUIDs, childUUID)
			}
		}
	}

	// Second pass: build RecordingInfo entries for root recordings
	var recordings []RecordingInfo
	for ruuid, entry := range rootLogs {
		// Skip active recordings
		if activeRecordings[ruuid] {
			continue
		}

		uuidShort := ruuid
		if len(ruuid) >= 8 {
			uuidShort = ruuid[:8]
		}

		info := RecordingInfo{
			UUID:        ruuid,
			UUIDShort:   uuidShort,
			RestartUUID: uuid.New().String(),
			SizeHuman:   formatSizeHuman(rootSizes[ruuid]),
		}

		// Attach child info
		if ci := children[ruuid]; ci != nil {
			if ci.chatUUID != "" {
				info.HasChat = true
				info.ChatUUID = ci.chatUUID
			}
			if len(ci.terminalUUIDs) > 0 {
				info.HasTerminal = true
				info.TerminalUUIDs = ci.terminalUUIDs
			}
		}

		// Load metadata if exists
		metadataPath := recordingsDir + "/session-" + ruuid + ".metadata.json"
		if metaData, err := os.ReadFile(metadataPath); err == nil {
			var meta RecordingMetadata
			if json.Unmarshal(metaData, &meta) == nil {
				info.Name = meta.Name
				info.Agent = meta.Agent
				info.AgentBadgeClass = agentBadgeClass(meta.Agent)
				info.KeptAt = meta.KeptAt
				info.IsKept = meta.KeptAt != nil
				if meta.EndedAt != nil {
					info.EndedAt = *meta.EndedAt
					info.EndedAgo = formatTimeAgo(*meta.EndedAt)
				} else {
					info.EndedAt = meta.StartedAt
					info.EndedAgo = formatTimeAgo(meta.StartedAt)
				}
				// Build restart query from metadata
				binary := meta.AgentBinary
				if binary == "" {
					// Old recordings: fall back to lowercased display name
					binary = strings.ToLower(meta.Agent)
				}
				info.Query = SessionPageQuery{
					Assistant:   binary,
					SessionMode: meta.SessionMode,
					Name:        meta.Name,
					BranchName:  meta.BranchName,
					WorkDir:     meta.WorkDir,
				}
			}
		} else {
			// No metadata, use file modification time
			if fileInfo, err := entry.Info(); err == nil {
				info.EndedAt = fileInfo.ModTime()
				info.EndedAgo = formatTimeAgo(fileInfo.ModTime())
			}
		}
		if info.AgentBadgeClass == "" {
			info.AgentBadgeClass = agentBadgeClass(info.Agent)
		}

		// Calculate ExpiresIn for non-kept recordings based on EndedAt
		// (info.EndedAt is already set from metadata or fallback above)
		if !info.IsKept {
			remaining := recentRecordingMaxAge - time.Since(info.EndedAt)
			if remaining > 0 {
				days := int(remaining.Hours()) / 24
				hours := int(remaining.Hours()) % 24
				if days > 0 {
					info.ExpiresIn = fmt.Sprintf("%dd%dh", days, hours)
				} else if hours > 0 {
					mins := int(remaining.Minutes()) % 60
					info.ExpiresIn = fmt.Sprintf("%dh%dm", hours, mins)
				} else {
					mins := int(remaining.Minutes())
					if mins < 1 {
						info.ExpiresIn = "<1m"
					} else {
						info.ExpiresIn = fmt.Sprintf("%dm", mins)
					}
				}
			} else {
				info.ExpiresIn = "soon"
			}
		}

		// Extract summary from chat events (if available), fall back to agent terminal log
		if info.HasChat {
			summaryLine, _ := getSessionSummaryFromChat(ruuid)
			info.SummaryLine = summaryLine
		}
		if info.SummaryLine == "" {
			info.SummaryLine = getSessionSummaryFromLog(ruuid)
		}

		recordings = append(recordings, info)
	}

	// Sort by most recent first (newest EndedAt timestamp first)
	sort.Slice(recordings, func(i, j int) bool {
		return recordings[i].EndedAt.After(recordings[j].EndedAt)
	})

	return recordings
}

// agentNameToBinary maps display names to binary names for recording grouping
func agentNameToBinary(name string) string {
	for _, cfg := range assistantConfigs {
		if cfg.Name == name {
			return cfg.Binary
		}
	}
	// Check availableAssistants for custom assistants
	for _, cfg := range availableAssistants {
		if cfg.Name == name {
			return cfg.Binary
		}
	}
	return strings.ToLower(name)
}

// loadEndedRecordingsByAgent returns ended recordings grouped by agent binary name
func loadEndedRecordingsByAgent(recordings []RecordingInfo) map[string][]RecordingInfo {
	result := make(map[string][]RecordingInfo)
	for _, rec := range recordings {
		agent := rec.Agent
		if agent == "" {
			agent = "unknown"
		}
		// Convert display name to binary name for consistent grouping
		binaryName := agentNameToBinary(agent)
		result[binaryName] = append(result[binaryName], rec)
	}
	return result
}

// TerminalDimensions holds calculated terminal dimensions from content analysis.
// This mirrors what embedded mode's JavaScript calculates in the browser.
type TerminalDimensions struct {
	Cols uint16 // Terminal columns (from max line length, capped 80-240)
	Rows uint32 // Terminal rows (from cursor positions and line count)
}

// cursorPosRegex matches ESC[row;colH cursor position sequences.
// Compiled once to avoid per-call overhead.
var cursorPosRegex = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// isScriptHeader returns true for lines added by the `script` command at the top.
func isScriptHeader(line string) bool {
	return strings.HasPrefix(line, "Script started on") || strings.HasPrefix(line, "Command:")
}

// isScriptFooter returns true for lines added by the `script` command at the bottom.
func isScriptFooter(line string) bool {
	return strings.HasPrefix(line, "Saving session") ||
		strings.HasPrefix(line, "Command exit status") ||
		strings.HasPrefix(line, "Script done on")
}

// calculateTerminalDimensions analyzes session.log content to calculate dimensions.
// Streams the file line-by-line to avoid reading huge recordings into memory.
//   - Cols: min(max(maxLineLength, 80), 240)
//   - Rows: max(maxCursorRow, lineCount, 24), capped at 10000
func calculateTerminalDimensions(logPath string) TerminalDimensions {
	rc, err := openLogReader(logPath)
	if err != nil {
		return TerminalDimensions{Cols: 240, Rows: 24}
	}
	defer rc.Close()
	f := rc // use the reader (works for both plain and gzip)

	maxUsedRow := uint32(1)
	lineCount := uint32(0)
	maxLineLength := 0
	inHeader := true
	footerLines := uint32(0)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // up to 1MB per line
	for scanner.Scan() {
		line := scanner.Text()

		// Skip script header lines at the start
		if inHeader && isScriptHeader(line) {
			continue
		}
		inHeader = false

		// Track trailing footer lines to subtract later
		if isScriptFooter(line) || (footerLines > 0 && strings.TrimSpace(line) == "") {
			footerLines++
		} else {
			footerLines = 0
		}

		lineCount++

		if len(line) > maxLineLength {
			maxLineLength = len(line)
		}

		for _, match := range cursorPosRegex.FindAllStringSubmatch(line, -1) {
			if len(match) >= 2 {
				var row int
				fmt.Sscanf(match[1], "%d", &row)
				if row > 0 && uint32(row) > maxUsedRow {
					maxUsedRow = uint32(row)
				}
			}
		}
	}

	// Subtract footer lines from count
	if footerLines > 0 && lineCount >= footerLines {
		lineCount -= footerLines
	}
	if lineCount == 0 {
		lineCount = 1
	}

	rows := maxUsedRow
	if lineCount > rows {
		rows = lineCount
	}
	if rows < 24 {
		rows = 24
	}
	const maxRows = uint32(10000)
	if rows > maxRows {
		rows = maxRows
	}

	cols := maxLineLength
	if cols < 80 {
		cols = 80
	}
	if cols > 240 {
		cols = 240
	}

	return TerminalDimensions{Cols: uint16(cols), Rows: rows}
}

// handleRecordingPage serves the recording playback page
// Query params:
//   - render=embedded: use embedded approach (data in HTML, no streaming)
//   - (default): use streaming approach (fetch data via JS)
// getSessionSummaryFromChat reads the last event from an agent-chat events JSONL file
// and returns a summary line and status color.
// summaryLine: "{who}: {message start}" -- always starts from beginning, truncated by CSS.
// status: "green" if last event is agent message with quick_replies (waiting for user),
//
//	"red" if agent is busy or user message unanswered, "" if unknown.
func getSessionSummaryFromChat(recordingUUID string) (summaryLine string, status string) {
	eventsPath := findChatEventsFile(recordingUUID)
	if eventsPath == "" {
		return "", ""
	}

	// Read last line of JSONL file efficiently (read last 4KB)
	f, err := os.Open(eventsPath)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return "", ""
	}

	// Read the tail of the file to find the last complete line
	readSize := int64(4096)
	if fi.Size() < readSize {
		readSize = fi.Size()
	}
	buf := make([]byte, readSize)
	_, err = f.ReadAt(buf, fi.Size()-readSize)
	if err != nil && err != io.EOF {
		return "", ""
	}

	// Find the last complete JSON line
	lines := bytes.Split(bytes.TrimRight(buf, "\n"), []byte("\n"))
	if len(lines) == 0 {
		return "", ""
	}
	lastLine := lines[len(lines)-1]

	// Parse the JSON event
	var event struct {
		Type         string   `json:"type"`
		Text         string   `json:"text"`
		QuickReplies []string `json:"quick_replies"`
	}
	if err := json.Unmarshal(lastLine, &event); err != nil {
		return "", ""
	}

	// Build summary line
	switch event.Type {
	case "userMessage":
		summaryLine = "You: " + sanitizeSummaryText(event.Text)
		status = "red" // user sent message, agent hasn't replied
	case "agentMessage", "verbalReply":
		summaryLine = "Agent: " + sanitizeSummaryText(event.Text)
		if len(event.QuickReplies) > 0 {
			status = "green" // waiting for user reply
		} else {
			status = "red" // agent sent update, still working
		}
	case "draw":
		summaryLine = "Agent: [diagram]"
		if len(event.QuickReplies) > 0 {
			status = "green"
		} else {
			status = "red"
		}
	default:
		// agentProgress, verbalProgress, etc.
		summaryLine = "Agent: " + sanitizeSummaryText(event.Text)
		status = "red" // still working
	}
	return summaryLine, status
}

// sanitizeSummaryText removes newlines, control characters, and excess whitespace
// from a chat message for single-line display.
func sanitizeSummaryText(s string) string {
	// Remove markdown-style formatting that won't render in plain text
	// Replace newlines with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// getSessionSummaryFromTerminal reads the last visible line from a session's
// virtual terminal as a fallback when no chat events exist.
func getSessionSummaryFromTerminal(sess *Session) string {
	sess.vtMu.Lock()
	defer sess.vtMu.Unlock()

	cols, rows := sess.vt.Size()
	// Find last non-empty line (scanning from bottom)
	var lastLine string
	for row := rows - 1; row >= 0; row-- {
		var line strings.Builder
		for col := 0; col < cols; col++ {
			cell := sess.vt.Cell(col, row)
			if cell.Char == 0 {
				line.WriteRune(' ')
			} else {
				line.WriteRune(cell.Char)
			}
		}
		trimmed := strings.TrimSpace(line.String())
		if trimmed != "" && trimmed != "❯" {
			lastLine = trimmed
			break
		}
	}
	if lastLine == "" {
		return ""
	}
	return sanitizeSummaryText(lastLine)
}

// ansiEscapeRe matches ANSI escape sequences (CSI, OSC, and simple escapes).
var ansiEscapeRe = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z@]|\][^\x07\x1b]*(?:\x07|\x1b\\)|\([A-Z0-9]|[>=<])`)

// orphanedAnsiParamsRe matches leftover ANSI parameter fragments that leak when
// we start reading in the middle of an escape sequence (e.g. "255;255m").
var orphanedAnsiParamsRe = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])[0-9]+(?:;[0-9]+)*[a-zA-Z]`)

// getSessionSummaryFromLog reads the tail of a root .log recording file,
// strips ANSI escape sequences, and returns the last non-empty line as a summary.
// Used as a fallback when no chat events are available.
func getSessionSummaryFromLog(recordingUUID string) string {
	logPath := resolveLogPath("session-" + recordingUUID)
	if logPath == "" {
		return ""
	}

	// For compressed files, read the tail via decompression (compressed files
	// can't be seeked, so we read through and keep the last 8KB)
	if strings.HasSuffix(logPath, ".log.gz") {
		rc, err := openLogReader(logPath)
		if err != nil {
			return ""
		}
		defer rc.Close()

		// Stream through keeping only the last 8KB
		buf := make([]byte, 8192)
		ring := make([]byte, 0, 8192)
		for {
			n, err := rc.Read(buf)
			if n > 0 {
				ring = append(ring, buf[:n]...)
				if len(ring) > 8192 {
					ring = ring[len(ring)-8192:]
				}
			}
			if err != nil {
				break
			}
		}
		buf = ring
		return extractSummaryFromBytes(buf)
	}

	// For plain .log files, seek to the tail directly
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return ""
	}

	// Read the last 8KB to find readable text
	readSize := int64(8192)
	if fi.Size() < readSize {
		readSize = fi.Size()
	}
	buf := make([]byte, readSize)
	_, err = f.ReadAt(buf, fi.Size()-readSize)
	if err != nil && err != io.EOF {
		return ""
	}

	return extractSummaryFromBytes(buf)
}

// extractSummaryFromBytes extracts a summary line from raw log bytes (tail of file).
func extractSummaryFromBytes(buf []byte) string {
	// Strip ANSI escape sequences
	clean := ansiEscapeRe.ReplaceAll(buf, nil)

	// Remove remaining non-printable control characters (except newline)
	var filtered []byte
	for _, b := range clean {
		if b == '\n' || (b >= 32 && b < 127) {
			filtered = append(filtered, b)
		}
	}

	// Find the last non-empty, meaningful line (skip prompts, script footers, and garbled text)
	lines := bytes.Split(bytes.TrimRight(filtered, "\n"), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(string(lines[i]))
		if trimmed == "" || trimmed == "❯" || trimmed == "$" || trimmed == "%" {
			continue
		}
		if strings.HasPrefix(trimmed, "Script done") || strings.HasPrefix(trimmed, "Script started") {
			continue
		}
		// Skip garbled TUI output: if less than half the characters are word chars
		// or spaces, it's likely cursor-positioned screen fragments, not real text.
		wordChars := 0
		for _, r := range trimmed {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '.' || r == ',' || r == '-' || r == '\'' || r == '"' || r == ':' || r == '!' || r == '?' {
				wordChars++
			}
		}
		if len(trimmed) > 10 && wordChars*2 < len(trimmed) {
			continue // too many non-word characters -- likely garbled
		}
		// Skip lines that are too short to be meaningful
		if len(trimmed) < 8 {
			continue
		}
		return "Agent: " + sanitizeSummaryText(trimmed)
	}
	return ""
}

// findChatEventsFile finds the .events.jsonl file for a parent recording UUID.
// Returns the full path, or empty string if not found.
func findChatEventsFile(parentUUID string) string {
	pattern := recordingsDir + "/session-" + parentUUID + "-*.events.jsonl"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// handleChatEventsFile serves the raw .events.jsonl file for a parent recording.
func handleChatEventsFile(w http.ResponseWriter, r *http.Request, parentUUID string) {
	if _, err := uuid.Parse(parentUUID); err != nil {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}
	path := findChatEventsFile(parentUUID)
	if path == "" {
		http.Error(w, "Chat events not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, path)
}

// handleChatPlaybackPage serves an HTML page that replays agent-chat events.
// It inlines the agent-chat CSS/JS from the embedded agent-chat-dist directory.
func handleChatPlaybackPage(w http.ResponseWriter, r *http.Request, parentUUID string) {
	if _, err := uuid.Parse(parentUUID); err != nil {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}
	path := findChatEventsFile(parentUUID)
	if path == "" {
		http.Error(w, "Chat recording not found", http.StatusNotFound)
		return
	}

	// Read embedded assets
	cssData, _ := agentChatDistFS.ReadFile("agent-chat-dist/style.css")
	canvasJS, _ := agentChatDistFS.ReadFile("agent-chat-dist/canvas-bundle.js")
	appJS, _ := agentChatDistFS.ReadFile("agent-chat-dist/app.js")

	// Load metadata for title
	title := "Chat Playback"
	metaPattern := recordingsDir + "/session-" + parentUUID + ".metadata.json"
	if metaData, err := os.ReadFile(metaPattern); err == nil {
		var meta RecordingMetadata
		if json.Unmarshal(metaData, &meta) == nil && meta.Name != "" {
			title = meta.Name + " -- Chat"
		}
	}

	eventsURL := "/recording/" + parentUUID + "/chat.events.jsonl"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>%s</title>
  <style>%s</style>
</head>
<body>
  <div id="app">
    <div id="chat">
      <div id="chat-header">
        <button id="btn-download" style="display:none"></button>
      </div>
      <div id="messages"></div>
      <div id="quick-replies"></div>
      <div id="input-bar">
        <span id="status-dot"></span>
        <textarea id="chat-input" rows="1" placeholder="Type a message..." disabled></textarea>
        <button id="btn-send" disabled>Send</button>
      </div>
    </div>
  </div>
  <script>var THEME_COOKIE_NAME = "swe-swe-theme"; var AGENT_CHAT_DEFER_STARTUP = true;</script>
  <script>%s</script>
  <script>%s</script>
  <script>startPlaybackMode(%q);</script>
</body>
</html>`, title, string(cssData), string(canvasJS), string(appJS), eventsURL)
}

func handleRecordingPage(w http.ResponseWriter, r *http.Request, recordingUUID string) {
	// Validate UUID format
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	// Check if recording exists (supports both .log and .log.gz)
	logPath := resolveLogPath("session-" + recordingUUID)
	if logPath == "" {
		http.Error(w, "Recording not found", http.StatusNotFound)
		return
	}

	// Load metadata if exists
	var metadata *RecordingMetadata
	metadataPath := recordingsDir + "/session-" + recordingUUID + ".metadata.json"
	if metaData, err := os.ReadFile(metadataPath); err == nil {
		var meta RecordingMetadata
		if json.Unmarshal(metaData, &meta) == nil {
			metadata = &meta
		}
	}

	// Determine name for display
	uuidShort := recordingUUID
	if len(recordingUUID) >= 8 {
		uuidShort = recordingUUID[:8]
	}
	name := "session-" + uuidShort
	if metadata != nil && metadata.Name != "" {
		name = metadata.Name
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Streaming approach (embedded mode removed to avoid reading entire log into memory)
	var cols uint16
	if metadata != nil && metadata.PlaybackCols > 0 {
		cols = metadata.PlaybackCols
	} else {
		// Legacy fallback: calculate from content (same algorithm as embedded mode's JS)
		dims := calculateTerminalDimensions(logPath)
		cols = dims.Cols
	}

	// Detect Safari (has "Safari" but not "Chrome"/"Chromium" which also include "Safari")
	// Safari struggles with large terminal row allocations, so use actual content rows
	userAgent := r.Header.Get("User-Agent")
	isSafari := strings.Contains(userAgent, "Safari") &&
		!strings.Contains(userAgent, "Chrome") &&
		!strings.Contains(userAgent, "Chromium")

	var maxRows uint32 = 100000
	if isSafari {
		// Use actual content rows for Safari to avoid memory allocation issues
		if metadata != nil && metadata.PlaybackRows > 0 {
			maxRows = metadata.PlaybackRows
		} else {
			// Fallback to a reasonable default for Safari
			maxRows = 10000
		}
	}

	opts := recordtui.StreamingOptions{
		Title:   name,
		DataURL: recordingUUID + "/session.log",
		FooterLink: recordtui.FooterLink{
			Text: "swe-swe",
			URL:  "https://github.com/choonkeat/swe-swe",
		},
		Cols:    cols,
		MaxRows: maxRows,
	}

	// Build TOC from timing + input + session log files if available.
	// Uses streaming BuildTOC to avoid reading the entire session
	// log into memory (recordings can be hundreds of MB).
	timingPath := recordingsDir + "/session-" + recordingUUID + ".timing"
	inputPath := recordingsDir + "/session-" + recordingUUID + ".input"
	timingFile, err := os.Open(timingPath)
	if err == nil {
		defer timingFile.Close()
		inputBytes, err := os.ReadFile(inputPath)
		if err == nil {
			sessionReader, err := openLogReader(logPath)
			if err == nil {
				defer sessionReader.Close()
				opts.TOC = recordtui.BuildTOC(timingFile, inputBytes, sessionReader)
			}
		}
	}

	html, err := recordtui.RenderStreamingHTML(opts)
	if err != nil {
		http.Error(w, "Failed to render playback", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(html))
}

// handleRecordingSessionLog serves raw session.log for streaming playback.
// The streaming JS cleaner handles header/footer stripping and clear sequence
// neutralization - this keeps JS as the single source of truth for streaming mode.
// Supports both plain .log and compressed .log.gz files (decompresses on the fly).
func handleRecordingSessionLog(w http.ResponseWriter, r *http.Request, recordingUUID string) {
	// Validate UUID format
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	logPath := resolveLogPath("session-" + recordingUUID)
	if logPath == "" {
		http.Error(w, "Recording not found", http.StatusNotFound)
		return
	}

	// For plain .log files, serve directly (supports range requests)
	if !strings.HasSuffix(logPath, ".gz") {
		http.ServeFile(w, r, logPath)
		return
	}

	// For .log.gz files, decompress and stream
	rc, err := openLogReader(logPath)
	if err != nil {
		http.Error(w, "Cannot read recording", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, rc)
}

// collectDescendantPIDs returns all descendant PIDs of the given root PID
// by traversing /proc. This catches processes in different process groups
// (e.g., MCP servers spawned with detached: true by AI agents).
func collectDescendantPIDs(rootPID int) []int {
	// Build PPID -> children map by scanning /proc
	children := make(map[int][]int)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		// Parse PPID from /proc/[pid]/stat
		// Format: pid (comm) state ppid ...
		// comm can contain spaces and ')' so find the LAST ')'
		statStr := string(data)
		lastParen := strings.LastIndex(statStr, ")")
		if lastParen < 0 || lastParen+2 >= len(statStr) {
			continue
		}
		fields := strings.Fields(statStr[lastParen+2:])
		if len(fields) < 2 {
			continue
		}
		// fields[0] = state, fields[1] = ppid
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}

	// BFS from rootPID to collect all descendants
	var result []int
	queue := children[rootPID]
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		result = append(result, pid)
		queue = append(queue, children[pid]...)
	}
	return result
}

// getProcessTreeRSS returns the total RSS (in bytes) of a process and all its descendants.
func getProcessTreeRSS(rootPID int) int64 {
	pids := append([]int{rootPID}, collectDescendantPIDs(rootPID)...)
	var totalRSS int64
	pageSize := int64(os.Getpagesize())
	for _, pid := range pids {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			continue
		}
		// fields[1] is RSS in pages
		rssPages, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		totalRSS += rssPages * pageSize
	}
	return totalRSS
}

// getAvailableMemory returns MemAvailable from /proc/meminfo in bytes.
func getAvailableMemory() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}

// getMaxSessionRSS returns the RSS of the largest active session (in bytes).
func getMaxSessionRSS() int64 {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	var maxRSS int64
	for _, sess := range sessions {
		if sess.Cmd == nil || sess.Cmd.Process == nil {
			continue
		}
		rss := getProcessTreeRSS(sess.Cmd.Process.Pid)
		if rss > maxRSS {
			maxRSS = rss
		}
	}
	return maxRSS
}

// checkMemoryForNewSession returns an error if there isn't enough memory
// to safely start a new session. Uses the largest active session's RSS as
// the expected footprint for the new session.
func checkMemoryForNewSession() error {
	avail := getAvailableMemory()
	if avail == 0 {
		return nil // can't determine, allow
	}
	maxRSS := getMaxSessionRSS()
	if maxRSS == 0 {
		return nil // no active sessions, allow
	}
	if maxRSS > avail {
		return fmt.Errorf("insufficient memory: largest session uses %s but only %s available",
			formatBytes(maxRSS), formatBytes(avail))
	}
	return nil
}

// formatBytes formats bytes as a human-readable string (e.g. "1.5 GB").
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// killSessionProcessGroup sends SIGTERM to the session's entire process group,
// waits briefly for graceful shutdown, then sends SIGKILL if still alive.
// Also traverses the process tree to kill any descendant processes that escaped
// the process group (e.g., MCP servers spawned in detached process groups).
func killSessionProcessGroup(s *Session) {
	if s.Cmd == nil || s.Cmd.Process == nil {
		return
	}

	pid := s.Cmd.Process.Pid

	// Collect all descendant PIDs BEFORE killing -- once the parent dies,
	// orphaned children get reparented to PID 1 and we lose the lineage.
	descendants := collectDescendantPIDs(pid)

	// Send SIGTERM to the entire process group (-pid)
	log.Printf("[KILL] killSessionProcessGroup: sending SIGTERM to process group -%d (server pid=%d)", pid, os.Getpid())
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		log.Printf("Failed to SIGTERM process group %d: %v", pid, err)
	}

	// Wait briefly for graceful shutdown
	done := make(chan struct{})
	go func() {
		defer recoverGoroutine(fmt.Sprintf("process wait for session %s", s.UUID))
		s.Cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Process group exited gracefully
	case <-time.After(3 * time.Second):
		// Force kill the entire process group
		log.Printf("[KILL] killSessionProcessGroup: sending SIGKILL to process group -%d (server pid=%d)", pid, os.Getpid())
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			log.Printf("Failed to SIGKILL process group %d: %v", pid, err)
		}
		s.Cmd.Wait()
	}

	// Kill any descendant processes that escaped the process group.
	// These may be in a different PGID (e.g., detached MCP servers)
	// and would not have received the group-wide signals above.
	for _, dpid := range descendants {
		if dpid == os.Getpid() {
			log.Printf("[BUG] collectDescendantPIDs included server PID %d, skipping!", dpid)
			continue
		}
		log.Printf("[KILL] killing escaped descendant %d (session pid %d, server pid=%d)", dpid, pid, os.Getpid())
		if err := syscall.Kill(dpid, syscall.SIGKILL); err == nil {
			log.Printf("Killed escaped descendant process %d (session pid %d)", dpid, pid)
		}
	}
}

// startSignalMonitor logs all catchable signals for crash forensics.
// SIGKILL (9) cannot be caught, so if the server dies without any signal log
// entry, it confirms the death was by SIGKILL.
// SIGURG and SIGCHLD are very noisy (Go runtime uses SIGURG for goroutine
// preemption, SIGCHLD fires on every child exit) so they are only logged
// when LOGGING=verbose is set.
func startSignalMonitor() {
	verbose := os.Getenv("LOGGING") == "verbose"
	sigs := make(chan os.Signal, 64)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT,
		syscall.SIGILL, syscall.SIGTRAP, syscall.SIGABRT, syscall.SIGBUS,
		syscall.SIGFPE, syscall.SIGUSR1, syscall.SIGSEGV, syscall.SIGUSR2,
		syscall.SIGPIPE, syscall.SIGALRM, syscall.SIGTERM, syscall.SIGCHLD,
		syscall.SIGCONT, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU,
		syscall.SIGURG, syscall.SIGXCPU, syscall.SIGXFSZ, syscall.SIGVTALRM,
		syscall.SIGPROF, syscall.SIGWINCH, syscall.SIGIO, syscall.SIGSYS)
	go func() {
		for sig := range sigs {
			// SIGURG (Go runtime goroutine preemption) and SIGCHLD (child exits)
			// are extremely frequent and harmless -- skip unless verbose.
			if !verbose && (sig == syscall.SIGURG || sig == syscall.SIGCHLD) {
				continue
			}
			log.Printf("[SIGNAL] pid=%d received signal %v", os.Getpid(), sig)
		}
	}()
	log.Printf("[SIGNAL] monitor started for pid=%d (verbose=%v)", os.Getpid(), verbose)
}

// startHeartbeat writes the current timestamp to /tmp/swe-swe-heartbeat every
// second. On unexpected death (SIGKILL), the file's mtime reveals when the
// server was last alive, narrowing the death window to ~1 second.
func startHeartbeat() {
	go func() {
		for {
			os.WriteFile("/tmp/swe-swe-heartbeat",
				[]byte(fmt.Sprintf("%d %s", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))),
				0644)
			time.Sleep(1 * time.Second)
		}
	}()
	log.Printf("[HEARTBEAT] started, writing to /tmp/swe-swe-heartbeat")
}

// probePort checks if something is listening on the given port by making an
// HTTP GET to localhost. Returns (listening, pageTitle). If the HTTP request
// succeeds, extracts <title> from the response body as a best-effort hint
// for the user. Falls back to a TCP connect if HTTP fails but port is open.
func probePort(port int) (bool, string) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client := &http.Client{
		Timeout: 500 * time.Millisecond,
		// Don't follow redirects -- we just want the first response
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(fmt.Sprintf("http://%s/", addr))
	if err == nil {
		defer resp.Body.Close()
		// Read up to 64KB to find <title>
		body := make([]byte, 64*1024)
		n, _ := io.ReadAtLeast(resp.Body, body, 1)
		if n > 0 {
			re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
			if m := re.FindSubmatch(body[:n]); len(m) > 1 {
				return true, strings.TrimSpace(string(m[1]))
			}
		}
		return true, ""
	}
	// HTTP failed -- try raw TCP to distinguish "not listening" from "not HTTP"
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false, ""
	}
	conn.Close()
	return true, ""
}

// endSessionByUUID terminates a session by UUID. Used by both REST API and MCP tool.
func endSessionByUUID(sessionUUID string) error {
	// Find the session and collect child sessions, but keep them in the map
	// so their ports stay reserved until cleanup completes.
	sessionsMu.Lock()
	session, exists := sessions[sessionUUID]
	if !exists {
		sessionsMu.Unlock()
		return fmt.Errorf("session not found")
	}

	// Collect child sessions (sessions whose ParentUUID matches this session)
	var childSessions []*Session
	var childUUIDs []string
	for childUUID, childSess := range sessions {
		if childSess.ParentUUID == sessionUUID {
			childSessions = append(childSessions, childSess)
			childUUIDs = append(childUUIDs, childUUID)
		}
	}
	sessionsMu.Unlock()

	// Collect all session ports for port-based cleanup after tree kill
	sessionPorts := []int{session.PreviewPort, session.AgentChatPort, session.PublicPort, session.CDPPort, session.VNCPort}
	for _, child := range childSessions {
		sessionPorts = append(sessionPorts, child.PreviewPort, child.AgentChatPort, child.PublicPort, child.CDPPort, child.VNCPort)
	}

	// End child sessions first
	for _, child := range childSessions {
		log.Printf("Cascade-ending child session %s (parent=%s)", child.UUID, sessionUUID)
		killSessionProcessGroup(child)
		child.Close()
	}

	// End the main session
	killSessionProcessGroup(session)
	session.Close()

	// Kill any remaining processes still listening on session ports.
	// Belt-and-suspenders: catches processes that escaped both the process
	// group kill and the /proc descendant scan (e.g., double-forked daemons).
	killProcessesOnPorts(sessionPorts)

	// Now remove sessions from the map -- ports are safe to reuse
	sessionsMu.Lock()
	delete(sessions, sessionUUID)
	for _, childUUID := range childUUIDs {
		delete(sessions, childUUID)
	}
	sessionsMu.Unlock()

	return nil
}

// handleSessionEndAPI handles POST /api/session/{uuid}/end
//
// Two-phase protocol for public port safety:
//  1. First call: if something is listening on the session's public port,
//     returns 409 Conflict with JSON {"publicPort": 5007, "message": "..."}
//  2. Second call with header X-Confirm-Public-Port: 5007 proceeds with ending.
func handleSessionEndAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse UUID from path: /api/session/{uuid}/end
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	path = strings.TrimSuffix(path, "/end")
	sessionUUID := path

	if sessionUUID == "" {
		http.Error(w, "Missing session UUID", http.StatusBadRequest)
		return
	}

	// Server-side public port check: probe before ending
	sessionsMu.RLock()
	session, exists := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	publicPort := session.PublicPort
	if publicPort != 0 {
		confirmed := r.Header.Get("X-Confirm-Public-Port")
		if confirmed != strconv.Itoa(publicPort) {
			listening, pageTitle := probePort(publicPort)
			if listening {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				resp := map[string]interface{}{
					"publicPort": publicPort,
					"message":    fmt.Sprintf("Something is listening on PUBLIC_PORT %d. Confirm to end.", publicPort),
				}
				if pageTitle != "" {
					resp["pageTitle"] = pageTitle
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
	}

	if err := endSessionByUUID(sessionUUID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBrowserStartAPI handles POST /api/session/{uuid}/browser/start
// Starts browser processes on demand. Idempotent -- returns success if already started.
func handleBrowserStartAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// API key authentication (shared with MCP endpoint)
	if key := r.URL.Query().Get("key"); key == "" || key != mcpAuthKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse UUID from path: /api/session/{uuid}/browser/start
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	path = strings.TrimSuffix(path, "/browser/start")
	sessionUUID := path

	if sessionUUID == "" {
		http.Error(w, "Missing session UUID", http.StatusBadRequest)
		return
	}

	sessionsMu.RLock()
	sess, exists := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.BrowserStarted {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"already_started"}`))
		return
	}

	if err := startSessionBrowser(sess); err != nil {
		log.Printf("Failed to start browser for session %s: %v", sessionUUID, err)
		http.Error(w, fmt.Sprintf("Failed to start browser: %v", err), http.StatusInternalServerError)
		return
	}

	// Push browserStarted:true to all connected WebSocket clients
	sess.BroadcastStatus()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"started"}`))
}

// handleVNCReadyAPI handles GET /api/session/{uuid}/vnc-ready
// Returns 200 if websockify is listening on the session's VNC port, 503 otherwise.
// This is a same-origin probe endpoint so the client can check real status codes
// instead of getting opaque responses from cross-origin no-cors requests.
func handleVNCReadyAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionUUID := strings.TrimSuffix(path, "/vnc-ready")

	if sessionUUID == "" {
		http.Error(w, "Missing session UUID", http.StatusBadRequest)
		return
	}

	sessionsMu.RLock()
	sess, exists := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.VNCPort == 0 {
		http.Error(w, "VNC not configured", http.StatusServiceUnavailable)
		return
	}

	// TCP connect to websockify to check if it's listening
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", sess.VNCPort), 500*time.Millisecond)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"ready":false}`))
		return
	}
	conn.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ready":true}`))
}

// killProcessesOnPorts finds and kills any processes listening on the given
// ports by parsing /proc/net/tcp. This is a last-resort cleanup for processes
// that escaped both process group signals and /proc descendant tracking.
func killProcessesOnPorts(ports []int) {
	if len(ports) == 0 {
		return
	}

	// Build a set of target ports (hex-encoded, as they appear in /proc/net/tcp)
	targetPorts := make(map[int]bool)
	for _, p := range ports {
		if p != 0 {
			targetPorts[p] = true
		}
	}
	if len(targetPorts) == 0 {
		return
	}

	// Parse /proc/net/tcp to find inodes of sockets listening on our ports.
	// Format: sl local_address rem_address st tx_queue rx_queue ... inode
	// State 0A = LISTEN
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return
	}

	targetInodes := make(map[string]int) // inode string -> port
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		// State must be LISTEN (0A)
		if fields[3] != "0A" {
			continue
		}
		// Parse port from local_address (format: hex_ip:hex_port)
		addrParts := strings.SplitN(fields[1], ":", 2)
		if len(addrParts) != 2 {
			continue
		}
		port, err := strconv.ParseInt(addrParts[1], 16, 32)
		if err != nil {
			continue
		}
		if targetPorts[int(port)] {
			targetInodes[fields[9]] = int(port)
		}
	}

	if len(targetInodes) == 0 {
		return
	}

	// Scan /proc to find PIDs holding these socket inodes
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue // skip non-PID entries and our own process
		}
		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			// link looks like "socket:[12345]"
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := link[len("socket:[") : len(link)-1]
			if port, ok := targetInodes[inode]; ok {
				log.Printf("[KILL] killProcessesOnPorts: sending SIGKILL to pid %d on port %d (server pid=%d)", pid, port, os.Getpid())
				if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
					log.Printf("Killed lingering process %d on port %d (session port cleanup)", pid, port)
				}
				break // one kill per PID is enough
			}
		}
	}
}

// --- MCP Orchestration Tools ---

func registerOrchestrationTools(server *mcp.Server) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("AddTool panicked: %v", r)
		}
	}()

	// list_sessions
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List all active agent sessions",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		type sessionInfo struct {
			UUID        string `json:"uuid"`
			Name        string `json:"name"`
			Assistant   string `json:"assistant"`
			ClientCount int    `json:"clientCount"`
			Duration    string `json:"duration"`
			WorkDir     string `json:"workDir"`
			BranchName  string `json:"branchName,omitempty"`
			PreviewPort int    `json:"previewPort"`
			PublicPort  int    `json:"publicPort"`
		}
		var result []sessionInfo
		sessionsMu.RLock()
		for _, sess := range sessions {
			if sess.Cmd.ProcessState != nil {
				continue
			}
			sess.mu.RLock()
			result = append(result, sessionInfo{
				UUID:        sess.UUID,
				Name:        sess.Name,
				Assistant:   sess.Assistant,
				ClientCount: len(sess.wsClients),
				Duration:    formatDuration(time.Since(sess.CreatedAt)),
				WorkDir:     sess.WorkDir,
				BranchName:  sess.BranchName,
				PreviewPort: sess.PreviewPort,
				PublicPort:  sess.PublicPort,
			})
			sess.mu.RUnlock()
		}
		sessionsMu.RUnlock()
		if result == nil {
			result = []sessionInfo{}
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})

	// create_session
	type createSessionArgs struct {
		Assistant string `json:"assistant" jsonschema:"Agent binary name (e.g. claude, gemini)"`
		Name      string `json:"name,omitempty" jsonschema:"Session display name"`
		Branch    string `json:"branch,omitempty" jsonschema:"Git branch to create worktree for"`
		RepoPath  string `json:"repo_path,omitempty" jsonschema:"Repository path (default /workspace)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_session",
		Description: "Create a new agent session",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createSessionArgs) (*mcp.CallToolResult, any, error) {
		if args.Assistant == "" {
			return nil, nil, fmt.Errorf("assistant is required")
		}
		sessionUUID := uuid.New().String()
		sess, _, err := getOrCreateSession(SessionParams{
			UUID:        sessionUUID,
			Assistant:   args.Assistant,
			Name:        args.Name,
			Branch:      args.Branch,
			RepoPath:    args.RepoPath,
			SessionMode: "chat",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create session: %w", err)
		}
		sess.startPTYReader()
		info := map[string]string{
			"uuid":      sess.UUID,
			"name":      sess.Name,
			"assistant": sess.Assistant,
			"workDir":   sess.WorkDir,
		}
		data, _ := json.Marshal(info)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})

	// end_session
	type endSessionArgs struct {
		UUID string `json:"uuid" jsonschema:"Session UUID to terminate"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "end_session",
		Description: "Gracefully terminate an agent session",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args endSessionArgs) (*mcp.CallToolResult, any, error) {
		if err := endSessionByUUID(args.UUID); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "session ended"}}}, nil, nil
	})

	// get_session_output
	type getOutputArgs struct {
		UUID string `json:"uuid" jsonschema:"Session UUID"`
		Mode string `json:"mode,omitempty" jsonschema:"Output mode: screen (default) or scrollback"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session_output",
		Description: "Read terminal output from a session (screen = current visible state, scrollback = full history)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getOutputArgs) (*mcp.CallToolResult, any, error) {
		sessionsMu.RLock()
		sess, exists := sessions[args.UUID]
		sessionsMu.RUnlock()
		if !exists {
			return nil, nil, fmt.Errorf("session not found")
		}

		var text string
		if args.Mode == "scrollback" {
			sess.vtMu.Lock()
			raw := sess.readRing()
			sess.vtMu.Unlock()
			// Strip ANSI escape sequences
			text = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[.*?[mGKHJP]`).ReplaceAllString(string(raw), "")
		} else {
			// Screen mode: read clean text from VT state
			sess.vtMu.Lock()
			cols, rows := sess.vt.Size()
			var buf strings.Builder
			for row := 0; row < rows; row++ {
				var line strings.Builder
				for col := 0; col < cols; col++ {
					cell := sess.vt.Cell(col, row)
					if cell.Char == 0 {
						line.WriteRune(' ')
					} else {
						line.WriteRune(cell.Char)
					}
				}
				buf.WriteString(strings.TrimRight(line.String(), " "))
				if row < rows-1 {
					buf.WriteRune('\n')
				}
			}
			sess.vtMu.Unlock()
			text = buf.String()
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})

	// send_session_input
	type sendInputArgs struct {
		UUID string `json:"uuid" jsonschema:"Session UUID"`
		Text string `json:"text" jsonschema:"Text to write to the session PTY"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_session_input",
		Description: "Write text to a session's terminal (PTY)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendInputArgs) (*mcp.CallToolResult, any, error) {
		sessionsMu.RLock()
		sess, exists := sessions[args.UUID]
		sessionsMu.RUnlock()
		if !exists {
			return nil, nil, fmt.Errorf("session not found")
		}
		// Strip trailing newlines/carriage-returns and send them after a delay
		// as a raw CR byte, matching the mobile keyboard pattern (300ms) so the
		// TUI processes the text before receiving Enter.
		text := strings.TrimRight(args.Text, "\r\n")
		hasTrailingNewline := len(text) < len(args.Text)
		if text != "" {
			if err := sess.WriteInputOrBuffer([]byte(text)); err != nil {
				return nil, nil, fmt.Errorf("write failed: %w", err)
			}
		}
		if hasTrailingNewline {
			time.Sleep(300 * time.Millisecond)
			if err := sess.WriteInputOrBuffer([]byte{'\r'}); err != nil {
				return nil, nil, fmt.Errorf("write failed: %w", err)
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "input sent"}}}, nil, nil
	})

	// list_worktrees
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_worktrees",
		Description: "List git worktrees with their active sessions",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		worktrees, err := listWorktrees()
		if err != nil {
			return nil, nil, err
		}
		// Attach active sessions
		sessionsMu.RLock()
		branchToSession := make(map[string]*Session)
		for _, sess := range sessions {
			if sess.BranchName != "" {
				branchToSession[sess.BranchName] = sess
			}
		}
		sessionsMu.RUnlock()
		for i := range worktrees {
			if sess, ok := branchToSession[worktrees[i].Name]; ok {
				sess.mu.RLock()
				worktrees[i].ActiveSession = &WorktreeSessionInfo{
					UUID:        sess.UUID,
					Name:        sess.Name,
					Assistant:   sess.Assistant,
					ClientCount: len(sess.wsClients),
					DurationStr: formatDuration(time.Since(sess.CreatedAt)),
				}
				sess.mu.RUnlock()
			}
		}
		data, _ := json.Marshal(map[string]interface{}{"worktrees": worktrees})
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})

	// list_recordings
	type listRecordingsArgs struct {
		Limit int `json:"limit,omitempty" jsonschema:"Maximum number of recordings to return (default 20)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_recordings",
		Description: "List ended session recordings",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listRecordingsArgs) (*mcp.CallToolResult, any, error) {
		recordings := loadEndedRecordings()
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > len(recordings) {
			limit = len(recordings)
		}
		// Sort newest first
		sort.Slice(recordings, func(i, j int) bool {
			return recordings[i].EndedAt.After(recordings[j].EndedAt)
		})
		type recInfo struct {
			UUID    string `json:"uuid"`
			Name    string `json:"name,omitempty"`
			Agent   string `json:"agent,omitempty"`
			EndedAt string `json:"endedAt,omitempty"`
			HasChat bool   `json:"hasChat,omitempty"`
		}
		var result []recInfo
		for _, r := range recordings[:limit] {
			result = append(result, recInfo{
				UUID:    r.UUID,
				Name:    r.Name,
				Agent:   r.Agent,
				EndedAt: r.EndedAgo,
				HasChat: r.HasChat,
			})
		}
		if result == nil {
			result = []recInfo{}
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})

	// prepare_repo
	type prepareRepoArgs struct {
		Mode string `json:"mode" jsonschema:"Preparation mode: workspace, clone, or create"`
		URL  string `json:"url,omitempty" jsonschema:"Repository URL (for clone mode)"`
		Name string `json:"name,omitempty" jsonschema:"Project name (for create mode)"`
		Path string `json:"path,omitempty" jsonschema:"Existing repo path (for workspace mode)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_repo",
		Description: "Clone, create, or prepare a repository for use",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args prepareRepoArgs) (*mcp.CallToolResult, any, error) {
		switch args.Mode {
		case "workspace":
			workDir := "/workspace"
			if args.Path != "" {
				cleaned := filepath.Clean(args.Path)
				if !strings.HasPrefix(cleaned, reposDir+"/") {
					return nil, nil, fmt.Errorf("invalid repository path")
				}
				workDir = cleaned
			}
			resp := map[string]interface{}{"path": workDir}
			// Soft fetch if git repo
			if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
				remoteCmd := exec.Command("git", "-C", workDir, "remote")
				if out, err := remoteCmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
					cmd := exec.Command("git", "-C", workDir, "fetch", "--all")
					if out, err := cmd.CombinedOutput(); err != nil {
						resp["warning"] = fmt.Sprintf("fetch failed: %s", string(out))
					}
				}
			}
			data, _ := json.Marshal(resp)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil

		case "clone":
			if args.URL == "" {
				return nil, nil, fmt.Errorf("url is required for clone mode")
			}
			sanitizedURL := sanitizeRepoURL(args.URL)
			if sanitizedURL == "" {
				return nil, nil, fmt.Errorf("invalid repository URL")
			}
			repoBase := filepath.Join(reposDir, sanitizedURL)
			repoPath := filepath.Join(repoBase, "workspace")
			if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
				cmd := exec.Command("git", "-C", repoPath, "fetch", "--all")
				if out, err := cmd.CombinedOutput(); err != nil {
					return nil, nil, fmt.Errorf("git fetch failed: %s", string(out))
				}
			} else {
				if err := os.MkdirAll(repoBase, 0755); err != nil {
					return nil, nil, fmt.Errorf("failed to create directory: %w", err)
				}
				cmd := exec.Command("git", "clone", args.URL, repoPath)
				if out, err := cmd.CombinedOutput(); err != nil {
					return nil, nil, fmt.Errorf("git clone failed: %s", string(out))
				}
			}
			if err := setupSweSweFiles(repoPath); err != nil {
				log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
			}
			data, _ := json.Marshal(map[string]string{"path": repoPath})
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil

		case "create":
			if args.Name == "" {
				return nil, nil, fmt.Errorf("name is required for create mode")
			}
			dirName := sanitizeProjectDirName(args.Name)
			if dirName == "" {
				return nil, nil, fmt.Errorf("project name must contain at least one letter or number")
			}
			repoPath := filepath.Join(reposDir, dirName, "workspace")
			if _, err := os.Stat(repoPath); err == nil {
				return nil, nil, fmt.Errorf("project '%s' already exists", dirName)
			}
			if err := os.MkdirAll(repoPath, 0755); err != nil {
				return nil, nil, fmt.Errorf("failed to create directory: %w", err)
			}
			cmd := exec.Command("git", "-C", repoPath, "init")
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, nil, fmt.Errorf("git init failed: %s", string(out))
			}
			commitCmd := exec.Command("git", "-C", repoPath,
				"-c", "user.name=swe-swe", "-c", "user.email=swe-swe@localhost",
				"commit", "--allow-empty", "-m", "initial")
			commitCmd.CombinedOutput() // non-fatal
			if err := setupSweSweFiles(repoPath); err != nil {
				log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
			}
			data, _ := json.Marshal(map[string]string{"path": repoPath})
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil

		default:
			return nil, nil, fmt.Errorf("invalid mode '%s': use workspace, clone, or create", args.Mode)
		}
	})

	// send_chat_message -- proxy to agent-chat orchestrator
	type sendChatArgs struct {
		UUID string `json:"uuid" jsonschema:"Session UUID"`
		Text string `json:"text" jsonschema:"Message text to send"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_chat_message",
		Description: "Send a message to a session's agent chat (as if a user sent it from the browser)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendChatArgs) (*mcp.CallToolResult, any, error) {
		sessionsMu.RLock()
		sess, exists := sessions[args.UUID]
		sessionsMu.RUnlock()
		if !exists {
			return nil, nil, fmt.Errorf("session not found")
		}
		if sess.AgentChatPort == 0 {
			return nil, nil, fmt.Errorf("session has no agent chat (terminal-only session)")
		}
		result, err := callAgentChatOrchestrator(sess.AgentChatPort, "send_chat_message", map[string]string{"text": args.Text})
		if err != nil {
			return nil, nil, fmt.Errorf("agent chat error: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result}}}, nil, nil
	})

	// get_chat_history -- proxy to agent-chat orchestrator
	type getChatArgs struct {
		UUID   string `json:"uuid" jsonschema:"Session UUID"`
		Cursor int64  `json:"cursor,omitempty" jsonschema:"Return events with seq > cursor. 0 returns all."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_chat_history",
		Description: "Get chat event history from a session's agent chat",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getChatArgs) (*mcp.CallToolResult, any, error) {
		sessionsMu.RLock()
		sess, exists := sessions[args.UUID]
		sessionsMu.RUnlock()
		if !exists {
			return nil, nil, fmt.Errorf("session not found")
		}
		if sess.AgentChatPort == 0 {
			return nil, nil, fmt.Errorf("session has no agent chat (terminal-only session)")
		}
		result, err := callAgentChatOrchestrator(sess.AgentChatPort, "get_chat_history", map[string]int64{"cursor": args.Cursor})
		if err != nil {
			return nil, nil, fmt.Errorf("agent chat error: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result}}}, nil, nil
	})

	return nil
}

// callAgentChatOrchestrator makes a JSON-RPC call to the agent-chat's
// /mcp/orchestrator StreamableHTTP endpoint.
func callAgentChatOrchestrator(port int, toolName string, args any) (string, error) {
	// Build MCP tools/call JSON-RPC request
	type mcpCallParams struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments,omitempty"`
	}
	type jsonRPCRequest struct {
		JSONRPC string        `json:"jsonrpc"`
		ID      int           `json:"id"`
		Method  string        `json:"method"`
		Params  mcpCallParams `json:"params"`
	}
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  mcpCallParams{Name: toolName, Arguments: args},
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/mcp/orchestrator", port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse JSON-RPC response to extract tool result text
	var rpcResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError,omitempty"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	// If response is SSE-formatted (e.g., "event: message\ndata: {...}"),
	// extract the JSON payload from the data line before unmarshalling.
	if bytes.HasPrefix(respBody, []byte("event:")) {
		for _, line := range bytes.Split(respBody, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data: ")) {
				respBody = bytes.TrimPrefix(line, []byte("data: "))
				break
			}
		}
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}
	if rpcResp.Error != nil {
		return "", fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}
	if len(rpcResp.Result.Content) > 0 {
		return rpcResp.Result.Content[0].Text, nil
	}
	return "", nil
}

// handleRecordingAPI routes recording API requests
func handleRecordingAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/recording/")

	// GET /api/recording/list
	if path == "list" && r.Method == http.MethodGet {
		handleListRecordings(w, r)
		return
	}

	// Routes with UUID: /api/recording/{uuid} or /api/recording/{uuid}/download
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	recordingUUID := parts[0]

	// Validate UUID format (basic check)
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	// DELETE /api/recording/{uuid}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		handleDeleteRecording(w, r, recordingUUID)
		return
	}

	// GET /api/recording/{uuid}/download
	if len(parts) == 2 && parts[1] == "download" && r.Method == http.MethodGet {
		handleDownloadRecording(w, r, recordingUUID)
		return
	}

	// POST /api/recording/{uuid}/keep
	if len(parts) == 2 && parts[1] == "keep" && r.Method == http.MethodPost {
		handleKeepRecording(w, r, recordingUUID)
		return
	}

	// PATCH /api/recording/{uuid}/rename
	if len(parts) == 2 && parts[1] == "rename" && r.Method == http.MethodPatch {
		handleRenameRecording(w, r, recordingUUID)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

// handleListRecordings returns a list of all recordings
func handleListRecordings(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No recordings directory yet
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"recordings":[]}`))
			return
		}
		log.Printf("Failed to read recordings directory: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Build map of active recording UUIDs (only for sessions with running processes)
	activeRecordings := make(map[string]bool)
	sessionsMu.RLock()
	for _, sess := range sessions {
		// Only consider recording "active" if the process is still running
		if sess.RecordingUUID != "" && sess.Cmd != nil && sess.Cmd.ProcessState == nil {
			activeRecordings[sess.RecordingUUID] = true
		}
	}
	sessionsMu.RUnlock()

	// First pass: identify child files for each parent
	type childPresence struct {
		hasChat     bool
		hasTerminal bool
	}
	childMap := make(map[string]*childPresence)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") {
			continue
		}
		rest := strings.TrimPrefix(name, "session-")
		var stem string
		var fileType string
		switch {
		case strings.HasSuffix(rest, ".events.jsonl"):
			stem = strings.TrimSuffix(rest, ".events.jsonl")
			fileType = "events"
		case strings.HasSuffix(rest, ".log.gz"):
			stem = strings.TrimSuffix(rest, ".log.gz")
			fileType = "log"
		case strings.HasSuffix(rest, ".log"):
			stem = strings.TrimSuffix(rest, ".log")
			fileType = "log"
		default:
			continue
		}
		parentUUID, childUUID, ok := parseRecordingFilename(stem)
		if !ok || childUUID == "" {
			continue
		}
		cp := childMap[parentUUID]
		if cp == nil {
			cp = &childPresence{}
			childMap[parentUUID] = cp
		}
		if fileType == "events" {
			if fi, err := entry.Info(); err == nil && fi.Size() > 0 {
				cp.hasChat = true
			}
		} else if fileType == "log" {
			cp.hasTerminal = true
		}
	}

	// Second pass: find root recordings by looking for .log or .log.gz files with single UUID
	recordings := make([]RecordingListItem, 0)
	seenUUIDs := make(map[string]bool) // deduplicate if both .log and .log.gz exist
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") {
			continue
		}

		// Extract UUID from filename: session-{uuid}.log or session-{uuid}.log.gz
		stem := strings.TrimPrefix(name, "session-")
		if strings.HasSuffix(stem, ".log.gz") {
			stem = strings.TrimSuffix(stem, ".log.gz")
		} else if strings.HasSuffix(stem, ".log") {
			stem = strings.TrimSuffix(stem, ".log")
		} else {
			continue
		}

		// Only process root recordings
		parentUUID, childUUID, ok := parseRecordingFilename(stem)
		if !ok || childUUID != "" {
			continue
		}
		recUUID := parentUUID

		// Skip if we've already seen this UUID (dedup .log and .log.gz)
		if seenUUIDs[recUUID] {
			continue
		}
		seenUUIDs[recUUID] = true

		// Get file info
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := RecordingListItem{
			UUID:      recUUID,
			SizeBytes: info.Size(),
			IsActive:  activeRecordings[recUUID],
		}

		// Attach child presence
		if cp := childMap[recUUID]; cp != nil {
			item.HasChat = cp.hasChat
			item.HasTerminal = cp.hasTerminal
		}

		// Check if timing file exists
		timingPath := recordingsDir + "/session-" + recUUID + ".timing"
		if _, err := os.Stat(timingPath); err == nil {
			item.HasTiming = true
		}

		// Load metadata if exists
		metadataPath := recordingsDir + "/session-" + recUUID + ".metadata.json"
		if metaData, err := os.ReadFile(metadataPath); err == nil {
			var meta RecordingMetadata
			if json.Unmarshal(metaData, &meta) == nil {
				item.Name = meta.Name
				item.Agent = meta.Agent
				item.StartedAt = &meta.StartedAt
				item.EndedAt = meta.EndedAt
				item.KeptAt = meta.KeptAt
			}
		}

		recordings = append(recordings, item)
	}

	// Sort by StartedAt descending (newest first)
	sort.Slice(recordings, func(i, j int) bool {
		if recordings[i].StartedAt == nil {
			return false
		}
		if recordings[j].StartedAt == nil {
			return true
		}
		return recordings[i].StartedAt.After(*recordings[j].StartedAt)
	})

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{"recordings": recordings}
	json.NewEncoder(w).Encode(response)
}

// handleDeleteRecording deletes a recording and its associated files (including children)
func handleDeleteRecording(w http.ResponseWriter, r *http.Request, uuid string) {
	// Check if recording is active (only block if process is still running)
	sessionsMu.RLock()
	for _, sess := range sessions {
		// Only consider recording "active" if the process is still running
		if sess.RecordingUUID == uuid && sess.Cmd != nil && sess.Cmd.ProcessState == nil {
			sessionsMu.RUnlock()
			http.Error(w, "Cannot delete active recording", http.StatusConflict)
			return
		}
	}
	sessionsMu.RUnlock()

	// Check if recording exists before deleting
	logPath := resolveLogPath("session-" + uuid)
	metaPath := recordingsDir + "/session-" + uuid + ".metadata.json"
	if logPath == "" {
		if _, err2 := os.Stat(metaPath); err2 != nil {
			http.Error(w, "Recording not found", http.StatusNotFound)
			return
		}
	}

	deleteRecordingFiles(uuid)
	log.Printf("Deleted recording group: %s", uuid)
	w.WriteHeader(http.StatusNoContent)
}

// handleKeepRecording marks a recording as "kept" so it won't be auto-deleted
func handleKeepRecording(w http.ResponseWriter, r *http.Request, uuid string) {
	metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"

	// Read existing metadata
	metaData, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Recording not found", http.StatusNotFound)
			return
		}
		log.Printf("Failed to read metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var meta RecordingMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		log.Printf("Failed to parse metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if already kept
	if meta.KeptAt != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"kept_at":      meta.KeptAt,
			"already_kept": true,
		})
		return
	}

	// Set KeptAt to now
	now := time.Now()
	meta.KeptAt = &now

	// Write back metadata
	updatedMeta, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(metadataPath, updatedMeta, 0644); err != nil {
		log.Printf("Failed to write metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Printf("Recording %s marked as kept", uuid)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"kept_at":      meta.KeptAt,
		"already_kept": false,
	})
}

// handleRenameRecording updates the Name field in recording metadata
func handleRenameRecording(w http.ResponseWriter, r *http.Request, uuid string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate name: max 256 chars, alphanumeric + spaces + hyphens + underscores + slashes + dots + @
	name := strings.TrimSpace(req.Name)
	if len(name) > 256 {
		http.Error(w, "Name too long (max 256 characters)", http.StatusBadRequest)
		return
	}
	if name != "" {
		for _, r := range name {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_' || r == '/' || r == '.' || r == '@') {
				http.Error(w, "Invalid characters in name", http.StatusBadRequest)
				return
			}
		}
	}

	metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"

	// Read existing metadata
	metaData, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Recording not found", http.StatusNotFound)
			return
		}
		log.Printf("Failed to read metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var meta RecordingMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		log.Printf("Failed to parse metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	meta.Name = name

	// Write back metadata
	updatedMeta, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(metadataPath, updatedMeta, 0644); err != nil {
		log.Printf("Failed to write metadata for %s: %v", uuid, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Printf("Recording %s renamed to %q", uuid, name)
	w.WriteHeader(http.StatusOK)
}

// handleDownloadRecording creates a zip archive of the recording files (including children)
func handleDownloadRecording(w http.ResponseWriter, r *http.Request, uuid string) {
	logPath := resolveLogPath("session-" + uuid)

	// Check if log file exists
	if logPath == "" {
		http.Error(w, "Recording not found", http.StatusNotFound)
		return
	}

	// Create zip in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add parent files (include both .log and .log.gz variants)
	parentFiles := []struct {
		path string
		name string
	}{
		{recordingsDir + "/session-" + uuid + ".log", "session.log"},
		{recordingsDir + "/session-" + uuid + ".log.gz", "session.log.gz"},
		{recordingsDir + "/session-" + uuid + ".timing", "session.timing"},
		{recordingsDir + "/session-" + uuid + ".metadata.json", "session.metadata.json"},
	}
	for _, f := range parentFiles {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		zf, _ := zipWriter.Create(f.name)
		zf.Write(data)
	}

	// Add child files (session-{uuid}-*)
	childMatches, _ := filepath.Glob(recordingsDir + "/session-" + uuid + "-*")
	for _, path := range childMatches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		zf, _ := zipWriter.Create(filepath.Base(path))
		zf.Write(data)
	}

	if err := zipWriter.Close(); err != nil {
		http.Error(w, "Failed to create archive", http.StatusInternalServerError)
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"recording-%s.zip\"", uuid[:8]))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
	log.Printf("Recording downloaded: %s", uuid)
}
