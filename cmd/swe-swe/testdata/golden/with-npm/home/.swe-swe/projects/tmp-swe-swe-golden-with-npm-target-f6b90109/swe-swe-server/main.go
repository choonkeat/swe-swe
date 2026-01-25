package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
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

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "f964a136"
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

// ANSI escape sequence helpers for terminal formatting
func ansiCyan(s string) string    { return "\033[0;36m" + s + "\033[0m" }
func ansiDim(s string) string     { return "\033[2m" + s + "\033[0m" }
func ansiYellow(s string) string  { return "\033[0;33m" + s + "\033[0m" }

// MOTDGracePeriod is how long to buffer input after displaying MOTD
// This gives users time to read the MOTD before the shell starts receiving input
const MOTDGracePeriod = 3 * time.Second

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
	// Input buffering during MOTD grace period
	inputBuffer   [][]byte  // buffered input during grace period
	inputBufferMu sync.Mutex
	graceUntil    time.Time // buffer input until this time
	// YOLO mode state
	yoloMode           bool   // Whether YOLO mode is active
	pendingReplacement string // If set, replace process with this command instead of ending session
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
		"yoloMode":      s.yoloMode,
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
	defer s.mu.Unlock()

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
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
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

			// Update virtual terminal state and ring buffer
			s.vtMu.Lock()
			s.vt.Write(buf[:n])
			s.writeToRing(buf[:n])
			s.vtMu.Unlock()

			// Broadcast to all clients
			s.Broadcast(buf[:n])
		}
	}()
}

