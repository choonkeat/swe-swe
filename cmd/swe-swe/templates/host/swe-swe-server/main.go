package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	recordtui "github.com/choonkeat/record-tui/playback"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hinshun/vt10x"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

//go:embed all:static
var staticFS embed.FS

//go:embed all:container-templates
var containerTemplatesFS embed.FS

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
)

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
)

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
	YoloRestartCmd  string             // Command to restart in YOLO mode (empty = not supported)
	Binary          string             // Binary name to check with exec.LookPath
	Homepage        bool               // Whether to show on homepage (false = hidden, e.g., shell)
	SlashCmdFormat  SlashCommandFormat // Slash command format ("md", "toml", or "" for none)
}

// SessionInfo holds session data for template rendering
type SessionInfo struct {
	UUID        string
	UUIDShort   string
	Name        string // User-assigned session name (optional)
	Assistant   string // binary name for URL
	ClientCount int
	CreatedAt   time.Time
	DurationStr string // human-readable duration (e.g., "5m", "1h 23m")
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
	UUID         string     `json:"uuid"`
	Name         string     `json:"name,omitempty"`
	Agent        string     `json:"agent"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
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
		YoloRestartCmd:  "claude --dangerously-skip-permissions --continue",
		Binary:          "claude",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdMD,
	},
	{
		Name:            "Gemini",
		ShellCmd:        "gemini",
		ShellRestartCmd: "gemini --resume",
		YoloRestartCmd:  "gemini --resume --approval-mode=yolo",
		Binary:          "gemini",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdTOML,
	},
	{
		Name:            "Codex",
		ShellCmd:        "codex",
		ShellRestartCmd: "codex resume --last",
		YoloRestartCmd:  "codex --yolo resume --last",
		Binary:          "codex",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdMD,
	},
	{
		Name:            "Goose",
		ShellCmd:        "goose session",
		ShellRestartCmd: "goose session -r",
		YoloRestartCmd:  "GOOSE_MODE=auto goose session -r",
		Binary:          "goose",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdFile,
	},
	{
		Name:            "Aider",
		ShellCmd:        "aider",
		ShellRestartCmd: "aider --restore-chat-history",
		YoloRestartCmd:  "aider --yes-always --restore-chat-history",
		Binary:          "aider",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdFile,
	},
	{
		Name:            "OpenCode",
		ShellCmd:        "opencode",
		ShellRestartCmd: "opencode --continue",
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
	RecordingUUID string             // UUID for recording files (separate from session UUID for restarts)
	Metadata      *RecordingMetadata // Recording metadata (saved on name change or visitor join)
	// Parent session relationship
	ParentUUID  string // UUID of parent session (for shell sessions opened from agent sessions)
	PreviewPort   int // App preview target port for this session
	AgentChatPort int // Agent chat MCP server port for this session
	// Input buffering during MOTD grace period
	inputBuffer   [][]byte // buffered input during grace period
	inputBufferMu sync.Mutex
	graceUntil    time.Time // buffer input until this time
	// YOLO mode state
	yoloMode           bool   // Whether YOLO mode is active
	pendingReplacement string // If set, replace process with this command instead of ending session
	// UI theme at session creation (for COLORFGBG env var)
	Theme string // "light" or "dark"
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

func buildSessionEnv(previewPort, agentChatPort int, theme, workDir string) []string {
	env := filterEnv(os.Environ(), "TERM", "PORT", "BROWSER", "PATH", "COLORFGBG", "AGENT_CHAT_PORT")
	env = append(env,
		"TERM=xterm-256color",
		fmt.Sprintf("PORT=%d", previewPort),
		fmt.Sprintf("AGENT_CHAT_PORT=%d", agentChatPort),
		"BROWSER=/home/app/.swe-swe/bin/swe-swe-open",
		"PATH=/home/app/.swe-swe/bin:"+os.Getenv("PATH"),
	)
	// Set COLORFGBG so CLI tools (vim, bat, ls --color, etc.) adapt to background
	if theme == "light" {
		env = append(env, "COLORFGBG=0;15") // dark-on-light
	} else {
		env = append(env, "COLORFGBG=15;0") // light-on-dark
	}
	// Append user-defined vars from swe-swe/env (last so they take precedence)
	if workDir != "" {
		env = append(env, loadEnvFile(filepath.Join(workDir, "swe-swe", "env"))...)
	}
	return env
}

func loadEnvFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, ok := strings.Cut(line, "="); ok {
			entries = append(entries, line)
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
	status := map[string]interface{}{
		"type":          "status",
		"viewers":       len(s.wsClients),
		"cols":          cols,
		"rows":          rows,
		"assistant":     s.AssistantConfig.Name,
		"sessionName":   s.Name,
		"uuidShort":     uuidShort,
		"workDir":       workDir,
		"previewPort":    s.PreviewPort,
		"agentChatPort":  s.AgentChatPort,
		"yoloMode":       s.yoloMode,
		"yoloSupported": s.AssistantConfig.YoloRestartCmd != "",
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

	// Close all WebSocket client connections
	for conn := range s.wsClients {
		conn.Close()
	}
	s.wsClients = make(map[*SafeConn]bool)

	// Kill the process and close PTY
	if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
		// Wait to reap the zombie process
		s.Cmd.Wait()
	}
	if s.PTY != nil {
		s.PTY.Close()
	}

	previewPort := s.PreviewPort
	acPort := s.AgentChatPort
	s.mu.Unlock()

	releasePreviewProxyServer(previewPort)
	releaseAgentChatProxyServer(acPort)
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

	// Wrap with script for recording (reuse existing recording UUID)
	cmdName, cmdArgs = wrapWithScript(cmdName, cmdArgs, s.RecordingUUID)

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = buildSessionEnv(s.PreviewPort, s.AgentChatPort, s.Theme, s.WorkDir)
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}

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
				// Kill the process if it's still running, otherwise cmd.Wait() blocks forever.
				if cmd != nil && cmd.Process != nil {
					if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
						log.Printf("Session %s: PTY broken but process (pid %d) still alive, killing it",
							s.UUID, cmd.Process.Pid)
						cmd.Process.Kill()
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

// previewProxyErrorPage returns an HTML error page for when the app is not running
// Uses fetch-based polling to avoid white flash on reload
// Note: %% is used to escape % characters in CSS (e.g., 50%%, 100vh) for fmt.Fprintf
const previewProxyErrorPage = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>App Preview</title>
    <script>
        (function(){var m=document.cookie.match(/(?:^|;\s*)swe-swe-theme=([^;]+)/);
        if(m)document.documentElement.setAttribute('data-theme',m[1]);})();
    </script>
    <style>
        :root {
            --pp-bg: #1e1e1e;
            --pp-text: #9ca3af;
            --pp-heading: #e5e7eb;
            --pp-instr-bg: #262626;
            --pp-instr-label: #6b7280;
            --pp-instr-text: #d1d5db;
            --pp-port: #60a5fa;
            --pp-status: #6b7280;
        }
        [data-theme="light"] {
            --pp-bg: #ffffff;
            --pp-text: #64748b;
            --pp-heading: #1e293b;
            --pp-instr-bg: #f1f5f9;
            --pp-instr-label: #94a3b8;
            --pp-instr-text: #334155;
            --pp-port: #2563eb;
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
        .container {
            text-align: center;
            padding: 2rem;
            max-width: 400px;
        }
        h1 { color: var(--pp-heading); font-size: 1.25rem; font-weight: 500; margin-bottom: 1.5rem; }
        .instruction {
            background: var(--pp-instr-bg);
            border-radius: 8px;
            padding: 1rem 1.25rem;
            margin: 1rem 0;
            text-align: left;
        }
        .instruction-label {
            font-size: 0.8rem;
            color: var(--pp-instr-label);
            margin-bottom: 0.5rem;
        }
        .instruction-text {
            color: var(--pp-instr-text);
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
            font-size: 0.9rem;
            line-height: 1.5;
        }
        .port { color: var(--pp-port); }
        .status {
            font-size: 0.8rem;
            color: var(--pp-status);
            margin-top: 1.5rem;
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
    <div class="container">
        <h1>App Preview</h1>
        <div class="instruction">
            <div class="instruction-label">Tell your agent:</div>
            <div class="instruction-text">Start a hot-reload web app on <span class="port">localhost:%s</span></div>
        </div>
        <div class="status">
            <span class="status-dot"></span>
            <span id="status-text">Listening for app...</span>
        </div>
    </div>
    <script>
        // Poll for app availability without page reload (no white flash)
        async function checkApp() {
            try {
                const response = await fetch(window.location.href, { method: 'HEAD' });
                // 502 = proxy error (this page), 200 = app is running
                if (response.ok) {
                    window.location.reload();
                }
            } catch (e) {
                // Network error, keep polling
            }
        }
        // Check every 3 seconds
        setInterval(checkApp, 3000);
    </script>
</body>
</html>`

// shellPageHTML is the double-iframe shell page that wraps user content.
// It manages navigation (back/forward/reload) via WebSocket commands from the parent UI.
// The inner iframe loads the actual user app content. The shell page connects to the
// debug WebSocket as an iframe client and relays navigation state to the parent.
const shellPageHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Shell</title>
<style>*{margin:0;padding:0}html,body{width:100%%;height:100%%;overflow:hidden}
#inner{width:100%%;height:100%%;border:none}</style>
</head>
<body>
<iframe id="inner" src=""></iframe>
<script>
(function(){
  'use strict';
  var inner = document.getElementById('inner');
  var params = new URLSearchParams(location.search);
  var initialPath = params.get('path') || '/';
  inner.src = initialPath;

  // Shell-level navigation tracking for full-page (non-SPA) navigations
  var _shellNavIdx = 0;
  var _shellNavMax = 0;
  var _shellInitialLoad = true;
  var _shellPendingBack = false;
  var _shellPendingForward = false;

  // Connect to debug WS as iframe client
  var wsUrl = (location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + location.host + '/__swe-swe-debug__/ws';
  var ws = null;
  var reconnectAttempts = 0;

  function send(msg) {
    if (ws && ws.readyState === 1) ws.send(JSON.stringify(msg));
  }

  function connect() {
    if (reconnectAttempts >= 5) return;
    try {
      ws = new WebSocket(wsUrl);
      ws.onopen = function() { reconnectAttempts = 0; };
      ws.onclose = function() {
        reconnectAttempts++;
        setTimeout(connect, Math.min(1000 * reconnectAttempts, 5000));
      };
      ws.onmessage = function(e) {
        try {
          var cmd = JSON.parse(e.data);
          if (cmd.t === 'navigate') {
            if (cmd.action === 'back') {
              if (_shellNavIdx > 0) {
                _shellNavIdx--;
                _shellPendingBack = true;
              }
              inner.contentWindow.history.back();
            } else if (cmd.action === 'forward') {
              if (_shellNavIdx < _shellNavMax) {
                _shellNavIdx++;
                _shellPendingForward = true;
              }
              inner.contentWindow.history.forward();
            } else if (cmd.url) {
              inner.src = cmd.url;
            }
          } else if (cmd.t === 'reload') {
            inner.contentWindow.location.reload();
          }
        } catch(err) {}
      };
    } catch(e) {}
  }

  // On inner iframe load, send urlchange + shell-level navstate
  inner.onload = function() {
    try {
      var url = inner.contentWindow.location.href;
      send({ t: 'urlchange', url: url, ts: Date.now() });
    } catch(e) {
      // Cross-origin: can't read inner URL
      send({ t: 'urlchange', url: inner.src, ts: Date.now() });
    }
    // Track full-page navigations at shell level.
    // _shellPendingBack/Forward are set when we trigger back/forward via WS command.
    // On initial load, do nothing. On subsequent loads, increment unless it was a back/forward.
    if (_shellInitialLoad) {
      _shellInitialLoad = false;
    } else if (_shellPendingBack) {
      _shellPendingBack = false;
    } else if (_shellPendingForward) {
      _shellPendingForward = false;
    } else {
      // New forward navigation (link click, form submit, navigate command)
      _shellNavIdx++;
      _shellNavMax = _shellNavIdx;
    }
    send({ t: 'navstate', canGoBack: _shellNavIdx > 0, canGoForward: _shellNavIdx < _shellNavMax });
  };

  connect();
})();
</script>
</body>
</html>`

// debugScriptTag is injected into HTML responses to enable debug channel
const debugScriptTag = `<script src="/__swe-swe-debug__/inject.js"></script>`

// debugInjectScriptRe matches <head> or <body> tag (case insensitive)
var debugInjectScriptRe = regexp.MustCompile(`(?i)(<head[^>]*>|<body[^>]*>)`)

// injectDebugScript injects the debug script tag after the FIRST <head> or <body> tag only
func injectDebugScript(body []byte) []byte {
	loc := debugInjectScriptRe.FindIndex(body)
	if loc == nil {
		return body // No match found
	}
	// Insert script tag after the first match
	result := make([]byte, 0, len(body)+len(debugScriptTag))
	result = append(result, body[:loc[1]]...)
	result = append(result, debugScriptTag...)
	result = append(result, body[loc[1]:]...)
	return result
}

// modifyCSPHeader modifies Content-Security-Policy header to allow debug script and WebSocket
func modifyCSPHeader(h http.Header) {
	csp := h.Get("Content-Security-Policy")
	if csp == "" {
		return
	}

	// Add 'self' to script-src for our injected script
	if strings.Contains(csp, "script-src") {
		csp = strings.Replace(csp, "script-src", "script-src 'self'", 1)
	} else {
		// No script-src directive, add one
		csp = csp + "; script-src 'self'"
	}

	// Add ws:, wss:, and 'self' to connect-src for WebSocket and fetch API
	if strings.Contains(csp, "connect-src") {
		csp = strings.Replace(csp, "connect-src", "connect-src 'self' ws: wss:", 1)
	} else {
		// No connect-src directive, add one
		csp = csp + "; connect-src 'self' ws: wss:"
	}

	h.Set("Content-Security-Policy", csp)
}

// DebugHub manages WebSocket connections between iframe debug scripts and agent
type DebugHub struct {
	iframeClients map[*websocket.Conn]bool // Connected iframe debug scripts
	agentConn     *websocket.Conn          // Connected agent (only one allowed)
	uiObservers   map[*websocket.Conn]bool // UI observers (receive iframe messages, read-only)
	mu            sync.RWMutex
}

// AddIframeClient registers an iframe debug script connection
func (h *DebugHub) AddIframeClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.iframeClients[conn] = true
	log.Printf("[DebugHub] Iframe client connected (total: %d)", len(h.iframeClients))
}

// RemoveIframeClient unregisters an iframe debug script connection
func (h *DebugHub) RemoveIframeClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.iframeClients, conn)
	log.Printf("[DebugHub] Iframe client disconnected (total: %d)", len(h.iframeClients))
}

// SetAgent registers the agent connection (replaces existing if any)
func (h *DebugHub) SetAgent(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.agentConn != nil {
		h.agentConn.Close() // Close previous agent connection
	}
	h.agentConn = conn
	log.Printf("[DebugHub] Agent connected")
}

// RemoveAgent unregisters the agent connection
func (h *DebugHub) RemoveAgent(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.agentConn == conn {
		h.agentConn = nil
		log.Printf("[DebugHub] Agent disconnected")
	}
}

// AddUIObserver registers a UI observer connection
func (h *DebugHub) AddUIObserver(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.uiObservers[conn] = true
	log.Printf("[DebugHub] UI observer connected (total: %d)", len(h.uiObservers))
}

// RemoveUIObserver unregisters a UI observer connection
func (h *DebugHub) RemoveUIObserver(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.uiObservers, conn)
	log.Printf("[DebugHub] UI observer disconnected (total: %d)", len(h.uiObservers))
}

// ForwardToAgent sends a message from iframe to the connected agent and UI observers
func (h *DebugHub) ForwardToAgent(msg []byte) {
	h.mu.RLock()
	agent := h.agentConn
	observers := make([]*websocket.Conn, 0, len(h.uiObservers))
	for conn := range h.uiObservers {
		observers = append(observers, conn)
	}
	h.mu.RUnlock()

	if agent != nil {
		if err := agent.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[DebugHub] Error forwarding to agent: %v", err)
		}
	}

	for _, conn := range observers {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[DebugHub] Error forwarding to UI observer: %v", err)
		}
	}
}

// SendToUIObservers sends a message to all connected UI observers only
func (h *DebugHub) SendToUIObservers(msg []byte) {
	h.mu.RLock()
	observers := make([]*websocket.Conn, 0, len(h.uiObservers))
	for conn := range h.uiObservers {
		observers = append(observers, conn)
	}
	h.mu.RUnlock()

	for _, conn := range observers {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[DebugHub] Error sending to UI observer: %v", err)
		}
	}
}

// ForwardToIframes sends a message from agent to all connected iframes
func (h *DebugHub) ForwardToIframes(msg []byte) {
	h.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(h.iframeClients))
	for conn := range h.iframeClients {
		clients = append(clients, conn)
	}
	h.mu.RUnlock()

	for _, conn := range clients {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[DebugHub] Error forwarding to iframe: %v", err)
		}
	}
}

// handleDebugIframeWS handles WebSocket connections from iframe debug scripts
func handleDebugIframeWS(debugHub *DebugHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[DebugHub] Iframe WS upgrade error: %v", err)
			return
		}
		defer conn.Close()

		debugHub.AddIframeClient(conn)
		defer debugHub.RemoveIframeClient(conn)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
					break
				}
				log.Printf("[DebugHub] Iframe read error: %v", err)
				break
			}
			// Forward message from iframe to agent
			debugHub.ForwardToAgent(msg)
		}
	}
}

// handleDebugUIObserverWS handles WebSocket connections from the terminal UI
// UI observers receive all iframe-originated messages but don't send to iframes
func handleDebugUIObserverWS(debugHub *DebugHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[DebugHub] UI observer WS upgrade error: %v", err)
			return
		}
		defer conn.Close()

		debugHub.AddUIObserver(conn)
		defer debugHub.RemoveUIObserver(conn)

		// Read loop: forward messages from UI to iframes (e.g., navigate commands)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			debugHub.ForwardToIframes(msg)
		}
	}
}

// handleDebugAgentWS handles WebSocket connection from the agent
func handleDebugAgentWS(debugHub *DebugHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[DebugHub] Agent WS upgrade error: %v", err)
			return
		}
		defer conn.Close()

		debugHub.SetAgent(conn)
		defer debugHub.RemoveAgent(conn)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
					break
				}
				log.Printf("[DebugHub] Agent read error: %v", err)
				break
			}
			// Forward message from agent to all iframes (e.g., DOM queries)
			debugHub.ForwardToIframes(msg)
		}
	}
}

// debugInjectJS is the debug script served at /__swe-swe-debug__/inject.js
// It captures console logs, errors, fetch/XHR requests and forwards them via WebSocket
const debugInjectJS = `(function() {
  'use strict';

  // Prevent double initialization
  if (window.__sweSweDebugInit) return;
  window.__sweSweDebugInit = true;

  var ws = null;
  var wsUrl = (location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + location.host + '/__swe-swe-debug__/ws';
  var messageQueue = [];
  var reconnectAttempts = 0;
  var maxReconnectAttempts = 5;

  // Serialize values safely (handle circular refs, DOM nodes, etc.)
  function serialize(val, depth) {
    if (depth === undefined) depth = 0;
    if (depth > 3) return '[max depth]';
    if (val === null) return null;
    if (val === undefined) return '[undefined]';
    if (typeof val === 'function') return '[function]';
    if (typeof val === 'symbol') return val.toString();
    if (val instanceof Error) return { name: val.name, message: val.message, stack: val.stack };
    if (val instanceof HTMLElement) return '<' + val.tagName.toLowerCase() + (val.id ? '#' + val.id : '') + '>';
    if (val instanceof Event) return { type: val.type, target: serialize(val.target, depth + 1) };
    if (Array.isArray(val)) return val.slice(0, 10).map(function(v) { return serialize(v, depth + 1); });
    if (typeof val === 'object') {
      try {
        var obj = {};
        var keys = Object.keys(val).slice(0, 20);
        for (var i = 0; i < keys.length; i++) {
          obj[keys[i]] = serialize(val[keys[i]], depth + 1);
        }
        return obj;
      } catch (e) {
        return '[object]';
      }
    }
    return val;
  }

  function send(msg) {
    var data = JSON.stringify(msg);
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(data);
    } else {
      messageQueue.push(data);
      if (messageQueue.length > 100) messageQueue.shift();
    }
  }

  function connect() {
    if (reconnectAttempts >= maxReconnectAttempts) return;

    try {
      ws = new WebSocket(wsUrl);

      ws.onopen = function() {
        reconnectAttempts = 0;
        // Flush queued messages
        while (messageQueue.length > 0) {
          ws.send(messageQueue.shift());
        }
      };

      ws.onclose = function() {
        reconnectAttempts++;
        setTimeout(connect, Math.min(1000 * reconnectAttempts, 5000));
      };

      ws.onerror = function() {
        // Error handling done in onclose
      };

      ws.onmessage = function(e) {
        try {
          var cmd = JSON.parse(e.data);
          if (cmd.t === 'query') {
            var el = document.querySelector(cmd.selector);
            send({
              t: 'queryResult',
              id: cmd.id,
              found: !!el,
              text: el ? el.textContent : null,
              html: el ? el.innerHTML.substring(0, 1000) : null,
              visible: el ? (el.offsetParent !== null || el.offsetWidth > 0 || el.offsetHeight > 0) : false,
              rect: el ? el.getBoundingClientRect() : null
            });
          }
        } catch (err) {
          // Ignore parse errors
        }
      };
    } catch (e) {
      // WebSocket not supported or blocked
    }
  }

  // Wrap console methods
  ['log', 'warn', 'error', 'info', 'debug'].forEach(function(method) {
    var original = console[method];
    console[method] = function() {
      var args = Array.prototype.slice.call(arguments);
      send({ t: 'console', m: method, args: args.map(function(a) { return serialize(a); }), ts: Date.now() });
      return original.apply(console, arguments);
    };
  });

  // Capture uncaught errors
  window.addEventListener('error', function(e) {
    send({
      t: 'error',
      msg: e.message,
      file: e.filename,
      line: e.lineno,
      col: e.colno,
      stack: e.error ? e.error.stack : null,
      ts: Date.now()
    });
  });

  // Capture unhandled promise rejections
  window.addEventListener('unhandledrejection', function(e) {
    send({
      t: 'rejection',
      reason: serialize(e.reason),
      ts: Date.now()
    });
  });

  // Wrap fetch
  var originalFetch = window.fetch;
  if (originalFetch) {
    window.fetch = function(input, init) {
      var url = typeof input === 'string' ? input : (input.url || String(input));
      var method = (init && init.method) || 'GET';
      var start = Date.now();

      return originalFetch.apply(this, arguments).then(function(response) {
        send({
          t: 'fetch',
          url: url,
          method: method,
          status: response.status,
          ok: response.ok,
          ms: Date.now() - start,
          ts: Date.now()
        });
        return response;
      }).catch(function(err) {
        send({
          t: 'fetch',
          url: url,
          method: method,
          error: err.message,
          ms: Date.now() - start,
          ts: Date.now()
        });
        throw err;
      });
    };
  }

  // Wrap XMLHttpRequest
  var XHROpen = XMLHttpRequest.prototype.open;
  var XHRSend = XMLHttpRequest.prototype.send;

  XMLHttpRequest.prototype.open = function(method, url) {
    this.__sweMethod = method;
    this.__sweUrl = url;
    this.__sweStart = null;
    return XHROpen.apply(this, arguments);
  };

  XMLHttpRequest.prototype.send = function() {
    var xhr = this;
    xhr.__sweStart = Date.now();

    xhr.addEventListener('loadend', function() {
      send({
        t: 'xhr',
        url: xhr.__sweUrl,
        method: xhr.__sweMethod,
        status: xhr.status,
        ok: xhr.status >= 200 && xhr.status < 300,
        ms: Date.now() - xhr.__sweStart,
        ts: Date.now()
      });
    });

    return XHRSend.apply(this, arguments);
  };

  // Navigation index tracking for back/forward button state
  var _navIdx = 0;
  var _navMax = 0;

  function stampState(idx) {
    try {
      var state = history.state;
      var merged = (state && typeof state === 'object') ? Object.assign({}, state) : {};
      merged.__sweSweNavIdx = idx;
      origReplace.call(history, merged, '', location.href);
    } catch(e) {}
  }

  function sendNavState() {
    send({ t: 'navstate', canGoBack: _navIdx > 0, canGoForward: _navIdx < _navMax });
  }

  // URL change detection for SPA navigations
  var lastUrl = location.href;
  function checkUrl() {
    if (location.href !== lastUrl) {
      lastUrl = location.href;
      send({ t: 'urlchange', url: location.href, ts: Date.now() });
    }
  }

  var origPush = history.pushState;
  var origReplace = history.replaceState;

  history.pushState = function() {
    _navIdx++;
    _navMax = _navIdx;
    origPush.apply(this, arguments);
    stampState(_navIdx);
    checkUrl();
    sendNavState();
  };

  history.replaceState = function() {
    origReplace.apply(this, arguments);
    stampState(_navIdx);
    checkUrl();
  };

  window.addEventListener('popstate', function(e) {
    var state = e.state;
    if (state && typeof state.__sweSweNavIdx === 'number') {
      _navIdx = state.__sweSweNavIdx;
    }
    checkUrl();
    sendNavState();
  });

  window.addEventListener('hashchange', function() {
    checkUrl();
    sendNavState();
  });

  // Initialize: read existing navIdx from history.state or assume end-of-stack
  (function() {
    var state = history.state;
    if (state && typeof state.__sweSweNavIdx === 'number') {
      _navIdx = state.__sweSweNavIdx;
      _navMax = _navIdx;
    } else {
      // First visit: stamp index 0
      _navIdx = 0;
      _navMax = 0;
      stampState(0);
    }
  })();

  // Connect to debug channel
  connect();

  // Send initial page load message (navstate sent by shell page on onload)
  send({ t: 'init', url: location.href, ts: Date.now() });
})();
`

type previewProxyState struct {
	targetMu       sync.RWMutex
	targetURL      *url.URL // nil = use default localhost:targetPort
	defaultTarget  *url.URL // The default localhost:targetPort
	defaultPortStr string   // String version of default port for error pages
}

type previewProxyServer struct {
	server   *http.Server
	listener net.Listener
	state    *previewProxyState
	debugHub *DebugHub
	refCount int
}

var (
	previewServersMu     sync.Mutex
	previewServers       = make(map[int]*previewProxyServer)
	previewProxyDisabled bool
)

// agentChatProxyServer is a simple reverse proxy for the agent chat MCP server.
// It reuses the same previewProxyServer struct for ref-counting and lifecycle.
var (
	agentChatServersMu sync.Mutex
	agentChatServers   = make(map[int]*previewProxyServer)
)

// startPreviewProxy starts the app preview reverse proxy server on the provided listener.
// It proxies requests to localhost:targetPort (or a dynamically set target URL)
// It also injects a debug script into HTML responses for console/error/network forwarding
func startPreviewProxy(listener net.Listener, targetPort int) (*previewProxyServer, error) {
	targetPortStr := strconv.Itoa(targetPort)
	defaultTarget, err := url.Parse("http://localhost:" + targetPortStr)
	if err != nil {
		return nil, fmt.Errorf("preview proxy: invalid target URL: %w", err)
	}

	state := &previewProxyState{
		defaultTarget:  defaultTarget,
		defaultPortStr: targetPortStr,
	}
	debugHub := &DebugHub{
		iframeClients: make(map[*websocket.Conn]bool),
		uiObservers:   make(map[*websocket.Conn]bool),
	}

	mux := http.NewServeMux()

	// Serve debug script
	mux.HandleFunc("/__swe-swe-debug__/inject.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(debugInjectJS))
	})

	// WebSocket endpoint for iframe debug scripts
	mux.HandleFunc("/__swe-swe-debug__/ws", handleDebugIframeWS(debugHub))

	// WebSocket endpoint for agent
	mux.HandleFunc("/__swe-swe-debug__/agent", handleDebugAgentWS(debugHub))

	// WebSocket endpoint for UI observers (terminal UI URL bar updates)
	mux.HandleFunc("/__swe-swe-debug__/ui", handleDebugUIObserverWS(debugHub))

	// HTTP endpoint: open a URL in the Preview pane (used by xdg-open shim)
	mux.HandleFunc("/__swe-swe-debug__/open", func(w http.ResponseWriter, r *http.Request) {
		rawURL := r.URL.Query().Get("url")
		if rawURL == "" {
			http.Error(w, "missing url parameter", http.StatusBadRequest)
			return
		}
		msg, _ := json.Marshal(map[string]string{"t": "open", "url": rawURL})
		debugHub.SendToUIObservers(msg)
		log.Printf("[DebugHub] open → %s", rawURL)
		w.WriteHeader(http.StatusOK)
	})

	// Shell page for double-iframe navigation
	mux.HandleFunc("/__swe-swe-shell__", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, shellPageHTML)
	})

	// Proxy all other requests
	mux.HandleFunc("/", handleProxyRequest(state))

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		log.Printf("Starting preview proxy on %s -> localhost:%d", listener.Addr().String(), targetPort)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Preview proxy error: %v", err)
		}
	}()

	return &previewProxyServer{
		server:   server,
		listener: listener,
		state:    state,
		debugHub: debugHub,
		refCount: 0,
	}, nil
}

func acquirePreviewProxyServer(previewPort int, listener net.Listener) error {
	previewServersMu.Lock()
	defer previewServersMu.Unlock()

	if ref, ok := previewServers[previewPort]; ok {
		ref.refCount++
		if listener != nil {
			listener.Close()
		}
		return nil
	}

	if listener == nil {
		var err error
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", previewProxyPort(previewPort)))
		if err != nil {
			return err
		}
	}

	server, err := startPreviewProxy(listener, previewPort)
	if err != nil {
		listener.Close()
		return err
	}
	server.refCount = 1
	previewServers[previewPort] = server
	return nil
}

func releasePreviewProxyServer(previewPort int) {
	previewServersMu.Lock()
	ref, ok := previewServers[previewPort]
	if !ok {
		previewServersMu.Unlock()
		return
	}
	ref.refCount--
	if ref.refCount > 0 {
		previewServersMu.Unlock()
		return
	}
	delete(previewServers, previewPort)
	previewServersMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ref.server.Shutdown(ctx); err != nil {
		log.Printf("Preview proxy shutdown error: %v", err)
	}
	ref.listener.Close()
}

// startAgentChatProxy starts a simple reverse proxy for the agent chat MCP server.
// Unlike the preview proxy, it does NOT inject debug scripts.
func startAgentChatProxy(listener net.Listener, targetPort int) (*previewProxyServer, error) {
	targetPortStr := strconv.Itoa(targetPort)
	defaultTarget, err := url.Parse("http://localhost:" + targetPortStr)
	if err != nil {
		return nil, fmt.Errorf("agent chat proxy: invalid target URL: %w", err)
	}

	state := &previewProxyState{
		defaultTarget:  defaultTarget,
		defaultPortStr: targetPortStr,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleProxyRequest(state))

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		log.Printf("Starting agent chat proxy on %s -> localhost:%d", listener.Addr().String(), targetPort)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Agent chat proxy error: %v", err)
		}
	}()

	return &previewProxyServer{
		server:   server,
		listener: listener,
		state:    state,
		refCount: 0,
	}, nil
}

func acquireAgentChatProxyServer(acPort int, listener net.Listener) error {
	agentChatServersMu.Lock()
	defer agentChatServersMu.Unlock()

	if ref, ok := agentChatServers[acPort]; ok {
		ref.refCount++
		if listener != nil {
			listener.Close()
		}
		return nil
	}

	if listener == nil {
		var err error
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", agentChatProxyPort(acPort)))
		if err != nil {
			return err
		}
	}

	server, err := startAgentChatProxy(listener, acPort)
	if err != nil {
		listener.Close()
		return err
	}
	server.refCount = 1
	agentChatServers[acPort] = server
	return nil
}

func releaseAgentChatProxyServer(acPort int) {
	agentChatServersMu.Lock()
	ref, ok := agentChatServers[acPort]
	if !ok {
		agentChatServersMu.Unlock()
		return
	}
	ref.refCount--
	if ref.refCount > 0 {
		agentChatServersMu.Unlock()
		return
	}
	delete(agentChatServers, acPort)
	agentChatServersMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ref.server.Shutdown(ctx); err != nil {
		log.Printf("Agent chat proxy shutdown error: %v", err)
	}
	ref.listener.Close()
}


// handleProxyRequest proxies requests to the current target
func handleProxyRequest(state *previewProxyState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state.targetMu.RLock()
		target := state.targetURL
		if target == nil {
			target = state.defaultTarget
		}
		state.targetMu.RUnlock()

		// WebSocket upgrade detection: relay raw bytes instead of HTTP proxy
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			handleWebSocketRelay(w, r, target)
			return
		}

		// Build the target URL with the request path
		targetURL := *target
		targetURL.Path = singleJoiningSlash(target.Path, r.URL.Path)
		targetURL.RawQuery = r.URL.RawQuery

		// Create HTTP client with TLS config that allows self-signed certs
		client := &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			// Don't follow redirects automatically - let the browser handle them
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		// Create outgoing request
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
		if err != nil {
			log.Printf("Preview proxy: failed to create request: %v", err)
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			return
		}

		// Copy headers from incoming request
		for key, values := range r.Header {
			// Skip hop-by-hop headers
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
		resp, err := client.Do(outReq)
		if err != nil {
			log.Printf("Preview proxy error: %v", err)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, previewProxyErrorPage, state.defaultPortStr)
			return
		}
		defer resp.Body.Close()

		// Process response (inject debug script for HTML, handle cookies)
		processProxyResponse(w, resp, target)
	}
}

// processProxyResponse handles the response: injects debug script for HTML, strips Domain from cookies
func processProxyResponse(w http.ResponseWriter, resp *http.Response, target *url.URL) {
	// Copy response headers, handling cookies specially
	for key, values := range resp.Header {
		if isHopByHopHeader(key) {
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

	// Check if HTML for debug script injection
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		// Non-HTML: pass through as-is
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// HTML response: read, decompress if needed, inject script
	var body []byte
	var readErr error

	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			log.Printf("Preview proxy: gzip decode error: %v", err)
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		body, readErr = io.ReadAll(gr)
		gr.Close()
	case "br":
		// Brotli requires external library, pass through unchanged
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	default:
		body, readErr = io.ReadAll(resp.Body)
	}

	if readErr != nil {
		log.Printf("Preview proxy: read body error: %v", readErr)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Inject the debug script
	injected := injectDebugScript(body)

	// Modify CSP header if present
	modifyCSPHeader(w.Header())

	// Update content length and remove encoding (we decompressed)
	w.Header().Set("Content-Length", strconv.Itoa(len(injected)))
	w.Header().Del("Content-Encoding")

	w.WriteHeader(resp.StatusCode)
	w.Write(injected)
}

// handleWebSocketRelay hijacks the client connection and relays raw bytes
// to/from the backend WebSocket server. This avoids the normal HTTP proxy
// path which strips hop-by-hop headers (Upgrade, Connection) that are
// required for the WebSocket handshake.
func handleWebSocketRelay(w http.ResponseWriter, r *http.Request, target *url.URL) {
	// Determine backend address
	backendHost := target.Hostname()
	backendPort := target.Port()
	if backendPort == "" {
		if target.Scheme == "https" {
			backendPort = "443"
		} else {
			backendPort = "80"
		}
	}
	backendAddr := net.JoinHostPort(backendHost, backendPort)

	// Dial the backend
	backendConn, err := net.DialTimeout("tcp", backendAddr, 10*time.Second)
	if err != nil {
		log.Printf("Preview proxy: WebSocket backend dial error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("Preview proxy: ResponseWriter does not support hijacking")
		backendConn.Close()
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		log.Printf("Preview proxy: hijack error: %v", err)
		backendConn.Close()
		return
	}

	// Reconstruct the HTTP upgrade request to send to backend
	reqPath := r.URL.RequestURI()
	var reqBuf bytes.Buffer
	fmt.Fprintf(&reqBuf, "%s %s HTTP/1.1\r\n", r.Method, reqPath)
	fmt.Fprintf(&reqBuf, "Host: %s\r\n", backendAddr)
	for key, values := range r.Header {
		for _, value := range values {
			fmt.Fprintf(&reqBuf, "%s: %s\r\n", key, value)
		}
	}
	reqBuf.WriteString("\r\n")

	// Send the upgrade request to backend
	if _, err := backendConn.Write(reqBuf.Bytes()); err != nil {
		log.Printf("Preview proxy: WebSocket backend write error: %v", err)
		clientConn.Close()
		backendConn.Close()
		return
	}

	// Read the backend's response and forward it to the client
	// We use a small buffer to read the 101 response header, then relay everything
	go func() {
		defer clientConn.Close()
		defer backendConn.Close()
		io.Copy(clientConn, backendConn)
	}()

	// Forward any buffered data from the client, then relay
	if clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		clientBuf.Read(buffered)
		backendConn.Write(buffered)
	}
	io.Copy(backendConn, clientConn)
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

// --- MCP stdio server ---

// mcpRequest represents a JSON-RPC 2.0 request
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // may be number, string, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpResponse represents a JSON-RPC 2.0 response
type mcpResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *mcpError   `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type mcpToolResult struct {
	Content []mcpToolContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var mcpTools = []mcpToolDef{
	{
		Name: "browser_debug_preview",
		Description: "Capture a snapshot of the Preview content by CSS selector. " +
			"Returns the text, HTML, and visibility of matching elements in the Preview. " +
			"This is the correct tool for inspecting the Preview — browser_snapshot cannot see Preview content.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector": map[string]interface{}{
					"type":        "string",
					"description": "CSS selector (e.g. 'h1', '.error-message', '#app')",
				},
			},
			"required": []string{"selector"},
		},
	},
	{
		Name: "browser_debug_preview_listen",
		Description: "Returns console logs, errors, and network requests from the Preview. " +
			"Listens for the specified duration and returns all messages. " +
			"This is the correct tool for debugging the Preview — browser_console_messages cannot see Preview output.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"duration_seconds": map[string]interface{}{
					"type":        "number",
					"description": "How long to listen (default: 5, max: 30)",
				},
			},
		},
	},
}

