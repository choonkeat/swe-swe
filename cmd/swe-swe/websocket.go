package main

import (
	"bufio"
	"context"
	"log"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alvinchoong/go-httphandler"
	"golang.org/x/net/websocket"
)

// Client represents a connected websocket client
type Client struct {
	conn         *websocket.Conn
	username     string
	cancelFunc   context.CancelFunc
	processMutex sync.Mutex
}

// ChatItem represents either a sender or content in the chat
type ChatItem struct {
	Type    string `json:"type"`
	Sender  string `json:"sender,omitempty"`
	Content string `json:"content,omitempty"`
}

// ClientMessage represents a message from the client with sender and content
type ClientMessage struct {
	Type         string `json:"type,omitempty"`
	Sender       string `json:"sender,omitempty"`
	Content      string `json:"content,omitempty"`
	FirstMessage bool   `json:"firstMessage,omitempty"`
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
}

// NewChatService creates a new chat service
func NewChatService(agentCLI1st string, agentCLINth string, deferStdinClose bool, jsonOutput bool) *ChatService {
	return &ChatService{
		clients:         make(map[*Client]bool),
		broadcast:       make(chan ChatItem),
		agentCLI1st:     parseAgentCLI(agentCLI1st),
		agentCLINth:     agentCLINth,
		deferStdinClose: deferStdinClose,
		jsonOutput:      jsonOutput,
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

	// Send welcome message using broadcast mechanism for consistent processing
	go func() {
		// Send the swe-swe bot item
		botSenderItem := ChatItem{
			Type:   "bot",
			Sender: "swe-swe",
		}
		s.BroadcastItem(botSenderItem)

		// Send welcome content
		log.Printf("[CHAT] Sending welcome message")
		welcomeMsg := "Welcome to the chat! Type something to start chatting."
		contentItem := ChatItem{
			Type:    "content",
			Content: welcomeMsg,
		}
		s.BroadcastItem(contentItem)
	}()
}

// UnregisterClient removes a client from the service
func (s *ChatService) UnregisterClient(client *Client) {
	s.mutex.Lock()
	delete(s.clients, client)
	s.mutex.Unlock()

	// Cancel any running processes for this client
	client.processMutex.Lock()
	if client.cancelFunc != nil {
		log.Printf("[WEBSOCKET] Client disconnected, cancelling any running processes")
		client.cancelFunc()
		client.cancelFunc = nil
	}
	client.processMutex.Unlock()
}

// BroadcastItem sends a chat item to all clients
func (s *ChatService) BroadcastItem(item ChatItem) {
	s.broadcast <- item
}

// parseAgentCLI parses the agent CLI string into a command slice
func parseAgentCLI(agentCLIStr string) []string {
	return strings.Fields(agentCLIStr)
}

// executeAgentCommand executes the configured agent command with the given prompt and streams the output
func executeAgentCommand(svc *ChatService, client *Client, prompt string, isFirstMessage bool) {
	// Create a context that can be cancelled when the client disconnects
	ctx, cancel := context.WithCancel(context.Background())

	// Store the cancel function in the client for later use
	client.processMutex.Lock()
	if client.cancelFunc != nil {
		// Cancel any existing process
		log.Printf("[WEBSOCKET] Terminating existing agent process before starting new one")
		client.cancelFunc()
		// Give the process a moment to terminate gracefully
		client.processMutex.Unlock()
		// Sleep briefly to allow the previous goroutine to finish
		time.Sleep(100 * time.Millisecond)
		client.processMutex.Lock()
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

	// Create stdin pipe
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("[ERROR] Failed to create stdin pipe: %v", err)
	}

	// If no placeholder, write prompt to stdin
	if stdin != nil && !hasPlaceholder {
		go func() {
			defer stdin.Close()
			_, err := stdin.Write([]byte(prompt + "\n"))
			if err != nil {
				log.Printf("[ERROR] Failed to write to stdin: %v", err)
			}
			log.Printf("[EXEC] Wrote prompt to stdin")
		}()
	} else if stdin != nil {
		if svc.deferStdinClose {
			// Defer closing stdin (for goose)
			defer stdin.Close()
		} else {
			// Close stdin immediately to signal EOF (for claude)
			stdin.Close()
		}
	}

	// Get stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[ERROR] Failed to create stdout pipe: %v", err)
		svc.BroadcastItem(ChatItem{
			Type:    "content",
			Content: "Error creating command pipe: " + err.Error(),
		})
		return
	}

	// Get stderr pipe
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[ERROR] Failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		log.Printf("[ERROR] Failed to start command: %v", err)
		svc.BroadcastItem(ChatItem{
			Type:    "content",
			Content: "Error starting agent command: " + err.Error(),
		})
		return
	}

	log.Printf("[EXEC] Process started with PID: %d", cmd.Process.Pid)

	// Send exec start event
	svc.BroadcastItem(ChatItem{
		Type: "exec_start",
	})

	// Handle stderr in a separate goroutine
	if stderr != nil {
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				log.Printf("[STDERR] %s", line)
			}
			if err := scanner.Err(); err != nil {
				log.Printf("[ERROR] Error reading stderr: %v", err)
			}
		}()
	}

	// Stream the output line by line
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			// Context cancelled, stop processing
			log.Printf("[EXEC] Process cancelled by context")
			svc.BroadcastItem(ChatItem{
				Type:    "content",
				Content: "\n[Process stopped by user]\n",
			})
			// Send exec end event to hide typing indicator
			svc.BroadcastItem(ChatItem{
				Type: "exec_end",
			})
			return
		default:
			line := scanner.Text()
			if line != "" {
				log.Printf("[STDOUT] %s", line)

				// Handle JSON output if enabled
				if svc.jsonOutput {
					// Send raw JSON to Elm for parsing
					svc.BroadcastItem(ChatItem{
						Type:    "claudejson",
						Content: line,
					})
				} else {
					// Regular text output
					svc.BroadcastItem(ChatItem{
						Type:    "content",
						Content: line + "\n",
					})
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
			svc.BroadcastItem(ChatItem{
				Type: "exec_end",
			})
			return
		default:
			log.Printf("[ERROR] Command completed with error: %v", err)
			svc.BroadcastItem(ChatItem{
				Type:    "content",
				Content: "Command completed with error: " + err.Error(),
			})
		}
	} else {
		log.Printf("[EXEC] Process completed successfully")
	}

	// Check for scanning errors
	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			// Send exec end event to hide typing indicator
			svc.BroadcastItem(ChatItem{
				Type: "exec_end",
			})
			return
		default:
			svc.BroadcastItem(ChatItem{
				Type:    "content",
				Content: "Error reading command output: " + err.Error(),
			})
		}
	}

	// Clear the cancel function when done
	client.processMutex.Lock()
	client.cancelFunc = nil
	client.processMutex.Unlock()

	// Send exec end event
	svc.BroadcastItem(ChatItem{
		Type: "exec_end",
	})
}

