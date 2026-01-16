package main

import (
	"bytes"
	"embed"
	"encoding/base64"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hinshun/vt10x"
)

//go:embed static/*
var staticFS embed.FS

// Version can be set at build time with: go build -ldflags "-X main.Version=<version>"
var Version = "dev"

var indexTemplate *template.Template
var selectionTemplate *template.Template

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// TermSize represents terminal dimensions
type TermSize struct {
	Rows uint16
	Cols uint16
}

// PollingClient represents an HTTP polling client
type PollingClient struct {
	ID       string
	Size     TermSize
	LastPoll time.Time
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
	Assistant AssistantConfig
	Sessions  []SessionInfo // sorted by CreatedAt desc (most recent first)
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
		ShellCmd:        "goose",
		ShellRestartCmd: "goose",
		Binary:          "goose",
	},
	{
		Name:            "Aider",
		ShellCmd:        "aider",
		ShellRestartCmd: "aider --restore-chat-history",
		Binary:          "aider",
	},
}

// Session represents a terminal session with multiple clients
type Session struct {
	UUID            string
	Assistant       string // The assistant key (e.g., "claude", "gemini", "custom")
	AssistantConfig AssistantConfig
	Cmd             *exec.Cmd
	PTY             *os.File
	wsClients       map[*websocket.Conn]bool      // WebSocket clients
	wsClientSizes   map[*websocket.Conn]TermSize  // WebSocket client terminal sizes
	pollClients     map[string]*PollingClient     // HTTP polling clients by clientId
	ptySize         TermSize                      // Current PTY dimensions (for dedup)
	mu              sync.RWMutex
	writeMu         sync.Mutex     // mutex for websocket writes (gorilla/websocket isn't concurrent-write safe)
	CreatedAt       time.Time      // when the session was created
	lastActive      time.Time
	vt              vt10x.Terminal // virtual terminal for screen state tracking
	vtMu            sync.Mutex     // separate mutex for VT operations
}

// AddClient adds a WebSocket client to the session
func (s *Session) AddClient(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsClients[conn] = true
	s.lastActive = time.Now()
	log.Printf("Client added to session %s (total: %d)", s.UUID, len(s.wsClients))

	// Broadcast status after lock is released
	go s.BroadcastStatus()
}

// RemoveClient removes a WebSocket client from the session
func (s *Session) RemoveClient(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.wsClients, conn)
	delete(s.wsClientSizes, conn)
	s.lastActive = time.Now()
	log.Printf("Client removed from session %s (total: %d)", s.UUID, len(s.wsClients))

	// Recalculate PTY size based on remaining clients (WS + polling)
	if (len(s.wsClientSizes) > 0 || len(s.pollClients) > 0) && s.PTY != nil {
		minRows, minCols := s.calculateMinSize()

		// Only resize if session's min size actually changed
		if s.ptySize.Rows != minRows || s.ptySize.Cols != minCols {
			s.ptySize = TermSize{Rows: minRows, Cols: minCols}
			pty.Setsize(s.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
			log.Printf("Session %s: resized PTY to %dx%d (from %d WS + %d polling clients)", s.UUID, minCols, minRows, len(s.wsClientSizes), len(s.pollClients))

			// Also resize the virtual terminal for accurate snapshots
			s.vtMu.Lock()
			s.vt.Resize(int(minCols), int(minRows))
			s.vtMu.Unlock()
		}
	}

	// Broadcast status after lock is released
	go s.BroadcastStatus()
}

// UpdateClientSize updates a client's terminal size and recalculates the PTY size
func (s *Session) UpdateClientSize(conn *websocket.Conn, rows, cols uint16) {
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
	if len(s.wsClientSizes) == 0 && len(s.pollClients) == 0 {
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

	// Include polling client sizes
	for _, pc := range s.pollClients {
		if pc.Size.Rows < minRows {
			minRows = pc.Size.Rows
		}
		if pc.Size.Cols < minCols {
			minCols = pc.Size.Cols
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

// ClientCount returns the number of connected clients (WebSocket + polling)
func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wsClients) + len(s.pollClients)
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

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
	status := map[string]interface{}{
		"type":      "status",
		"viewers":   len(s.wsClients) + len(s.pollClients),
		"cols":      cols,
		"rows":      rows,
		"assistant": s.AssistantConfig.Name,
	}

	data, err := json.Marshal(status)
	if err != nil {
		log.Printf("BroadcastStatus marshal error: %v", err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastStatus write error: %v", err)
		}
	}
	log.Printf("Session %s: broadcast status (viewers=%d, size=%dx%d)", s.UUID, len(s.wsClients)+len(s.pollClients), cols, rows)
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

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastChatMessage write error: %v", err)
		}
	}
}