// runMCP runs an MCP stdio server, reading JSON-RPC from in and writing to out.
func runMCP(in io.Reader, out io.Writer, endpoint string) {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	writeResponse := func(resp mcpResponse) {
		data, err := json.Marshal(resp)
		if err != nil {
			log.Printf("[mcp] marshal error: %v", err)
			return
		}
		fmt.Fprintf(out, "%s\n", data)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req mcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(mcpResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &mcpError{Code: -32700, Message: "Parse error"},
			})
			continue
		}

		// Parse request ID
		var reqID interface{}
		if req.ID != nil {
			json.Unmarshal(req.ID, &reqID)
		}

		// Notifications (no id) — just acknowledge silently
		if req.ID == nil {
			log.Printf("[mcp] notification: %s", req.Method)
			continue
		}

		switch req.Method {
		case "initialize":
			writeResponse(mcpResponse{
				JSONRPC: "2.0",
				ID:      reqID,
				Result: map[string]interface{}{
					"protocolVersion": "2025-11-25",
					"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
					"serverInfo": map[string]interface{}{
						"name":    "swe-swe-preview",
						"version": Version,
					},
				},
			})

		case "ping":
			writeResponse(mcpResponse{
				JSONRPC: "2.0",
				ID:      reqID,
				Result:  map[string]interface{}{},
			})

		case "tools/list":
			writeResponse(mcpResponse{
				JSONRPC: "2.0",
				ID:      reqID,
				Result:  map[string]interface{}{"tools": mcpTools},
			})

		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeResponse(mcpResponse{
					JSONRPC: "2.0",
					ID:      reqID,
					Error:   &mcpError{Code: -32602, Message: "Invalid params"},
				})
				continue
			}

			switch params.Name {
			case "browser_debug_preview":
				var args struct {
					Selector string `json:"selector"`
				}
				if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Selector == "" {
					writeResponse(mcpResponse{
						JSONRPC: "2.0",
						ID:      reqID,
						Error:   &mcpError{Code: -32602, Message: "Invalid params: selector is required"},
					})
					continue
				}
				result := mcpPreviewQuery(endpoint, args.Selector)
				writeResponse(mcpResponse{
					JSONRPC: "2.0",
					ID:      reqID,
					Result:  result,
				})

			case "browser_debug_preview_listen":
				var args struct {
					DurationSeconds float64 `json:"duration_seconds"`
				}
				json.Unmarshal(params.Arguments, &args)
				if args.DurationSeconds <= 0 {
					args.DurationSeconds = 5
				}
				if args.DurationSeconds > 30 {
					args.DurationSeconds = 30
				}
				result := mcpPreviewListen(endpoint, time.Duration(args.DurationSeconds*float64(time.Second)))
				writeResponse(mcpResponse{
					JSONRPC: "2.0",
					ID:      reqID,
					Result:  result,
				})

			default:
				writeResponse(mcpResponse{
					JSONRPC: "2.0",
					ID:      reqID,
					Error:   &mcpError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", params.Name)},
				})
			}

		default:
			writeResponse(mcpResponse{
				JSONRPC: "2.0",
				ID:      reqID,
				Error:   &mcpError{Code: -32601, Message: "Method not found"},
			})
		}
	}
}