var (
	sessions            = make(map[string]*Session)
	sessionsMu          sync.RWMutex
	shellCmd        string
	shellRestartCmd string
	workingDir      string
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
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 100vh;
            margin: 0;
            background: #1e1e1e;
            color: #9ca3af;
        }
        .container {
            text-align: center;
            padding: 2rem;
            max-width: 400px;
        }
        h1 { color: #e5e7eb; font-size: 1.25rem; font-weight: 500; margin-bottom: 1.5rem; }
        .instruction {
            background: #262626;
            border-radius: 8px;
            padding: 1rem 1.25rem;
            margin: 1rem 0;
            text-align: left;
        }
        .instruction-label {
            font-size: 0.8rem;
            color: #6b7280;
            margin-bottom: 0.5rem;
        }
        .instruction-text {
            color: #d1d5db;
            font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
            font-size: 0.9rem;
            line-height: 1.5;
        }
        .port { color: #60a5fa; }
        .status {
            font-size: 0.8rem;
            color: #6b7280;
            margin-top: 1.5rem;
        }
        .status-dot {
            display: inline-block;
            width: 6px;
            height: 6px;
            background: #6b7280;
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
            <div class="instruction-text">Start a hot-reload web app on port <span class="port">%s</span></div>
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

	// Add ws: and wss: to connect-src for WebSocket
	if strings.Contains(csp, "connect-src") {
		csp = strings.Replace(csp, "connect-src", "connect-src ws: wss:", 1)
	} else {
		// No connect-src directive, add one
		csp = csp + "; connect-src ws: wss:"
	}

	h.Set("Content-Security-Policy", csp)
}

// DebugHub manages WebSocket connections between iframe debug scripts and agent
type DebugHub struct {
	iframeClients map[*websocket.Conn]bool // Connected iframe debug scripts
	agentConn     *websocket.Conn          // Connected agent (only one allowed)
	mu            sync.RWMutex
}

// Global debug hub instance for the preview proxy
var debugHub = &DebugHub{
	iframeClients: make(map[*websocket.Conn]bool),
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

// ForwardToAgent sends a message from iframe to the connected agent
func (h *DebugHub) ForwardToAgent(msg []byte) {
	h.mu.RLock()
	agent := h.agentConn
	h.mu.RUnlock()

	if agent != nil {
		if err := agent.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[DebugHub] Error forwarding to agent: %v", err)
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
func handleDebugIframeWS(w http.ResponseWriter, r *http.Request) {
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

// handleDebugAgentWS handles WebSocket connection from the agent
func handleDebugAgentWS(w http.ResponseWriter, r *http.Request) {
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

  // Connect to debug channel
  connect();

  // Send initial page load message
  send({ t: 'init', url: location.href, ts: Date.now() });
})();
`

// Dynamic proxy target state
var (
	proxyTargetMu       sync.RWMutex
	proxyTargetURL      *url.URL // nil = use default localhost:targetPort
	proxyDefaultTarget  *url.URL // The default localhost:targetPort
	proxyDefaultPortStr string   // String version of default port for error pages
)

// startPreviewProxy starts the app preview reverse proxy server on port 9899
// It proxies requests to localhost:targetPort (or a dynamically set target URL)
// It also injects a debug script into HTML responses for console/error/network forwarding
func startPreviewProxy(targetPort string) {
	var err error
	proxyDefaultTarget, err = url.Parse("http://localhost:" + targetPort)
	if err != nil {
		log.Printf("Preview proxy: invalid target URL: %v", err)
		return
	}
	proxyDefaultPortStr = targetPort

	mux := http.NewServeMux()

	// Serve debug script
	mux.HandleFunc("/__swe-swe-debug__/inject.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(debugInjectJS))
	})

	// WebSocket endpoint for iframe debug scripts
	mux.HandleFunc("/__swe-swe-debug__/ws", handleDebugIframeWS)

	// WebSocket endpoint for agent
	mux.HandleFunc("/__swe-swe-debug__/agent", handleDebugAgentWS)

	// API endpoint to get current proxy target
	mux.HandleFunc("GET /__swe-swe-debug__/target", handleGetProxyTarget)

	// API endpoint to set proxy target
	mux.HandleFunc("POST /__swe-swe-debug__/target", handleSetProxyTarget)

	// Proxy all other requests
	mux.HandleFunc("/", handleProxyRequest)

	log.Printf("Starting preview proxy on :9899 -> localhost:%s", targetPort)
	if err := http.ListenAndServe(":9899", mux); err != nil {
		log.Printf("Preview proxy error: %v", err)
	}
}

// handleGetProxyTarget returns the current proxy target URL
func handleGetProxyTarget(w http.ResponseWriter, r *http.Request) {
	proxyTargetMu.RLock()
	target := proxyTargetURL
	proxyTargetMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	var targetStr string
	if target != nil {
		targetStr = target.String()
	} else {
		targetStr = proxyDefaultTarget.String()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"url":       targetStr,
		"isDefault": target == nil,
	})
}

// handleSetProxyTarget sets the proxy target URL from JSON body
func handleSetProxyTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Empty URL resets to default
	if req.URL == "" {
		proxyTargetMu.Lock()
		proxyTargetURL = nil
		proxyTargetMu.Unlock()
		log.Printf("Preview proxy: reset to default target localhost:%s", proxyDefaultPortStr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"url":       proxyDefaultTarget.String(),
			"isDefault": true,
		})
		return
	}

	// Parse and validate the URL
	target, err := url.Parse(req.URL)
	if err != nil {
		http.Error(w, "Invalid URL: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Ensure scheme is http or https
	if target.Scheme != "http" && target.Scheme != "https" {
		http.Error(w, "URL must be http or https", http.StatusBadRequest)
		return
	}

	proxyTargetMu.Lock()
	proxyTargetURL = target
	proxyTargetMu.Unlock()

	log.Printf("Preview proxy: target set to %s", target.String())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"url":       target.String(),
		"isDefault": false,
	})
}

// handleProxyRequest proxies requests to the current target
func handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	proxyTargetMu.RLock()
	target := proxyTargetURL
	if target == nil {
		target = proxyDefaultTarget
	}
	proxyTargetMu.RUnlock()

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
		fmt.Fprintf(w, previewProxyErrorPage, target.String())
		return
	}
	defer resp.Body.Close()

	// Process response (inject debug script for HTML, handle cookies)
	processProxyResponse(w, resp, target)
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

// runDebugListen connects to the debug channel and prints messages to stdout
// This allows agents to receive debug messages from the user's app
func runDebugListen(endpoint string) {
	// Default to the preview proxy debug endpoint
	if endpoint == "" {
		endpoint = "ws://localhost:9899/__swe-swe-debug__/agent"
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
		endpoint = "ws://localhost:9899/__swe-swe-debug__/agent"
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

func main() {
	addr := flag.String("addr", ":9898", "Listen address")
	version := flag.Bool("version", false, "Show version and exit")
	debugListen := flag.Bool("debug-listen", false, "Listen for debug messages from preview proxy")
	debugQuery := flag.String("debug-query", "", "Send DOM query to preview proxy (CSS selector)")
	debugEndpoint := flag.String("debug-endpoint", "", "Debug WebSocket endpoint (default: ws://localhost:9899/__swe-swe-debug__/agent)")
	flag.StringVar(&shellCmd, "shell", "claude", "Command to execute")
	flag.StringVar(&shellRestartCmd, "shell-restart", "claude --continue", "Command to restart on process death")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	flag.Parse()

	// Handle --version flag
	if *version {
		fmt.Printf("swe-swe-server %s (%s)\n", Version, GitCommit)
		os.Exit(0)
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

	// Start session reaper
	go sessionReaper()

	// Start preview proxy if SWE_PREVIEW_TARGET_PORT is set (for split-pane preview)
	if previewTargetPort := os.Getenv("SWE_PREVIEW_TARGET_PORT"); previewTargetPort != "" {
		go startPreviewProxy(previewTargetPort)
	}

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

			// Load recordings grouped by agent
			recordingsByAgent := loadEndedRecordingsByAgent()

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
			data := struct {
				Agents     []AgentWithSessions
				NewUUID    string
				HasSSLCert bool
				Debug      bool
			}{
				Agents:     agents,
				NewUUID:    uuid.New().String(),
				HasSSLCert: hasSSLCert == nil,
				Debug:      debugMode,
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

		// Recording API endpoints
		if strings.HasPrefix(r.URL.Path, "/api/recording/") {
			handleRecordingAPI(w, r)
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

// excludeFromCopy lists directories that should never be copied to worktrees
var excludeFromCopy = []string{".git", ".swe-swe"}

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

// copyFileOrDir copies a file or directory from src to dst, preserving permissions
func copyFileOrDir(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	// Handle symlinks
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}

	// Handle directories
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			srcPath := src + "/" + entry.Name()
			dstPath := dst + "/" + entry.Name()
			if err := copyFileOrDir(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}

	// Handle regular files
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copySweSweDocsDir copies .swe-swe/docs/ directory to the worktree
// This directory contains agent documentation (AGENTS.md, browser-automation.md, docker.md, etc.)
func copySweSweDocsDir(srcDir, destDir string) error {
	srcDocsDir := srcDir + "/.swe-swe/docs"
	if _, err := os.Stat(srcDocsDir); os.IsNotExist(err) {
		return nil // No .swe-swe/docs directory, nothing to copy
	}

	destDocsDir := destDir + "/.swe-swe/docs"
	if err := os.MkdirAll(destDocsDir, 0755); err != nil {
		return fmt.Errorf("failed to create .swe-swe/docs directory: %w", err)
	}

	entries, err := os.ReadDir(srcDocsDir)
	if err != nil {
		return fmt.Errorf("failed to read .swe-swe/docs directory: %w", err)
	}

	var copied []string
	for _, entry := range entries {
		name := entry.Name()

		srcPath := srcDocsDir + "/" + name
		dstPath := destDocsDir + "/" + name
		if err := copyFileOrDir(srcPath, dstPath); err != nil {
			log.Printf("Warning: failed to copy .swe-swe/docs/%s to worktree: %v", name, err)
			continue
		}
		copied = append(copied, name)
	}

	if len(copied) > 0 {
		log.Printf("Copied .swe-swe/docs/ files to worktree: %v", copied)
	}
	return nil
}

// copyUntrackedFiles symlinks directories and copies files for untracked dotfiles, CLAUDE.md, and AGENTS.md
// Directories are symlinked (absolute path) so agent configs stay in sync across worktrees
// Files are copied for potential per-worktree isolation (e.g., .env)
// Items in excludeFromCopy and files tracked in git are skipped
func copyUntrackedFiles(srcDir, destDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	var symlinked, copied []string
	for _, entry := range entries {
		name := entry.Name()

		// Check if this file matches our patterns: dotfiles, CLAUDE.md, AGENTS.md
		shouldProcess := false
		if strings.HasPrefix(name, ".") {
			shouldProcess = true
		} else if name == "CLAUDE.md" || name == "AGENTS.md" {
			shouldProcess = true
		}

		if !shouldProcess {
			continue
		}

		// Check exclusion list
		excluded := false
		for _, exc := range excludeFromCopy {
			if name == exc {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// Check if tracked in git
		if isTrackedInGit(srcDir, name) {
			continue
		}

		srcPath := srcDir + "/" + name
		dstPath := destDir + "/" + name

		if entry.IsDir() {
			// Symlink directories using absolute path
			if err := os.Symlink(srcPath, dstPath); err != nil {
				log.Printf("Warning: failed to symlink %s to worktree: %v", name, err)
				continue
			}
			symlinked = append(symlinked, name)
		} else {
			// Copy files
			if err := copyFileOrDir(srcPath, dstPath); err != nil {
				log.Printf("Warning: failed to copy %s to worktree: %v", name, err)
				continue
			}
			copied = append(copied, name)
		}
	}

	if len(symlinked) > 0 {
		log.Printf("Symlinked directories to worktree: %v", symlinked)
	}
	if len(copied) > 0 {
		log.Printf("Copied files to worktree: %v", copied)
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

// createWorktree creates or re-enters a git worktree for the given branch name
// Priority:
// 1. If worktree exists -> return existing path (re-entry)
// 2. If local branch exists -> git worktree add <path> <branch> (no -b)
// 3. If remote branch exists -> git worktree add --track -b <branch> <path> origin/<branch>
// 4. Otherwise -> git worktree add -b <branch> <path> (fresh branch)
func createWorktree(branchName string) (string, error) {
	if branchName == "" {
		return "", fmt.Errorf("branch name cannot be empty")
	}

	// Use worktreeDirName for filesystem path (converts "/" to "--")
	// but keep branchName unchanged for git operations
	worktreePath := worktreeDir + "/" + worktreeDirName(branchName)

	// Priority 1: Re-enter existing worktree
	if worktreeExists(branchName) {
		log.Printf("Re-entering existing worktree at %s", worktreePath)
		return worktreePath, nil
	}

	// Ensure worktree directory exists
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create worktree directory: %w", err)
	}

	var cmd *exec.Cmd
	var output []byte
	var err error

	// Priority 2: Attach to existing local branch
	if localBranchExists(branchName) {
		log.Printf("Attaching worktree to existing local branch %s", branchName)
		cmd = exec.Command("git", "worktree", "add", worktreePath, branchName)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to create worktree for existing branch: %w (output: %s)", err, string(output))
		}
	} else if remoteBranchExists(branchName) {
		// Priority 3: Track remote branch
		log.Printf("Creating worktree tracking remote branch origin/%s", branchName)
		cmd = exec.Command("git", "worktree", "add", "--track", "-b", branchName, worktreePath, "origin/"+branchName)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to create worktree tracking remote: %w (output: %s)", err, string(output))
		}
	} else {
		// Priority 4: Create fresh branch
		log.Printf("Creating new worktree with fresh branch %s", branchName)
		cmd = exec.Command("git", "worktree", "add", worktreePath, "-b", branchName)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to create worktree: %w (output: %s)", err, string(output))
		}
	}

	log.Printf("Created worktree at %s (branch: %s)", worktreePath, branchName)

	// Copy untracked files to the worktree (graceful degradation on failure)
	gitRoot, err := getGitRoot()
	if err != nil {
		log.Printf("Warning: could not determine git root for copying untracked files: %v", err)
	} else {
		if err := copyUntrackedFiles(gitRoot, worktreePath); err != nil {
			log.Printf("Warning: failed to copy untracked files to worktree: %v", err)
		}
		// Also copy .swe-swe/docs/ directory
		if err := copySweSweDocsDir(gitRoot, worktreePath); err != nil {
			log.Printf("Warning: failed to copy .swe-swe/docs/ to worktree: %v", err)
		}
	}

	return worktreePath, nil
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
	recentRecordingMaxAge       = time.Hour
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
		uuid    string
		endedAt time.Time
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

		// Determine end time
		endedAt := meta.StartedAt
		if meta.EndedAt != nil {
			endedAt = *meta.EndedAt
		}

		agent := meta.Agent
		if agent == "" {
			agent = "unknown"
		}
		// Map display name to binary name
		agent = agentNameToBinary(agent)

		recentByAgent[agent] = append(recentByAgent[agent], recentRecording{
			uuid:    uuid,
			endedAt: endedAt,
		})
	}

	// For each agent, delete old/excess recordings
	now := time.Now()
	for agent, recordings := range recentByAgent {
		// Sort by endedAt descending (newest first)
		sort.Slice(recordings, func(i, j int) bool {
			return recordings[i].endedAt.After(recordings[j].endedAt)
		})

		for i, rec := range recordings {
			shouldDelete := false

			// Delete if beyond the per-agent limit
			if i >= maxRecentRecordingsPerAgent {
				shouldDelete = true
			}

			// Delete if older than max age
			if now.Sub(rec.endedAt) > recentRecordingMaxAge {
				shouldDelete = true
			}

			if shouldDelete {
				deleteRecordingFiles(rec.uuid)
				log.Printf("Auto-deleted recent recording %s (agent=%s, age=%v, position=%d)",
					rec.uuid[:8], agent, now.Sub(rec.endedAt).Round(time.Minute), i+1)
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

// getOrCreateSession returns an existing session or creates a new one
// The assistant parameter is the key from availableAssistants (e.g., "claude", "gemini", "custom")
// The name parameter sets the session name (optional, can be empty)
// The workDir parameter sets the working directory for the session (empty = use server cwd)
func getOrCreateSession(sessionUUID string, assistant string, name string, workDir string) (*Session, bool, error) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if sess, ok := sessions[sessionUUID]; ok {
		return sess, false, nil // existing session
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

	// If session has a name, create a git worktree for it
	var branchName string
	if name != "" && workDir == "" {
		branchName = deriveBranchName(name)
		if branchName != "" {
			var err error
			workDir, err = createWorktree(branchName)
			if err != nil {
				log.Printf("Warning: failed to create worktree for branch %s: %v", branchName, err)
				// Continue without worktree - will use /workspace
			}
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
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
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
		BranchName:      branchName,
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

	// Get optional session name from query param
	sessionName := r.URL.Query().Get("name")

	// Get optional parent session UUID to inherit workDir (for shell sessions)
	parentUUID := r.URL.Query().Get("parent")
	var workDir string
	if parentUUID != "" {
		sessionsMu.RLock()
		if parentSess, ok := sessions[parentUUID]; ok {
			workDir = parentSess.WorkDir
			log.Printf("Shell session inheriting workDir from parent %s: %s", parentUUID, workDir)
		}
		sessionsMu.RUnlock()
	}

	sess, isNew, err := getOrCreateSession(sessionUUID, assistant, sessionName, workDir)
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
				// Validate: max 32 chars, alphanumeric + spaces + hyphens + underscores
				if len(name) > 32 {
					log.Printf("Session rename rejected: name too long (%d chars)", len(name))
					continue
				}
				valid := true
				for _, r := range name {
					if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_') {
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

	// script -q (quiet) -f (flush) --log-timing=file -c "command" logfile
	return "script", []string{
		"-q", "-f",
		"--log-timing=" + timingPath,
		"-c", fullCmd,
		logPath,
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
	UUID      string
	UUIDShort string
	Name      string
	Agent     string
	EndedAgo  string     // "15m ago", "2h ago", "yesterday"
	EndedAt   time.Time  // actual timestamp for sorting
	KeptAt    *time.Time // When user marked this recording to keep (nil = recent, auto-deletable)
	IsKept    bool       // Convenience field for templates
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
func loadEndedRecordingsByAgent() map[string][]RecordingInfo {
	recordings := loadEndedRecordings()
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
//   - render=streaming: use streaming approach (experimental, fetch data via JS)
//   - (default): use embedded approach (stable, data in HTML)
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

	if renderMode == "streaming" {
		// Use pre-calculated playback dimensions from metadata (fast path).
		// Fall back to calculating from content for older recordings without these fields.
		var cols uint16
		var rows uint32
		if metadata != nil && metadata.PlaybackCols > 0 {
			cols = metadata.PlaybackCols
			rows = metadata.PlaybackRows
		} else {
			// Legacy fallback: calculate from content (same algorithm as embedded mode's JS)
			dims := calculateTerminalDimensions(logPath)
			cols = dims.Cols
			rows = dims.Rows
		}

		opts := recordtui.StreamingOptions{
			Title:   name,
			DataURL: recordingUUID + "/session.log",
			FooterLink: recordtui.FooterLink{
				Text: "swe-swe",
				URL:  "https://github.com/choonkeat/swe-swe",
			},
			Cols:          cols,
			EstimatedRows: rows,
		}
		html, err := recordtui.RenderStreamingHTML(opts)
		if err != nil {
			http.Error(w, "Failed to render playback", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(html))
		return
	}

	// Default: embedded approach - read file, embed content in HTML
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
			"kept_at":        meta.KeptAt,
			"already_kept":   true,
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