// websocketHandler handles websocket connections
func websocketHandler(svc *ChatService) websocket.Handler {
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

			// Handle stop command
			if clientMsg.Type == "stop" {
				log.Printf("[WEBSOCKET] Received stop command from client")
				client.processMutex.Lock()
				if client.cancelFunc != nil {
					log.Printf("[WEBSOCKET] Cancelling running process")
					client.cancelFunc()
					client.cancelFunc = nil
				}
				client.processMutex.Unlock()
				continue
			}

			// Broadcast the user sender first
			userItem := ChatItem{
				Type:   "user",
				Sender: clientMsg.Sender,
			}
			svc.BroadcastItem(userItem)

			// Broadcast the user content
			contentItem := ChatItem{
				Type:    "content",
				Content: clientMsg.Content,
			}
			svc.BroadcastItem(contentItem)

			// If it's from a user, send a streamed response from swe-swe
			if clientMsg.Sender == "USER" {
				// Log that we received a message from user
				log.Printf("[CHAT] Received message from %s: %s", clientMsg.Sender, clientMsg.Content)

				// First, send the swe-swe bot item
				botSenderItem := ChatItem{
					Type:   "bot",
					Sender: "swe-swe",
				}
				svc.BroadcastItem(botSenderItem)

				// Execute agent command and stream response
				go func() {
					executeAgentCommand(svc, client, clientMsg.Content, clientMsg.FirstMessage)
				}()
			}
		}
	}
}

// chatWebsocketHandler creates a websocket handler using the go-httphandler pattern
func chatWebsocketHandler(svc *ChatService) httphandler.RequestHandler {
	return func(r *http.Request) httphandler.Responder {
		return httphandler.ResponderFunc(websocketHandler(svc).ServeHTTP)
	}
}