// BroadcastExit sends a process exit notification to all connected clients
func (s *Session) BroadcastExit(exitCode int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exitJSON := map[string]interface{}{
		"type":     "exit",
		"exitCode": exitCode,
	}

	data, err := json.Marshal(exitJSON)
	if err != nil {
		log.Printf("BroadcastExit marshal error: %v", err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastExit write error: %v", err)
		}
	}
	log.Printf("Session %s: broadcast exit (code=%d)", s.UUID, exitCode)
}

// Close terminates the session
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all WebSocket client connections
	for conn := range s.wsClients {
		conn.Close()
	}
	s.wsClients = make(map[*websocket.Conn]bool)
	s.pollClients = make(map[string]*PollingClient)

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

// GenerateSnapshot creates ANSI escape sequences to recreate the current screen state
func (s *Session) GenerateSnapshot() []byte {
	s.vtMu.Lock()
	defer s.vtMu.Unlock()

	var buf bytes.Buffer

	cols, rows := s.vt.Size()

	// Clear screen and move cursor to home
	buf.WriteString("\x1b[2J")  // clear entire screen
	buf.WriteString("\x1b[H")   // cursor to home (1,1)

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

	return buf.Bytes()
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
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	s.Cmd = cmd
	s.PTY = ptmx
	s.lastActive = time.Now()

	log.Printf("Restarted process for session %s (pid=%d)", s.UUID, cmd.Process.Pid)
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
					return
				}

				if exitCode == 0 {
					log.Printf("Session %s: process exited successfully (code 0), not restarting", s.UUID)
					exitMsg := []byte("\r\n[Process exited successfully]\r\n")
					s.vtMu.Lock()
					s.vt.Write(exitMsg)
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
				s.vtMu.Unlock()
				s.Broadcast(restartMsg)

				// Wait a bit before restarting
				time.Sleep(500 * time.Millisecond)

				if err := s.RestartProcess(s.AssistantConfig.ShellRestartCmd); err != nil {
					log.Printf("Session %s: failed to restart process: %v", s.UUID, err)
					errMsg := []byte("\r\n[Failed to restart process: " + err.Error() + "]\r\n")
					s.vtMu.Lock()
					s.vt.Write(errMsg)
					s.vtMu.Unlock()
					s.Broadcast(errMsg)
					return
				}

				continue
			}

			// Update virtual terminal state
			s.vtMu.Lock()
			s.vt.Write(buf[:n])
			s.vtMu.Unlock()

			// Broadcast to all clients
			s.Broadcast(buf[:n])
		}
	}()
}