// mcpPreviewQuery connects to the debug WebSocket, sends a DOM query, and returns the result.
func mcpPreviewQuery(endpoint string, selector string) mcpToolResult {
	if endpoint == "" {
		endpoint = defaultDebugEndpoint()
	}

	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		return mcpToolResult{
			Content: []mcpToolContent{{Type: "text", Text: fmt.Sprintf("Connection error: %v", err)}},
			IsError: true,
		}
	}
	defer conn.Close()

	queryID := fmt.Sprintf("q%d", time.Now().UnixNano())
	query := fmt.Sprintf(`{"t":"query","id":"%s","selector":"%s"}`, queryID, selector)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(query)); err != nil {
		return mcpToolResult{
			Content: []mcpToolContent{{Type: "text", Text: fmt.Sprintf("Send error: %v", err)}},
			IsError: true,
		}
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		return mcpToolResult{
			Content: []mcpToolContent{{Type: "text", Text: fmt.Sprintf("Read error: %v", err)}},
			IsError: true,
		}
	}

	return mcpToolResult{
		Content: []mcpToolContent{{Type: "text", Text: string(message)}},
	}
}

// mcpPreviewListen connects to the debug WebSocket and collects messages for the specified duration.
func mcpPreviewListen(endpoint string, duration time.Duration) mcpToolResult {
	if endpoint == "" {
		endpoint = defaultDebugEndpoint()
	}

	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		return mcpToolResult{
			Content: []mcpToolContent{{Type: "text", Text: fmt.Sprintf("Connection error: %v", err)}},
			IsError: true,
		}
	}
	defer conn.Close()

	var messages []string
	deadline := time.After(duration)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			messages = append(messages, string(message))
		}
	}()

	select {
	case <-deadline:
		conn.Close() // will cause ReadMessage to error and goroutine to exit
		<-done
	case <-done:
	}

	if len(messages) == 0 {
		return mcpToolResult{
			Content: []mcpToolContent{{Type: "text", Text: "No messages received during listen period"}},
		}
	}

	result := strings.Join(messages, "\n")
	return mcpToolResult{
		Content: []mcpToolContent{{Type: "text", Text: result}},
	}
}

