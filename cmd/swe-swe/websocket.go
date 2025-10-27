package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alvinchoong/go-httphandler"
	"github.com/creack/pty"
	"golang.org/x/net/websocket"
)

// Client represents a connected websocket client
type Client struct {
	conn                  *websocket.Conn
	username              string
	browserSessionID      string   // Browser tab session ID
	claudeSessionID       string   // CLI session ID from stream-json (current/latest)
	claudeSessionHistory []string // History of session IDs (max 10, newest at end)
	hasStartedSession     bool     // Track if first message sent
	cancelFunc            context.CancelFunc
	activeProcess         *exec.Cmd          // Track the active process for proper termination
	processMutex          sync.Mutex
	allowedTools          []string // Track allowed tools for this client
	skipPermissions       bool     // Track if user chose to skip all permissions
	pendingToolPermission string   // Track which tool is pending permission
	lastKilledSessionID   string   // Track killed session to avoid reuse
	lastUserMessage       string   // Save message for replay after permission
	lastActiveSessionID   string   // Save session ID to resume after permission grant
}

// ChatItem represents either a sender or content in the chat
type ChatItem struct {
	Type      string `json:"type"`
	Sender    string `json:"sender,omitempty"`
	Content   string `json:"content,omitempty"`
	ToolInput string `json:"toolInput,omitempty"` // For permission requests
}

// ClientMessage represents a message from the client with sender and content
type ClientMessage struct {
	Type            string   `json:"type,omitempty"`
	Sender          string   `json:"sender,omitempty"`
	Content         string   `json:"content,omitempty"`
	FirstMessage    bool     `json:"firstMessage,omitempty"`
	SessionID       string   `json:"sessionID,omitempty"`       // Browser session ID
	ClaudeSessionID string   `json:"claudeSessionID,omitempty"` // Claude session ID from browser
	AllowedTools    []string `json:"allowedTools,omitempty"`    // For permission responses
	SkipPermissions bool     `json:"skipPermissions,omitempty"` // User chose to skip all permissions
	Query           string   `json:"query,omitempty"`           // For fuzzy search queries
	MaxResults      int      `json:"maxResults,omitempty"`      // Maximum number of search results
}

// ChatService manages the chat room state
type ChatService struct {
	clients         map[*Client]bool
	broadcast       chan ChatItem
	mutex           sync.Mutex
	agentCLI1st     []string
	agentCLINth     string
	deferStdinClose bool
	jsonOutput      bool
	toolUseCache    map[string]ToolUseInfo // Cache tool use info by ID
	cacheMutex      sync.Mutex
	fuzzyMatcher    *FuzzyMatcher // File fuzzy matcher
}

// ToolUseInfo stores information about a tool use
type ToolUseInfo struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

// ClaudeMessage represents a message from Claude's JSON output
type ClaudeMessage struct {
	Type      string                `json:"type"`
	SessionID string                `json:"session_id,omitempty"`
	Message   *ClaudeMessageContent `json:"message,omitempty"`
}

type ClaudeMessageContent struct {
	Role    string          `json:"role"`
	Content []ClaudeContent `json:"content"`
}

type ClaudeContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Content   string          `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// terminateProcess gracefully terminates a process with a timeout
func terminateProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	
	log.Printf("[PROCESS] Attempting graceful termination of process PID: %d", cmd.Process.Pid)
	
	// Try graceful shutdown first with interrupt signal
	err := cmd.Process.Signal(os.Interrupt)
	if err != nil {
		log.Printf("[PROCESS] Failed to send interrupt signal: %v", err)
	}
	
	// Wait for process to terminate gracefully
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	select {
	case <-time.After(30 * time.Second):
		// Force kill after timeout
		log.Printf("[PROCESS] Graceful termination timed out, force killing PID: %d", cmd.Process.Pid)
		err := cmd.Process.Kill()
		if err != nil {
			log.Printf("[PROCESS] Failed to kill process: %v", err)
		}
	case err := <-done:
		if err != nil {
			log.Printf("[PROCESS] Process terminated with error: %v", err)
		} else {
			log.Printf("[PROCESS] Process terminated gracefully")
		}
	}
}

// NewChatService creates a new chat service
func NewChatService(agentCLI1st string, agentCLINth string, deferStdinClose bool, jsonOutput bool) *ChatService {
	// Get current working directory for fuzzy matcher
	workingDir, err := os.Getwd()
	if err != nil {
		log.Printf("Failed to get working directory: %v", err)
		workingDir = "."
	}

	// Initialize fuzzy matcher
	fuzzyMatcher := NewFuzzyMatcher(workingDir)

	// Index files in background
	go func() {
		if err := fuzzyMatcher.IndexFiles(); err != nil {
			log.Printf("Failed to index files: %v", err)
		} else {
			log.Printf("Indexed %d files for fuzzy matching", fuzzyMatcher.GetFileCount())
		}
	}()

	// Start periodic re-indexing to catch new files
	go func() {
		ticker := time.NewTicker(2 * time.Minute) // Re-index every 2 minutes
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				if err := fuzzyMatcher.IndexFiles(); err != nil {
					log.Printf("Periodic file re-indexing failed: %v", err)
				} else {
					log.Printf("Periodic re-index completed: %d files", fuzzyMatcher.GetFileCount())
				}
			}
		}
	}()

	return &ChatService{
		clients:         make(map[*Client]bool),
		broadcast:       make(chan ChatItem),
		agentCLI1st:     parseAgentCLI(agentCLI1st),
		agentCLINth:     agentCLINth,
		deferStdinClose: deferStdinClose,
		jsonOutput:      jsonOutput,
		toolUseCache:    make(map[string]ToolUseInfo),
		fuzzyMatcher:    fuzzyMatcher,
	}
}

// Run starts the chat service
func (s *ChatService) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			// Context cancelled, clean up and exit
			s.mutex.Lock()
			for client := range s.clients {
				client.conn.Close()
			}
			s.mutex.Unlock()
			return ctx.Err()
		case item, ok := <-s.broadcast:
			if !ok {
				// Channel closed
				return nil
			}
			s.mutex.Lock()
			for client := range s.clients {
				if err := websocket.JSON.Send(client.conn, item); err != nil {
					log.Printf("Error sending message to client: %v", err)
					delete(s.clients, client)
					client.conn.Close()
				}
			}
			s.mutex.Unlock()
		}
	}
}

// RegisterClient adds a new client to the service
func (s *ChatService) RegisterClient(client *Client) {
	s.mutex.Lock()
	s.clients[client] = true
	s.mutex.Unlock()

	log.Printf("[WEBSOCKET] New client connected")

	// Send welcome message directly to this client only
	go func() {
		// Wait a moment for client to establish session ID
		time.Sleep(100 * time.Millisecond)

		// Send the swe-swe bot item
		botSenderItem := ChatItem{
			Type:   "bot",
			Sender: "swe-swe",
		}
		if err := websocket.JSON.Send(client.conn, botSenderItem); err != nil {
			log.Printf("Error sending welcome bot item: %v", err)
			return
		}

		// Send welcome content
		log.Printf("[CHAT] Sending welcome message to session: %s", client.browserSessionID)
		welcomeMsg := "Welcome to the chat! Type something to start chatting."
		contentItem := ChatItem{
			Type:    "content",
			Content: welcomeMsg,
		}
		if err := websocket.JSON.Send(client.conn, contentItem); err != nil {
			log.Printf("Error sending welcome content: %v", err)
		}
	}()
}

// UnregisterClient removes a client from the service
func (s *ChatService) UnregisterClient(client *Client) {
	s.mutex.Lock()
	delete(s.clients, client)
	s.mutex.Unlock()

	// Do not cancel running processes when client disconnects - let them continue
	log.Printf("[WEBSOCKET] Client disconnected, but keeping processes running")
}

// BroadcastItem sends a chat item to all clients
func (s *ChatService) BroadcastItem(item ChatItem) {
	s.broadcast <- item
}

// BroadcastToSession sends a chat item only to clients with matching session ID
func (s *ChatService) BroadcastToSession(item ChatItem, sessionID string) {
	if sessionID == "" {
		// If no session ID, broadcast to all (fallback for compatibility)
		s.BroadcastItem(item)
		return
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	for client := range s.clients {
		if client.browserSessionID == sessionID {
			if err := websocket.JSON.Send(client.conn, item); err != nil {
				log.Printf("Error sending message to client session %s: %v", sessionID, err)
				delete(s.clients, client)
				client.conn.Close()
			}
		}
	}
}

// parseAgentCLI parses the agent CLI string into a command slice
func parseAgentCLI(agentCLIStr string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	escapeNext := false
	
	for _, r := range agentCLIStr {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}
		
		switch r {
		case '\\':
			escapeNext = true
		case '"':
			inQuotes = !inQuotes
		case ' ', '\t', '\n':
			if inQuotes {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	
	return args
}

// isPermissionError checks if an error message is a permission error
func isPermissionError(content string) bool {
	return strings.Contains(content, "requested permissions") ||
		strings.Contains(content, "haven't granted it yet") ||
		strings.Contains(content, "permission denied") ||
		strings.Contains(content, "This command requires approval")
}

// startReplacementSession creates a fresh Claude session to replace the killed one
func startReplacementSession(ctx context.Context, svc *ChatService, client *Client) {
	log.Printf("[PERMISSION] Starting replacement session")
	
	// Start new Claude session with simple wait command
	// This happens synchronously - we need the session ID before showing permission dialog
	executeAgentCommandWithSession(ctx, svc, client, "wait", false, []string{}, false, "", true)
	log.Printf("[PERMISSION] Replacement session started and tracked in history")
}

// stopProcessWithReplacement immediately stops a process and starts a replacement session
// This unifies process stopping for both manual stops and permission errors
func stopProcessWithReplacement(ctx context.Context, svc *ChatService, client *Client, reason string) {
	client.processMutex.Lock()
	processToStop := client.activeProcess
	client.processMutex.Unlock()
	
	if processToStop == nil {
		log.Printf("[PROCESS] No active process to stop for reason: %s", reason)
		return
	}
	
	log.Printf("[PROCESS] Stopping process PID: %d (reason: %s)", processToStop.Process.Pid, reason)
	
	// Try graceful interrupt first (immediate, non-blocking)
	err := interruptProcess(processToStop)
	if err != nil {
		log.Printf("[PROCESS] Failed to interrupt process, using SIGKILL: %v", err)
		// Force kill immediately - don't wait
		if killErr := processToStop.Process.Kill(); killErr != nil {
			log.Printf("[PROCESS] Failed to kill process: %v", killErr)
		}
	}
	
	// Clear the active process immediately
	client.processMutex.Lock()
	client.activeProcess = nil
	client.processMutex.Unlock()
	
	// Start replacement session immediately (this is the key optimization)
	startReplacementSession(ctx, svc, client)
	
	// Send stop confirmation to UI
	svc.BroadcastToSession(ChatItem{
		Type:    "content",
		Content: fmt.Sprintf("\n[Process stopped: %s]\n", reason),
	}, client.browserSessionID)
	
	// Send exec end event to hide typing indicator
	svc.BroadcastToSession(ChatItem{
		Type: "exec_end",
	}, client.browserSessionID)
	
	log.Printf("[PROCESS] Process stopped and replacement session started")
}

// tryExecuteWithSessionHistory attempts to execute the command with session IDs from history
func tryExecuteWithSessionHistory(parentctx context.Context, svc *ChatService, client *Client, prompt string, isFirstMessage bool, allowedTools []string, skipPermissions bool, primarySessionID string) {
	// Build a list of session IDs to try, starting with the provided one
	var sessionIDsToTry []string
	
	// If we have a primary session ID from the client message, try it first
	if primarySessionID != "" {
		sessionIDsToTry = append(sessionIDsToTry, primarySessionID)
	}
	
	// Then try session IDs from history in reverse order (newest first)
	client.processMutex.Lock()
	historyLen := len(client.claudeSessionHistory)
	for i := historyLen - 1; i >= 0; i-- {
		sessionID := client.claudeSessionHistory[i]
		// Don't add duplicates
		if sessionID != primarySessionID {
			sessionIDsToTry = append(sessionIDsToTry, sessionID)
		}
	}
	client.processMutex.Unlock()
	
	// If this is a subsequent message and we have session IDs to try
	if !isFirstMessage && len(sessionIDsToTry) > 0 {
		log.Printf("[SESSION DEBUG] Attempting to resume with %d session IDs: %v", len(sessionIDsToTry), sessionIDsToTry)
		for i, sessionID := range sessionIDsToTry {
			if i > 0 {
				log.Printf("[SESSION] Retrying with older session ID from history (attempt %d/%d): %s", i+1, len(sessionIDsToTry), sessionID)
			} else {
				log.Printf("[SESSION DEBUG] Trying primary session ID: %s", sessionID)
			}
			
			// Skip redundant validation - let execution handle session errors directly
			
			// Try execution with this session ID
			result := executeAgentCommandWithSession(parentctx, svc, client, prompt, isFirstMessage, allowedTools, skipPermissions, sessionID, false)
			
			switch result {
			case ExecutionSuccess:
				// Success! Update the current session ID if different
				client.processMutex.Lock()
				if client.claudeSessionID != sessionID {
					log.Printf("[SESSION] Successfully resumed with session ID: %s", sessionID)
					client.claudeSessionID = sessionID
				}
				client.processMutex.Unlock()
				return
			case ExecutionPermissionError:
				// Permission error - do not retry with other sessions
				log.Printf("[SESSION] Permission error detected, stopping session retry attempts")
				return
			case ExecutionSessionError:
				// Session error - continue trying with next session ID
				log.Printf("[SESSION] Session error with ID %s, will try next session", sessionID)
				continue
			default:
				// Other error - continue trying with next session ID
				continue
			}
		}
		
		// All session IDs failed, try without resume (fresh start)
		log.Printf("[SESSION] All session IDs failed, starting fresh conversation")
		executeAgentCommandWithSession(parentctx, svc, client, prompt, true, allowedTools, skipPermissions, "", false)
	} else {
		// First message or no session IDs available
		executeAgentCommandWithSession(parentctx, svc, client, prompt, isFirstMessage, allowedTools, skipPermissions, "", false)
	}
}

// ExecutionResult represents the outcome of a command execution
type ExecutionResult int

const (
	ExecutionSuccess ExecutionResult = iota
	ExecutionPermissionError
	ExecutionSessionError
	ExecutionOtherError
)

// validateClaudeSession removed - redundant validation that wastes tokens and adds delay
// Session errors are now handled directly during command execution

// executeAgentCommandWithSession executes the configured agent command with a specific session ID
// Returns ExecutionResult to indicate success, permission error (no retry), or other errors (retry allowed)
func executeAgentCommandWithSession(parentctx context.Context, svc *ChatService, client *Client, prompt string, isFirstMessage bool, allowedTools []string, skipPermissions bool, claudeSessionID string, quiet bool) ExecutionResult {
	// Create a context that can be cancelled when the client disconnects
	ctx, cancel := context.WithCancel(parentctx)

	// Store the cancel function in the client for later use
	client.processMutex.Lock()
	if client.cancelFunc != nil {
		// Cancel any existing process
		log.Printf("[WEBSOCKET] Terminating existing agent process before starting new one")
		client.cancelFunc()
		client.cancelFunc = nil
		// Also terminate the actual process if it exists
		if client.activeProcess != nil {
			processToStop := client.activeProcess
			client.activeProcess = nil
			client.processMutex.Unlock()
			
			// Use immediate termination without session replacement since we're starting a new process
			log.Printf("[PROCESS] Stopping existing process PID: %d (reason: new process starting)", processToStop.Process.Pid)
			err := interruptProcess(processToStop)
			if err != nil {
				log.Printf("[PROCESS] Failed to interrupt existing process, using SIGKILL: %v", err)
				if killErr := processToStop.Process.Kill(); killErr != nil {
					log.Printf("[PROCESS] Failed to kill existing process: %v", killErr)
				}
			}
			
			// Brief pause to allow cleanup
			time.Sleep(100 * time.Millisecond)
			client.processMutex.Lock()
		}
	}
	client.cancelFunc = cancel
	client.processMutex.Unlock()

	// Prepare the agent command with prompt substitution
	var cmdArgs []string
	svc.mutex.Lock()
	if isFirstMessage {
		// Use the first message command
		cmdArgs = make([]string, len(svc.agentCLI1st))
		copy(cmdArgs, svc.agentCLI1st)
	} else {
		// Use the nth message command for subsequent messages
		cmdArgs = parseAgentCLI(svc.agentCLINth)
	}
	svc.mutex.Unlock()

	// Check if the command contains the ? placeholder
	hasPlaceholder := slices.Contains(cmdArgs, "?")

	// Replace placeholder with actual prompt if present
	for i, arg := range cmdArgs {
		if arg == "?" {
			cmdArgs[i] = prompt
		}
	}

	// Check if this is Claude agent and modify the command
	if len(cmdArgs) > 0 && cmdArgs[0] == "claude" {
		// Remove --dangerously-skip-permissions if present
		newArgs := []string{}
		for _, arg := range cmdArgs {
			if arg != "--dangerously-skip-permissions" {
				newArgs = append(newArgs, arg)
			}
		}
		cmdArgs = newArgs

		// Add session resume support for subsequent messages  
		if !isFirstMessage && claudeSessionID != "" {
			// Use session ID directly - let command execution handle validation
			// Insert --resume flag and session ID after claude command (without --continue to preserve full conversation history)
			cmdArgs = append([]string{cmdArgs[0], "--resume", claudeSessionID}, cmdArgs[1:]...)
			log.Printf("[SESSION] Using --resume with Claude session ID: %s", claudeSessionID)
		}

		// Add --dangerously-skip-permissions only if user explicitly chose to skip
		if skipPermissions {
			// Find position to insert the flag (after claude command and potential --resume)
			insertPos := 1
			if !isFirstMessage && claudeSessionID != "" {
				insertPos = 3 // After claude --resume sessionID
			}
			cmdArgs = append(cmdArgs[:insertPos], append([]string{"--dangerously-skip-permissions"}, cmdArgs[insertPos:]...)...)
		}
		// ALWAYS add allowed tools if we have them (separate from skipPermissions)
		if len(allowedTools) > 0 {
			log.Printf("[PERMISSIONS] Passing allowed tools to Claude: %v", allowedTools)
			cmdArgs = append(cmdArgs, "--allowedTools", strings.Join(allowedTools, ","))
		}
	}

	// Log the command execution
	log.Printf("[EXEC] Executing command: %#v", cmdArgs)
	log.Printf("[EXEC] Full prompt: %#v", prompt)
	log.Printf("[EXEC] Has placeholder: %#v", hasPlaceholder)

	// Execute the configured agent command
	var cmd *exec.Cmd
	if len(cmdArgs) > 1 {
		cmd = exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, cmdArgs[0])
	}

	// Check if this is the goose command
	isGooseCommand := len(cmdArgs) > 0 && cmdArgs[0] == "goose"

	var stdin io.WriteCloser
	var stdout io.ReadCloser
	var stderr io.ReadCloser
	var ptmx *os.File
	var err error

	if isGooseCommand {
		// Use PTY for goose command
		ptmx, err = pty.Start(cmd)
		if err != nil {
			log.Printf("[ERROR] Failed to start PTY: %v", err)
			svc.BroadcastToSession(ChatItem{
				Type:    "content",
				Content: "Error starting PTY: " + err.Error(),
			}, client.browserSessionID)
			return ExecutionOtherError
		}
		defer func() {
			_ = ptmx.Close()
		}()

		// Set terminal size (optional)
		pty.Setsize(ptmx, &pty.Winsize{
			Rows: 24,
			Cols: 80,
		})

		stdin = ptmx
		stdout = ptmx
		// PTY combines stdout and stderr
	} else {
		// Use regular pipes for non-goose commands
		// Create stdin pipe
		stdin, err = cmd.StdinPipe()
		if err != nil {
			log.Printf("[ERROR] Failed to create stdin pipe: %v", err)
		}

		// Get stdout pipe
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			log.Printf("[ERROR] Failed to create stdout pipe: %v", err)
			svc.BroadcastToSession(ChatItem{
				Type:    "content",
				Content: "Error creating command pipe: " + err.Error(),
			}, client.browserSessionID)
			return ExecutionOtherError
		}

		// Get stderr pipe
		stderr, err = cmd.StderrPipe()
		if err != nil {
			log.Printf("[ERROR] Failed to create stderr pipe: %v", err)
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			log.Printf("[ERROR] Failed to start command: %v", err)
			svc.BroadcastToSession(ChatItem{
				Type:    "content",
				Content: "Error starting agent command: " + err.Error(),
			}, client.browserSessionID)
			return ExecutionOtherError
		}
	}
	
	// Store the active process
	client.processMutex.Lock()
	client.activeProcess = cmd
	log.Printf("[PROCESS] Stored active process PID: %d", cmd.Process.Pid)
	client.processMutex.Unlock()

	// Handle stdin writing
	if stdin != nil && !hasPlaceholder {
		go func() {
			if !isGooseCommand {
				defer stdin.Close()
			}
			_, err := stdin.Write([]byte(prompt + "\n"))
			if err != nil {
				log.Printf("[ERROR] Failed to write to stdin: %v", err)
			}
			log.Printf("[EXEC] Wrote prompt to stdin")
		}()
	} else if stdin != nil && !isGooseCommand {
		if svc.deferStdinClose {
			// Defer closing stdin (for goose)
			defer stdin.Close()
		} else {
			// Close stdin immediately to signal EOF (for claude)
			stdin.Close()
		}
	}

	log.Printf("[EXEC] Process started with PID: %d", cmd.Process.Pid)

	// Send exec start event (unless quiet)
	if !quiet {
		svc.BroadcastToSession(ChatItem{
			Type: "exec_start",
		}, client.browserSessionID)
	}

	// Capture stderr content for error detection
	var stderrContent strings.Builder
	var stderrMutex sync.Mutex
	
	// Handle stderr in a separate goroutine (only for non-PTY commands)
	if stderr != nil {
		go func() {
			scanner := bufio.NewScanner(stderr)
			// Increase scanner buffer size to handle large lines (default is 64KB)
			const maxScanTokenSize = 1024 * 1024 // 1MB
			buf := make([]byte, 0, 64*1024)      // Start with 64KB buffer
			scanner.Buffer(buf, maxScanTokenSize)
			for scanner.Scan() {
				line := scanner.Text()
				log.Printf("[STDERR] %s", line)
				
				// Capture stderr content for error detection
				stderrMutex.Lock()
				stderrContent.WriteString(line)
				stderrContent.WriteString("\n")
				stderrMutex.Unlock()
			}
			if err := scanner.Err(); err != nil {
				log.Printf("[ERROR] Error reading stderr: %v", err)
			}
		}()
	}

	// For goose command, monitor output for the prompt in the first 5 seconds
	var promptDetected bool
	var promptTimer *time.Timer
	if isGooseCommand {
		promptTimer = time.NewTimer(5 * time.Second)
		defer promptTimer.Stop()
	}

	// Stream the output line by line
	scanner := bufio.NewScanner(stdout)
	// Increase scanner buffer size to handle large lines (default is 64KB)
	const maxScanTokenSize = 1024 * 1024 // 1MB
	buf := make([]byte, 0, 64*1024)      // Start with 64KB buffer
	scanner.Buffer(buf, maxScanTokenSize)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			// Context cancelled, stop processing
			log.Printf("[EXEC] Process cancelled by context")
			if !quiet {
				svc.BroadcastToSession(ChatItem{
					Type:    "content",
					Content: "\n[Process stopped by user]\n",
				}, client.browserSessionID)
			}
			// Send exec end event to hide typing indicator (unless quiet)
			if !quiet {
				svc.BroadcastToSession(ChatItem{
					Type: "exec_end",
				}, client.browserSessionID)
			}
			return ExecutionOtherError
		default:
			line := scanner.Text()
			if line != "" {
				log.Printf("[STDOUT] %s", line)

				// Check for goose prompt in the first 5 seconds
				if isGooseCommand && !promptDetected && promptTimer != nil {
					select {
					case <-promptTimer.C:
						// Timer expired, stop checking for prompt
						promptDetected = true
					default:
						if strings.Contains(line, "Do you want to switch back to the original working directory?") {
							log.Printf("[EXEC] Detected goose prompt, sending 'n'")
							promptDetected = true
							// Send "n" to the PTY
							if _, err := stdin.Write([]byte("n\n")); err != nil {
								log.Printf("[ERROR] Failed to send 'n' to goose: %v", err)
							}
						}
					}
				}

				// Handle JSON output if enabled
				if svc.jsonOutput {
					// Try to parse the JSON to detect tool uses and permission errors
					var claudeMsg ClaudeMessage
					if err := json.Unmarshal([]byte(line), &claudeMsg); err == nil {
						// Extract and store Claude session ID if present (always update to handle session resumption)
						if claudeMsg.SessionID != "" {
							client.processMutex.Lock()
							oldSessionID := client.claudeSessionID
							client.claudeSessionID = claudeMsg.SessionID
							
							// Add to history if it's a new session ID
							if oldSessionID != claudeMsg.SessionID {
								// Append to history
								client.claudeSessionHistory = append(client.claudeSessionHistory, claudeMsg.SessionID)
								
								// Keep only the last 10 session IDs
								if len(client.claudeSessionHistory) > 10 {
									client.claudeSessionHistory = client.claudeSessionHistory[len(client.claudeSessionHistory)-10:]
								}
							}
							client.processMutex.Unlock()
							
							if oldSessionID == "" {
								log.Printf("[SESSION] Extracted Claude session ID: %s for browser session: %s (history: %d)", client.claudeSessionID, client.browserSessionID, len(client.claudeSessionHistory))
								// Send Claude session ID back to browser for storage
								svc.BroadcastToSession(ChatItem{
									Type:    "claude_session_id",
									Content: client.claudeSessionID,
								}, client.browserSessionID)
							} else if oldSessionID != client.claudeSessionID {
								log.Printf("[SESSION] Updated Claude session ID from %s to %s for browser session: %s (history: %d)", oldSessionID, client.claudeSessionID, client.browserSessionID, len(client.claudeSessionHistory))
								// Send updated Claude session ID back to browser
								svc.BroadcastToSession(ChatItem{
									Type:    "claude_session_id", 
									Content: client.claudeSessionID,
								}, client.browserSessionID)
							}
						}

						// Check for tool uses and cache them
						if claudeMsg.Type == "assistant" && claudeMsg.Message != nil {
							for _, content := range claudeMsg.Message.Content {
								if content.Type == "tool_use" && content.ID != "" {
									// Cache tool use info
									svc.cacheMutex.Lock()
									svc.toolUseCache[content.ID] = ToolUseInfo{
										Name:  content.Name,
										Input: string(content.Input),
									}
									svc.cacheMutex.Unlock()
								}
							}
						}

						// Check for permission errors in tool results
						if claudeMsg.Type == "user" && claudeMsg.Message != nil {
							for _, content := range claudeMsg.Message.Content {
								if content.Type == "tool_result" && content.IsError {
									// Check if this is a permission error
									if isPermissionError(content.Content) {
										// Get the tool info from cache
										svc.cacheMutex.Lock()
										toolInfo, exists := svc.toolUseCache[content.ToolUseID]
										svc.cacheMutex.Unlock()

										if exists {
											// Send permission request to frontend
											svc.BroadcastToSession(ChatItem{
												Type:      "permission_request",
												Content:   content.Content,
												Sender:    toolInfo.Name,  // Tool name in Sender field
												ToolInput: toolInfo.Input, // Include tool input details
											}, client.browserSessionID)

											// Track which tool is pending permission and save context for recovery
											client.processMutex.Lock()
											client.pendingToolPermission = toolInfo.Name
											client.lastKilledSessionID = client.claudeSessionID  // Save session to avoid reuse
											client.lastActiveSessionID = client.claudeSessionID   // Save session to resume after permission grant
											// Note: We'll save the last user message in the main message handler
											client.processMutex.Unlock()

											// Use the original permission interruption approach - interrupt but don't kill
											log.Printf("[EXEC] Permission error detected, interrupting process with SIGINT")
											
											// Store the active process reference but DON'T clear it yet - we'll resume it
											client.processMutex.Lock()
											processToInterrupt := client.activeProcess
											// Keep client.activeProcess set so we can resume it later
											client.processMutex.Unlock()
											
											// Interrupt the process with SIGINT (like Escape key) instead of killing it
											// This should pause Claude's execution while keeping the session alive
											if processToInterrupt != nil {
												err := interruptProcess(processToInterrupt)
												if err != nil {
													log.Printf("[EXEC] Failed to interrupt process, falling back to terminate: %v", err)
													terminateProcess(processToInterrupt)
													// Clear activeProcess if we had to terminate
													client.processMutex.Lock()
													client.activeProcess = nil
													client.processMutex.Unlock()
												}
											}
											
											// ADD SESSION WARMING: Start replacement session immediately
											startReplacementSession(parentctx, svc, client)
											
											// Send exec end event to hide typing indicator
											svc.BroadcastToSession(ChatItem{
												Type: "exec_end",
											}, client.browserSessionID)
											
											// Process is now guaranteed dead, no need for cmd.Wait()
											return ExecutionPermissionError
										}
									}
								}
							}
						}
					}

					// Send raw JSON to Elm for parsing (unless quiet)
					if !quiet {
						svc.BroadcastToSession(ChatItem{
							Type:    "claudejson",
							Content: line,
						}, client.browserSessionID)
					}
				} else {
					// Regular text output (unless quiet)
					if !quiet {
						svc.BroadcastToSession(ChatItem{
							Type:    "content",
							Content: line + "\n",
						}, client.browserSessionID)
					}
				}
			}
		}
	}

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		select {
		case <-ctx.Done():
			// Context cancelled, don't send error message
			log.Printf("[EXEC] Process cancelled, exit error ignored: %v", err)
			// Send exec end event to hide typing indicator
			svc.BroadcastToSession(ChatItem{
				Type: "exec_end",
			}, client.browserSessionID)
			return ExecutionOtherError
		default:
			log.Printf("[ERROR] Command completed with error: %v", err)

			// Check if this was a Claude session resumption error
			if !isFirstMessage && client.claudeSessionID != "" && len(cmdArgs) > 0 && cmdArgs[0] == "claude" {
				errorMsg := err.Error()
				
				// Also get stderr content for better error detection
				stderrMutex.Lock()
				stderrText := stderrContent.String()
				stderrMutex.Unlock()
				
				log.Printf("[SESSION DEBUG] Checking error for session issues. Error: %s, STDERR: %s", errorMsg, stderrText)
				
				// Check for common session resumption error patterns in both error message and stderr
				combinedError := errorMsg + " " + stderrText
				if (strings.Contains(combinedError, "session") && (strings.Contains(combinedError, "not found") ||
					strings.Contains(combinedError, "invalid") || strings.Contains(combinedError, "expired"))) ||
					strings.Contains(combinedError, "No conversation found") {
					log.Printf("[SESSION] Claude session resumption failed for session ID: %s", claudeSessionID)
					// Return session error to indicate session resume failed
					return ExecutionSessionError
				} else {
					log.Printf("[SESSION DEBUG] Error does not match session failure patterns")
				}
			}

			svc.BroadcastToSession(ChatItem{
				Type:    "content",
				Content: "Command completed with error: " + err.Error(),
			}, client.browserSessionID)
		}
	} else {
		log.Printf("[EXEC] Process completed successfully")
	}

	// Check for scanning errors
	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			// Send exec end event to hide typing indicator
			svc.BroadcastToSession(ChatItem{
				Type: "exec_end",
			}, client.browserSessionID)
			return ExecutionOtherError
		default:
			svc.BroadcastToSession(ChatItem{
				Type:    "content",
				Content: "Error reading command output: " + err.Error(),
			}, client.browserSessionID)
		}
	}

	// Clear the cancel function when done and mark session as started
	client.processMutex.Lock()
	client.cancelFunc = nil
	// Mark session as started after first successful command execution
	if isFirstMessage {
		client.hasStartedSession = true
		log.Printf("[SESSION] Marked session as started for browser session: %s", client.browserSessionID)
	}
	client.processMutex.Unlock()

	// Send exec end event (unless quiet)
	if !quiet {
		svc.BroadcastToSession(ChatItem{
			Type: "exec_end",
		}, client.browserSessionID)
	}
	
	// Clear the active process as it has completed
	client.processMutex.Lock()
	client.activeProcess = nil
	client.processMutex.Unlock()
	
	// Return true to indicate successful execution
	return ExecutionSuccess
}

// websocketHandler handles websocket connections
func websocketHandler(ctx context.Context, svc *ChatService) websocket.Handler {
	return func(ws *websocket.Conn) {
		client := &Client{
			conn:     ws,
			username: "USER", // Default username for all users
		}

		svc.RegisterClient(client)
		defer svc.UnregisterClient(client)

		// Handle incoming messages
		for {
			var clientMsg ClientMessage
			if err := websocket.JSON.Receive(ws, &clientMsg); err != nil {
				log.Printf("Error receiving message: %v", err)
				break
			}

			// Extract and store browser session ID from client message
			if clientMsg.SessionID != "" && client.browserSessionID == "" {
				client.browserSessionID = clientMsg.SessionID
				log.Printf("[WEBSOCKET] Client assigned browser session ID: %s", client.browserSessionID)
			}

			// Store Claude session ID if provided (important for reconnections)
			if clientMsg.ClaudeSessionID != "" && client.claudeSessionID == "" {
				client.claudeSessionID = clientMsg.ClaudeSessionID
				// Add to history as well
				client.claudeSessionHistory = append(client.claudeSessionHistory, clientMsg.ClaudeSessionID)
				// Mark session as already started since we're resuming
				client.hasStartedSession = true
				log.Printf("[WEBSOCKET] Client resuming Claude session ID: %s", client.claudeSessionID)
			}

			// Handle stop command
			if clientMsg.Type == "stop" {
				log.Printf("[WEBSOCKET] Received stop command from client")
				
				// Cancel the context first to stop any ongoing processing
				client.processMutex.Lock()
				if client.cancelFunc != nil {
					log.Printf("[WEBSOCKET] Cancelling running process context")
					client.cancelFunc()
					client.cancelFunc = nil
				}
				client.processMutex.Unlock()
				
				// Stop process with immediate replacement (non-blocking)
				go stopProcessWithReplacement(ctx, svc, client, "user stop")
				continue
			}

			// Handle manual file index refresh
			if clientMsg.Type == "refresh_file_index" {
				log.Printf("[WEBSOCKET] Received manual file index refresh request")
				go func() {
					if err := svc.fuzzyMatcher.IndexFiles(); err != nil {
						log.Printf("Manual file index refresh failed: %v", err)
					} else {
						log.Printf("Manual file index refresh completed: %d files", svc.fuzzyMatcher.GetFileCount())
					}
				}()
				continue
			}

			// Handle fuzzy search
			if clientMsg.Type == "fuzzy_search" {
				log.Printf("[WEBSOCKET] Received fuzzy search query: %s", clientMsg.Query)

				maxResults := clientMsg.MaxResults
				if maxResults <= 0 {
					maxResults = 50 // Default limit
				}

				// Perform fuzzy search
				results := svc.fuzzyMatcher.Search(clientMsg.Query, maxResults)

				// Send results back to client
				response := ChatItem{
					Type: "fuzzy_search_results",
				}

				// Convert results to JSON
				if jsonData, err := json.Marshal(results); err == nil {
					response.Content = string(jsonData)
				} else {
					log.Printf("[ERROR] Failed to marshal fuzzy search results: %v", err)
					response.Content = "[]" // Empty results
				}

				// Send directly to this client only
				if err := websocket.JSON.Send(client.conn, response); err != nil {
					log.Printf("Error sending fuzzy search results: %v", err)
				}
				continue
			}

			// Handle permission response
			if clientMsg.Type == "permission_response" {
				log.Printf("[WEBSOCKET] Received permission response")
				// Update client's allowed tools
				client.processMutex.Lock()
				pendingTool := client.pendingToolPermission
				client.allowedTools = clientMsg.AllowedTools
				client.skipPermissions = clientMsg.SkipPermissions
				client.pendingToolPermission = "" // Clear pending tool
				client.processMutex.Unlock()

				// Check if the pending tool was granted permission
				toolWasAllowed := false
				if pendingTool != "" {
					for _, tool := range clientMsg.AllowedTools {
						if tool == pendingTool {
							toolWasAllowed = true
							break
						}
					}
				}

				// Echo back the permission response as a user message
				var responseText string
				if clientMsg.SkipPermissions {
					responseText = "YOLO"
				} else if toolWasAllowed {
					responseText = "y"
				} else {
					responseText = "n"
				}

				// Broadcast the user sender first
				userItem := ChatItem{
					Type:   "user",
					Sender: "USER",
				}
				svc.BroadcastToSession(userItem, client.browserSessionID)

				// Broadcast the user's response
				contentItem := ChatItem{
					Type:    "content",
					Content: responseText,
				}
				svc.BroadcastToSession(contentItem, client.browserSessionID)

				// Only send continue if permission was granted or skip permissions is enabled
				if toolWasAllowed || clientMsg.SkipPermissions {
					// Send bot sender item to switch back to swe-swe
					botSenderItem := ChatItem{
						Type:   "bot",
						Sender: "swe-swe",
					}
					svc.BroadcastToSession(botSenderItem, client.browserSessionID)

					go func() {
						// Get the saved message to replay
						client.processMutex.Lock()
						messageToReplay := client.lastUserMessage
						killedSessionID := client.lastKilledSessionID
						savedSessionID := client.lastActiveSessionID  // Get the session ID we saved when permission was needed
						client.processMutex.Unlock()
						
						// If we have a saved message, replay it; otherwise send "continue"
						if messageToReplay != "" && killedSessionID != "" {
							// Use the saved session ID to resume where we left off
							// This preserves context instead of starting fresh
							log.Printf("[PERMISSION] Replaying user message with saved session ID after permission granted: %s", savedSessionID)
							tryExecuteWithSessionHistory(ctx, svc, client, messageToReplay, false, clientMsg.AllowedTools, clientMsg.SkipPermissions, savedSessionID)
						} else {
							// Fallback to old behavior if no saved message
							tryExecuteWithSessionHistory(ctx, svc, client, "continue", false, clientMsg.AllowedTools, clientMsg.SkipPermissions, clientMsg.ClaudeSessionID)
						}
					}()
				}
				// If permission was denied, don't send continue - the process has already been terminated
				continue
			}

			// Broadcast the user sender first
			userItem := ChatItem{
				Type:   "user",
				Sender: clientMsg.Sender,
			}
			svc.BroadcastToSession(userItem, client.browserSessionID)

			// Broadcast the user content
			contentItem := ChatItem{
				Type:    "content",
				Content: clientMsg.Content,
			}
			svc.BroadcastToSession(contentItem, client.browserSessionID)

			// If it's from a user, send a streamed response from swe-swe
			if clientMsg.Sender == "USER" {
				// Log that we received a message from user
				log.Printf("[CHAT] Received message from %s: %s", clientMsg.Sender, clientMsg.Content)
				
				// Save the user message for potential replay after permission errors
				client.processMutex.Lock()
				client.lastUserMessage = clientMsg.Content
				client.processMutex.Unlock()

				// First, send the swe-swe bot item
				botSenderItem := ChatItem{
					Type:   "bot",
					Sender: "swe-swe",
				}
				svc.BroadcastToSession(botSenderItem, client.browserSessionID)

				// Execute agent command and stream response
				go func() {
					// Use client's current allowed tools and skip permissions settings
					client.processMutex.Lock()
					allowedTools := client.allowedTools
					skipPermissions := client.skipPermissions
					// Check both hasStartedSession and FirstMessage flag from frontend
					isFirstMessage := !client.hasStartedSession && clientMsg.FirstMessage
					client.processMutex.Unlock()
					tryExecuteWithSessionHistory(ctx, svc, client, clientMsg.Content, isFirstMessage, allowedTools, skipPermissions, clientMsg.ClaudeSessionID)
				}()
			}
		}
	}
}

// chatWebsocketHandler creates a websocket handler using the go-httphandler pattern
func chatWebsocketHandler(svc *ChatService) httphandler.RequestHandler {
	return func(r *http.Request) httphandler.Responder {
		return httphandler.ResponderFunc(websocketHandler(r.Context(), svc).ServeHTTP)
	}
}