var (
	sessions            = make(map[string]*Session)
	sessionsMu          sync.RWMutex
	shellCmd            string
	shellRestartCmd     string
	sessionTTL          time.Duration
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
	flag.DurationVar(&sessionTTL, "session-ttl", time.Hour, "Session keepalive after last disconnect")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	flag.Parse()

	// Handle --version flag
	if *version {
		fmt.Println("swe-swe-server version dev")
		os.Exit(0)
	}

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

			// Build AgentWithSessions for all available assistants
			agents := make([]AgentWithSessions, 0, len(availableAssistants))
			for _, assistant := range availableAssistants {
				agents = append(agents, AgentWithSessions{
					Assistant: assistant,
					Sessions:  sessionsByAssistant[assistant.Binary], // nil if no sessions
				})
			}

			// Check if SSL certificate is available
			_, hasSSLCert := os.Stat(tlsCertPath)
			data := struct {
				Agents     []AgentWithSessions
				NewUUID    string
				HasSSLCert bool
			}{
				Agents:     agents,
				NewUUID:    uuid.New().String(),
				HasSSLCert: hasSSLCert == nil,
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

		// Polling paths: /session/{uuid}/client/{clientId}/poll and /send
		if strings.HasPrefix(r.URL.Path, "/session/") && strings.Contains(r.URL.Path, "/client/") {
			// Parse path: /session/{uuid}/client/{clientId}/{action}
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/session/"), "/")
			if len(parts) >= 4 && parts[1] == "client" {
				sessionUUID := parts[0]
				clientID := parts[2]
				action := parts[3]

				switch action {
				case "poll":
					handlePollRecv(w, r, sessionUUID, clientID)
					return
				case "send":
					handlePollSend(w, r, sessionUUID, clientID)
					return
				}
			}
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
	log.Printf("  session-ttl: %v", sessionTTL)
	if workingDir != "" {
		log.Printf("  working-directory: %s", workingDir)
	}
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

// sessionReaper periodically cleans up expired sessions and stale polling clients
func sessionReaper() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sessionsMu.Lock()
		for uuid, sess := range sessions {
			// Clean up stale polling clients (no poll in 60 seconds)
			sess.mu.Lock()
			expiredCount := 0
			for clientID, pc := range sess.pollClients {
				if time.Since(pc.LastPoll) > 60*time.Second {
					delete(sess.pollClients, clientID)
					expiredCount++
					log.Printf("Polling client expired: session=%s client=%s", uuid, clientID)
				}
			}
			sess.mu.Unlock()

			// Resize PTY/VT and broadcast status if any polling clients were removed
			if expiredCount > 0 {
				// Recalculate size after removing polling clients
				sess.mu.Lock()
				minRows, minCols := sess.calculateMinSize()
				hasClients := len(sess.wsClientSizes) > 0 || len(sess.pollClients) > 0
				sizeChanged := sess.ptySize.Rows != minRows || sess.ptySize.Cols != minCols

				if hasClients && sess.PTY != nil && sizeChanged {
					sess.ptySize = TermSize{Rows: minRows, Cols: minCols}
					pty.Setsize(sess.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
					sess.vtMu.Lock()
					sess.vt.Resize(int(minCols), int(minRows))
					sess.vtMu.Unlock()
					log.Printf("Session %s: resized PTY to %dx%d after %d polling clients expired", uuid, minCols, minRows, expiredCount)
				}
				sess.mu.Unlock()

				go sess.BroadcastStatus()
			}

			// Only expire sessions with no clients that have been idle for TTL
			if sess.ClientCount() == 0 && time.Since(sess.LastActive()) > sessionTTL {
				log.Printf("Session expired: %s (idle for %v)", uuid, time.Since(sess.LastActive()))
				sess.Close()
				delete(sessions, uuid)
			}
		}
		sessionsMu.Unlock()
	}
}

// getOrCreateSession returns an existing session or creates a new one
// The assistant parameter is the key from availableAssistants (e.g., "claude", "gemini", "custom")
func getOrCreateSession(sessionUUID string, assistant string) (*Session, bool, error) {
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

	// Create new session with PTY using assistant's shell command
	cmdName, cmdArgs := parseCommand(cfg.ShellCmd)
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, false, err
	}

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	// NOTE: Commented out - injecting text via PTY doesn't actually reach Claude's
	// conversation context; it only displays in the terminal. Browser automation
	// instructions should be in CLAUDE.md or system prompt instead.
	// if browserEndpoint := os.Getenv("BROWSER_WS_ENDPOINT"); browserEndpoint != "" {
	// 	browserPrompt := `You have browser automation capabilities via MCP Playwright tools (mcp__playwright__*).
	// If browser tools are unavailable or not working, read .swe-swe/browser-automation.md for troubleshooting.
	// User can watch the browser via VNC at http://chrome.lvh.me:1977/vnc_auto.html
	//
	// ` + "\n"
	// 	ptmx.Write([]byte(browserPrompt))
	// }

	sess := &Session{
		UUID:            sessionUUID,
		Assistant:       assistant,
		AssistantConfig: cfg,
		Cmd:             cmd,
		PTY:             ptmx,
		wsClients:       make(map[*websocket.Conn]bool),
		wsClientSizes:   make(map[*websocket.Conn]TermSize),
		pollClients:     make(map[string]*PollingClient),
		ptySize:         TermSize{Rows: 24, Cols: 80},
		CreatedAt:       time.Now(),
		lastActive:      time.Now(),
		vt:              vt10x.New(vt10x.WithSize(80, 24)),
	}
	sessions[sessionUUID] = sess

	log.Printf("Created new session: %s (assistant=%s, pid=%d)", sessionUUID, cfg.Name, cmd.Process.Pid)
	return sess, true, nil // new session
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, sessionUUID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Get assistant from query param
	assistant := r.URL.Query().Get("assistant")
	if assistant == "" {
		log.Printf("WebSocket error: no assistant specified")
		conn.WriteMessage(websocket.TextMessage, []byte("Error: no assistant specified"))
		return
	}

	sess, isNew, err := getOrCreateSession(sessionUUID, assistant)
	if err != nil {
		log.Printf("Session creation error: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Error creating session: "+err.Error()))
		return
	}

	// Add this client to the session
	sess.AddClient(conn)
	defer sess.RemoveClient(conn)

	log.Printf("WebSocket connected: session=%s (new=%v)", sessionUUID, isNew)

	// If this is a new session, start the PTY reader goroutine
	if isNew {
		sess.startPTYReader()
	} else {
		// Send snapshot to catch up the new client with existing screen state
		snapshot := sess.GenerateSnapshot()
		sess.writeMu.Lock()
		err := conn.WriteMessage(websocket.BinaryMessage, snapshot)
		sess.writeMu.Unlock()
		if err != nil {
			log.Printf("Failed to send snapshot: %v", err)
		} else {
			log.Printf("Sent screen snapshot to new client (%d bytes)", len(snapshot))
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
			log.Printf("WebSocket read error: %v", err)
			break
		}

		// Handle text (JSON) messages
		if messageType == websocket.TextMessage {
			var msg struct {
				Type     string          `json:"type"`
				Data     json.RawMessage `json:"data,omitempty"`
				UserName string          `json:"userName,omitempty"`
				Text     string          `json:"text,omitempty"`
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
				sess.writeMu.Lock()
				err := conn.WriteJSON(response)
				sess.writeMu.Unlock()
				if err != nil {
					log.Printf("Failed to send pong: %v", err)
				}
			case "chat":
				// Handle incoming chat message
				if msg.UserName != "" && msg.Text != "" {
					sess.BroadcastChatMessage(msg.UserName, msg.Text)
					log.Printf("Chat message from %s: %s", msg.UserName, msg.Text)
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
				sendFileUploadResponse(sess, conn, false, "", "Invalid upload format")
				continue
			}
			filename := string(data[3 : 3+nameLen])
			fileData := data[3+nameLen:]

			// Sanitize filename: only keep the base name, no path traversal
			filename = sanitizeFilename(filename)
			if filename == "" {
				sendFileUploadResponse(sess, conn, false, "", "Invalid filename")
				continue
			}

			// Create uploads directory if it doesn't exist
			uploadsDir := ".swe-swe/uploads"
			if err := os.MkdirAll(uploadsDir, 0755); err != nil {
				log.Printf("Failed to create uploads directory: %v", err)
				sendFileUploadResponse(sess, conn, false, filename, "Failed to create uploads directory")
				continue
			}

			// Save the file to the uploads directory
			filePath := uploadsDir + "/" + filename
			if err := os.WriteFile(filePath, fileData, 0644); err != nil {
				log.Printf("File upload error: %v", err)
				sendFileUploadResponse(sess, conn, false, filename, err.Error())
				continue
			}

			log.Printf("File uploaded: %s (%d bytes)", filePath, len(fileData))
			sendFileUploadResponse(sess, conn, true, filename, "")

			// Send the file path to PTY - Claude Code will detect it and read from disk
			absPath, err := os.Getwd()
			if err != nil {
				absPath = "."
			}
			absFilePath := absPath + "/" + filePath
			if _, err := sess.PTY.Write([]byte(absFilePath)); err != nil {
				log.Printf("PTY write error for uploaded file path: %v", err)
			}
			continue
		}

		// Regular terminal input
		if _, err := sess.PTY.Write(data); err != nil {
			log.Printf("PTY write error: %v", err)
			break
		}
	}

	log.Printf("WebSocket disconnected: session=%s", sessionUUID)
}

// parseCommand splits a command string into executable and arguments
func parseCommand(cmdStr string) (string, []string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return cmdStr, nil
	}
	return parts[0], parts[1:]
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
func sendFileUploadResponse(sess *Session, conn *websocket.Conn, success bool, filename, errMsg string) {
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
	sess.writeMu.Lock()
	err := conn.WriteJSON(response)
	sess.writeMu.Unlock()
	if err != nil {
		log.Printf("Failed to send file upload response: %v", err)
	}
}

// handlePollRecv handles GET requests for polling terminal snapshots
func handlePollRecv(w http.ResponseWriter, r *http.Request, sessionUUID, clientID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get assistant from query param
	assistant := r.URL.Query().Get("assistant")
	if assistant == "" {
		http.Error(w, "Missing assistant parameter", http.StatusBadRequest)
		return
	}

	// Get or create session
	sess, isNewSession, err := getOrCreateSession(sessionUUID, assistant)
	if err != nil {
		log.Printf("Poll error: %v", err)
		http.Error(w, "Session error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse optional size from query params (used by initial poll)
	initialRows := uint16(24)
	initialCols := uint16(80)
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		if v, err := strconv.ParseUint(rowsStr, 10, 16); err == nil && v > 0 {
			initialRows = uint16(v)
		}
	}
	if colsStr := r.URL.Query().Get("cols"); colsStr != "" {
		if v, err := strconv.ParseUint(colsStr, 10, 16); err == nil && v > 0 {
			initialCols = uint16(v)
		}
	}

	// Register/update polling client
	sess.mu.Lock()
	isNewClient := sess.pollClients[clientID] == nil
	sizeChanged := false
	if isNewClient {
		sess.pollClients[clientID] = &PollingClient{
			ID:       clientID,
			Size:     TermSize{Rows: initialRows, Cols: initialCols},
			LastPoll: time.Now(),
		}
		log.Printf("Polling client registered: session=%s client=%s size=%dx%d", sessionUUID, clientID, initialCols, initialRows)
	} else {
		// Check if size changed (client may have rotated device, etc.)
		pc := sess.pollClients[clientID]
		if pc.Size.Rows != initialRows || pc.Size.Cols != initialCols {
			pc.Size = TermSize{Rows: initialRows, Cols: initialCols}
			sizeChanged = true
			log.Printf("Polling client size changed: session=%s client=%s size=%dx%d", sessionUUID, clientID, initialCols, initialRows)
		}
		pc.LastPoll = time.Now()
	}
	sess.lastActive = time.Now()
	sess.mu.Unlock()

	// When a polling client joins or size changes, recalculate PTY size
	if isNewClient || sizeChanged {
		if isNewClient {
			go sess.BroadcastStatus()
		}

		// Recalculate minimum size across all clients and resize PTY + VT
		sess.mu.Lock()
		minRows, minCols := sess.calculateMinSize()
		ptySizeChanged := sess.ptySize.Rows != minRows || sess.ptySize.Cols != minCols

		if sess.PTY != nil && ptySizeChanged {
			sess.ptySize = TermSize{Rows: minRows, Cols: minCols}
			pty.Setsize(sess.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
			log.Printf("Polling client %s: resized PTY to %dx%d", clientID, minCols, minRows)

			// Also resize the virtual terminal for accurate snapshots
			sess.vtMu.Lock()
			sess.vt.Resize(int(minCols), int(minRows))
			sess.vtMu.Unlock()
		}
		sess.mu.Unlock()

		// For new sessions created by polling client, start the PTY reader
		// This is critical: without it, PTY output never gets written to VT
		if isNewSession {
			sess.startPTYReader()
			log.Printf("New polling session %s: waiting for initial PTY output...", sessionUUID)
			waited := 0
			for i := 0; i < 40; i++ { // Wait up to 4 seconds
				sess.vtMu.Lock()
				// Check for non-whitespace content (VT initializes with spaces)
				vtContent := strings.TrimSpace(sess.vt.String())
				hasContent := vtContent != ""
				sess.vtMu.Unlock()
				if hasContent {
					log.Printf("New polling session %s: got content after %dms", sessionUUID, waited)
					break
				}
				time.Sleep(100 * time.Millisecond)
				waited += 100
			}
			if waited >= 4000 {
				log.Printf("New polling session %s: timeout waiting for PTY output", sessionUUID)
			}
		}
	}

	// Generate terminal snapshot
	snapshot := sess.GenerateSnapshot()
	log.Printf("Poll snapshot: session=%s size=%d bytes", sessionUUID, len(snapshot))

	// Get current terminal size
	sess.mu.RLock()
	rows, cols := sess.calculateMinSize()
	viewerCount := len(sess.wsClients) + len(sess.pollClients)
	sess.mu.RUnlock()

	// Return JSON response
	response := map[string]interface{}{
		"terminal":  base64.StdEncoding.EncodeToString(snapshot),
		"viewers":   viewerCount,
		"cols":      cols,
		"rows":      rows,
		"assistant": sess.AssistantConfig.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Poll response encode error: %v", err)
	}
}

// handlePollSend handles POST requests for sending input from polling clients
func handlePollSend(w http.ResponseWriter, r *http.Request, sessionUUID, clientID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session
	sessionsMu.RLock()
	sess, exists := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Verify polling client exists
	sess.mu.RLock()
	pollClient := sess.pollClients[clientID]
	sess.mu.RUnlock()
	if pollClient == nil {
		http.Error(w, "Polling client not found - call poll endpoint first", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Type string `json:"type"` // "input" or "resize"
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	sess.mu.Lock()
	sess.lastActive = time.Now()
	pollClient.LastPoll = time.Now()
	sess.mu.Unlock()

	switch req.Type {
	case "input":
		// Write to PTY character-by-character with small delays
		// This simulates typing and helps TUIs that expect gradual input
		data := []byte(req.Data)
		for i, b := range data {
			if _, err := sess.PTY.Write([]byte{b}); err != nil {
				log.Printf("Poll send PTY write error: %v", err)
				http.Error(w, "PTY write error", http.StatusInternalServerError)
				return
			}
			// Small delay between characters (skip delay after last char)
			if i < len(data)-1 {
				time.Sleep(5 * time.Millisecond)
			}
		}
		log.Printf("Poll send input: session=%s client=%s len=%d", sessionUUID, clientID, len(req.Data))

	case "resize":
		// Parse resize data: "rows,cols"
		var rows, cols uint16
		if _, err := fmt.Sscanf(req.Data, "%d,%d", &rows, &cols); err != nil {
			http.Error(w, "Invalid resize format, expected 'rows,cols'", http.StatusBadRequest)
			return
		}

		sess.mu.Lock()
		pollClient.Size = TermSize{Rows: rows, Cols: cols}

		// Recalculate PTY size
		minRows, minCols := sess.calculateMinSize()

		// Only resize if session's min size actually changed
		if sess.PTY != nil && (sess.ptySize.Rows != minRows || sess.ptySize.Cols != minCols) {
			sess.ptySize = TermSize{Rows: minRows, Cols: minCols}
			pty.Setsize(sess.PTY, &pty.Winsize{Rows: minRows, Cols: minCols})
			log.Printf("Poll resize: session=%s client=%s size=%dx%d", sessionUUID, clientID, minCols, minRows)

			// Also resize the virtual terminal for accurate snapshots
			sess.vtMu.Lock()
			sess.vt.Resize(int(minCols), int(minRows))
			sess.vtMu.Unlock()
		}
		sess.mu.Unlock()

	default:
		http.Error(w, "Unknown type: "+req.Type, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
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