// runDebugListen connects to the debug channel and prints messages to stdout
// This allows agents to receive debug messages from the user's app
func runDebugListen(endpoint string) {
	// Default to the preview proxy debug endpoint
	if endpoint == "" {
		endpoint = defaultDebugEndpoint()
	}

	fmt.Fprintf(os.Stderr, "[debug-listen] Connecting to %s\n", endpoint)

	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug-listen] Connection error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Fprintf(os.Stderr, "[debug-listen] Connected. Messages will be printed to stdout as JSON lines.\n")
	fmt.Fprintf(os.Stderr, "[debug-listen] Press Ctrl+C to disconnect.\n")

	// Handle interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					fmt.Fprintf(os.Stderr, "[debug-listen] Connection closed\n")
				} else {
					fmt.Fprintf(os.Stderr, "[debug-listen] Read error: %v\n", err)
				}
				return
			}
			// Print each message as a JSON line to stdout
			fmt.Printf("%s\n", message)
		}
	}()

	// Wait for interrupt or connection close
	select {
	case <-done:
	case <-interrupt:
		fmt.Fprintf(os.Stderr, "\n[debug-listen] Disconnecting...\n")
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}

// runDebugQuery sends a DOM query to the debug channel and prints the response
func runDebugQuery(endpoint string, selector string) {
	// Default to the preview proxy debug endpoint
	if endpoint == "" {
		endpoint = defaultDebugEndpoint()
	}

	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug-query] Connection error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Generate a unique query ID
	queryID := fmt.Sprintf("q%d", time.Now().UnixNano())

	// Send query
	query := fmt.Sprintf(`{"t":"query","id":"%s","selector":"%s"}`, queryID, selector)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(query)); err != nil {
		fmt.Fprintf(os.Stderr, "[debug-query] Send error: %v\n", err)
		os.Exit(1)
	}

	// Wait for response with timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug-query] Read error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s\n", message)
}

