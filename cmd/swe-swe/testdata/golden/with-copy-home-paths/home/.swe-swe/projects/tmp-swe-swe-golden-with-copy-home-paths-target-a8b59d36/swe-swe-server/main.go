package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/choonkeat/swe-swe/cmd/swe-swe-server/playback"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hinshun/vt10x"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

//go:embed static/*
var staticFS embed.FS

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "e247c820"
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

// AssistantConfig holds the configuration for an AI coding assistant
type AssistantConfig struct {
	Name            string // Display name
	ShellCmd        string // Command to start the assistant
	ShellRestartCmd string // Command to restart (resume) the assistant
	Binary          string // Binary name to check with exec.LookPath
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
	UUID      string     `json:"uuid"`
	Name      string     `json:"name,omitempty"`
	Agent     string     `json:"agent"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	KeptAt    *time.Time `json:"kept_at,omitempty"` // When user marked this recording to keep (nil = recent, auto-deletable)
	Command   []string   `json:"command"`
	Visitors  []Visitor  `json:"visitors,omitempty"`
	MaxCols   uint16     `json:"max_cols,omitempty"` // Max terminal columns during recording
	MaxRows   uint16     `json:"max_rows,omitempty"` // Max terminal rows during recording
	WorkDir   string     `json:"work_dir,omitempty"` // Working directory for VS Code links in playback
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
		Binary:          "claude",
	},
	{
		Name:            "Gemini",
		ShellCmd:        "gemini",
		ShellRestartCmd: "gemini --resume",
		Binary:          "gemini",
	},
	{
		Name:            "Codex",
		ShellCmd:        "codex",
		ShellRestartCmd: "codex resume --last",
		Binary:          "codex",
	},
	{
		Name:            "Goose",
		ShellCmd:        "goose session",
		ShellRestartCmd: "goose session -r",
		Binary:          "goose",
	},
	{
		Name:            "Aider",
		ShellCmd:        "aider",
		ShellRestartCmd: "aider --restore-chat-history",
		Binary:          "aider",
	},
	{
		Name:            "OpenCode",
		ShellCmd:        "opencode",
		ShellRestartCmd: "opencode --continue",
		Binary:          "opencode",
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
		"type":        "status",
		"viewers":     len(s.wsClients),
		"cols":        cols,
		"rows":        rows,
		"assistant":   s.AssistantConfig.Name,
		"sessionName": s.Name,
		"uuidShort":   uuidShort,
		"workDir":     workDir,
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

				if clientCount == 0 {
					log.Printf("Session %s: process died with no clients, not restarting", s.UUID)
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

				if exitCode == 0 {
					log.Printf("Session %s: process exited successfully (code 0), not restarting", s.UUID)
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

					exitMsg := []byte("\r\n[Process exited successfully]\r\n")
					s.vtMu.Lock()
					s.vt.Write(exitMsg)
					s.writeToRing(exitMsg)
					s.vtMu.Unlock()
					s.Broadcast(exitMsg)

					// Send structured exit message so browser can prompt user
					s.BroadcastExit(0)
					return
				}

				// Notify clients of restart
				restartMsg := []byte(fmt.Sprintf("\r\n[Process exited with code %d, restarting...]\r\n", exitCode))
				s.vtMu.Lock()
				s.vt.Write(restartMsg)
				s.writeToRing(restartMsg)
				s.vtMu.Unlock()
				s.Broadcast(restartMsg)

				// Wait a bit before restarting
				time.Sleep(500 * time.Millisecond)

				if err := s.RestartProcess(s.AssistantConfig.ShellRestartCmd); err != nil {
					log.Printf("Session %s: failed to restart process: %v", s.UUID, err)
					errMsg := []byte("\r\n[Failed to restart process: " + err.Error() + "]\r\n")
					s.vtMu.Lock()
					s.vt.Write(errMsg)
					s.writeToRing(errMsg)
					s.vtMu.Unlock()
					s.Broadcast(errMsg)
					return
				}

				continue
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

func main() {
	addr := flag.String("addr", ":9898", "Listen address")
	version := flag.Bool("version", false, "Show version and exit")
	flag.StringVar(&shellCmd, "shell", "claude", "Command to execute")
	flag.StringVar(&shellRestartCmd, "shell-restart", "claude --continue", "Command to restart on process death")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	flag.Parse()

	// Handle --version flag
	if *version {
		fmt.Printf("swe-swe-server %s (%s)\n", Version, GitCommit)
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Root path: show assistant selection page
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// No-cache for homepage to ensure latest version
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")

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

			// Build AgentWithSessions for all available assistants
			agents := make([]AgentWithSessions, 0, len(availableAssistants))
			for _, assistant := range availableAssistants {
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


		// Recording playback page
		if strings.HasPrefix(r.URL.Path, "/recording/") {
			recordingUUID := strings.TrimPrefix(r.URL.Path, "/recording/")
			if recordingUUID == "" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			handleRecordingPage(w, r, recordingUUID)
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

// worktreeDir is the base directory for git worktrees
var worktreeDir = "/worktrees"

// excludeFromCopy lists directories that should never be copied to worktrees
var excludeFromCopy = []string{".git", ".swe-swe", "swe-swe"}

// worktreeCommandTemplates contains templates for generated worktree commands
var worktreeCommandTemplates = map[string]string{
	"merge-this-worktree": `# Merge This Worktree

Branch: {{.BranchName}}
Target: {{.TargetBranch}}
Worktree: {{.WorktreePath}}
Main repo: /workspace

## Instructions

Help the user merge this worktree branch to the target branch.

1. Check for uncommitted changes: ` + "`git status`" + `
   - If dirty, ask user: commit, stash, or abort?

2. Fetch latest: ` + "`git -C /workspace fetch origin`" + `

3. Check if target has moved:
   - ` + "`git -C /workspace log {{.TargetBranch}}..origin/{{.TargetBranch}} --oneline`" + `
   - If commits exist, ask if user wants to update target first

4. Perform merge:
   - ` + "`git -C /workspace checkout {{.TargetBranch}} && git merge {{.BranchName}}`" + `
   - Ask user about merge strategy (ff, no-ff, squash) if they have preference

5. If conflict:
   - Show conflicted files
   - Help resolve conversationally
   - Don't leave user in broken state

6. After successful merge:
   - ` + "`git -C /workspace worktree remove {{.WorktreePath}}`" + `
   - ` + "`git -C /workspace branch -d {{.BranchName}}`" + `
   - Confirm cleanup complete
`,
	"discard-this-worktree": `# Discard This Worktree

Branch: {{.BranchName}}
Worktree: {{.WorktreePath}}
Main repo: /workspace

## Instructions

Help the user discard this worktree and its branch.

1. Check for uncommitted changes: ` + "`git status`" + `
   - If dirty, WARN user: "You have uncommitted changes that will be lost"
   - List the files
   - Ask for explicit confirmation

2. Check for unpushed commits:
   - ` + "`git log origin/{{.BranchName}}..{{.BranchName}} --oneline 2>/dev/null || git log --oneline`" + `
   - If unpushed commits exist, WARN user
   - Ask for explicit confirmation

3. After confirmation:
   - ` + "`git -C /workspace worktree remove --force {{.WorktreePath}}`" + `
   - ` + "`git -C /workspace branch -D {{.BranchName}}`" + `
   - Confirm cleanup complete
`,
}

// worktreeCommandData contains the data for rendering worktree command templates
type worktreeCommandData struct {
	BranchName   string
	TargetBranch string
	WorktreePath string
}

// generateWorktreeCommands creates the swe-swe/ directory with commands in the worktree
func generateWorktreeCommands(worktreePath, branchName string) error {
	sweSweDir := worktreePath + "/swe-swe"
	if err := os.MkdirAll(sweSweDir, 0755); err != nil {
		return fmt.Errorf("failed to create swe-swe directory: %w", err)
	}

	// Copy setup from main workspace
	srcSetup := "/workspace/swe-swe/setup"
	dstSetup := sweSweDir + "/setup"
	if err := copyFileOrDir(srcSetup, dstSetup); err != nil {
		log.Printf("Warning: failed to copy swe-swe/setup to worktree: %v", err)
	}

	// Prepare template data
	data := worktreeCommandData{
		BranchName:   branchName,
		TargetBranch: getMainRepoBranch(),
		WorktreePath: worktreePath,
	}

	// Generate merge and discard commands
	for name, tmplStr := range worktreeCommandTemplates {
		tmpl, err := template.New(name).Parse(tmplStr)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("failed to execute template %s: %w", name, err)
		}

		cmdPath := sweSweDir + "/" + name
		if err := os.WriteFile(cmdPath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
		log.Printf("Generated worktree command: %s", cmdPath)
	}

	return nil
}

// generateMOTD creates the terminal MOTD displaying available swe-swe commands
func generateMOTD(workDir, branchName string) string {
	// Determine the workspace directory
	wsDir := "/workspace"
	if workDir != "" && strings.HasPrefix(workDir, worktreeDir) {
		wsDir = workDir
	}
	sweSweDir := wsDir + "/swe-swe"

	// Check if swe-swe directory exists
	entries, err := os.ReadDir(sweSweDir)
	if err != nil {
		return "" // No swe-swe directory, no MOTD
	}

	// Check if any command files exist
	hasCommands := false
	hasSetup := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		hasCommands = true
		if entry.Name() == "setup" {
			hasSetup = true
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
	sb.WriteString(ansiDim("Tip: @swe-swe to see available commands") + "\r\n")

	// Show "Try this" only if setup exists and hasn't been done
	if hasSetup && !setupDone {
		sb.WriteString(ansiDim("Try this:") + " " + ansiCyan("@swe-swe/setup") + "\r\n")
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

	// Generate swe-swe/ commands for the worktree
	if err := generateWorktreeCommands(worktreePath, branchName); err != nil {
		log.Printf("Warning: failed to generate swe-swe commands for worktree: %v", err)
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

	// Create new session with PTY using assistant's shell command
	cmdName, cmdArgs := parseCommand(cfg.ShellCmd)

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

	sess, isNew, err := getOrCreateSession(sessionUUID, assistant, sessionName, "")
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
		motd := generateMOTD(sess.WorkDir, sess.BranchName)
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
func (s *Session) saveMetadata() error {
	s.mu.RLock()
	metadata := s.Metadata
	recordingUUID := s.RecordingUUID
	s.mu.RUnlock()

	if metadata == nil {
		return nil // Nothing to save
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

// handleRecordingPage serves the recording playback page
func handleRecordingPage(w http.ResponseWriter, r *http.Request, recordingUUID string) {
	// Validate UUID format
	if len(recordingUUID) < 32 {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	// Check if recording exists
	logPath := recordingsDir + "/session-" + recordingUUID + ".log"
	logContent, err := os.ReadFile(logPath)
	if err != nil {
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

	// Try to load timing file for animated playback
	timingPath := recordingsDir + "/session-" + recordingUUID + ".timing"
	timingContent, timingErr := os.ReadFile(timingPath)

	// Get terminal dimensions and workDir from metadata (default to 0 for auto-fit)
	var cols, rows uint16
	var workDir string
	if metadata != nil {
		cols = metadata.MaxCols
		rows = metadata.MaxRows
		workDir = metadata.WorkDir
	}

	if timingErr == nil && len(timingContent) > 0 {
		// Parse timing file and render animated playback
		frames, err := playback.ParseTimingFile(logContent, timingContent)
		if err != nil || len(frames) == 0 {
			// Fallback to static if parsing fails
			html := playback.RenderStaticHTML(logContent, name, "/")
			w.Write([]byte(html))
			return
		}

		html, err := playback.RenderPlaybackHTML(frames, name, "/", cols, rows, workDir)
		if err != nil {
			http.Error(w, "Failed to render playback", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(html))
	} else {
		// No timing file - show static content
		html := playback.RenderStaticHTML(logContent, name, "/")
		w.Write([]byte(html))
	}
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