func defaultDebugEndpoint() string {
	if previewPort := os.Getenv("SWE_PREVIEW_PORT"); previewPort != "" {
		return fmt.Sprintf("ws://localhost:5%s/__swe-swe-debug__/agent", previewPort)
	}
	// In session context, PORT is set to the session's preview port
	if port := os.Getenv("PORT"); port != "" {
		return fmt.Sprintf("ws://localhost:5%s/__swe-swe-debug__/agent", port)
	}
	return "ws://localhost:9899/__swe-swe-debug__/agent"
}

func main() {
	addr := flag.String("addr", ":9898", "Listen address")
	version := flag.Bool("version", false, "Show version and exit")
	debugListen := flag.Bool("debug-listen", false, "Listen for debug messages from preview proxy")
	debugQuery := flag.String("debug-query", "", "Send DOM query to preview proxy (CSS selector)")
	debugEndpoint := flag.String("debug-endpoint", "", "Debug WebSocket endpoint (default: ws://localhost:5${SWE_PREVIEW_PORT}/__swe-swe-debug__/agent if SWE_PREVIEW_PORT is set; otherwise ws://localhost:9899/__swe-swe-debug__/agent)")
	mcpMode := flag.Bool("mcp", false, "Run as MCP stdio server exposing preview debug tools")
	dumpTemplates := flag.String("dump-container-templates", "", "Dump all container templates to directory and exit")
	noPreviewProxy := flag.Bool("no-preview-proxy", false, "Disable the preview proxy server (useful for dev mode)")
	flag.StringVar(&shellCmd, "shell", "claude", "Command to execute")
	flag.StringVar(&shellRestartCmd, "shell-restart", "claude --continue", "Command to restart on process death")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	flag.Parse()
	previewProxyDisabled = *noPreviewProxy

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

	// Handle --mcp flag
	if *mcpMode {
		runMCP(os.Stdin, os.Stdout, *debugEndpoint)
		return
	}

	// Handle --debug-listen flag
	if *debugListen {
		runDebugListen(*debugEndpoint)
		return
	}

	// Handle --debug-query flag
	if *debugQuery != "" {
		runDebugQuery(*debugEndpoint, *debugQuery)
		return
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
	indexContent, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		log.Fatal(err)
	}
	indexTemplate, err = template.New("index").Parse(string(indexContent))
	if err != nil {
		log.Fatal(err)
	}

	selectionContent, err := staticFS.ReadFile("static/selection.html")
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
					Name:        sess.Name,
					Assistant:   sess.Assistant,
					ClientCount: sess.ClientCount(),
					CreatedAt:   sess.CreatedAt,
					DurationStr: formatDuration(time.Since(sess.CreatedAt)),
				}
				sessionsByAssistant[sess.Assistant] = append(sessionsByAssistant[sess.Assistant], info)
			}
			sessionsMu.RUnlock()

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
			}
			if err := selectionTemplate.Execute(w, data); err != nil {
				log.Printf("Selection template error: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
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

		// Recording playback page and raw session data
		if strings.HasPrefix(r.URL.Path, "/recording/") {
			path := strings.TrimPrefix(r.URL.Path, "/recording/")
			if path == "" {
				http.Redirect(w, r, "/", http.StatusFound)
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
			sessionsMu.RLock()
			existingSession, exists := sessions[sessionUUID]
			sessionsMu.RUnlock()
			if exists && existingSession.Assistant != assistant {
				correctURL := fmt.Sprintf("/session/%s?assistant=%s", sessionUUID, existingSession.Assistant)
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
				Version:       Version,
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
	if err := http.ListenAndServe(*addr, nil); err != nil {
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

	// Normalize unicode and remove diacritics (e.g., é → e)
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
// Used to provision swe-swe files (.mcp.json, .swe-swe/docs/*, swe-swe/setup)
// into cloned repos and new projects that weren't set up by swe-swe init.
// Idempotent: skips files that already exist.
func setupSweSweFiles(destDir string) error {
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

		// Skip if destination already exists
		if _, err := os.Stat(destPath); err == nil {
			return nil
		}

		content, err := containerTemplatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded %s: %w", path, err)
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
		// /repos/ doesn't exist or can't be read — return empty list
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

	// Write swe-swe files (.mcp.json, .swe-swe/docs/*, swe-swe/setup) into cloned repo
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

	// Write swe-swe files (.mcp.json, .swe-swe/docs/*, swe-swe/setup) into new project
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
	maxRecentRecordingsPerAgent = 5
	recentRecordingMaxAge       = 48 * time.Hour
)

// cleanupRecentRecordings deletes old/excess recent recordings (those without KeptAt set)
// For each agent, keeps only the most recent maxRecentRecordingsPerAgent recordings
// and deletes any unkept recordings older than recentRecordingMaxAge
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

	// Collect all recent (unkept) recordings grouped by agent
	type recentRecording struct {
		uuid  string
		mtime time.Time // log file mtime — reflects last activity, not metadata timestamps
	}
	recentByAgent := make(map[string][]recentRecording)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".metadata.json") {
			continue
		}

		// Extract UUID
		uuid := strings.TrimPrefix(name, "session-")
		uuid = strings.TrimSuffix(uuid, ".metadata.json")

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

		// Use log file mtime for age — it reflects the last write to the
		// recording, so a long session that just ended won't be immediately
		// eligible for cleanup (unlike EndedAt which may be unset on crash,
		// falling back to StartedAt).
		logPath := recordingsDir + "/session-" + uuid + ".log"
		logInfo, err := os.Stat(logPath)
		if err != nil {
			continue
		}
		mtime := logInfo.ModTime()

		agent := meta.Agent
		if agent == "" {
			agent = "unknown"
		}
		// Map display name to binary name
		agent = agentNameToBinary(agent)

		recentByAgent[agent] = append(recentByAgent[agent], recentRecording{
			uuid:  uuid,
			mtime: mtime,
		})
	}

	// For each agent, delete old/excess recordings
	now := time.Now()
	for agent, recordings := range recentByAgent {
		// Sort by mtime descending (newest first)
		sort.Slice(recordings, func(i, j int) bool {
			return recordings[i].mtime.After(recordings[j].mtime)
		})

		for i, rec := range recordings {
			shouldDelete := false

			// Delete if beyond the per-agent limit
			if i >= maxRecentRecordingsPerAgent {
				shouldDelete = true
			}

			// Delete if older than max age (based on log file mtime)
			if now.Sub(rec.mtime) > recentRecordingMaxAge {
				shouldDelete = true
			}

			if shouldDelete {
				deleteRecordingFiles(rec.uuid)
				log.Printf("Auto-deleted recent recording %s (agent=%s, age=%v, position=%d)",
					rec.uuid[:8], agent, now.Sub(rec.mtime).Round(time.Minute), i+1)
			}
		}
	}
}

// deleteRecordingFiles removes all files for a recording
func deleteRecordingFiles(uuid string) {
	patterns := []string{
		recordingsDir + "/session-" + uuid + ".log",
		recordingsDir + "/session-" + uuid + ".timing",
		recordingsDir + "/session-" + uuid + ".metadata.json",
	}
	for _, path := range patterns {
		os.Remove(path)
	}
}

func previewProxyPort(port int) int {
	return 50000 + port
}

func agentChatProxyPort(port int) int {
	return 40000 + port
}

func agentChatPortFromPreview(previewPort int) int {
	return previewPort + 1000
}

// findAvailablePortPair finds a preview port and its derived agent chat port where all 4 addresses
// (preview proxy, preview app, agent chat proxy, agent chat app) are available.
// Returns preview port, preview proxy listener, agent chat port, agent chat proxy listener.
func findAvailablePortPair() (int, net.Listener, int, net.Listener, error) {
	for port := previewPortStart; port <= previewPortEnd; port++ {
		previewListener, err := net.Listen("tcp", fmt.Sprintf(":%d", previewProxyPort(port)))
		if err != nil {
			continue
		}
		appListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			previewListener.Close()
			continue
		}
		appListener.Close()

		acPort := agentChatPortFromPreview(port)
		acProxyListener, err := net.Listen("tcp", fmt.Sprintf(":%d", agentChatProxyPort(acPort)))
		if err != nil {
			previewListener.Close()
			continue
		}
		acAppListener, err := net.Listen("tcp", fmt.Sprintf(":%d", acPort))
		if err != nil {
			previewListener.Close()
			acProxyListener.Close()
			continue
		}
		acAppListener.Close()

		return port, previewListener, acPort, acProxyListener, nil
	}
	return 0, nil, 0, nil, fmt.Errorf("no available port pair in preview range %d-%d", previewPortStart, previewPortEnd)
}

// getOrCreateSession returns an existing session or creates a new one
// The assistant parameter is the key from availableAssistants (e.g., "claude", "gemini", "custom")
// The name parameter sets the display name (optional, can be empty)
// The branch parameter is used for worktree creation (optional, separate from display name)
// The workDir parameter sets the working directory for the session (empty = use server cwd)
// The repoPath parameter sets the base repo for worktree creation (empty = /workspace)
func getOrCreateSession(sessionUUID string, assistant string, name string, branch string, workDir string, repoPath string, parentUUID string, parentName string, theme string) (*Session, bool, error) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if sess, ok := sessions[sessionUUID]; ok {
		// Check if the session's process has exited - clean up and create fresh session
		if sess.Cmd != nil && sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
			log.Printf("Cleaning up dead session on reconnect: %s (exit code=%d)", sessionUUID, sess.Cmd.ProcessState.ExitCode())
			sess.Close()
			delete(sessions, sessionUUID)
			// Fall through to create a new session
		} else {
			return sess, false, nil // existing session
		}
	}

	// Find the assistant config
	var cfg AssistantConfig
	var found bool
	for _, a := range availableAssistants {
		if a.Binary == assistant {
			cfg = a
			found = true
			break
		}
	}
	if !found {
		return nil, false, fmt.Errorf("unknown assistant: %s", assistant)
	}

	// Ensure recordings directory exists
	if err := ensureRecordingsDir(); err != nil {
		log.Printf("Warning: failed to create recordings directory: %v", err)
	}

	// Determine working directory and create worktree if needed
	if workDir == "" {
		// If repoPath provided, use it as base; otherwise default to /workspace
		baseRepo := repoPath
		if baseRepo == "" {
			baseRepo = "/workspace"
		}

		// If branch is provided, create/use worktree
		if branch != "" {
			// Use createWorktreeInRepo which supports both /workspace and external repos
			var err error
			workDir, err = createWorktreeInRepo(baseRepo, branch)
			if err != nil {
				log.Printf("Warning: failed to create worktree for branch %s in %s: %v", branch, baseRepo, err)
				// Fall back to base repo without worktree
				workDir = baseRepo
			}
		} else {
			// No branch specified, use base repo directly
			workDir = baseRepo
		}
	}

	var previewPort int
	var previewListener net.Listener
	var acPort int
	var acListener net.Listener
	if parentUUID != "" {
		if parentSess, ok := sessions[parentUUID]; ok {
			previewPort = parentSess.PreviewPort
			acPort = parentSess.AgentChatPort
		}
	}
	if previewPort == 0 {
		var err error
		previewPort, previewListener, acPort, acListener, err = findAvailablePortPair()
		if err != nil {
			return nil, false, err
		}
	}

	// Inherit name from parent session if this is a shell session with a parent
	if name == "" && parentName != "" && assistant == "shell" {
		name = parentName + " (Terminal)"
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

	// Generate recording UUID
	recordingUUID := uuid.New().String()

	// For shell assistant, resolve $SHELL at runtime
	shellCmdToUse := cfg.ShellCmd
	if assistant == "shell" {
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
	cmdName, cmdArgs = wrapWithScript(cmdName, cmdArgs, recordingUUID)
	log.Printf("Recording session to: %s/session-%s.{log,timing}", recordingsDir, recordingUUID)

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = buildSessionEnv(previewPort, acPort, theme, workDir)
	if workDir != "" {
		cmd.Dir = workDir
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, false, err
	}

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	now := time.Now()
	sess := &Session{
		UUID:            sessionUUID,
		Name:            name,
		BranchName:      branch,
		WorkDir:         workDir,
		Assistant:       assistant,
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
		ParentUUID:      parentUUID,
		PreviewPort:     previewPort,
		AgentChatPort:   acPort,
		Theme:           theme,
		yoloMode:        detectYoloMode(cfg.ShellCmd), // Detect initial YOLO mode from startup command
		Metadata: &RecordingMetadata{
			UUID:      recordingUUID,
			Name:      name,
			Agent:     cfg.Name,
			StartedAt: now,
			Command:   append([]string{cmdName}, cmdArgs...),
			MaxCols:   80, // Default starting size
			MaxRows:   24,
			WorkDir:   workDir,
		},
	}
	sessions[sessionUUID] = sess

	if !previewProxyDisabled {
		if err := acquirePreviewProxyServer(previewPort, previewListener); err != nil {
			if previewListener != nil {
				previewListener.Close()
			}
			return nil, false, err
		}
		if err := acquireAgentChatProxyServer(acPort, acListener); err != nil {
			if acListener != nil {
				acListener.Close()
			}
			return nil, false, err
		}
	} else {
		if previewListener != nil {
			previewListener.Close()
		}
		if acListener != nil {
			acListener.Close()
		}
	}

	// Save metadata immediately so recordings are properly tracked even if session ends unexpectedly
	if err := sess.saveMetadata(); err != nil {
		log.Printf("Failed to save initial metadata: %v", err)
	}

	log.Printf("Created new session: %s (assistant=%s, pid=%d, recording=%s)", sessionUUID, cfg.Name, cmd.Process.Pid, recordingUUID)
	return sess, true, nil // new session
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
	if parentUUID != "" {
		sessionsMu.RLock()
		if parentSess, ok := sessions[parentUUID]; ok {
			workDir = parentSess.WorkDir
			parentName = parentSess.Name
			log.Printf("Shell session inheriting workDir from parent %s: %s", parentUUID, workDir)
		}
		sessionsMu.RUnlock()
	}

	// Get theme hint from client for COLORFGBG env var
	theme := r.URL.Query().Get("theme")

	sess, isNew, err := getOrCreateSession(sessionUUID, assistant, sessionName, branchParam, workDir, pwd, parentUUID, parentName, theme)
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

				// Kill process - pendingReplacement will cause process to be replaced
				if cmd != nil && cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGTERM)
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

			// Create uploads directory if it doesn't exist
			uploadsDir := ".swe-swe/uploads"
			if err := os.MkdirAll(uploadsDir, 0755); err != nil {
				log.Printf("Failed to create uploads directory: %v", err)
				sendFileUploadResponse(conn, false, filename, "Failed to create uploads directory")
				continue
			}

			// Save the file to the uploads directory
			filePath := uploadsDir + "/" + filename
			if err := os.WriteFile(filePath, fileData, 0644); err != nil {
				log.Printf("File upload error: %v", err)
				sendFileUploadResponse(conn, false, filename, err.Error())
				continue
			}

			log.Printf("File uploaded: %s (%d bytes)", filePath, len(fileData))
			sendFileUploadResponse(conn, true, filename, "")

			// Send the file path to PTY - Claude Code will detect it and read from disk
			absPath, err := os.Getwd()
			if err != nil {
				absPath = "."
			}
			absFilePath := absPath + "/" + filePath
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
	recordingUUID := s.RecordingUUID
	s.mu.RUnlock()

	if metadata == nil {
		return nil // Nothing to save
	}

	// Calculate playback dimensions when session ends (only once)
	if metadata.EndedAt != nil && metadata.PlaybackCols == 0 {
		logPath := fmt.Sprintf("%s/session-%s.log", recordingsDir, recordingUUID)
		dims := calculateTerminalDimensions(logPath)
		s.mu.Lock()
		s.Metadata.PlaybackCols = dims.Cols
		s.Metadata.PlaybackRows = dims.Rows
		s.mu.Unlock()
	}

	path := fmt.Sprintf("%s/session-%s.metadata.json", recordingsDir, recordingUUID)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// wrapWithScript wraps a command with the Linux script command for recording
// Returns the new command name and arguments to record terminal output and timing
func wrapWithScript(cmdName string, cmdArgs []string, recordingUUID string) (string, []string) {
	// Build the full command string for script -c
	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd += " " + strings.Join(cmdArgs, " ")
	}

	logPath := fmt.Sprintf("%s/session-%s.log", recordingsDir, recordingUUID)
	timingPath := fmt.Sprintf("%s/session-%s.timing", recordingsDir, recordingUUID)
	inputPath := fmt.Sprintf("%s/session-%s.input", recordingsDir, recordingUUID)

	// script -q (quiet) -f (flush) -T timing -I input -O output -c "command"
	return "script", []string{
		"-q", "-f",
		"-T", timingPath,
		"-I", inputPath,
		"-O", logPath,
		"-c", fullCmd,
	}
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
	UUID      string     `json:"uuid"`
	Name      string     `json:"name,omitempty"`
	Agent     string     `json:"agent,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	KeptAt    *time.Time `json:"kept_at,omitempty"`
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

// loadEndedRecordings returns a list of ended recordings for the homepage
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

	// Find ended recordings (those with metadata and ended_at set, or inactive without ended_at)
	var recordings []RecordingInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		// Extract UUID
		uuid := strings.TrimPrefix(name, "session-")
		uuid = strings.TrimSuffix(uuid, ".log")

		// Skip active recordings
		if activeRecordings[uuid] {
			continue
		}

		uuidShort := uuid
		if len(uuid) >= 8 {
			uuidShort = uuid[:8]
		}

		info := RecordingInfo{
			UUID:      uuid,
			UUIDShort: uuidShort,
		}

		// Load metadata if exists
		metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"
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

		// Calculate ExpiresIn for non-kept recordings based on log file mtime
		if !info.IsKept {
			logPath := recordingsDir + "/session-" + uuid + ".log"
			var logMtime time.Time
			if logStat, err := os.Stat(logPath); err == nil {
				logMtime = logStat.ModTime()
			} else {
				logMtime = info.EndedAt
			}
			remaining := recentRecordingMaxAge - time.Since(logMtime)
			if remaining > 0 {
				hours := int(remaining.Hours())
				mins := int(remaining.Minutes()) % 60
				if hours > 0 {
					info.ExpiresIn = fmt.Sprintf("%dh%dm", hours, mins)
				} else if mins < 1 {
					info.ExpiresIn = "<1m"
				} else {
					info.ExpiresIn = fmt.Sprintf("%dm", mins)
				}
			} else {
				info.ExpiresIn = "soon"
			}
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

// calculateTerminalDimensions analyzes session.log content to calculate dimensions.
// This replicates the embedded mode's JavaScript calculation:
//   - Cols: min(max(maxLineLength, 80), 240)
//   - Rows: max(maxCursorRow, lineCount, 24)
func calculateTerminalDimensions(logPath string) TerminalDimensions {
	content, err := os.ReadFile(logPath)
	if err != nil {
		return TerminalDimensions{Cols: 240, Rows: 24} // defaults
	}

	// Strip metadata (same as embedded mode)
	stripped := recordtui.StripMetadata(string(content))

	// Parse cursor position sequences: ESC[row;colH
	// This finds the maximum row used by cursor positioning
	maxUsedRow := uint32(1)
	cursorPosRegex := regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
	matches := cursorPosRegex.FindAllStringSubmatch(stripped, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			var row int
			fmt.Sscanf(match[1], "%d", &row)
			if row > 0 && uint32(row) > maxUsedRow {
				maxUsedRow = uint32(row)
			}
		}
	}

	// Count newlines as fallback minimum height
	lineCount := uint32(strings.Count(stripped, "\n") + 1)

	// Calculate rows: max(maxUsedRow, lineCount, 24)
	rows := maxUsedRow
	if lineCount > rows {
		rows = lineCount
	}
	if rows < 24 {
		rows = 24
	}

	// Cap at reasonable maximum to avoid huge pages from logs with many newlines
	// 10000 rows is plenty for any reasonable terminal session
	// The streaming template uses large scrollback (1M) so no content is lost
	const maxRows = uint32(10000)
	if rows > maxRows {
		rows = maxRows
	}

	// Calculate cols from max line length
	normalized := strings.ReplaceAll(stripped, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	maxLineLength := 0
	for _, line := range lines {
		if len(line) > maxLineLength {
			maxLineLength = len(line)
		}
	}
	// Clamp to 80-240 range
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
func handleRecordingPage(w http.ResponseWriter, r *http.Request, recordingUUID string) {
	// Validate UUID format
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	// Check if recording exists
	logPath := recordingsDir + "/session-" + recordingUUID + ".log"
	if _, err := os.Stat(logPath); err != nil {
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

	// Check render mode query parameter
	renderMode := r.URL.Query().Get("render")

	if renderMode == "embedded" {
		// Embedded approach - read file, embed content in HTML
		content, err := os.ReadFile(logPath)
		if err != nil {
			http.Error(w, "Failed to read recording", http.StatusInternalServerError)
			return
		}
		// Strip metadata and neutralize clear sequences
		stripped := recordtui.StripMetadata(string(content))
		html, err := recordtui.RenderHTML([]recordtui.Frame{
			{Timestamp: 0, Content: stripped},
		}, recordtui.Options{
			Title: name,
			FooterLink: recordtui.FooterLink{
				Text: "swe-swe",
				URL:  "https://github.com/choonkeat/swe-swe",
			},
		})
		if err != nil {
			http.Error(w, "Failed to render playback", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(html))
		return
	}

	// Default: streaming approach
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

	// Build TOC from timing + input files if available
	timingPath := recordingsDir + "/session-" + recordingUUID + ".timing"
	inputPath := recordingsDir + "/session-" + recordingUUID + ".input"
	timingFile, err := os.Open(timingPath)
	if err == nil {
		defer timingFile.Close()
		inputBytes, err := os.ReadFile(inputPath)
		if err == nil {
			sessionBytes, _ := os.ReadFile(logPath)
			opts.TOC = recordtui.BuildTOC(timingFile, inputBytes, sessionBytes)
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
func handleRecordingSessionLog(w http.ResponseWriter, r *http.Request, recordingUUID string) {
	// Validate UUID format
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	logPath := recordingsDir + "/session-" + recordingUUID + ".log"
	http.ServeFile(w, r, logPath)
}

// handleSessionEndAPI handles POST /api/session/{uuid}/end
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

	// Find and close the session
	sessionsMu.Lock()
	session, exists := sessions[sessionUUID]
	if !exists {
		sessionsMu.Unlock()
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	delete(sessions, sessionUUID)
	sessionsMu.Unlock()

	// Send SIGINT first for graceful shutdown, then SIGKILL after timeout
	if session.Cmd != nil && session.Cmd.Process != nil {
		session.Cmd.Process.Signal(syscall.SIGINT)
		// Wait briefly for graceful shutdown
		done := make(chan struct{})
		go func() {
			session.Cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Process exited gracefully
		case <-time.After(2 * time.Second):
			// Force kill if still running
			session.Cmd.Process.Kill()
		}
	}

	// Close session resources (PTY, WebSocket clients, save metadata)
	session.Close()

	w.WriteHeader(http.StatusNoContent)
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

	// Find all recordings by looking for .log files
	recordings := make([]RecordingListItem, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		// Extract UUID from filename: session-{uuid}.log
		uuid := strings.TrimPrefix(name, "session-")
		uuid = strings.TrimSuffix(uuid, ".log")

		// Get file info
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := RecordingListItem{
			UUID:      uuid,
			SizeBytes: info.Size(),
			IsActive:  activeRecordings[uuid],
		}

		// Check if timing file exists
		timingPath := recordingsDir + "/session-" + uuid + ".timing"
		if _, err := os.Stat(timingPath); err == nil {
			item.HasTiming = true
		}

		// Load metadata if exists
		metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"
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

// handleDeleteRecording deletes a recording and its associated files
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

	// Delete all files matching session-{uuid}.*
	patterns := []string{
		recordingsDir + "/session-" + uuid + ".log",
		recordingsDir + "/session-" + uuid + ".timing",
		recordingsDir + "/session-" + uuid + ".metadata.json",
	}

	deleted := false
	for _, path := range patterns {
		if err := os.Remove(path); err == nil {
			deleted = true
			log.Printf("Deleted recording file: %s", path)
		}
	}

	if !deleted {
		http.Error(w, "Recording not found", http.StatusNotFound)
		return
	}

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

// handleDownloadRecording creates a zip archive of the recording files
func handleDownloadRecording(w http.ResponseWriter, r *http.Request, uuid string) {
	logPath := recordingsDir + "/session-" + uuid + ".log"
	timingPath := recordingsDir + "/session-" + uuid + ".timing"
	metadataPath := recordingsDir + "/session-" + uuid + ".metadata.json"

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		http.Error(w, "Recording not found", http.StatusNotFound)
		return
	}

	// Create zip in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add files to zip
	files := []struct {
		path string
		name string
	}{
		{logPath, "session.log"},
		{timingPath, "session.timing"},
		{metadataPath, "session.metadata.json"},
	}

	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue // Skip missing files
		}
		zf, err := zipWriter.Create(f.name)
		if err != nil {
			continue
		}
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
