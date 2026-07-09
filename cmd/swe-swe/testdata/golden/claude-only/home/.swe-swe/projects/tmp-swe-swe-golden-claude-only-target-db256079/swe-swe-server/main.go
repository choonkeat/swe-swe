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
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
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
	"github.com/choonkeat/swe-swe/forkconvo"
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
var forkConfirmTemplate *template.Template

// forkConfirmData feeds the GET /api/fork confirm page. When Error is non-empty
// the source could not be validated and the Fork button is suppressed.
type forkConfirmData struct {
	SourceUUID string
	SourceName string
	Assistant  string
	Bubble     string
	Mode       string
	Error      string
	// InitSha is the fork source repo's init-commit SHA. The confirm page
	// emits it so the client can look up this repo's env-vars blob in
	// localStorage ((origin, init_sha) key, same as the settings panel /
	// new-session dialog) and attach it to the fork POST -- so the forked
	// session inherits the repo env vars. Empty when the workdir is not a git
	// repo (or its worktree is gone); the client then simply attaches nothing.
	InitSha string
	// OfferForkWhole adds a "Fork the whole session instead" button to the
	// error modal. Set when a per-bubble fork failed but a whole-session fork
	// (no bubble anchor -> last-persisted-reply) would still succeed.
	OfferForkWhole bool
	// OfferForkBeforeActive adds a "Fork before that tool use" button to the
	// error modal. Set when the active-tail guard fired (agent mid-tool-call).
	// The button POSTs with bypass_active=1, which skips the guard and forks
	// at the last chat reply -- the point BEFORE the in-flight tool_use --
	// letting the user escape a session that is actually dead/stuck.
	OfferForkBeforeActive bool
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Reject cross-site WebSocket handshakes (CSWSH). Same-host, the tunnel
	// apex, and "{port}.{apex}" per-port subdomains are allowed; see
	// checkWebSocketOrigin in auth.go.
	CheckOrigin: checkWebSocketOrigin,
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
	filesPortStart     = 9000
	filesPortEnd       = 9019
	proxyPortOffset    = 20000
)

func previewProxyPort(port int) int   { return proxyPortOffset + port }
func agentChatProxyPort(port int) int { return proxyPortOffset + port }
func cdpProxyPort(port int) int       { return proxyPortOffset + port }
func vncProxyPort(port int) int       { return proxyPortOffset + port }
func filesProxyPort(port int) int     { return proxyPortOffset + port }

// flagPassed reports whether the user explicitly passed a flag on the
// command line. Used to distinguish "flag at default value" from "flag
// not given at all" so an env-var override can win over the default
// without clobbering a user's explicit flag.
func flagPassed(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

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
	SlashCmdNone SlashCommandFormat = ""     // No commands (Shell, Goose, Aider)
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
	WorkDir     string // working directory; omit if the default workspaceDir
	ParentUUID  string // parent session UUID (shell sub-sessions)
	Debug       bool   // debug UI flag
	ExtraArgs   string // extra CLI flags appended to the agent command (optional)
	// CheckoutBranch is the branch the session's WorkDir was on at start,
	// captured even when no worktree branch was requested. It is the prefill
	// fallback for a recording's "+ New" (see PrefillBranch) and is deliberately
	// NOT part of Encode() so restart/session URLs stay unchanged.
	CheckoutBranch string
}

// RepoRoot maps the session's working directory back to the repo checkout the
// New Session dialog lists in its Where dropdown (the inverse of
// resolveWorkingDirectory): a worktree workdir belongs to its main repo, so
// /worktrees/<b> -> default workspace and /repos/{name}/worktrees/<b> ->
// /repos/{name}/workspace. Empty string means the default workspace (the
// dialog's "workspace" option). Used by recordings' "+ New" prefill.
func (q SessionPageQuery) RepoRoot() string {
	if q.WorkDir == "" {
		return ""
	}
	workDir := filepath.Clean(q.WorkDir)
	if workDir == workspaceDir || strings.HasPrefix(workDir, worktreeDir+"/") {
		return ""
	}
	if isWorktreeWorkDir(workDir) {
		return filepath.Join(filepath.Dir(filepath.Dir(workDir)), "workspace")
	}
	return workDir
}

// PrefillBranch is the branch a recording's "+ New" button should pre-fill into
// the New Session dialog: the explicit worktree branch when one was used, else
// the branch the session's checkout was actually on. Kept separate from
// BranchName/Encode() so only the prefill -- not restart URLs -- gains the
// checkout fallback.
func (q SessionPageQuery) PrefillBranch() string {
	if q.BranchName != "" {
		return q.BranchName
	}
	return q.CheckoutBranch
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
	if q.WorkDir != "" && q.WorkDir != workspaceDir {
		v.Set("pwd", q.WorkDir)
	}
	if q.ParentUUID != "" {
		v.Set("parent", q.ParentUUID)
	}
	if q.Debug {
		v.Set("debug", "1")
	}
	if q.ExtraArgs != "" {
		v.Set("extra_args", q.ExtraArgs)
	}
	return template.URL(v.Encode())
}

type SessionInfo struct {
	UUID          string
	UUIDShort     string
	ClientCount   int
	CreatedAt     time.Time
	DurationStr   string // human-readable duration (e.g., "5m", "1h 23m")
	PublicPort    int    // PUBLIC_PORT env var value (e.g. 5000)
	Query         SessionPageQuery
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
	Assistant AssistantConfig
	Sessions  []SessionInfo // sorted by CreatedAt desc (most recent first)
}

// RecordingMetadata stores information about a terminal recording session
type RecordingMetadata struct {
	UUID          string     `json:"uuid"`
	Name          string     `json:"name,omitempty"`
	Agent         string     `json:"agent"`
	AgentBinary   string     `json:"agent_binary,omitempty"`   // binary name for URLs (e.g. "claude"); empty in old recordings
	RecordingType string     `json:"recording_type,omitempty"` // "agent", "chat", "terminal"
	SessionMode   string     `json:"session_mode,omitempty"`   // "terminal" or "chat"
	BranchName    string     `json:"branch_name,omitempty"`    // git branch / worktree name (set only when a worktree branch was requested)
	// CheckoutBranch is the branch the session's WorkDir was actually on at
	// start. Unlike BranchName it is captured even when no worktree branch was
	// passed (default-workspace / dogfood sessions), so a recording's "+ New"
	// can prefill the branch that otherwise survives only inside the display name.
	CheckoutBranch string    `json:"checkout_branch,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	KeptAt        *time.Time `json:"kept_at,omitempty"` // When user marked this recording to keep (nil = recent, auto-deletable)
	Command       []string   `json:"command"`
	Visitors      []Visitor  `json:"visitors,omitempty"`
	MaxCols       uint16     `json:"max_cols,omitempty"`      // Max terminal columns during recording
	MaxRows       uint16     `json:"max_rows,omitempty"`      // Max terminal rows during recording
	PlaybackCols  uint16     `json:"playback_cols,omitempty"` // Content-based cols for playback (calculated at session end)
	PlaybackRows  uint32     `json:"playback_rows,omitempty"` // Content-based rows for playback (calculated at session end)
	WorkDir       string     `json:"work_dir,omitempty"`      // Working directory for VS Code links in playback
	ExtraArgs     string     `json:"extra_args,omitempty"`    // Extra CLI flags appended to the agent command (for restart)
	SummaryLine   string     `json:"summary_line,omitempty"`  // Cached one-line summary extracted from .log tail before compression (avoids per-request gzip decompression on the homepage)
	// AgentSessionID is the agent-side session id (e.g. Claude's .jsonl filename
	// stem, codex's rollout id) captured at session spawn so /api/fork can
	// locate the exact source conversation without resorting to mtime guesses.
	// Older recordings predate this field and leave it empty; fork falls back
	// to the legacy lookup in that case.
	AgentSessionID string `json:"agent_session_id,omitempty"`
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
		SlashCmdFormat:  SlashCmdNone,
	},
	{
		Name:            "Aider",
		ShellCmd:        "aider",
		ShellRestartCmd: "aider --restore-chat-history",
		YoloShellCmd:    "aider --yes-always",
		YoloRestartCmd:  "aider --yes-always --restore-chat-history",
		Binary:          "aider",
		Homepage:        true,
		SlashCmdFormat:  SlashCmdNone,
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
		Name:            "Pi",
		ShellCmd:        "pi",
		ShellRestartCmd: "pi --continue",
		YoloShellCmd:    "", // YOLO mode not supported
		YoloRestartCmd:  "", // YOLO mode not supported
		Binary:          "pi",
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
	ExtraArgs       string // Extra CLI flags appended to the agent command (for restart)
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
	metadataMu      sync.Mutex         // serializes saveMetadata writes (atomic temp+rename below isn't enough on its own)
	// Parent session relationship
	ParentUUID     string // UUID of parent session (for shell sessions opened from agent sessions)
	PreviewPort    int    // App preview target port for this session
	AgentChatPort  int    // Agent chat MCP server port for this session
	PublicPort     int    // Public (no-auth) port for this session
	CDPPort        int    // Chrome DevTools Protocol port for this session
	VNCPort        int    // VNC port for browser view for this session
	FilesPort      int    // Files (md-serve) port for this session
	BrowserPIDs    []int         // PIDs of browser processes (Xvfb, Chromium, x11vnc, noVNC)
	BrowserDataDir string        // Per-session Chromium user data directory
	BrowserProcs   *browserProcs // Full handle incl. the CDP forwarder server
	BrowserStarted bool   // Whether browser processes have been started
	// RemoteBrowserID is the session id returned by a remote browser-backend
	// (empty in local mode); set when -agent-view points at a backend URL.
	RemoteBrowserID string
	// RemoteVNCTarget is "host:port" of the remote websockify when Agent View
	// runs on a remote backend; the per-session VNC reverse proxy targets it
	// instead of localhost. Empty in local mode.
	RemoteVNCTarget string
	// RemoteCDPProxyServer is a local reverse proxy on sess.CDPPort forwarding
	// to the remote chromium's CDP endpoint, so the agent's Playwright MCP
	// (--cdp-endpoint http://localhost:CDPPort) works unchanged in remote mode.
	RemoteCDPProxyServer *http.Server
	FilesPID             int // PID of the per-session md-serve process (0 if not started)
	// YOLO mode state
	yoloMode           bool   // Whether YOLO mode is active
	pendingReplacement string // If set, replace process with this command instead of ending session
	// UI theme at session creation (for COLORFGBG env var)
	Theme string // "light" or "dark"
	// SharePassword, when non-empty, is the password a shared-session guest
	// types to log in scoped to THIS session (see session_share.go). It lives
	// only in memory, so it dies when the session ends -- that is the whole
	// revocation model. Guarded by mu.
	SharePassword string
	// Agent Chat sidecar (nil for terminal-only sessions)
	AgentChatCmd    *exec.Cmd
	agentChatCancel context.CancelFunc // cancels sessionCtx (stops sidecar watcher)
	// MCP-less mode: the mcp-cli-proxy processes swe-swe-server launched for this
	// session (nil in native-MCP mode). Killed on session teardown.
	McpLessProxies []*exec.Cmd
	SessionMode     string             // "terminal" or "chat"
	ChatLogPath     string             // AGENT_CHAT_EVENT_LOG path for this session (chat mode only)
	AgentSessionID  string             // agent-side conversation id (e.g. Claude .jsonl stem); captured at spawn for /api/fork
	// Per-session preview proxy (hosted in swe-swe-server, not a separate process)
	PreviewProxy         *agentproxy.Proxy // Per-session preview proxy instance
	SessionMux           http.Handler      // Handles /proxy/{uuid}/preview/ AND /proxy/{uuid}/agentchat/
	PreviewProxyServer   *http.Server      // Per-port listener for preview proxy (port-based mode)
	AgentChatProxyServer *http.Server      // Per-port listener for agent chat proxy (port-based mode)
	VNCProxyServer       *http.Server      // Per-port listener for vnc proxy (auth-checked websockify reverse proxy)
	FilesProxyServer     *http.Server      // Per-port listener for files proxy (auth-checked md-serve reverse proxy)
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
	SID           string // session UUID; used to scope GIT_CONFIG_GLOBAL per-session
}

func buildSessionEnv(p SessionEnvParams) []string {
	env := filterEnv(os.Environ(), "TERM", "PORT", "BROWSER", "PATH", "COLORFGBG", "AGENT_CHAT_PORT", "AGENT_CHAT_DISABLE", "PUBLIC_PORT", "BROWSER_CDP_PORT", "BROWSER_VNC_PORT", "GH_TOKEN", "GITLAB_TOKEN")
	env = append(env,
		"TERM=xterm-256color",
		fmt.Sprintf("PORT=%d", p.PreviewPort),
		fmt.Sprintf("AGENT_CHAT_PORT=%d", p.AgentChatPort),
		fmt.Sprintf("PUBLIC_PORT=%d", p.PublicPort),
		fmt.Sprintf("BROWSER_CDP_PORT=%d", p.CDPPort),
		fmt.Sprintf("BROWSER_VNC_PORT=%d", p.VNCPort),
		"BROWSER="+filepath.Join(sweHomeDir, "bin", "swe-swe-open"),
		"PATH="+filepath.Join(workspaceDir, ".swe-swe", "proxy")+":"+filepath.Join(sweHomeDir, "proxy")+":"+filepath.Join(sweHomeDir, "bin")+":"+os.Getenv("PATH"),
		// Wire the per-session credential helper into git for HTTPS remotes.
		// The helper (git-credential-swe-swe) dials @swe-swe-broker, which
		// resolves the calling session via SO_PEERCRED + ancestry walk and
		// returns whatever credentials the user stored for the host. Empty
		// store -> helper emits nothing -> git falls back to its normal
		// prompt or the next configured helper.
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=swe-swe",
	)
	// Per-session GIT_CONFIG_GLOBAL: a sid-scoped gitconfig file that
	// includes the user's ~/.gitconfig as a baseline and overrides
	// [user] name/email when the session has a saved author identity.
	// Server-side writes (by writeSessionGitconfig) take effect on the
	// next git invocation in the session, no shell restart needed.
	if p.SID != "" {
		path, err := ensureSessionGitconfig(p.SID, p.WorkDir)
		if err != nil {
			log.Printf("Session %s: ensureSessionGitconfig failed: %v (per-session author identity disabled)", p.SID, err)
		} else {
			env = append(env, "GIT_CONFIG_GLOBAL="+path)
		}
	}
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
	// Surface the session's stored per-host HTTPS tokens under the conventional
	// CLI env var names (github.com -> GH_TOKEN, gitlab.com/gitlab.* ->
	// GITLAB_TOKEN) so tools like prctx pick them up without re-entry. Env is
	// materialized at spawn, so a token saved mid-session reaches the NEXT
	// session (or the next PTY restart), not the running process. Placed before
	// the .swe-swe/env load so a user-defined GH_TOKEN/GITLAB_TOKEN wins.
	env = append(env, sessionTokenEnv(p.SID)...)
	// Repo env vars saved via the Settings panel (in-memory, per session).
	// Reserved keys (PATH, GH_TOKEN, GIT_CONFIG_*, ports...) are dropped so
	// the textarea can't break the credential broker or proxies. Placed
	// before the .swe-swe/env file load so the checked-in file wins any
	// collision. $VAR expands against the session env built above.
	if p.SID != "" {
		kept, dropped := sessionEnvVars(p.SID, envLookup(env))
		if len(dropped) > 0 {
			log.Printf("Session %s: repo env vars dropped reserved keys: %v", p.SID, dropped)
		}
		env = append(env, kept...)
	}
	// Append user-defined vars from .swe-swe/env (last so they take precedence).
	// Expand $VAR references against the session env built above, so a line like
	// PATH=/usr/local/go/bin:$PATH prepends to the SESSION PATH (which includes
	// /home/app/.swe-swe/bin) rather than the server's PATH (which does not).
	// Without this, such a line would silently drop the swe-swe PATH prefixes,
	// causing `agent-chat`, `agent-whiteboard`, etc. to resolve to the wrong
	// binary (or fail to resolve) and MCP servers to fail to start.
	if p.WorkDir != "" {
		env = append(env, loadEnvFile(filepath.Join(p.WorkDir, ".swe-swe", "env"), envLookup(env))...)
	}
	return env
}

// envLookup returns a function that looks up a key in the given KEY=VALUE
// slice, falling back to os.Getenv if not found. Used to expand $VAR
// references in .swe-swe/env against the session env being built, not the
// server's env.
func envLookup(env []string) func(string) string {
	return func(k string) string {
		prefix := k + "="
		// Iterate in reverse so later (appended) values override earlier ones,
		// matching the semantics of exec: the last KEY=VALUE wins.
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				return env[i][len(prefix):]
			}
		}
		return os.Getenv(k)
	}
}

func loadEnvFile(path string, lookup func(string) string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseEnvLines(string(data), lookup, nil, nil)
}

// parseEnvLines parses KEY=VALUE lines from raw text, skipping blanks and
// #-comments and expanding $VAR references in each value against earlier
// lines then `lookup` (nil -> os.Getenv). If drop is non-nil and returns
// true for a (trimmed) key, that line is skipped entirely -- not stored,
// not available for later expansion -- and the key is appended to *dropped.
// Shared by loadEnvFile (.swe-swe/env, no drops) and the repo env-vars
// store (drops reserved keys) so both parse identically.
func parseEnvLines(raw string, lookup func(string) string, drop func(string) bool, dropped *[]string) []string {
	if lookup == nil {
		lookup = os.Getenv
	}
	var entries []string
	local := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok {
			if drop != nil && drop(strings.TrimSpace(key)) {
				if dropped != nil {
					*dropped = append(*dropped, strings.TrimSpace(key))
				}
				continue
			}
			val = os.Expand(val, func(k string) string {
				if v, ok := local[k]; ok {
					return v
				}
				return lookup(k)
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

// WriteInput writes data directly to the session PTY.
func (s *Session) WriteInput(data []byte) error {
	_, err := s.PTY.Write(data)
	return err
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

// buildStatusPayload returns the map sent over the WebSocket as a session
// status frame. Pure function over Session state plus the WS-client count
// passed in (caller already holds s.mu); extracted from BroadcastStatus so
// unit tests can assert the JSON shape without spinning up real clients.
func (s *Session) buildStatusPayload(viewers int, rows, cols uint16) map[string]interface{} {
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
		"viewers":            viewers,
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
		"filesProxyPort":     filesProxyPort(s.FilesPort),
		"yoloMode":           s.yoloMode,
		"yoloSupported":      s.AssistantConfig.YoloRestartCmd != "",
		"browserStarted":     s.BrowserStarted,
		"agentViewAvailable": agentViewAvailable(),
		"publicHostname":     getLiveTunnelHostname(),
	}
	if agentChatPort != 0 {
		status["agentChatProxyPort"] = agentChatProxyPort(agentChatPort)
	}
	// tunnelStatus rides along when the tunnel supervisor has
	// observed at least one event. State="" means no supervisor or
	// pre-startup -- the frontend treats that as "not in tunnel mode."
	if ts := getLiveTunnelStatus(); ts.State != "" {
		status["tunnelStatus"] = ts
	}
	return status
}

// BroadcastStatus sends current session status (viewers, PTY size, assistant) to all clients
func (s *Session) BroadcastStatus() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, cols := s.calculateMinSize()
	status := s.buildStatusPayload(len(s.wsClients), rows, cols)

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

// BroadcastJSON marshals v and sends it as a text frame to every connected
// client. Used for idempotent state pushes (e.g. session_cred_state) so
// co-viewers do not go stale when one browser changes credential/signing
// state. The payload must be a control message the frontend tolerates as
// a duplicate -- ordering is not guaranteed, only eventual consistency.
func (s *Session) BroadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("BroadcastJSON marshal error: %v", err)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastJSON write error: %v", err)
		}
	}
}

// effectiveWorkDir returns the session's working directory, falling back
// to the server's cwd (matching buildStatusPayload). WorkDir is set at
// session creation and never mutated, so this needs no lock.
func (s *Session) effectiveWorkDir() string {
	if s.WorkDir != "" {
		return s.WorkDir
	}
	wd, _ := os.Getwd()
	return wd
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

	// Include worktree info if session is in a worktree (default repo's
	// /worktrees/<x> or an external repo's /repos/{name}/worktrees/<x>).
	if isWorktreeWorkDir(s.WorkDir) && s.BranchName != "" {
		msg["worktree"] = map[string]string{
			"path":         s.WorkDir,
			"branch":       s.BranchName,
			"targetBranch": getMainRepoBranch(),
		}
	}

	return msg
}

// isWorktreeWorkDir reports whether workDir is a swe-swe-managed git worktree
// checkout -- it sits directly inside a "worktrees" directory. This covers both
// the default repo's /worktrees/<branch> and an external repo's
// /repos/{name}/worktrees/<branch>. Branch names are flattened by
// worktreeDirName (slashes -> "--"), so a worktree dir is always a single
// segment whose parent is literally "worktrees".
func isWorktreeWorkDir(workDir string) bool {
	if workDir == "" {
		return false
	}
	return filepath.Base(filepath.Dir(filepath.Clean(workDir))) == "worktrees"
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

	// MCP-less mode: kill this session's mcp-cli-proxy fleet. These are children
	// of swe-swe-server (not the agent's process group), so killSessionProcessGroup
	// below does not reach them -- stop them explicitly.
	stopMcpLessFleet(s.McpLessProxies)

	// Shut down per-port proxy servers
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if s.PreviewProxyServer != nil {
		s.PreviewProxyServer.Shutdown(shutdownCtx)
	}
	if s.AgentChatProxyServer != nil {
		s.AgentChatProxyServer.Shutdown(shutdownCtx)
	}
	if s.VNCProxyServer != nil {
		s.VNCProxyServer.Shutdown(shutdownCtx)
	}
	if s.FilesProxyServer != nil {
		s.FilesProxyServer.Shutdown(shutdownCtx)
	}

	// Stop the session's Agent View backend (local stack or remote allocation)
	stopSessionAgentView(s)

	// Stop the per-session md-serve (Files tab)
	stopSessionMdServe(s)

	// Close all WebSocket client connections
	for conn := range s.wsClients {
		conn.Close()
	}
	s.wsClients = make(map[*SafeConn]bool)

	// Kill the full process tree (including children in different PGIDs,
	// e.g. claude creates its own process group).  Reuse the same
	// descendant-aware kill that endSessionByUUID uses.
	s.mu.Unlock()
	killSessionProcessGroup(s)
	s.mu.Lock()
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
		oldPID := s.Cmd.Process.Pid
		s.Cmd.Wait()
		untrackPid(oldPID)
		unregisterSessionPid(oldPID)
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
		SID:           s.UUID,
	})
	// Mirror the initial-spawn env (see createSession): SESSION_UUID and the
	// per-session MCP key must survive a restart, or the in-container shims
	// (open/xdg-open -> preview proxy open endpoint) break afterwards.
	// issueSessionKey is idempotent, so this returns the same key.
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("SESSION_UUID=%s", s.UUID),
		fmt.Sprintf("MCP_AUTH_KEY=%s", issueSessionKey(s.UUID)),
	)
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

	trackPid(cmd.Process.Pid)
	registerSessionPid(cmd.Process.Pid, s.UUID)
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
					ptyPID := 0
					if cmd.Process != nil {
						ptyPID = cmd.Process.Pid
					}
					if err := cmd.Wait(); err != nil {
						// Wait returns error for non-zero exit
						if exitErr, ok := err.(*exec.ExitError); ok {
							exitCode = exitErr.ExitCode()
						}
					}
					untrackPid(ptyPID)
					unregisterSessionPid(ptyPID)
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
	sessions   = make(map[string]*Session)
	sessionsMu sync.RWMutex

	// pendingSessions holds creation intents staged by POST /api/session
	// (kind "new") and POST /api/fork (kind "fork") that have not yet been
	// materialized into a live session. The first WebSocket client to open the
	// new session URL consumes the entry, calls getOrCreateSession from inside
	// the WS handler, and -- because the UUID is brand new at that point --
	// gets isNew=true. This is important: it ensures the PTY starts after the
	// client's geometry is known, so claude renders its first frame at the
	// right size and a joining-client snapshot is never needed.
	//
	// It is also the load-bearing invariant of the no-ghost-session design: a
	// session materializes ONLY when its UUID has a staged intent here (or is
	// already live). A bare GET/navigation/WS-reconnect to an unknown UUID
	// must NOT create a session.
	pendingSessions   = make(map[string]stagedSession)
	pendingSessionsMu sync.Mutex

	shellCmd            string
	shellRestartCmd     string
	workingDir          string
	availableAssistants []AssistantConfig // Populated at startup by detectAvailableAssistants

	// SSL certificate download endpoint
	tlsCertPath string // Path to TLS certificate file

	// serverCtx is cancelled on SIGINT/SIGTERM for graceful shutdown.
	// Session contexts derive from this so all processes are cleaned up.
	serverCtx context.Context
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
	// addr defaults to empty so resolveListenAddr can distinguish "user passed
	// --addr" from "fell through to default".  The Dockerfile CMD always
	// passes -addr explicitly, so compose mode is unaffected.
	addr := flag.String("addr", "", "Listen address (overrides SWE_PORT/PORT; default :1977)")
	bind := flag.String("bind", "",
		"Listen address (host:port). Overrides --addr and SWE_PORT/PORT. "+
			"Env: SWE_BIND. In tunnel mode, recommend 127.0.0.1:1977 so only "+
			"the localhost tunnel client can reach swe-swe-server.")
	version := flag.Bool("version", false, "Show version and exit")
	dumpTemplates := flag.String("dump-container-templates", "", "Dump all container templates to directory and exit")
	flag.StringVar(&shellCmd, "shell", "claude", "Command to execute")
	flag.StringVar(&shellRestartCmd, "shell-restart", "claude --continue", "Command to restart on process death")
	flag.StringVar(&workingDir, "working-directory", "", "Working directory for shell (defaults to current directory)")
	tsAuthKey := flag.String("tailscale-authkey", "", "Tailscale auth key (env: TS_AUTHKEY); when set, tailscaled is spawned and joined to the tailnet")
	tsHostname := flag.String("tailscale-hostname", "", "Tailscale hostname to advertise (env: TS_HOSTNAME)")
	tsStateDir := flag.String("tailscale-state-dir", "", "Tailscale state directory (env: TS_STATE_DIR; default /var/lib/tailscale)")
	tsDisable := flag.Bool("tailscale-disable", false, "Disable Tailscale bootstrap even if TS_AUTHKEY is set (env: TS_DISABLE=1)")
	tunnelServerURL := flag.String("tunnel-server-url", "",
		"Tunnel server URL (e.g. https://tunnel.example.com). When set, "+
			"swe-swe-server spawns the swe-swe-tunnel client as a child "+
			"process and consumes its lifecycle events on stdout to learn "+
			"the assigned public hostname. Empty disables tunnel mode. "+
			"Env: SWE_TUNNEL_SERVER_URL.")
	tunnelUnique := flag.String("tunnel-unique", "",
		"Bare unique label for the tunnel registration (server appends "+
			"-tunnel suffix). Optional; empty falls through to whatever "+
			"the tunnel client picks itself (typically a generated label "+
			"or one persisted in its identity store). Env: SWE_TUNNEL_UNIQUE.")
	tunnelBin := flag.String("tunnel-bin", "swe-swe-tunnel",
		"Path to the swe-swe-tunnel binary. Defaults to swe-swe-tunnel "+
			"on $PATH. Env: SWE_TUNNEL_BIN.")
	tunnelClientCert := flag.String("tunnel-client-cert", "",
		"Path to a PEM-encoded mTLS client certificate to present to "+
			"the tunnel server. Empty means no client cert; the agent "+
			"then connects without one and a daemon running with "+
			"--mtls-ca will reject the handshake. Env: "+
			"SWE_TUNNEL_CLIENT_CERT.")
	workspaceFlag := flag.String("workspace", "",
		"Main repo directory the agent operates in. Default /workspace "+
			"(container). Env: SWE_WORKSPACE_DIR. Set for host-native runs.")
	worktreesFlag := flag.String("worktrees", "",
		"Directory holding per-session git worktrees. Default /worktrees. "+
			"Env: SWE_WORKTREES_DIR.")
	reposFlag := flag.String("repos", "",
		"Directory for external repo clones. Default /repos. "+
			"Env: SWE_REPOS_DIR.")
	sweHomeFlag := flag.String("swe-home", "",
		"The .swe-swe home holding per-session proxy/ and bin/ (helpers + "+
			"swe-swe-open shim). Default /home/app/.swe-swe. Env: SWE_HOME_DIR.")
	agentView := flag.String("agent-view", "local",
		"Agent View backend: local (in-process display stack) | off (hide the "+
			"tab) | <backend-url> (offload to a swe-swe/browser-backend). "+
			"Env: SWE_AGENT_VIEW.")
	mode := flag.String("mode", "server",
		"server (default) | browser-backend (run the standalone Agent View "+
			"allocation service instead of the swe-swe UI server).")
	browserBackendMax := flag.Int("browser-backend-max", 0,
		"browser-backend mode: max concurrent browser sessions (0 = auto from "+
			"the VNC port range).")
	browserBackendHost := flag.String("browser-backend-host", "",
		"browser-backend mode: hostname clients should dial for the CDP/VNC "+
			"ports (env: SWE_BROWSER_BACKEND_HOST).")
	flag.Parse()

	// Resolve the Agent View backend (flag -> env -> default "local"). On a
	// lean host with no display stack, local mode reports the tab unavailable
	// rather than 500ing on browser/start.
	resolveAgentViewBackend(*agentView, flagPassed("agent-view"))

	// Resolve host paths: flag -> env -> default. Defaults reproduce the
	// container layout, so compose mode is unchanged; dockerless `swe-swe up`
	// passes -workspace/-swe-home (etc.) for host-native paths.
	workspaceDir = firstNonEmpty(*workspaceFlag, os.Getenv("SWE_WORKSPACE_DIR"), workspaceDir)
	worktreeDir = firstNonEmpty(*worktreesFlag, os.Getenv("SWE_WORKTREES_DIR"), worktreeDir)
	reposDir = firstNonEmpty(*reposFlag, os.Getenv("SWE_REPOS_DIR"), reposDir)
	sweHomeDir = firstNonEmpty(*sweHomeFlag, os.Getenv("SWE_HOME_DIR"), sweHomeDir)
	recordingsDir = filepath.Join(workspaceDir, ".swe-swe", "recordings")

	// Resolve the swe-swe-server bind early so the tunnel supervisor can
	// log the correct OPEN AT URL ({port}.{hostname}) when it learns the
	// tunnel hostname; tunneld demuxes by the same port.
	listenAddr, landingAddr := resolveListenAddr(*bind, *addr, os.Getenv("SWE_BIND"), os.Getenv("SWE_PORT"), os.Getenv("PORT"))

	// Override CDP port range from environment (set by docker-compose).
	// Parsed BEFORE the browser-backend dispatch: the allocation service
	// hands these exact ports to clients, so ignoring the env there meant
	// the container's published/documented ranges silently did not apply.
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

	// Override VNC port range from environment (set by docker-compose).
	// Same before-dispatch requirement as SWE_CDP_PORTS above.
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

	// browser-backend mode: run the standalone Agent View allocation service
	// instead of the UI server. Same binary + same display stack, exposed over
	// the network for lean (dockerless) hosts to offload Agent View to.
	if *mode == "browser-backend" {
		host := firstNonEmpty(*browserBackendHost, os.Getenv("SWE_BROWSER_BACKEND_HOST"), "")
		token := os.Getenv("SWE_BROWSER_BACKEND_TOKEN")
		log.Fatal(runBrowserBackend(listenAddr, *browserBackendMax, token, host))
	}

	// Tunnel-mode subprocess supervisor. Trigger is non-empty
	// --tunnel-server-url (or its env equivalent). When set, spawn the
	// swe-swe-tunnel client as a child process and consume its JSONL
	// event stream. Companion plan:
	// /workspace/tasks/2026-04-29-tunnel-subprocess-pivot.md.
	resolvedTunnelServerURL := *tunnelServerURL
	if envURL, ok := os.LookupEnv("SWE_TUNNEL_SERVER_URL"); ok && !flagPassed("tunnel-server-url") {
		resolvedTunnelServerURL = envURL
	}
	resolvedTunnelUnique := *tunnelUnique
	if envU, ok := os.LookupEnv("SWE_TUNNEL_UNIQUE"); ok && !flagPassed("tunnel-unique") {
		resolvedTunnelUnique = envU
	}
	resolvedTunnelBin := *tunnelBin
	if envBin, ok := os.LookupEnv("SWE_TUNNEL_BIN"); ok && !flagPassed("tunnel-bin") {
		resolvedTunnelBin = envBin
	}
	resolvedTunnelClientCert := *tunnelClientCert
	if envCert, ok := os.LookupEnv("SWE_TUNNEL_CLIENT_CERT"); ok && !flagPassed("tunnel-client-cert") {
		resolvedTunnelClientCert = envCert
	}
	if resolvedTunnelServerURL != "" {
		go runTunnelSupervisor(context.Background(), tunnelSupervisorOpts{
			ServerURL:      resolvedTunnelServerURL,
			Unique:         resolvedTunnelUnique,
			BinPath:        resolvedTunnelBin,
			LocalAddr:      listenAddr,
			ClientCertPath: resolvedTunnelClientCert,
		})
	}

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

	// (SWE_CDP_PORTS / SWE_VNC_PORTS are parsed earlier, before the
	// browser-backend mode dispatch.)

	// Override proxy port offset from environment (set by docker-compose / .env)
	if offsetStr := os.Getenv("SWE_PROXY_PORT_OFFSET"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			proxyPortOffset = v
		}
	}

	// MCP auth keys are issued per session (see mcp_authkey.go), so there is
	// no global orchestration key to generate at boot.

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
	// Baseline parent identity at boot. Paired with the shutdown log, this
	// makes a graceful "exit 0" attributable to whoever forwarded the signal.
	log.Printf("[STARTUP] pid=%d launched by %s", os.Getpid(), describeParentProcess())

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

	forkConfirmContent, err := pageTemplatesFS.ReadFile("page-templates/fork-confirm.html")
	if err != nil {
		log.Fatal(err)
	}
	forkConfirmTemplate, err = template.New("fork-confirm").Parse(string(forkConfirmContent))
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

	// One-shot recovery scan: rename any unparseable metadata.json to
	// .corrupt so they stop hiding their recordings on the homepage. Also
	// reaps stale .tmp files left by an interrupted atomic write.
	quarantineCorruptMetadata()

	// Start session reaper and compression worker
	go sessionReaper()
	go compressionWorker()
	go pendingSessionSweeper()

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
	// mcpAuthMiddleware authenticates by the per-session key and injects the
	// resolved caller session UUID into the request context, so create_session
	// can inherit the calling session's git credentials (see mcp_authkey.go).
	http.Handle("/mcp", mcpAuthMiddleware(orchHandler))

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
				assistant     string
				index         int
				recordingUUID string
				sessionMode   string
				sess          *Session
				pid           int // root PID for memory tracking
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
						ExtraArgs:   sess.ExtraArgs,
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

			// Load recordings (sorted by timestamp) for the page-level recordings list.
			recordings := loadEndedRecordings()

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
				agents = append(agents, AgentWithSessions{
					Assistant: assistant,
					Sessions:  sessionsByAssistant[assistant.Binary], // nil if no sessions
				})
			}

			// Check if SSL certificate is available
			_, hasSSLCert := os.Stat(tlsCertPath)

			// Get the workspace origin URL for the default repo
			defaultRepoUrl, err := getWorkspaceOriginURL()
			if err != nil {
				// Fallback to /workspace if we can't get origin URL
				defaultRepoUrl = workspaceDir
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

		// Session share-link endpoint: POST /api/session/{uuid}/share ->
		// {url, password} for boxing a guest into just this session.
		if strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/share") {
			handleSessionShareAPI(w, r)
			return
		}

		// New-session staging endpoint: POST /api/session/new -> 302 /session/{new-uuid}.
		// Stages a "new" creation intent so the WS handler is permitted to
		// materialize the session. Must be checked before the generic
		// "/api/session/" + suffix routes below.
		if r.URL.Path == "/api/session/new" {
			handleNewSessionAPI(w, r)
			return
		}

		// Session fork API endpoint:
		//   GET  /api/fork/{source-uuid} -> skeleton confirm page (no side effects)
		//   POST /api/fork/{source-uuid} -> fork + 302 /session/{new-uuid}
		if strings.HasPrefix(r.URL.Path, "/api/fork/") {
			handleSessionForkAPI(w, r)
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

		// Files (md-serve) readiness probe -- same rationale as vnc-ready.
		if strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/files-ready") {
			handleFilesReadyAPI(w, r)
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
			var localUserName, localUserEmail, sessionWorkDir string
			if exists {
				localUserName, localUserEmail = readLocalGitUser(existingSession.WorkDir)
				sessionWorkDir = existingSession.WorkDir
			} else {
				// Session not yet created (e.g. "+ New" from a recording link).
				// The pwd query param carries the workdir if non-default.
				sessionWorkDir = r.URL.Query().Get("pwd")
			}
			// Empty WorkDir means server cwd (see Session.WorkDir comment).
			// Resolve so it matches the dialog's canonical whereKey (data.path).
			if sessionWorkDir == "" {
				if cwd, err := os.Getwd(); err == nil {
					sessionWorkDir = cwd
				}
			}
			// Init-commit SHA gives the browser a repo-identity it can bind
			// stored signing keys to (auto-restore only fires when the
			// browser's (origin, init_sha) trust entry matches the current
			// session). Empty for non-git workdirs or empty repos.
			initSHA := repoInitSHA(sessionWorkDir)
			// Local-repo signing overrides: any key set in <workdir>/.git/config
			// will silently win over the per-session GIT_CONFIG_GLOBAL we
			// write. Surfaced in the SSH Signing tab so users can fix the
			// trap (typically a host-leftover `gpg.format = openpgp`) before
			// they hit "gpg failed to sign" on commit.
			localGPGOverrides := readLocalSigningOverrides(sessionWorkDir)
			// Host of the workdir's origin remote, used to autofill the
			// Git HTTPS Host field so a non-github forge's stored creds
			// apply without the user switching Host first.
			localRemoteHost := readLocalRemoteHost(sessionWorkDir)
			data := struct {
				UUID              string
				UUIDShort         string
				Assistant         string
				AssistantName     string
				Version           string
				LocalUserName     string
				LocalUserEmail    string
				WhereKey          string
				InitSHA           string
				LocalGPGOverrides string
				LocalRemoteHost   string
			}{
				UUID:              sessionUUID,
				UUIDShort:         uuidShort,
				Assistant:         assistant,
				AssistantName:     assistantName,
				Version:           Version + "-" + GitCommit,
				LocalUserName:     localUserName,
				LocalUserEmail:    localUserEmail,
				WhereKey:          sessionWorkDir,
				InitSHA:           initSHA,
				LocalGPGOverrides: localGPGOverrides,
				LocalRemoteHost:   localRemoteHost,
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

	// listenAddr/landingAddr were resolved earlier so the tunnel supervisor
	// can log the correct OPEN AT URL; see tailscale.go for the decision rule.
	tsCfg := resolveTailscaleConfig(*tsAuthKey, *tsHostname, *tsStateDir, *tsDisable)

	log.Printf("swe-swe-server v%s", Version)
	log.Printf("Starting server on %s", listenAddr)
	log.Printf("  shell: %s", shellCmd)
	if shellRestartCmd != shellCmd {
		log.Printf("  shell-restart: %s", shellRestartCmd)
	}
	if workingDir != "" {
		log.Printf("  working-directory: %s", workingDir)
	}
	if landingAddr != "" {
		log.Printf("  landing server: %s", landingAddr)
	}

	// Start signal monitor and heartbeat for crash forensics
	startSignalMonitor()
	startHeartbeat()
	startSubreaper()
	startBrokerListener()

	// Signal-aware shutdown: cancel serverCtx on SIGINT/SIGTERM.
	// We use an explicit signal.Notify channel rather than
	// signal.NotifyContext so the shutdown log can name WHICH signal
	// fired (NotifyContext cancels the context but hides the signal).
	var serverCancel context.CancelFunc
	serverCtx, serverCancel = context.WithCancel(context.Background())
	defer serverCancel()
	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	// Tailscale bootstrap -- dormant unless TS_AUTHKEY is set.
	startTailscale(serverCtx, tsCfg)

	// Landing/health server on $PORT, if separate from the swe-swe listener.
	startLandingServer(serverCtx, landingAddr, listenAddr, tsCfg)

	// Set up embedded auth if SWE_SWE_PASSWORD is set (dockerfile-only mode).
	// In compose mode, Traefik + auth service handle authentication externally.
	var handler http.Handler
	if authPassword := os.Getenv("SWE_SWE_PASSWORD"); authPassword != "" {
		handler = setupEmbeddedAuth(authPassword)
		log.Printf("Embedded auth enabled (SWE_SWE_PASSWORD set)")
	}

	srv := &http.Server{Addr: listenAddr, Handler: handler}
	go func() {
		defer recoverGoroutine("shutdown handler")
		sig := <-shutdownSig
		// Restore default disposition so a second SIGINT/SIGTERM force-
		// terminates instead of hanging on a wedged graceful shutdown.
		signal.Stop(shutdownSig)
		// Propagate cancellation to tailscale, the landing server, and all
		// session child contexts derived from serverCtx.
		serverCancel()
		// Reaching here means SIGINT/SIGTERM was delivered from outside this
		// process (the only path to a graceful exit 0). Name the signal and
		// log the parent so an unexplained exit can be attributed (su ->
		// docker stop forwarded; init/orchestrator -> platform).
		log.Printf("Shutting down server (received signal %v) -- parent %s", sig, describeParentProcess())
		// Close all sessions in parallel.  Each Session.Close routes through
		// killSessionProcessGroup which has a SIGTERM grace period of up to
		// 3 seconds; closing serially under sessionsMu would gate every
		// session on the previous one and turn shutdown into N*3 seconds.
		sessionsMu.Lock()
		toClose := make([]*Session, 0, len(sessions))
		for uuid, sess := range sessions {
			log.Printf("Closing session %s on shutdown", uuid)
			toClose = append(toClose, sess)
			delete(sessions, uuid)
		}
		sessionsMu.Unlock()
		var closeWG sync.WaitGroup
		for _, sess := range toClose {
			closeWG.Add(1)
			go func(s *Session) {
				defer closeWG.Done()
				defer recoverGoroutine(fmt.Sprintf("shutdown close session %s", s.UUID))
				s.Close()
			}(sess)
		}
		closeWG.Wait()
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

// workspaceDir is the main repo directory the agent operates in. It defaults
// to the container path /workspace and is overridable via -workspace (env
// SWE_WORKSPACE_DIR) for host-native (dockerless) runs where the repo lives
// at an arbitrary host path.
var workspaceDir = "/workspace"

// sweHomeDir is the .swe-swe home that holds the per-session proxy/ and bin/
// dirs (credential/signing helpers + the swe-swe-open shim). Defaults to the
// container path /home/app/.swe-swe; overridable via -swe-home (env
// SWE_HOME_DIR) so a dockerless run can point at the dumped project bin/.
// (firstNonEmpty, used to resolve flag -> env -> default, lives in tailscale.go.)
var sweHomeDir = "/home/app/.swe-swe"

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

		// Skip the root entry. fs.WalkDir invokes the walker with
		// path == "container-templates" (no trailing slash) for the root,
		// which TrimPrefix below leaves unchanged. Without this guard the
		// walker would create an empty <destDir>/container-templates/ wrapper.
		if path == "container-templates" {
			return nil
		}

		relPath := strings.TrimPrefix(path, "container-templates/")
		if relPath == "" || relPath == "." {
			return nil // skip root (defensive: handled above)
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
// Used to provision swe-swe files (.swe-swe/docs/*)
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

	// One-shot migration: swe-swe/env -> .swe-swe/env. Earlier versions of
	// swe-swe stored per-workspace env vars in swe-swe/env (which violated the
	// "swe-swe/ is for @-mentionable agent commands only" convention). The new
	// home is .swe-swe/env. If only the old path exists, rename it in place so
	// existing workspaces self-heal on the next session prepare.
	oldEnvPath := filepath.Join(destDir, "swe-swe", "env")
	newEnvPath := filepath.Join(destDir, ".swe-swe", "env")
	if _, err := os.Stat(newEnvPath); os.IsNotExist(err) {
		if _, err := os.Stat(oldEnvPath); err == nil {
			if mkErr := os.MkdirAll(filepath.Dir(newEnvPath), 0755); mkErr == nil {
				if rnErr := os.Rename(oldEnvPath, newEnvPath); rnErr == nil {
					log.Printf("Migrated env file: %s -> %s", oldEnvPath, newEnvPath)
				} else {
					log.Printf("Warning: failed to migrate %s -> %s: %v", oldEnvPath, newEnvPath, rnErr)
				}
			}
		}
	}

	// Clean up legacy swe-swe/ directory. Older versions created swe-swe/setup
	// and swe-swe/env in workspaces. The scaffolding has been removed and env
	// migrated above, so remove the directory if it exists.
	legacyDir := filepath.Join(destDir, "swe-swe")
	if _, err := os.Stat(legacyDir); err == nil {
		if err := os.RemoveAll(legacyDir); err != nil {
			log.Printf("Warning: failed to remove legacy swe-swe/ directory: %v", err)
		} else {
			log.Printf("Removed legacy swe-swe/ directory: %s", legacyDir)
		}
	}

	return fs.WalkDir(containerTemplatesFS, "container-templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root entry. fs.WalkDir invokes the walker with
		// path == "container-templates" (no trailing slash) for the root,
		// which TrimPrefix below leaves unchanged. Without this guard the
		// walker would create an empty <destDir>/container-templates/ wrapper.
		if path == "container-templates" {
			return nil
		}

		relPath := strings.TrimPrefix(path, "container-templates/")
		if relPath == "" || relPath == "." {
			return nil // skip root (defensive: handled above)
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

// isTrackedInGit checks if a file is tracked in git
// Returns true if the file is tracked, false otherwise
func isTrackedInGit(repoDir, relativePath string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", relativePath)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

// ensureSweSweFiles symlinks swe-swe files from the base repo into a worktree.
// Processes: dotfiles (except .git), CLAUDE.md, and AGENTS.md.
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
		} else if name == "CLAUDE.md" || name == "AGENTS.md" {
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

// agentContextIncludeLine is upserted into the assistant's primary context file
// so every agent is pointed at the standard environment documentation.
const agentContextIncludeLine = "See .swe-swe/docs/AGENTS.md (if it exists) for context of this current environment"

// upsertAgentDocInclude appends agentContextIncludeLine to the assistant's primary
// context file (CLAUDE.md for claude, AGENTS.md for other agents) if it's not already
// present. No-op for shell/custom/unknown assistants or an empty workDir.
func upsertAgentDocInclude(workDir, assistant string) error {
	if workDir == "" {
		return nil
	}
	var target string
	switch assistant {
	case "claude":
		target = "CLAUDE.md"
	case "gemini", "codex", "aider", "goose", "opencode":
		target = "AGENTS.md"
	default:
		return nil
	}

	path := filepath.Join(workDir, target)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == agentContextIncludeLine {
			return nil
		}
	}

	var b bytes.Buffer
	b.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString(agentContextIncludeLine)
	b.WriteByte('\n')

	if err := os.WriteFile(path, b.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
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
	cmd := exec.Command("git", "-C", workspaceDir, "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// worktreeExists checks if a worktree directory already exists for the given
// branch name in the DEFAULT repo (/worktrees). This is intentionally scoped to
// the default repo: its only caller (handleWorktreeCheckAPI) receives a session
// name with no repo context, and the same branch name in a different external
// repo is a legitimately distinct worktree -- treating it as a conflict here
// would produce false positives. See listWorktrees for the cross-repo view.
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

// listWorktrees returns a list of existing worktree directories across every
// repo: the default repo's /worktrees plus each external repo's
// /repos/{name}/worktrees. Path disambiguates worktrees that share a branch
// name across different repos.
func listWorktrees() ([]WorktreeInfo, error) {
	// Collect every worktrees container. The default repo's dedicated
	// /worktrees, then one per external repo at /repos/{name}/worktrees.
	containers := []string{worktreeDir}
	if repoEntries, err := os.ReadDir(reposDir); err == nil {
		for _, re := range repoEntries {
			if re.IsDir() {
				containers = append(containers, filepath.Join(reposDir, re.Name(), "worktrees"))
			}
		}
	}

	worktrees := []WorktreeInfo{}
	for _, container := range containers {
		entries, err := os.ReadDir(container)
		if err != nil {
			continue // container may not exist yet
		}
		for _, entry := range entries {
			if entry.IsDir() {
				// Convert directory name back to branch name (e.g., "style--foo" -> "style/foo")
				worktrees = append(worktrees, WorktreeInfo{
					Name: branchNameFromDir(entry.Name()),
					Path: filepath.Join(container, entry.Name()),
				})
			}
		}
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
	cmd := exec.Command("git", "-C", workspaceDir, "remote", "get-url", "origin")
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

// renameSession validates and applies a new display name to sess: persists
// metadata, broadcasts status, and propagates the rename to child shell
// sessions as "<name> (Terminal)". Shared by every rename path (browser WS
// rename_session, MCP set_session_name) so validation and child propagation
// never diverge.
func renameSession(sess *Session, name string) error {
	name = strings.TrimSpace(name)
	// Validate: max 256 chars, alphanumeric + spaces + hyphens + underscores + slashes + dots + @
	if len(name) > 256 {
		return fmt.Errorf("name too long (%d chars, max 256)", len(name))
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_' || r == '/' || r == '.' || r == '@') {
			return fmt.Errorf("invalid character %q in name %q", r, name)
		}
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
	return nil
}

// isWorkspaceRepo checks if the given URL matches the /workspace repo's origin
func isWorkspaceRepo(repoURL string) bool {
	// Normalize input
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return false
	}

	// Check if it's a local path that is /workspace
	if repoURL == workspaceDir || repoURL == workspaceDir+"/" {
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
		// Optional HTTPS credentials for a private clone. Never persisted
		// server-side beyond the transient broker context of the clone call.
		CredHost     string `json:"credHost"`
		CredUsername string `json:"credUsername"`
		CredToken    string `json:"credToken"`
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
		handleRepoPrepareClone(w, req.URL, req.CredHost, req.CredUsername, req.CredToken)
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
	workDir := workspaceDir
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

	// Check if .swe-swe/env exists
	if _, err := os.Stat(filepath.Join(workDir, ".swe-swe", "env")); err == nil {
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

	// Report whether a remote exists, but never fetch here: prepare must
	// respond instantly so the dialog unblocks. The dialog freshens remote
	// refs afterwards via /api/repo/branches?fetch=1 in the background.
	remoteCmd := exec.Command("git", "-C", workDir, "remote")
	remoteOutput, err := remoteCmd.Output()
	response["hasRemote"] = err == nil && len(strings.TrimSpace(string(remoteOutput))) > 0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRepoPrepareClone handles the clone mode - clone external URL (hard fail).
// credHost/credUsername/credToken are optional HTTPS credentials wired through
// the broker for the duration of the clone (private repos); all empty ->
// bare clone (public repos). Never embeds credentials in the URL.
func handleRepoPrepareClone(w http.ResponseWriter, url, credHost, credUsername, credToken string) {
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

	justCloned := false

	// Check if already cloned
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		// Already cloned, fetch instead (but still hard fail for clone mode)
		log.Printf("Repository already exists at %s, fetching", repoPath)
		output, err := runGitWithTransientCred(credHost, credUsername, credToken, "-C", repoPath, "fetch", "--all")
		if err != nil {
			log.Printf("Git fetch failed: %v, output: %s", err, string(output))
			if cloneNeedsAuth(string(output)) {
				writeCloneAuthNeeded(w, credHost)
				return
			}
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

		output, err := runGitWithTransientCred(credHost, credUsername, credToken, "clone", url, repoPath)
		if err != nil {
			log.Printf("Git clone failed: %v, output: %s", err, string(output))
			if cloneNeedsAuth(string(output)) {
				writeCloneAuthNeeded(w, credHost)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Git clone failed: %s", string(output))})
			return
		}
		justCloned = true
	}

	// Set up swe-swe files (.swe-swe/docs/*) and clean up legacy .mcp.json
	if err := setupSweSweFiles(repoPath); err != nil {
		log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
	}

	remoteOutput, remoteErr := exec.Command("git", "-C", repoPath, "remote").Output()

	resp := map[string]interface{}{
		"path":        repoPath,
		"isWorkspace": false,
		// justCloned lets the dialog skip the redundant background fetch: a
		// fresh clone already has all refs local.
		"justCloned": justCloned,
		"hasRemote":  remoteErr == nil && len(strings.TrimSpace(string(remoteOutput))) > 0,
		// initSha binds the browser's autosync-trust entry to this exact repo
		// so a supplied PAT auto-restores in the new session without a second
		// "trust this device?" prompt.
		"initSha": repoInitSHA(repoPath),
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".swe-swe", "env")); err == nil {
		resp["hasEnvFile"] = true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeCloneAuthNeeded emits the structured "the clone needs HTTPS auth"
// response the dialog uses to reveal/rescue credential fields.
func writeCloneAuthNeeded(w http.ResponseWriter, host string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"needsAuth": true,
		"host":      host,
		"error":     "Authentication required for this repository.",
	})
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

	// Set up swe-swe files (.swe-swe/docs/*) and clean up legacy .mcp.json
	if err := setupSweSweFiles(repoPath); err != nil {
		log.Printf("Warning: failed to setup swe-swe files in %s: %v", repoPath, err)
	}

	resp := map[string]interface{}{
		"path":        repoPath,
		"isWorkspace": false,
		"isNew":       true,
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".swe-swe", "env")); err == nil {
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
		repoPath = workspaceDir
	}

	// Security check: only allow /workspace or /repos/* paths
	if repoPath != workspaceDir && !strings.HasPrefix(repoPath, reposDir+"/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid repository path"})
		return
	}

	// Clean path to prevent traversal
	repoPath = filepath.Clean(repoPath)

	// fetch=1: freshen remote refs before listing. Soft-fail -- a failed
	// fetch still returns the cached branch list, plus a warning. The dialog
	// calls this in the background after the instant no-fetch listing.
	warning := ""
	if r.URL.Query().Get("fetch") == "1" {
		remoteOutput, err := exec.Command("git", "-C", repoPath, "remote").Output()
		if err == nil && len(strings.TrimSpace(string(remoteOutput))) > 0 {
			log.Printf("Fetching all for %s", repoPath)
			if out, err := exec.Command("git", "-C", repoPath, "fetch", "--all").CombinedOutput(); err != nil {
				log.Printf("Git fetch failed (continuing with cached): %v, output: %s", err, string(out))
				warning = "Unable to fetch latest changes. Using cached branches."
			}
		} else {
			log.Printf("No remote configured for %s, skipping fetch", repoPath)
		}
	}

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

	branchesResponse := map[string]interface{}{
		"branches": branches,
		// init_sha lets the new-session dialog locate this repo's env-vars blob
		// in localStorage (keyed by (origin, init_sha), same as the terminal-ui
		// settings panel) so it can attach it to the creation POST. Empty for a
		// non-git or shallow-history path -- the dialog then just skips env.
		"init_sha": repoInitSHA(repoPath),
	}
	if warning != "" {
		branchesResponse["warning"] = warning
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(branchesResponse)
}

// resolveWorkingDirectory calculates the working directory for a session
// based on repoPath and optional branchName.
//   - Branch blank: return repoPath (no worktree)
//   - /workspace (or any of its worktrees) + branch: /worktrees/{branch}
//   - External /repos/{name}/workspace (or its worktrees) + branch:
//     /repos/{name}/worktrees/{branch}
//
// Worktrees are always anchored off the MAIN repo, never nested under whatever
// checkout we were launched from. Launching a session from /worktrees/<x>
// (itself a worktree of /workspace) must create siblings under /worktrees, not
// /worktrees/worktrees/<branch>; likewise an external-repo worktree must not
// nest a second "worktrees" level. Each repo keeps its own worktrees container
// so the same branch name in different repos never shares a physical directory.
func resolveWorkingDirectory(repoPath, branchName string) string {
	if branchName == "" {
		return repoPath
	}

	dirName := worktreeDirName(branchName)

	// Default repo: /workspace and all of its worktrees (/worktrees/<x>) share
	// the dedicated /worktrees directory.
	if repoPath == workspaceDir || strings.HasPrefix(repoPath, worktreeDir+"/") {
		return filepath.Join(worktreeDir, dirName)
	}

	// repoPath is itself an external-repo worktree (its parent dir is named
	// "worktrees", e.g. /repos/{name}/worktrees/<x>): reuse that same container
	// instead of nesting another level.
	if filepath.Base(filepath.Dir(repoPath)) == "worktrees" {
		return filepath.Join(filepath.Dir(repoPath), dirName)
	}

	// External main checkout (/repos/{name}/workspace): worktrees live in a
	// sibling /repos/{name}/worktrees directory.
	return filepath.Join(filepath.Dir(repoPath), "worktrees", dirName)
}

// createWorktreeInRepo creates a worktree for a specific repo
// Supports both /workspace and external repos at different paths
func createWorktreeInRepo(repoPath, branchName string) (string, error) {
	if branchName == "" {
		return repoPath, nil
	}

	// If the requested branch is already the one checked out in this repo, run
	// directly in the repo instead of `git worktree add`-ing a branch git
	// considers already checked out (which fails with "already checked out at
	// ..."). This is exactly what a recording's "+ New" produces when the branch
	// it recovered from the checkout is still the repo's current branch.
	if cur, err := getCurrentBranch(repoPath); err == nil && cur == branchName {
		log.Printf("Branch %s is already checked out at %s; running in place", branchName, repoPath)
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
		// Two-phase: collect exited sessions under the lock, then call
		// Close() outside it.  killSessionProcessGroup can block for up
		// to 3 seconds (SIGTERM grace), and holding sessionsMu that long
		// would stall every session list/create request.
		sessionsMu.Lock()
		var toReap []*Session
		for uuid, sess := range sessions {
			// Only clean up sessions where the process has exited
			if sess.Cmd != nil && sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
				log.Printf("Session cleaned up (process exited): %s", uuid)
				toReap = append(toReap, sess)
				delete(sessions, uuid)
			}
		}
		sessionsMu.Unlock()
		for _, s := range toReap {
			s.Close()
		}

		// Clean up old recent recordings
		cleanupRecentRecordings()
	}
}

// Constants for recording cleanup
const (
	recentRecordingMaxAge = 14 * 24 * time.Hour
)

// compressCh receives .log file paths that should be gzip-compressed.
// Both endSessionByUUID (prompt) and the scheduler (safety net) send paths
// through this channel; a single compressionWorker goroutine consumes it,
// ensuring no concurrent compression of the same file.
var compressCh = make(chan string, 256)

// cleanupRecentRecordings compresses ended session logs and deletes old recordings.
// Compression: .log files from ended sessions are gzip-compressed to .log.gz, then
// the original .log is removed. This runs lazily (every minute via sessionReaper)
// rather than at session end, avoiding delays during endSession.
// Expiry: recordings without KeptAt are deleted recentRecordingMaxAge after EndedAt.
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

	// Compress .log files from ended sessions to .log.gz
	compressEndedSessionLogs(entries, activeRecordings)

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

// compressEndedSessionLogs enqueues .log files from ended sessions for compression.
// Skips active sessions and files that already have a .log.gz counterpart.
// Actual compression is performed by compressionWorker via compressCh.
func compressEndedSessionLogs(entries []os.DirEntry, activeRecordings map[string]bool) {
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		// Skip .log.gz files (they end with .log but also match .log.gz -- handled by HasSuffix)
		if strings.HasSuffix(name, ".log.gz") {
			continue
		}

		stem := strings.TrimPrefix(name, "session-")
		stem = strings.TrimSuffix(stem, ".log")

		parentUUID, _, ok := parseRecordingFilename(stem)
		if !ok {
			continue
		}
		if activeRecordings[parentUUID] {
			continue
		}

		logPath := recordingsDir + "/" + name

		// Skip if already compressed
		gzPath := logPath + ".gz"
		if _, err := os.Stat(gzPath); err == nil {
			os.Remove(logPath)
			continue
		}

		// Enqueue for compression by the worker
		select {
		case compressCh <- logPath:
		default:
			// Channel full; worker will catch it on the next scheduler tick
		}
	}
}

// compressFileGzip compresses src to dst using gzip. Writes to a temp file first
// to avoid partial .log.gz files if interrupted.
func compressFileGzip(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		gz.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dst)
}

// compressionWorker is the single consumer of compressCh. It compresses each
// .log file to .log.gz, then removes the original. Safe to receive duplicates:
// already-compressed or missing files are skipped.
func compressionWorker() {
	for logPath := range compressCh {
		gzPath := logPath + ".gz"

		// Skip if already compressed or source gone
		if _, err := os.Stat(gzPath); err == nil {
			os.Remove(logPath)
			continue
		}
		if _, err := os.Stat(logPath); err != nil {
			continue
		}

		if err := compressFileGzip(logPath, gzPath); err != nil {
			log.Printf("Failed to compress %s: %v", filepath.Base(logPath), err)
			continue
		}
		// Cache one-line summary in metadata.json BEFORE removing the plain .log,
		// so the homepage never needs to decompress .log.gz to render. Only do this
		// for root recordings (no "-child-" segment) -- children don't have their
		// own metadata.json.
		cacheRootRecordingSummary(logPath)
		os.Remove(logPath)
		log.Printf("Compressed recording %s", filepath.Base(logPath))
	}
}

// cacheRootRecordingSummary extracts the tail-of-log summary from a still-uncompressed
// root recording .log file and writes it into the sibling metadata.json under
// "summary_line". Best-effort: any error is logged and ignored. Skips child recordings
// (which encode the parent UUID + "-child-" + child UUID in the filename).
func cacheRootRecordingSummary(logPath string) {
	base := filepath.Base(logPath)
	if !strings.HasPrefix(base, "session-") || !strings.HasSuffix(base, ".log") {
		return
	}
	stem := strings.TrimSuffix(strings.TrimPrefix(base, "session-"), ".log")
	parentUUID, childUUID, ok := parseRecordingFilename(stem)
	if !ok || childUUID != "" {
		return // not a root recording -- children have no metadata.json of their own
	}
	uuidStr := parentUUID

	// Read the last 8 KB of the plain .log via seek (cheap, no decompression).
	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return
	}
	readSize := int64(8192)
	if fi.Size() < readSize {
		readSize = fi.Size()
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, fi.Size()-readSize); err != nil && err != io.EOF {
		return
	}
	terminalSummary := extractSummaryFromBytes(buf)

	metadataPath := recordingsDir + "/session-" + uuidStr + ".metadata.json"
	metaBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return
	}
	// Round-trip through a generic map so we don't drop unknown fields written
	// by older or newer server versions.
	var m map[string]interface{}
	if err := json.Unmarshal(metaBytes, &m); err != nil {
		return
	}

	// Prefer the agent-chat events JSONL if present: it captures the actual
	// last user/agent message, whereas the terminal tail is noisy TUI output.
	// Fall back to any already-cached summary_line, then to the terminal tail.
	var summary string
	if chatSummary, _ := getSessionSummaryFromChat(uuidStr); chatSummary != "" {
		summary = chatSummary
	} else if existing, _ := m["summary_line"].(string); existing != "" {
		return
	} else {
		summary = terminalSummary
	}
	if summary == "" {
		return
	}
	m["summary_line"] = summary
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	tmp := metadataPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return
	}
	if err := os.Rename(tmp, metadataPath); err != nil {
		os.Remove(tmp)
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

func filesPortFromPreview(previewPort int) int {
	return previewPort + 6000
}

// displayNumberFromPreview derives a unique X11 display number from a preview port.
// Preview port 3000 -> DISPLAY=:1, 3001 -> :2, etc.
func displayNumberFromPreview(previewPort int) int {
	return (previewPort - previewPortStart) + 1
}

// startSessionBrowser starts per-session Xvfb, Chromium, x11vnc, and noVNC processes.
// The session gets its own isolated X11 display and browser instance.
// startSessionBrowser brings up the local in-process Agent View stack for a
// session by delegating to the shared startBrowserProcs (see browser_backend.go).
// The x11vnc internal port is offset by the VNC range size so it never collides
// with a session's websockify port.
func startSessionBrowser(sess *Session) error {
	b, err := startBrowserProcs(
		sess.UUID,
		displayNumberFromPreview(sess.PreviewPort),
		sess.CDPPort,
		// Chromium's real loopback CDP port, offset past the range like the
		// x11vnc internal VNC port below.
		sess.CDPPort+(cdpPortEnd-cdpPortStart+1),
		sess.VNCPort,
		sess.VNCPort+(vncPortEnd-vncPortStart+1),
		// Local mode: chromium already shares localhost with the dev server.
		"",
	)
	if err != nil {
		return err
	}
	sess.BrowserPIDs = b.pids
	sess.BrowserDataDir = b.dataDir
	sess.BrowserProcs = b
	sess.BrowserStarted = true
	return nil
}

// stopSessionBrowser kills all browser processes for a session and cleans up
// the per-session Chromium user data directory.
func stopSessionBrowser(sess *Session) {
	// Close the CDP forwarder first so its port frees with the processes.
	if sess.BrowserProcs != nil && sess.BrowserProcs.cdpSrv != nil {
		sess.BrowserProcs.cdpSrv.Close()
		sess.BrowserProcs.cdpSrv = nil
	}
	sess.BrowserProcs = nil
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

// startSessionMdServe launches a per-session md-serve rooted at the session's
// working directory, listening on sess.FilesPort. md-serve renders the workDir
// as a read-only, live-reloading repo browser for the Files tab. The process is
// non-critical: a failure to start is logged but does not abort session creation.
//
// Invoked via `npx -y @choonkeat/md-serve@latest` so the Files tab always gets
// the published latest at session start, rather than whatever version was baked
// into the image at build time.
func startSessionMdServe(sess *Session) error {
	cmd := exec.Command("npx", "-y", "@choonkeat/md-serve@latest",
		"-dir", sess.WorkDir,
		"-addr", fmt.Sprintf(":%d", sess.FilesPort),
	)
	// Put npx + the md-serve child it spawns into their own process group so
	// stopSessionMdServe can kill both with a single kill(-pgid). Without this,
	// SIGKILL on the captured npx PID would orphan md-serve (which then survives
	// as a re-parented child of swe-swe-server, still holding sess.FilesPort).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Use a clean PORT-free environment so md-serve cannot accidentally pick up
	// the server's own PORT and ignore -addr. We still inherit the rest of the
	// environment (PATH for the md-serve binary, etc.) minus any PORT entry.
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PORT=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start md-serve on port %d: %w", sess.FilesPort, err)
	}
	filesPID := cmd.Process.Pid
	trackPid(filesPID)
	registerSessionPid(filesPID, sess.UUID)
	sess.FilesPID = filesPID
	log.Printf("Started md-serve on port %d, dir %s (PID %d) for session %s", sess.FilesPort, sess.WorkDir, filesPID, sess.UUID)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("md-serve wait (PID %d, session %s)", filesPID, sess.UUID))
		defer untrackPid(filesPID)
		err := cmd.Wait()
		if err != nil {
			log.Printf("md-serve exited with error (PID %d, session %s): %v", filesPID, sess.UUID, err)
		} else {
			log.Printf("md-serve exited normally (PID %d, session %s)", filesPID, sess.UUID)
		}
	}()
	return nil
}

// stopSessionMdServe kills the per-session md-serve process group, if one was
// started. We must kill the whole process group (kill(-pgid)) rather than the
// captured PID alone: the captured PID is the npx wrapper, and md-serve runs as
// its child. Killing only the wrapper would leave md-serve orphaned and still
// bound to sess.FilesPort. startSessionMdServe sets Setpgid so pgid == filesPID.
func stopSessionMdServe(sess *Session) {
	if sess.FilesPID == 0 {
		return
	}
	if err := syscall.Kill(-sess.FilesPID, syscall.SIGKILL); err != nil {
		// Process group may have already exited
		if !errors.Is(err, syscall.ESRCH) {
			log.Printf("Failed to kill md-serve process group %d for session %s: %v", sess.FilesPID, sess.UUID, err)
		}
	} else {
		log.Printf("[KILL] Killed md-serve process group %d for session %s (server PID %d)", sess.FilesPID, sess.UUID, os.Getpid())
	}
	sess.FilesPID = 0
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
	RepoPath            string // base repo for worktree creation (required for MCP create_session)
	ParentUUID          string // parent session UUID (for child sessions)
	ParentName          string // parent session name
	ParentRecordingUUID string // parent recording UUID
	Theme               string // terminal theme
	SessionMode         string // "terminal" or "chat"
	ExtraArgs           string // extra CLI flags appended to the agent command (whitespace-split)
	PrepopulateChatLog  string // when non-empty, copy this file into the new session's chat event log before the agent starts (used by /api/fork)
	// InheritCredsFrom names a session whose git credentials/signing this
	// new session should inherit (set by MCP create_session from the
	// authenticated calling session). Distinct from ParentUUID, which also
	// implies terminal-child semantics (shared ports, "terminal" recording);
	// inheritance must NOT trigger those, so it has its own field.
	InheritCredsFrom string
	// EnvRaw is the browser's repo env-vars blob, carried on the creation
	// intent from POST /api/session/new. It is applied to the session's env
	// store BEFORE buildSessionEnv at spawn -- the WS materializes and spawns
	// the PTY before it reads any client set_env frame, so this is the only
	// way the blob reaches a brand-new browser session's process env. Never
	// persisted; memory-only, exactly like set_env.
	EnvRaw string
}

// stagedSession is a creation intent parked in pendingSessions until the first
// WebSocket client materializes it. kind is "new" or "fork" (logging/diagnostics
// only); stagedAt drives TTL eviction by the sweeper. orphanCleanupPath, when
// non-empty, is a file written up front by the staging handler (the forked agent
// rollout .jsonl that forkconvo.Fork creates) that must be removed if the intent
// is evicted unconsumed. It is NEVER the source session's chat log -- only a
// file the fork itself created -- so deleting it on eviction is safe.
type stagedSession struct {
	params            SessionParams
	kind              string // "new" | "fork"
	stagedAt          time.Time
	orphanCleanupPath string
}

// pendingSessionTTL bounds how long an unconsumed creation intent lives. A POST
// that 302s but whose WS never connects (tab closed, network died) would
// otherwise leak the map entry -- and, for forks, the rollout .jsonl that
// forkconvo.Fork already wrote to disk. The sweeper evicts entries older than
// this and cleans up the orphaned file recorded in orphanCleanupPath.
const pendingSessionTTL = 10 * time.Minute

// stageSession parks a creation intent for uuid. kind is "new" or "fork".
// orphanCleanupPath (may be empty) names a file the caller wrote up front that
// the sweeper should delete if this intent is evicted unconsumed -- it must be a
// file the caller itself created, never a shared/source file.
func stageSession(uuid string, params SessionParams, kind, orphanCleanupPath string) {
	pendingSessionsMu.Lock()
	pendingSessions[uuid] = stagedSession{params: params, kind: kind, stagedAt: time.Now(), orphanCleanupPath: orphanCleanupPath}
	pendingSessionsMu.Unlock()
}

// resolveStagedMode returns the effective session mode when materializing a
// staged session: the staged intent's own mode wins, falling back to the mode
// carried on the redirect query (urlMode) when the intent left it unset. The
// "new" staging path historically staged assistant only, so without this
// fallback a "Start Chat" POST would materialize as a terminal session.
func resolveStagedMode(stagedMode, urlMode string) string {
	if stagedMode != "" {
		return stagedMode
	}
	return urlMode
}

// takePendingSession atomically removes and returns the staged intent for uuid,
// if any. ok is false when no intent was staged.
func takePendingSession(uuid string) (stagedSession, bool) {
	pendingSessionsMu.Lock()
	defer pendingSessionsMu.Unlock()
	staged, ok := pendingSessions[uuid]
	if ok {
		delete(pendingSessions, uuid)
	}
	return staged, ok
}

// pendingSessionSweeper evicts creation intents that were never consumed within
// pendingSessionTTL. For forks it also removes the orphaned chat log that
// forkconvo.Fork wrote on the POST, so an abandoned fork doesn't leak a .jsonl.
func pendingSessionSweeper() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		var evicted []stagedSession
		pendingSessionsMu.Lock()
		for uuid, staged := range pendingSessions {
			if now.Sub(staged.stagedAt) > pendingSessionTTL {
				evicted = append(evicted, staged)
				delete(pendingSessions, uuid)
			}
		}
		pendingSessionsMu.Unlock()
		for _, staged := range evicted {
			log.Printf("pendingSessions: evicted stale %s intent %s (age > %s)", staged.kind, staged.params.UUID, pendingSessionTTL)
			// orphanCleanupPath is a file the staging handler itself created
			// (e.g. a forked agent rollout .jsonl). It is never a source/shared
			// file, so removing it on eviction cannot lose another session's data.
			if staged.orphanCleanupPath != "" {
				if err := os.Remove(staged.orphanCleanupPath); err != nil && !os.IsNotExist(err) {
					log.Printf("pendingSessions: failed to remove orphaned file %s: %v", staged.orphanCleanupPath, err)
				}
			}
		}
	}
}

// errSessionGone is returned by getOrCreateSession when a UUID has no live
// session and creation is not permitted (allowCreate=false). It is the
// load-bearing signal of the no-ghost-session invariant: a bare GET /
// navigation / WS-reconnect to an unknown or ended UUID must NOT spawn a
// session. The WS handler translates this into a "session_gone" client message.
var errSessionGone = errors.New("session gone")

// getOrCreateSession returns an existing session, or creates a new one when
// allowCreate is true. When allowCreate is false and no live session exists for
// p.UUID, it returns errSessionGone WITHOUT creating anything. The live-check
// and the create decision happen under a single lock acquisition, so a second
// concurrent WS connect (e.g. after consuming the staged intent) cannot race
// between "is it live?" and "insert it" and spuriously see the session as gone.
func getOrCreateSession(p SessionParams, allowCreate bool) (*Session, bool, error) {
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
			// Fall through to create a new session (only if allowCreate)
		} else {
			return sess, false, nil // existing session
		}
	}

	// No live session for this UUID. Creation requires explicit permission --
	// otherwise this is a stale/ended/bogus UUID and we refuse rather than
	// materialize a ghost session.
	if !allowCreate {
		return nil, false, errSessionGone
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
			baseRepo = workspaceDir
		}

		// If branch is provided, create/use worktree.
		// Previously we silently fell back to baseRepo on failure, which hid the
		// common "branch already checked out at /workspace" case from users --
		// they got a session in /workspace with no indication their worktree
		// request was dropped. Return the error instead so the caller can show it.
		if p.Branch != "" {
			var err error
			workDir, err = createWorktreeInRepo(baseRepo, p.Branch)
			if err != nil {
				return nil, false, fmt.Errorf("worktree for branch %q in %s: %w", p.Branch, baseRepo, err)
			}
		} else {
			// No branch specified, use base repo directly
			workDir = baseRepo
		}
	}

	if err := upsertAgentDocInclude(workDir, p.Assistant); err != nil {
		log.Printf("Warning: failed to upsert agent doc include in %s: %v", workDir, err)
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

	// Create new session with PTY using assistant's shell command, plus any
	// extra CLI flags the user supplied (e.g. --channels server:agent-chat).
	cmdName, cmdArgs := buildAgentArgv(shellCmdToUse, p.ExtraArgs)
	if p.ExtraArgs != "" {
		log.Printf("Session %s: extra CLI args: %s", p.UUID, p.ExtraArgs)
	}

	// Capture the agent's conversation id so /api/fork can locate the source
	// .jsonl reliably. Three paths: id already specified in argv (--resume
	// uuid, codex resume uuid, ...), explicit injection (claude --session-id),
	// or post-spawn filesystem watch (codex, pi). See agent_session_id.go.
	var (
		agentSessionID   string
		agentWatchDir    string
		agentPreSnapshot map[string]struct{}
		unlockAgentSpawn func()
	)
	if p.SessionMode == "chat" {
		agentSessionID = parseKnownAgentSessionID(p.Assistant, cmdArgs)
		if agentSessionID != "" {
			log.Printf("Session %s: agent (%s) session id parsed from argv: %s", p.UUID, p.Assistant, agentSessionID)
		} else {
			var injectedID string
			cmdArgs, injectedID = injectAgentSessionID(p.Assistant, cmdArgs)
			if injectedID != "" {
				agentSessionID = injectedID
				log.Printf("Session %s: injected agent (%s) session id: %s", p.UUID, p.Assistant, injectedID)
			} else if dir := agentSessionDir(p.Assistant, workDir); dir != "" {
				// Need watch-based capture. Hold per-assistant mutex through
				// snapshot+spawn+first-new-file so concurrent codex/pi spawns
				// can't confuse each other.
				unlockAgentSpawn = acquireAgentSpawnLock(p.Assistant)
				agentWatchDir = dir
				agentPreSnapshot = agentSessionFileSnapshot(dir)
			}
		}
	}

	// Wrap with script for recording
	cmdName, cmdArgs = wrapWithScript(cmdName, cmdArgs, recPrefix)
	log.Printf("Recording session to: %s/%s.{log,timing}", recordingsDir, recPrefix)

	// Populate the child's repo env store BEFORE buildSessionEnv reads it --
	// buildSessionEnv bakes the result into cmd.Env, which pty.Start freezes
	// below, so anything not in the store by now never reaches the process.
	// (a) MCP create_session / fork inherit the parent's blob; the credential
	// counterpart, inheritSessionCredentials, is deferred until after session
	// registration because it rewrites the child's gitconfig against the real
	// workDir, but env only needs the two UUIDs, so it must run here, not there.
	// (b) The browser new-session flow stages its blob on the creation intent
	// (SessionParams.EnvRaw), delivered here because the WS materializes and
	// spawns before it ever reads a client set_env frame.
	if p.InheritCredsFrom != "" {
		inheritSessionEnv(p.InheritCredsFrom, p.UUID)
	}
	if p.EnvRaw != "" {
		setSessionEnv(p.UUID, p.EnvRaw)
	}

	env := buildSessionEnv(SessionEnvParams{
		PreviewPort:   previewPort,
		AgentChatPort: acPort,
		PublicPort:    pubPort,
		CDPPort:       cdpPort,
		VNCPort:       vncPort,
		Theme:         p.Theme,
		WorkDir:       workDir,
		SessionMode:   p.SessionMode,
		SID:           p.UUID,
	})
	env = append(env, fmt.Sprintf("SESSION_UUID=%s", p.UUID))
	// Per-session MCP auth key: authenticates this session's agent to the
	// orchestration /mcp endpoint and the per-session HTTP APIs, and lets
	// the server identify the caller (see mcp_authkey.go).
	env = append(env, fmt.Sprintf("MCP_AUTH_KEY=%s", issueSessionKey(p.UUID)))

	// Set up chat event log recording for chat sessions
	var chatRecordingUUID string
	var chatLogPath string
	if p.SessionMode == "chat" {
		chatRecordingUUID = uuid.New().String()
		chatPrefix := recordingPrefix(recordingUUID, chatRecordingUUID)
		chatLogPath = fmt.Sprintf("%s/%s.events.jsonl", recordingsDir, chatPrefix)
		// /api/fork prepopulates the new session's chat event log so the
		// browser tab shows the same history bubbles as the source session
		// before the user types anything.
		if p.PrepopulateChatLog != "" {
			if err := copyFile(p.PrepopulateChatLog, chatLogPath); err != nil {
				return nil, false, fmt.Errorf("prepopulate chat log: %w", err)
			}
			log.Printf("Session %s: prepopulated chat event log from %s", p.UUID, p.PrepopulateChatLog)
		}
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

	// MCP-less mode: swe-swe-server (not the agent's native MCP client) owns the
	// MCP servers. Launch one mcp-cli-proxy per server for this session --
	// agent-chat only for chat sessions -- and point the agent's `mcp` CLI at
	// this session's socket dir via SWE_MCP_DIR. The agent boots with NO MCP
	// config (the entrypoint skips it in mcp-less mode) and reaches every tool
	// through `mcp <server> <tool>` over these sockets.
	var mcpLessProxies []*exec.Cmd
	if mcpLessEnabled() {
		mcpSockDir := filepath.Join(mcpLessSocketRoot, p.UUID)
		env = append(env, "SWE_MCP_DIR="+mcpSockDir)
		var fleetErr error
		mcpLessProxies, fleetErr = launchMcpLessFleet(p.SessionMode, mcpSockDir, env, workDir)
		if fleetErr != nil {
			log.Printf("Session %s: mcp-less fleet launch failed: %v", p.UUID, fleetErr)
		}
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

	trackPid(cmd.Process.Pid)
	registerSessionPid(cmd.Process.Pid, p.UUID)

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	// Capture the branch the working directory is actually on so a recording's
	// "+ New" can prefill it even when no worktree branch was passed. Skip a
	// detached HEAD ("HEAD") -- there's no branch name to reproduce.
	checkoutBranch, _ := getCurrentBranch(workDir)
	if checkoutBranch == "HEAD" {
		checkoutBranch = ""
	}

	now := time.Now()
	sess := &Session{
		UUID:            p.UUID,
		Name:            name,
		BranchName:      p.Branch,
		WorkDir:         workDir,
		ExtraArgs:       p.ExtraArgs,
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
		FilesPort:       filesPortFromPreview(previewPort),
		Theme:           p.Theme,
		yoloMode:        detectYoloMode(shellCmdToUse), // Detect initial YOLO mode from startup command
		AgentChatCmd:    agentChatCmd,
		agentChatCancel: sessionCancel,
		McpLessProxies:  mcpLessProxies,
		SessionMode:     p.SessionMode,
		ChatLogPath:     chatLogPath,
		AgentSessionID:  agentSessionID,
		Metadata: &RecordingMetadata{
			UUID:           recordingUUID,
			Name:           name,
			Agent:          cfg.Name,
			AgentBinary:    cfg.Binary,
			RecordingType:  recType,
			SessionMode:    p.SessionMode,
			BranchName:     p.Branch,
			CheckoutBranch: checkoutBranch,
			StartedAt:      now,
			Command:        append([]string{cmdName}, cmdArgs...),
			MaxCols:        80, // Default starting size
			MaxRows:        24,
			WorkDir:        workDir,
			ExtraArgs:      p.ExtraArgs,
			AgentSessionID: agentSessionID,
		},
	}
	sessions[p.UUID] = sess

	// Inherit git credentials/signing from the authenticated calling session
	// (MCP create_session). Done after the session is registered so the
	// child's gitconfig is rewritten against its real workDir. Repo env vars
	// are NOT inherited here -- they are baked into cmd.Env at spawn, so
	// inheritSessionEnv runs earlier (before buildSessionEnv), not here.
	if p.InheritCredsFrom != "" {
		inheritSessionCredentials(p.InheritCredsFrom, p.UUID, workDir)
	}

	// If the agent session id wasn't known synchronously, watch the agent's
	// session directory for the new file. The per-assistant spawn lock is
	// released by the watcher when it terminates (success or 10s timeout).
	if unlockAgentSpawn != nil {
		go func() {
			defer unlockAgentSpawn()
			captureAgentSessionIDViaWatch(sess, agentWatchDir, agentPreSnapshot)
		}()
	}

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
		// Tunnel mode safety: tunneld dials the per-port listeners directly
		// without Traefik's ForwardAuth in front. Wrap each per-port handler
		// in requireAuthCookie so the apex login cookie is validated before
		// any traffic reaches the upstream. No-op when SWE_SWE_PASSWORD is
		// empty (legacy compose mode where Traefik handles auth externally).
		authPassword := os.Getenv("SWE_SWE_PASSWORD")

		previewPP := previewProxyPort(previewPort)
		previewSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", previewPP),
			Handler: corsWrapper(requireAuthCookie(authPassword, sess.UUID, portPreviewProxy)),
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
			Handler: corsWrapper(requireAuthCookie(authPassword, sess.UUID, agentChatProxyHandler(acTarget))),
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

		// VNC proxy at vncProxyPort (default 27000-27019). Reverse-proxies
		// HTTP + WebSocket upgrade to localhost:vncPort (websockify on
		// 7000-7019). httputil.ReverseProxy supports WS upgrade since Go
		// 1.12, so /websockify, /vnc_lite.html, and the noVNC static assets
		// all flow through the same handler. Auth-wrapped exactly like
		// preview/agent-chat above; in legacy/Traefik mode that wrap is a
		// no-op since SWE_SWE_PASSWORD is empty.
		vncTarget, _ := url.Parse(fmt.Sprintf("http://localhost:%d", vncPort))
		vncReverseProxy := httputil.NewSingleHostReverseProxy(vncTarget)
		// websockify presents itself with its own Host; rewriting the Host
		// header to match the target avoids virtual-host filters and CORS
		// quirks if websockify ever adds them. The target is resolved per
		// request so a remote browser-backend (sess.RemoteVNCTarget, set on
		// browser/start) redirects here without rebuilding the proxy; local
		// mode keeps targeting localhost:vncPort.
		vncReverseProxy.Director = func(req *http.Request) {
			host := vncTarget.Host
			if sess.RemoteVNCTarget != "" {
				host = sess.RemoteVNCTarget
			}
			req.URL.Scheme = "http"
			req.URL.Host = host
			req.Host = host
		}
		vncPP := vncProxyPort(vncPort)
		vncSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", vncPP),
			Handler: requireAuthCookie(authPassword, sess.UUID, vncReverseProxy),
		}
		go func() {
			defer recoverGoroutine(fmt.Sprintf("vnc proxy for session %s", sess.UUID))
			ln, err := net.Listen("tcp", vncSrv.Addr)
			if err != nil {
				log.Printf("Session %s: vnc proxy port %d unavailable: %v", sess.UUID, vncPP, err)
				return
			}
			log.Printf("Session %s: vnc proxy listening on :%d", sess.UUID, vncPP)
			if err := vncSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Printf("Session %s: vnc proxy server error: %v", sess.UUID, err)
			}
		}()
		sess.VNCProxyServer = vncSrv

		// Files proxy at filesProxyPort (default 29000-29019). Plain
		// reverse-proxy to the per-session md-serve on localhost:FilesPort
		// (9000-9019); md-serve renders full pages, so no DebugHub/inject.js
		// machinery is needed. Auth-wrapped exactly like preview/agent-chat
		// above; in legacy/Traefik mode that wrap is a no-op since
		// SWE_SWE_PASSWORD is empty.
		filesTarget, _ := url.Parse(fmt.Sprintf("http://localhost:%d", sess.FilesPort))
		filesReverseProxy := httputil.NewSingleHostReverseProxy(filesTarget)
		filesPP := filesProxyPort(sess.FilesPort)
		filesSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", filesPP),
			Handler: corsWrapper(requireAuthCookie(authPassword, sess.UUID, filesReverseProxy)),
		}
		go func() {
			defer recoverGoroutine(fmt.Sprintf("files proxy for session %s", sess.UUID))
			ln, err := net.Listen("tcp", filesSrv.Addr)
			if err != nil {
				log.Printf("Session %s: files proxy port %d unavailable: %v", sess.UUID, filesPP, err)
				return
			}
			log.Printf("Session %s: files proxy listening on :%d", sess.UUID, filesPP)
			if err := filesSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Printf("Session %s: files proxy server error: %v", sess.UUID, err)
			}
		}()
		sess.FilesProxyServer = filesSrv

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

	// Eagerly start the per-session md-serve for the Files tab. Child sessions
	// inherit the parent's FilesPort, so they share the parent's md-serve (a
	// second instance on the same port would fail to bind). md-serve is
	// non-critical: log a start failure but do not abort session creation.
	if p.ParentUUID == "" {
		if err := startSessionMdServe(sess); err != nil {
			log.Printf("Failed to start md-serve for session %s: %v", sess.UUID, err)
		}
	}

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

	// Optional extra CLI flags appended to the agent invocation (e.g. "--channels server:agent-chat")
	extraArgs := r.URL.Query().Get("extra_args")

	// POST /api/session ("new") and POST /api/fork ("fork") stage a creation
	// intent under sessionUUID and 302-redirect the browser here without
	// spawning anything. If this UUID has a staged entry, consume it and use
	// those params instead of the URL query -- they carry the real wiring
	// (WorkDir, ExtraArgs with --resume appended, PrepopulateChatLog, etc.).
	// The entry is removed on first consumption so a reconnect after the
	// session is live falls through to the normal existing-session path.
	staged, isPending := takePendingSession(sessionUUID)

	params := SessionParams{
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
		ExtraArgs:           extraArgs,
		// A terminal child (ParentUUID set) inherits its parent agent
		// session's git credentials/signing/author + repo env vars, so the
		// user's Terminal tab gets the same PATs, commit signing, and env
		// as the agent -- otherwise it spawns with an empty SID and a bare
		// gitconfig. One-time snapshot at spawn; reuses the same inheritance
		// path as MCP create_session. Empty parentUUID no-ops in the inherit
		// helpers. Fork/new-session flows override params entirely below
		// (isPending), so this only takes effect for live terminal spawns.
		InheritCredsFrom: parentUUID,
	}
	if isPending {
		// Preserve the UUID from the URL but override everything else with
		// the staged params -- the URL only carries assistant + session
		// mode for routing, the staged entry carries the real wiring.
		staged.params.UUID = sessionUUID
		// A staged intent that left SessionMode unset falls back to the mode
		// carried on the redirect query (read into sessionMode above), so
		// ?session=chat is not silently downgraded to "terminal" here -- which
		// would leave the agent-chat sidecar unbound, the chat probe forever
		// failing, and chat dead. Fork staging sets SessionMode explicitly, so
		// this never clobbers it.
		staged.params.SessionMode = resolveStagedMode(staged.params.SessionMode, sessionMode)
		params = staged.params
	}

	// Creation is permitted only when there is an explicit intent for this
	// UUID: either a staged "new"/"fork" intent, or this is a child shell
	// whose live parent was already validated above (a specified-but-missing
	// parent returned earlier, so parentUUID != "" here implies a live parent).
	// Otherwise getOrCreateSession returns errSessionGone for an unknown UUID
	// instead of materializing a ghost session -- and a live session always
	// attaches regardless (the allowCreate flag only governs the create path).
	allowCreate := isPending || parentUUID != ""

	sess, isNew, err := getOrCreateSession(params, allowCreate)
	if errors.Is(err, errSessionGone) {
		// No live session and no permission to create: stale tab, ended
		// session, or a bogus/bookmarked UUID. Tell the client it's gone (and
		// whether it can be resumed via fork) so it can show an ended screen
		// with [Resume]/[New] instead of a blank or ghost terminal.
		_, _, _, ferr := validateForkSourceCheap(sessionUUID)
		canResume := ferr == nil
		hasChat, hasTerminal := sessionGoneRecordings(sessionUUID)
		log.Printf("Session %s gone (no live session, no creation intent); canResume=%v hasChat=%v hasTerminal=%v (remote=%s)", sessionUUID, canResume, hasChat, hasTerminal, remoteAddr)
		if data, jerr := json.Marshal(map[string]interface{}{
			"type":        "session_gone",
			"uuid":        sessionUUID,
			"canResume":   canResume,
			"hasChat":     hasChat,
			"hasTerminal": hasTerminal,
		}); jerr == nil {
			conn.WriteMessage(websocket.TextMessage, data)
		}
		// 4003: gone, do NOT reconnect (distinct from 4001 parent-retry and
		// 4002 fatal-creation-error).
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(4003, "session gone"))
		return
	}
	if err != nil {
		log.Printf("Session creation error: %v (remote=%s)", err, remoteAddr)
		// Send the full error as JSON before closing -- the WS close-reason
		// field is capped at 123 bytes and would truncate the useful tail of
		// git's output (e.g. "fatal: 'main' is already checked out at ...").
		// Close code 4002 tells the client "fatal, don't reconnect".
		if data, jerr := json.Marshal(map[string]string{
			"type":    "session_error",
			"message": err.Error(),
		}); jerr == nil {
			conn.WriteMessage(websocket.TextMessage, data)
		}
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(4002, "session creation failed"))
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

	// Push the connect-time credential/signing snapshot so the Settings
	// panel reflects true server-side state without a manual Save. Sent to
	// this connection only; later state changes are broadcast to all conns.
	if err := conn.WriteJSON(buildSessionCredState(sess.UUID, sess.effectiveWorkDir())); err != nil {
		log.Printf("Session %s: failed to send session_cred_state: %v", sess.UUID, err)
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
				if err := renameSession(sess, msg.Name); err != nil {
					log.Printf("Session rename rejected: %v", err)
				}
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
			case "set_credentials":
				// Browser supplies per-session HTTPS credentials over the
				// already-authenticated WebSocket. Write-only: there is no
				// API to read them back to the browser. Server-side they
				// flow OUT to git via the broker socket.
				var payload struct {
					Host                 string `json:"host"`
					Username             string `json:"username"`
					Token                string `json:"token"`
					Name                 string `json:"name"`
					Email                string `json:"email"`
					SigningPrivateKeyPEM string `json:"signing_private_key_pem"`
					SigningPassphrase    string `json:"signing_passphrase"`
					SigningKeyLabel      string `json:"signing_key_label"`
				}
				if err := json.Unmarshal(msg.Data, &payload); err != nil {
					log.Printf("Session %s: set_credentials invalid payload: %v", sess.UUID, err)
					continue
				}
				host := strings.TrimSpace(payload.Host)
				if host == "" {
					host = "github.com"
				}

				// Parse the signing key (CPU-bound, may decrypt) BEFORE
				// taking the compound lock -- never hold a lock across it.
				var (
					parsedKey          SigningKey
					haveParsedKey      bool
					signingFingerprint string
					signingError       string
				)
				if pem := strings.TrimSpace(payload.SigningPrivateKeyPEM); pem != "" {
					key, err := parseSigningKey([]byte(pem), payload.SigningPassphrase, strings.TrimSpace(payload.SigningKeyLabel))
					if err != nil {
						signingError = err.Error()
						log.Printf("Session %s: signing key parse failed: %v", sess.UUID, err)
					} else {
						parsedKey = key
						haveParsedKey = true
						signingFingerprint = key.Fingerprint
					}
				}

				// Apply the credential + author + signing-key updates as one
				// critical section so a concurrent session_cred_state snapshot
				// never sees a half-applied update (new author, old key, etc.).
				sessionCredStateMu.Lock()
				setCredential(sess.UUID, host, CredentialBag{
					Username: strings.TrimSpace(payload.Username),
					Token:    payload.Token,
				})
				setAuthor(sess.UUID, AuthorIdent{
					Name:  strings.TrimSpace(payload.Name),
					Email: strings.TrimSpace(payload.Email),
				})
				if haveParsedKey {
					setSigningKey(sess.UUID, parsedKey)
					log.Printf("Session %s: stored signing key (fp=%s, label=%q)", sess.UUID, parsedKey.Fingerprint, parsedKey.Label)
				}
				sessionCredStateMu.Unlock()

				// Refresh the cached workdir email (a user may have just
				// fixed their local identity) before the rewrite, which
				// reads it via cachedEffectiveGitEmail.
				refreshSessionEffectiveEmail(sess.UUID, sess.effectiveWorkDir())
				if err := writeSessionGitconfig(sess.UUID, sess.effectiveWorkDir()); err != nil {
					log.Printf("Session %s: writeSessionGitconfig failed: %v", sess.UUID, err)
				}
				log.Printf("Session %s: stored credentials for host=%q", sess.UUID, host)
				// Push the refreshed state to all conns of this session so
				// co-viewers do not go stale (idempotent; see BroadcastJSON).
				sess.BroadcastJSON(buildSessionCredState(sess.UUID, sess.effectiveWorkDir()))
				ack := map[string]any{
					"type":  "credentials_stored",
					"host":  host,
					"hosts": listCredentialHosts(sess.UUID),
				}
				if signingFingerprint != "" {
					ack["signing_fingerprint"] = signingFingerprint
				}
				if signingError != "" {
					ack["signing_error"] = signingError
				}
				if err := conn.WriteJSON(ack); err != nil {
					log.Printf("Session %s: failed to ack set_credentials: %v", sess.UUID, err)
				}
			case "test_credentials":
				// Validate the supplied HTTPS credentials by attempting an
				// authenticated GET against the host. Does not persist the
				// credentials; the user explicitly hits Save Credentials
				// after a successful test.
				var payload struct {
					Host     string `json:"host"`
					Username string `json:"username"`
					Token    string `json:"token"`
				}
				if err := json.Unmarshal(msg.Data, &payload); err != nil {
					log.Printf("Session %s: test_credentials invalid payload: %v", sess.UUID, err)
					continue
				}
				host := strings.TrimSpace(payload.Host)
				if host == "" {
					host = "github.com"
				}
				ack := map[string]any{"type": "credentials_tested"}
				if strings.TrimSpace(payload.Token) == "" {
					ack["ok"] = false
					ack["message"] = "Token is empty"
				} else {
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					ok, message := testGitCredentials(ctx, host, strings.TrimSpace(payload.Username), payload.Token)
					cancel()
					ack["ok"] = ok
					ack["message"] = message
					log.Printf("Session %s: test_credentials host=%q ok=%v", sess.UUID, host, ok)
				}
				if err := conn.WriteJSON(ack); err != nil {
					log.Printf("Session %s: failed to ack test_credentials: %v", sess.UUID, err)
				}
			case "set_env":
				// Browser supplies the repo env-vars textarea blob over the
				// authenticated WS. Stored in memory only (never on disk);
				// injected into NEWLY opened sessions by buildSessionEnv. The
				// current session is unaffected -- env is materialized at spawn.
				var payload struct {
					Raw string `json:"raw"`
				}
				if err := json.Unmarshal(msg.Data, &payload); err != nil {
					log.Printf("Session %s: set_env invalid payload: %v", sess.UUID, err)
					continue
				}
				sessionCredStateMu.Lock()
				setSessionEnv(sess.UUID, payload.Raw)
				sessionCredStateMu.Unlock()
				// Reserved keys the user tried to set are reported back so the
				// UI can show which were ignored. Lookup is throwaway here (we
				// only use the dropped list, not the expanded values).
				_, dropped := sessionEnvVars(sess.UUID, os.Getenv)
				log.Printf("Session %s: stored repo env vars (%d reserved keys dropped)", sess.UUID, len(dropped))
				sess.BroadcastJSON(buildSessionCredState(sess.UUID, sess.effectiveWorkDir()))
				if dropped == nil {
					dropped = []string{}
				}
				if err := conn.WriteJSON(map[string]any{"type": "env_stored", "dropped": dropped}); err != nil {
					log.Printf("Session %s: failed to ack set_env: %v", sess.UUID, err)
				}
			case "clear_env":
				// "Forget on this device" also clears the server-side copy so a
				// co-viewer's next session doesn't silently inherit stale vars.
				sessionCredStateMu.Lock()
				clearSessionEnv(sess.UUID)
				sessionCredStateMu.Unlock()
				log.Printf("Session %s: cleared repo env vars", sess.UUID)
				sess.BroadcastJSON(buildSessionCredState(sess.UUID, sess.effectiveWorkDir()))
				if err := conn.WriteJSON(map[string]any{"type": "env_cleared"}); err != nil {
					log.Printf("Session %s: failed to ack clear_env: %v", sess.UUID, err)
				}
			case "set_signing_key":
				// Browser supplies an SSH signing key independent of the
				// HTTPS PAT. Mirrors the signing branch of set_credentials
				// but never touches credential / author state.
				var payload struct {
					SigningPrivateKeyPEM string `json:"signing_private_key_pem"`
					SigningPassphrase    string `json:"signing_passphrase"`
					SigningKeyLabel      string `json:"signing_key_label"`
				}
				if err := json.Unmarshal(msg.Data, &payload); err != nil {
					log.Printf("Session %s: set_signing_key invalid payload: %v", sess.UUID, err)
					continue
				}
				ack := map[string]any{"type": "signing_key_stored"}
				pem := strings.TrimSpace(payload.SigningPrivateKeyPEM)
				if pem == "" {
					ack["error"] = "no key provided"
				} else {
					key, err := parseSigningKey([]byte(pem), payload.SigningPassphrase, strings.TrimSpace(payload.SigningKeyLabel))
					if err != nil {
						ack["error"] = err.Error()
						log.Printf("Session %s: set_signing_key parse failed: %v", sess.UUID, err)
					} else {
						setSigningKey(sess.UUID, key)
						if err := writeSessionGitconfig(sess.UUID, sess.effectiveWorkDir()); err != nil {
							log.Printf("Session %s: writeSessionGitconfig failed: %v", sess.UUID, err)
						}
						ack["fingerprint"] = key.Fingerprint
						log.Printf("Session %s: stored signing key (fp=%s, label=%q)", sess.UUID, key.Fingerprint, key.Label)
						// Refresh all conns: a freshly auto-restored key may flip
						// signing_active true (e.g. after the connect-time snapshot
						// reported "no signing key" before auto-restore landed).
						sess.BroadcastJSON(buildSessionCredState(sess.UUID, sess.effectiveWorkDir()))
					}
				}
				if err := conn.WriteJSON(ack); err != nil {
					log.Printf("Session %s: failed to ack set_signing_key: %v", sess.UUID, err)
				}
			case "verify_signing_key":
				// Parse + decrypt the supplied key, return the fingerprint,
				// then discard. Does NOT persist and does NOT contact the forge.
				var payload struct {
					SigningPrivateKeyPEM string `json:"signing_private_key_pem"`
					SigningPassphrase    string `json:"signing_passphrase"`
					SigningKeyLabel      string `json:"signing_key_label"`
				}
				if err := json.Unmarshal(msg.Data, &payload); err != nil {
					log.Printf("Session %s: verify_signing_key invalid payload: %v", sess.UUID, err)
					continue
				}
				ack := map[string]any{"type": "signing_key_verified"}
				pem := strings.TrimSpace(payload.SigningPrivateKeyPEM)
				if pem == "" {
					ack["error"] = "no key provided"
				} else {
					key, err := parseSigningKey([]byte(pem), payload.SigningPassphrase, strings.TrimSpace(payload.SigningKeyLabel))
					if err != nil {
						ack["error"] = err.Error()
					} else {
						ack["fingerprint"] = key.Fingerprint
					}
				}
				if err := conn.WriteJSON(ack); err != nil {
					log.Printf("Session %s: failed to ack verify_signing_key: %v", sess.UUID, err)
				}
			case "verify_stored_signing_key":
				// Verify the key ALREADY registered for this session (e.g.
				// auto-restored) without re-sending the PEM, which the form
				// may not hold this session. Signs a tiny test payload with
				// the in-memory signer to prove it is loadable and functional,
				// then returns the fingerprint. Acks with the same
				// signing_key_verified type the frontend already handles.
				ack := map[string]any{"type": "signing_key_verified"}
				if key, ok := getSigningKey(sess.UUID); !ok || key.Signer == nil {
					ack["error"] = "no signing key registered this session"
				} else if _, err := signSSH([]byte("swe-swe verify"), key.Signer, "git"); err != nil {
					ack["error"] = "stored key failed to sign: " + err.Error()
				} else {
					ack["fingerprint"] = key.Fingerprint
				}
				if err := conn.WriteJSON(ack); err != nil {
					log.Printf("Session %s: failed to ack verify_stored_signing_key: %v", sess.UUID, err)
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
			if err := sess.WriteInput([]byte(absFilePath)); err != nil {
				log.Printf("PTY write error for uploaded file path: %v", err)
			}
			continue
		}

		// Regular terminal input
		if err := sess.WriteInput(data); err != nil {
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

// buildAgentArgv parses the assistant shell command and appends optional
// extra CLI flags supplied by the user (e.g. "--channels server:agent-chat").
// Both inputs are whitespace-split; quoted args with spaces are not supported.
// Pure function -- no PTY, no env -- intended to be unit tested.
func buildAgentArgv(shellCmd, extraArgs string) (string, []string) {
	cmdName, cmdArgs := parseCommand(shellCmd)
	if extra := strings.Fields(extraArgs); len(extra) > 0 {
		cmdArgs = append(cmdArgs, extra...)
	}
	return cmdName, cmdArgs
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
	// Serialize concurrent saveMetadata calls on the same session. Without this,
	// two goroutines (e.g. simultaneous WebSocket reconnects) can race on
	// os.WriteFile: a longer prior write followed by a shorter rewrite leaves
	// trailing garbage from the longer write, producing valid-JSON-followed-by-
	// junk that breaks json.Unmarshal in loadEndedRecordings and makes the
	// recording disappear from the homepage.
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()

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
	// Atomic write: temp file + rename. Prevents readers from ever seeing a
	// half-written or stale-tail metadata.json.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// quarantineCorruptMetadata scans the recordings directory at startup, renames
// any *.metadata.json that fails json.Unmarshal to *.metadata.json.corrupt, and
// removes any leftover *.metadata.json.tmp files from a prior interrupted
// atomic write. Logged so the operator can see what was touched. Recordings
// whose metadata is quarantined will fall through loadEndedRecordings' mtime
// fallback path and remain visible on the homepage.
func quarantineCorruptMetadata() {
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		return
	}
	now := time.Now()
	var corrupt, tmps, stale int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Clean up stale .tmp files from interrupted atomic writes
		if strings.HasPrefix(name, "session-") && strings.HasSuffix(name, ".metadata.json.tmp") {
			path := recordingsDir + "/" + name
			if err := os.Remove(path); err == nil {
				tmps++
			}
			continue
		}
		// Clean up old .corrupt files past TTL (same as recording expiry)
		if strings.HasPrefix(name, "session-") && strings.HasSuffix(name, ".metadata.json.corrupt") {
			path := recordingsDir + "/" + name
			if fi, err := entry.Info(); err == nil && now.Sub(fi.ModTime()) > recentRecordingMaxAge {
				if err := os.Remove(path); err == nil {
					stale++
				}
			}
			continue
		}
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".metadata.json") {
			continue
		}
		path := recordingsDir + "/" + name
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) == 0 {
			continue // tolerate empty placeholders, loadEndedRecordings handles them
		}
		var probe RecordingMetadata
		if err := json.Unmarshal(data, &probe); err == nil {
			continue
		}
		dst := path + ".corrupt"
		if err := os.Rename(path, dst); err != nil {
			log.Printf("quarantineCorruptMetadata: failed to rename %s: %v", name, err)
			continue
		}
		corrupt++
		log.Printf("quarantineCorruptMetadata: quarantined %s -> %s", name, filepath.Base(dst))
	}
	if corrupt > 0 || tmps > 0 || stale > 0 {
		log.Printf("quarantineCorruptMetadata: corrupt=%d tmp_removed=%d stale_corrupt_removed=%d", corrupt, tmps, stale)
	}
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
// Terminal output is written uncompressed to a .log file. Compression to .log.gz
// happens lazily in the cleanup scheduler (cleanupRecentRecordings) after the
// session ends. This avoids the complexity and reliability issues of real-time
// gzip via FIFO (gzip buffers output internally and only flushes on EOF, which
// conflicts with session termination via SIGKILL).
func wrapWithScript(cmdName string, cmdArgs []string, prefix string) (string, []string) {
	// Build the full command string for script -c
	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd += " " + strings.Join(cmdArgs, " ")
	}

	logPath := fmt.Sprintf("%s/%s.log", recordingsDir, prefix)
	timingPath := fmt.Sprintf("%s/%s.timing", recordingsDir, prefix)
	inputPath := fmt.Sprintf("%s/%s.input", recordingsDir, prefix)

	wrapperScript := scriptWrapperCommand(runtime.GOOS, logPath, timingPath, inputPath, fullCmd)
	return "bash", []string{"-c", wrapperScript}
}

// scriptWrapperCommand builds the shell command that records a PTY session.
// Linux uses util-linux `script` with separate timing/input/output files,
// enabling timed playback. macOS/BSD `script` has none of those flags
// (-f/-T/-I/-O), so it records combined output to the .log file only -- no
// timing file, so playback is plain (untimed) on macOS. The caller still
// computes the .timing/.input paths; they simply will not exist on macOS,
// which the cleanup + playback paths already tolerate (they stat for
// existence and gate on HasTiming).
func scriptWrapperCommand(goos, logPath, timingPath, inputPath, fullCmd string) string {
	if goos == "linux" {
		return fmt.Sprintf(
			`script -q -f -T %[1]q -I %[2]q -O %[3]q -c %[4]q`,
			timingPath, inputPath, logPath, fullCmd,
		)
	}
	// BSD/macOS: `script [-q] file command ...` -- record combined output to
	// the log file; run the command via bash -c to preserve the full command
	// string (BSD script takes the command as trailing argv, not -c).
	return fmt.Sprintf(`script -q %[1]q /bin/bash -c %[2]q`, logPath, fullCmd)
}

// resolveLogPath returns the path to a recording's log file, checking for both
// compressed (.log.gz) and uncompressed (.log) variants. Prefers .log.gz.
// Returns empty string if neither exists.
// sessionGoneRecordings reports which playback artifacts exist on disk for an
// ended session, so the "session has ended" screen can offer "View recorded
// Chat / Terminal" links. Both viewers are keyed by the session UUID (the same
// key fork uses -- see loadEndedForkSource / handleChatPlaybackPage), so the
// frontend builds the /recording/<uuid> and /recording/<uuid>/chat URLs from
// the uuid it already has; no extra identifier needs to cross the wire.
func sessionGoneRecordings(sourceUUID string) (hasChat, hasTerminal bool) {
	if _, err := uuid.Parse(sourceUUID); err != nil {
		return false, false
	}
	hasChat = findChatEventsFile(sourceUUID) != ""
	hasTerminal = resolveLogPath("session-"+sourceUUID) != ""
	return hasChat, hasTerminal
}

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

// gzipDecompressSem bounds the number of concurrent gzip decompressions of
// recording logs. Each in-flight reader holds a slot until Close. Without this
// bound, N concurrent recording fetches spawn N parallel gzip decoders, which
// can spike CPU/load. Sized to NumCPU (min 2) so we use available cores but
// don't oversubscribe.
var gzipDecompressSem = func() chan struct{} {
	n := runtime.NumCPU()
	if n < 2 {
		n = 2
	}
	return make(chan struct{}, n)
}()

// openLogReader opens a log file for reading, transparently decompressing gzip.
// Returns a ReadCloser that must be closed by the caller. For gzip files, this
// blocks until a slot in gzipDecompressSem is available.
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
		// Acquire a decompression slot. Released by gzipReadCloser.Close.
		gzipDecompressSem <- struct{}{}
		gz, err := gzip.NewReader(f)
		if err != nil {
			<-gzipDecompressSem
			f.Close()
			return nil, err
		}
		return &gzipReadCloser{gz: gz, f: f}, nil
	}
	return f, nil
}

type gzipReadCloser struct {
	gz     *gzip.Reader
	f      *os.File
	closed bool
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }
func (g *gzipReadCloser) Close() error {
	if g.closed {
		return nil
	}
	g.closed = true
	g.gz.Close()
	err := g.f.Close()
	<-gzipDecompressSem
	return err
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
	HasTiming   bool       `json:"has_timing"`
	SizeBytes   int64      `json:"size_bytes"`
	IsActive    bool       `json:"is_active,omitempty"`
}

// RecordingInfo holds recording data for template rendering
type RecordingInfo struct {
	UUID            string
	UUIDShort       string
	Name            string
	Agent           string
	AgentBadgeClass string
	EndedAgo        string           // "15m ago", "2h ago", "yesterday"
	EndedAt         time.Time        // actual timestamp for sorting
	KeptAt          *time.Time       // When user marked this recording to keep (nil = recent, auto-deletable)
	IsKept          bool             // Convenience field for templates
	ExpiresIn       string           // "59m", "30m" - time until auto-deletion (only for non-kept)
	HasChat         bool             // has a chat .events.jsonl child recording
	HasTerminal     bool             // has a terminal .log child recording
	ChatUUID        string           // child UUID for chat playback URL
	TerminalUUIDs   []string         // child UUIDs for terminal playback
	SizeHuman       string           // human-readable total size ("2.4 GB")
	SummaryLine     string           // one-line summary from last chat event
	RestartUUID     string           // fresh UUID for "restart" link
	Query           SessionPageQuery // params to restart a similar session
	CanResume       bool             // forkable via /api/fork (chat mode, claude/codex, has chat log)
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
		metaParsed := false
		if metaData, err := os.ReadFile(metadataPath); err == nil {
			var meta RecordingMetadata
			if uerr := json.Unmarshal(metaData, &meta); uerr == nil {
				metaParsed = true
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
					Assistant:      binary,
					SessionMode:    meta.SessionMode,
					Name:           meta.Name,
					BranchName:     meta.BranchName,
					CheckoutBranch: meta.CheckoutBranch,
					WorkDir:        meta.WorkDir,
					ExtraArgs:      meta.ExtraArgs,
				}
				// Forkable via /api/fork only for chat-mode claude/codex
				// recordings that have a chat event log to prepopulate.
				info.CanResume = info.HasChat && meta.SessionMode == "chat" && (binary == "claude" || binary == "codex")
				// Prefer the cached summary if it was extracted at end-of-session.
				// This avoids decompressing the entire .log.gz on every homepage render.
				info.SummaryLine = meta.SummaryLine
			} else {
				log.Printf("loadEndedRecordings: corrupt metadata for %s: %v (falling back to mtime)", ruuid[:8], uerr)
			}
		}
		if !metaParsed {
			// No metadata file, or it failed to parse. Fall back to log file
			// mtime so the recording still appears on the homepage instead of
			// silently vanishing. Default the agent to "claude" so the
			// recording lands in a visible bucket -- homepage rendering only
			// queries availableAssistants by binary name, and an empty agent
			// would otherwise be bucketed under "" and never shown.
			if fileInfo, err := entry.Info(); err == nil {
				info.EndedAt = fileInfo.ModTime()
				info.EndedAgo = formatTimeAgo(fileInfo.ModTime())
			}
			info.Agent = "Claude"
			info.AgentBadgeClass = agentBadgeClass("Claude")
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
		if info.SummaryLine == "" && info.HasChat {
			summaryLine, _ := getSessionSummaryFromChat(ruuid)
			info.SummaryLine = summaryLine
		}
		if info.SummaryLine == "" {
			// Only attempt the log-tail fallback for plain .log files; .log.gz
			// would require full-file decompression (gzip is not seekable) and
			// would block the homepage for minutes on a busy host. The compression
			// worker now caches summary_line into metadata.json before gzipping,
			// so the .log.gz path should normally never be hit here.
			logPath := resolveLogPath("session-" + ruuid)
			if logPath != "" && !strings.HasSuffix(logPath, ".log.gz") {
				info.SummaryLine = getSessionSummaryFromLog(ruuid)
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
//
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
        <div id="voice-controls" hidden>
          <select id="voice-select"></select>
        </div>
        <button id="btn-download" title="Export chat as HTML"><svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 2v8M4.5 7.5 8 11l3.5-3.5M3 13h10"/></svg></button>
      </div>
      <div id="messages">
        <div id="quick-replies"></div>
      </div>
      <div id="chat-footer" hidden>
        <div id="input-bar">
          <button id="btn-attach" title="Attach files" disabled></button>
          <button id="btn-voice" title="Toggle voice mode"></button>
          <div id="input-container">
            <textarea id="chat-input" rows="1" placeholder="Type a message..." disabled></textarea>
            <div id="file-staging"></div>
            <div id="autocomplete-dropdown"></div>
          </div>
          <button id="btn-send" disabled>Send</button>
        </div>
      </div>
      <input type="file" id="file-picker" multiple hidden>
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
	// Already reaped (e.g. called from both endSessionByUUID and Close).
	if s.Cmd.ProcessState != nil {
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
	untrackPid(pid)
	unregisterSessionPid(pid)
	clearSessionCredentials(s.UUID)
	clearSessionKey(s.UUID)
	removeSessionGitconfig(s.UUID)

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

// describeParentProcess returns a human-readable description of this
// process's parent: ppid plus the parent's comm and full cmdline read
// from /proc. A graceful "exit 0" only happens when SIGINT/SIGTERM is
// delivered from outside the process (see the shutdown handler in main),
// so recording WHO the parent is at startup and at shutdown is the
// cheapest way to attribute an unexplained graceful exit -- e.g. parent
// "su" means docker-mode container stop forwarded the signal, while an
// init/orchestrator parent points at the platform. On any /proc read
// error it still returns the numeric ppid so the caller always logs
// something.
func describeParentProcess() string {
	ppid := os.Getppid()
	comm := ""
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid)); err == nil {
		comm = strings.TrimSpace(string(b))
	}
	cmdline := ""
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ppid)); err == nil {
		// /proc cmdline is NUL-separated; render it space-separated.
		cmdline = strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
	}
	return fmt.Sprintf("ppid=%d comm=%q cmdline=%q", ppid, comm, cmdline)
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

// startSubreaper is platform-specific: on Linux it marks this process as a
// child subreaper (prctl) and starts the /proc orphan reaper (subreaper_linux.go);
// elsewhere it is a no-op (subreaper_other.go), since PR_SET_CHILD_SUBREAPER and
// /proc are Linux-specific.

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

	// Enqueue recording logs for prompt compression.
	// The parent session's log is session-{recUUID}.log; child logs are
	// session-{parentRecUUID}-{childRecUUID}.log.
	if session.RecordingUUID != "" {
		parentLogPath := fmt.Sprintf("%s/session-%s.log", recordingsDir, session.RecordingUUID)
		select {
		case compressCh <- parentLogPath:
		default:
		}
		for _, child := range childSessions {
			if child.RecordingUUID != "" {
				childLogPath := fmt.Sprintf("%s/session-%s-%s.log", recordingsDir, session.RecordingUUID, child.RecordingUUID)
				select {
				case compressCh <- childLogPath:
				default:
				}
			}
		}
	}

	return nil
}

// handleSessionForkAPI handles GET /api/fork/{source-session-uuid}.
//
// Two anchor modes:
//
//  1. Default (no query params): forks at the last chat reply, mirroring
//     pre-bubble-anchor behavior. Used by callers that just want "fork
//     here, now."
//
//  2. ?bubble=<seq>&mode=after|replay|before: forks at a specific chat
//     bubble in the .events.jsonl. The resolver (fork_resolve.go) maps the
//     bubble to the agent-side tool_use_id / call_id via either an
//     AgentToolSeq stamp (new agent-chat) or text correlation (legacy).
//
// Fork supports chat-mode claude and codex sessions, live or ended. Pi
// is deliberately deferred to pi's native runtime.fork; other agents have
// no forkconvo support yet.
// handleNewSessionAPI handles POST /api/session/new. It mints a UUID, stages a
// "new" creation intent for it, and 302-redirects to /session/{uuid} with the
// dialog's params echoed onto the URL.
//
// The staged intent is the load-bearing gate of the no-ghost-session design:
// the WS handler will only materialize a session whose UUID is either already
// live or has a staged intent here. The session's real wiring (workdir, branch,
// worktree creation) is still resolved by the WS handler from these query
// params, exactly as the old window.location navigation did -- the intent only
// grants permission to create.
func handleNewSessionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	assistant := r.FormValue("assistant")
	if assistant == "" {
		http.Error(w, "missing assistant", http.StatusBadRequest)
		return
	}
	var validAssistant bool
	for _, a := range availableAssistants {
		if a.Binary == assistant {
			validAssistant = true
			break
		}
	}
	if !validAssistant {
		http.Error(w, "unknown assistant: "+assistant, http.StatusBadRequest)
		return
	}

	newUUID := uuid.New().String()
	// Stage the full creation wiring from the dialog. The WS handler that
	// materializes the session replaces its URL-derived params with this staged
	// entry (params = staged.params), so any field NOT staged here is silently
	// lost -- which is how the session name, branch, pwd, extra_args and chat
	// mode all went missing after the staging refactor (a2a0a4802). Mirror the
	// fork path and stage them all. WorkDir is intentionally left empty:
	// getOrCreateSession resolves it from RepoPath+Branch downstream, exactly as
	// the pre-staging query-string flow did.
	stageSession(newUUID, SessionParams{
		UUID:        newUUID,
		Assistant:   assistant,
		Name:        r.FormValue("name"),
		Branch:      deriveBranchName(r.FormValue("branch")),
		RepoPath:    r.FormValue("pwd"),
		Theme:       r.FormValue("theme"),
		SessionMode: r.FormValue("session"),
		ExtraArgs:   r.FormValue("extra_args"),
		// Repo env-vars blob for this repo, sent by the dialog when the browser
		// holds one under the matching (origin, init_sha) trust key. Applied to
		// the session env store at materialization, before the PTY spawns, so a
		// brand-new session actually gets the vars (a set_env over the WS would
		// arrive after spawn -- too late). Memory-only, never persisted.
		EnvRaw: r.FormValue("env"),
	}, "new", "")

	// Echo the dialog's params onto the redirect so the WS handler resolves the
	// session exactly as the old navigation did.
	// "color" is CSS-only (the client reads it off the URL to theme the page);
	// the server doesn't act on it but must echo it so the look survives the
	// POST -> redirect, exactly as the old query-string navigation carried it.
	q := url.Values{}
	q.Set("assistant", assistant)
	for _, key := range []string{"session", "name", "branch", "pwd", "extra_args", "debug", "theme", "color"} {
		if v := r.FormValue(key); v != "" {
			q.Set(key, v)
		}
	}
	http.Redirect(w, r, "/session/"+newUUID+"?"+q.Encode(), http.StatusFound)
}

// forkSourceUUID extracts the source session UUID from an /api/fork/{uuid} path.
func forkSourceUUID(r *http.Request) string {
	u := strings.TrimPrefix(r.URL.Path, "/api/fork/")
	return strings.TrimSuffix(u, "/")
}

// validateForkSourceCheap runs the side-effect-free guards shared by the GET
// confirm page and the POST executor: the source must resolve, be a chat-mode
// session with a chat event log, and use a fork-capable assistant. It does NOT
// run the active-tail guard or write anything. Returns the resolved source and
// the forkconvo agent, or an HTTP status + error to surface.
func validateForkSourceCheap(sourceUUID string) (*hydratedForkSource, forkconvo.Agent, int, error) {
	if sourceUUID == "" {
		return nil, "", http.StatusBadRequest, fmt.Errorf("missing source session UUID")
	}
	src, err := resolveForkSource(sourceUUID)
	if err != nil {
		return nil, "", http.StatusNotFound, err
	}
	if src.SessionMode != "chat" {
		return nil, "", http.StatusBadRequest, fmt.Errorf("fork supports chat-mode sessions only")
	}
	if src.ChatLogPath == "" {
		return nil, "", http.StatusConflict, fmt.Errorf("source session has no chat event log (predates fork support?)")
	}
	switch src.Assistant {
	case "claude":
		return src, forkconvo.AgentClaude, 0, nil
	case "codex":
		return src, forkconvo.AgentCodex, 0, nil
	default:
		return nil, "", http.StatusBadRequest, fmt.Errorf("fork does not support assistant %q (claude and codex only)", src.Assistant)
	}
}

// handleSessionForkAPI dispatches by method:
//
//	GET  /api/fork/{uuid} -> a skeleton confirm page (no side effects). This
//	     makes the fork URL safe to follow passively (prefetch, unfurl, refresh,
//	     back-button) -- nothing is forked until the user confirms.
//	POST /api/fork/{uuid} -> actually fork + stage + 302 to the new session.
func handleSessionForkAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleForkConfirmPage(w, r)
	case http.MethodPost:
		handleForkExecute(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleForkConfirmPage renders the GET skeleton + confirm modal. It runs only
// the cheap, side-effect-free guards so the modal can show a real error or a
// ready-to-fork prompt; the heavy guards (active-tail) and the fork itself run
// on POST. bubble/mode from the query are carried forward as hidden form fields.
func handleForkConfirmPage(w http.ResponseWriter, r *http.Request) {
	sourceUUID := forkSourceUUID(r)
	data := forkConfirmData{
		SourceUUID: sourceUUID,
		Bubble:     r.URL.Query().Get("bubble"),
		Mode:       r.URL.Query().Get("mode"),
	}
	src, _, _, verr := validateForkSourceCheap(sourceUUID)
	if verr != nil {
		data.Error = verr.Error()
	} else {
		data.SourceName = src.Name
		data.Assistant = src.Assistant
		// Expose the repo init_sha so the page can locate this repo's env-vars
		// blob in localStorage and attach it to the fork POST (see the script
		// in fork-confirm.html). Empty for a non-git / missing workdir.
		data.InitSha = repoInitSHA(src.WorkDir)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := forkConfirmTemplate.Execute(w, data); err != nil {
		log.Printf("fork confirm page render error: %v", err)
	}
}

// renderForkError renders the fork-confirm modal in its error state (styled
// HTML) instead of dumping raw text. The per-bubble fork flow opens in a new
// browser tab, so a plain http.Error would leave the user staring at an
// unstyled error string (see the reported blank-page 409). When offerWhole is
// true the modal also offers a "Fork the whole session instead" button, which
// POSTs without a bubble anchor (last-persisted-reply semantics). When
// offerBeforeActive is true it offers a "Fork before that tool use" button,
// which POSTs bypass_active=1 to skip the active-tail guard and fork at the
// last chat reply (used when the source may be a dead/stuck session).
func renderForkError(w http.ResponseWriter, status int, sourceUUID, message string, offerWhole, offerBeforeActive bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	data := forkConfirmData{
		SourceUUID:            sourceUUID,
		Error:                 message,
		OfferForkWhole:        offerWhole,
		OfferForkBeforeActive: offerBeforeActive,
	}
	if err := forkConfirmTemplate.Execute(w, data); err != nil {
		log.Printf("fork error page render error: %v", err)
	}
}

// handleForkExecute performs the actual fork on POST: full validation (including
// the active-tail guard), forkconvo.Fork (writes the new rollout), stages the
// "fork" creation intent, and 302-redirects to the new session.
func handleForkExecute(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	sourceUUID := forkSourceUUID(r)

	src, fcAgent, status, err := validateForkSourceCheap(sourceUUID)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	// Resolve the per-agent session id captured at spawn. Falls back to
	// fingerprint (claude only) and then mtime when neither is available.
	agentSessionID := src.AgentSessionID
	if agentSessionID == "" && src.Assistant == "claude" {
		if id, ferr := fingerprintClaudeSessionByEvents(src.WorkDir, src.ChatLogPath); ferr == nil {
			agentSessionID = id
			log.Printf("INFO: /api/fork %s: AgentSessionID recovered by fingerprint -> %s", sourceUUID, id)
		} else {
			log.Printf("WARN: /api/fork %s: fingerprint failed (%v); falling back to mtime in %s", sourceUUID, ferr, src.WorkDir)
			var mtimeErr error
			agentSessionID, mtimeErr = findLatestClaudeSessionInWorkDir(src.WorkDir)
			if mtimeErr != nil {
				http.Error(w, "locate claude session: "+mtimeErr.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	if agentSessionID == "" {
		http.Error(w, fmt.Sprintf("no agent_session_id captured for %s; cannot fork", src.Assistant), http.StatusConflict)
		return
	}

	// Resolve the per-agent rollout file once: needed by the ACTIVE-tail
	// guard below and by the bubble anchor resolver further down.
	agentJsonl, jerr := agentSessionFilePath(src.Assistant, src.WorkDir, agentSessionID)
	if jerr != nil {
		http.Error(w, "locate agent session file: "+jerr.Error(), http.StatusInternalServerError)
		return
	}

	// ACTIVE-tail guard: refuse to fork a source whose agent is mid-work
	// with an unresolved non-chat tool_use (bash/edit/grep/...). Truncating
	// mid-tool-call either yields an invalid resume point or silently
	// strips in-flight work the user hasn't seen the result of, so we'd
	// rather make the caller wait for the source to settle.
	//
	// bypass_active=1 is the deliberate escape hatch (the "Fork before that
	// tool use" button on the guard's own error page): the source may be a
	// dead/stuck session that will never settle, so the user opts in to
	// forking at the last chat reply -- the point BEFORE the in-flight
	// tool_use -- accepting that the in-flight work is dropped. That anchor
	// (AnchorLastChatReply) never truncates mid-tool-call, so it's safe; we
	// also drop any bubble anchor below since the tail is unreliable.
	bypassActive := r.FormValue("bypass_active") == "1"
	if !bypassActive {
		active, aerr := forkSourceTailActive(src.Assistant, agentJsonl)
		if aerr != nil {
			http.Error(w, "classify source tail state: "+aerr.Error(), http.StatusInternalServerError)
			return
		}
		if active {
			// A whole-session fork hits this same guard, so don't offer it;
			// offer the guard-bypassing "before that tool use" fork instead.
			renderForkError(w, http.StatusConflict,
				sourceUUID,
				"This session's agent is mid-work on a tool call. If it finishes, try forking again. If the session is dead or stuck, resume from earlier (before that tool use) instead.",
				false, true)
			return
		}
	}

	// Resolve the anchor. Default = last chat reply (existing semantics).
	// If ?bubble= is set, walk the bubble-anchored path: locate the bubble
	// in the chat events file, map to the agent tool_use id, pass that as
	// an explicit forkconvo anchor.
	forkOpts := forkconvo.Opts{
		Agent:           fcAgent,
		SourceSessionID: agentSessionID,
		Anchor:          forkconvo.AnchorLastChatReply,
	}
	// bubble/mode arrive as POST form fields (carried forward from the GET
	// confirm page's hidden inputs).
	mode := r.FormValue("mode")
	if mode == "" {
		mode = "after"
	}
	if bubbleRaw := r.FormValue("bubble"); bubbleRaw != "" && !bypassActive {
		bubbleSeq, perr := strconv.ParseInt(bubbleRaw, 10, 64)
		if perr != nil || bubbleSeq <= 0 {
			http.Error(w, "bubble param must be a positive integer", http.StatusBadRequest)
			return
		}
		resolved, rerr := resolveBubbleAnchor(src.ChatLogPath, agentJsonl, src.Assistant, bubbleSeq, mode)
		switch {
		case rerr == nil:
			log.Printf("INFO: /api/fork %s: bubble seq=%d resolved via %s -> %s (tool=%s, kind=%s)",
				sourceUUID, bubbleSeq, resolved.ResolverUsed, resolved.AnchorID, resolved.ToolName, resolved.BubbleKind)
			forkOpts.Anchor = resolved.AnchorID
		case errors.Is(rerr, ErrAnchorNotYetPersisted):
			// The freshest bubble's tool_use hasn't been flushed to the agent
			// transcript yet (the agent may still be blocked inside that
			// send_message). Fork at the last persisted reply -- identical to a
			// manual session-id fork -- instead of erroring. forkOpts.Anchor is
			// already AnchorLastChatReply from the default above.
			log.Printf("INFO: /api/fork %s: bubble seq=%d not yet persisted to transcript; falling back to last-chat-reply anchor",
				sourceUUID, bubbleSeq)
		case errors.Is(rerr, ErrProgressBubbleNotForkable):
			// The active-tail guard already passed, so a whole-session fork works.
			renderForkError(w, http.StatusConflict, sourceUUID,
				"That was a progress update, not a reply, so it can't be used as a fork point. You can fork the whole session instead.",
				true, false)
			return
		default:
			// Any other anchor failure. The active-tail guard passed above, so a
			// whole-session fork is a viable fallback -- offer it rather than
			// dumping the raw error onto a blank page.
			log.Printf("WARN: /api/fork %s: bubble seq=%d anchor unresolved: %v", sourceUUID, bubbleSeq, rerr)
			renderForkError(w, http.StatusConflict, sourceUUID,
				"Couldn't pin this fork to that specific message. You can fork the whole session instead.",
				true, false)
			return
		}
	}

	forkRes, err := forkconvo.Fork(forkOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("fork %s session: %s", src.Assistant, err.Error()), http.StatusInternalServerError)
		return
	}
	log.Printf("Forked %s session %s -> %s (anchor %s, mode %s) for swe-swe session %s",
		src.Assistant, agentSessionID, forkRes.NewSessionID, forkRes.AnchorUUID, mode, sourceUUID)

	newUUID := uuid.New().String()
	extraArgs := buildForkResumeArgs(src.Assistant, src.ExtraArgs, forkRes.NewSessionID)
	forkName := src.Name
	if forkName == "" {
		forkName = "fork"
	} else {
		forkName = "fork: " + forkName
	}

	// Stage the creation intent; the first WS client to hit /session/<newUUID>
	// will materialize the session from this entry. Starting the PTY only
	// after a client connects ensures the agent's first render happens at
	// the client's actual geometry, sidestepping the "blocked-on-stdin
	// during SIGWINCH leaves a blank screen" bug that an eager
	// getOrCreateSession would hit here.
	//
	// orphanCleanupPath = the rollout .jsonl forkconvo.Fork just created for
	// the NEW session id. If the user never connects, the sweeper deletes it.
	// (Best-effort: an unresolvable path just means no cleanup, never an error.)
	orphanPath, _ := agentSessionFilePath(src.Assistant, src.WorkDir, forkRes.NewSessionID)
	stageSession(newUUID, buildForkSessionParams(newUUID, src, extraArgs, forkName, r.FormValue("env")), "fork", orphanPath)

	http.Redirect(w, r, fmt.Sprintf("/session/%s?assistant=%s&session=chat", newUUID, src.Assistant), http.StatusFound)
}

// buildForkSessionParams assembles the staged SessionParams for a fork.
//
// InheritCredsFrom = src.UUID is the fix for forks silently dropping the source
// session's git auth: it drives the same spawn-time inheritance MCP
// create_session and the terminal-child path use, so the fork picks up the
// source's HTTPS credentials, git author, SSH signing key, and repo env vars
// (inheritSessionEnv runs before buildSessionEnv, inheritSessionCredentials
// after -- see getOrCreateSession). For an ended source whose in-memory stores
// are already cleared this is a no-op; env then falls back to the browser
// localStorage blob in EnvRaw. Credentials/signing have no such fallback, so
// forking a fully ended session still can't recover them -- matching prior
// behavior -- but the common active-source fork now carries everything.
func buildForkSessionParams(newUUID string, src *hydratedForkSource, extraArgs, forkName, envRaw string) SessionParams {
	return SessionParams{
		UUID:               newUUID,
		Assistant:          src.Assistant,
		Name:               forkName,
		WorkDir:            src.WorkDir,
		SessionMode:        "chat",
		ExtraArgs:          extraArgs,
		Theme:              src.Theme,
		PrepopulateChatLog: src.ChatLogPath,
		InheritCredsFrom:   src.UUID,
		// Repo env vars the fork inherits, sent by the confirm page from the
		// browser's localStorage blob (the source session's own env store may
		// already be cleared -- forks routinely resume ended sessions). Applied
		// to the store before the forked session spawns, layered over the
		// inherited blob above. Memory-only.
		EnvRaw: envRaw,
	}
}

// buildForkResumeArgs constructs the ExtraArgs string for a forked session.
// Per-agent resume conventions:
//
//	claude: appended "--resume <id>" (claude takes resume as a flag, so
//	        ordering with other extra args doesn't matter).
//	codex:  prepended "resume <id>" (codex takes resume as a subcommand,
//	        which must come right after the binary name; any pre-existing
//	        global flags in src.ExtraArgs stay before the subcommand).
//
// Other agents fall through to claude's append behavior; if we ever add
// more, branch them here.
func buildForkResumeArgs(assistant, srcExtraArgs, newSessionID string) string {
	srcExtraArgs = strings.TrimSpace(srcExtraArgs)
	switch assistant {
	case "codex":
		// codex global flags (if any) stay leading; "resume <id>" follows.
		if srcExtraArgs == "" {
			return "resume " + newSessionID
		}
		return srcExtraArgs + " resume " + newSessionID
	default:
		return strings.TrimSpace(srcExtraArgs + " --resume " + newSessionID)
	}
}

// agentSessionFilePath returns the absolute path to the per-agent session
// file (claude jsonl, codex rollout, ...) for resolving bubble anchors.
func agentSessionFilePath(assistant, workDir, agentSessionID string) (string, error) {
	switch assistant {
	case "claude":
		if workDir == "" {
			return "", fmt.Errorf("empty workdir")
		}
		encoded := encodeClaudeProjectDir(workDir)
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "projects", encoded, agentSessionID+".jsonl"), nil
	case "codex":
		// codex stores rollouts under ~/.codex/sessions/YYYY/MM/DD/rollout-...-<id>.jsonl.
		// We don't know the timestamp; walk the tree under the YYYY root and
		// take the file that ends in -<id>.jsonl. The dir's not large enough
		// for this to matter perf-wise.
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root := filepath.Join(home, ".codex", "sessions")
		var match string
		walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			name := d.Name()
			if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, "-"+agentSessionID+".jsonl") {
				match = p
			}
			return nil
		})
		if walkErr != nil {
			return "", walkErr
		}
		if match == "" {
			return "", fmt.Errorf("codex rollout for session %s not found under %s", agentSessionID, root)
		}
		return match, nil
	}
	return "", fmt.Errorf("agentSessionFilePath: unsupported assistant %q", assistant)
}

// forkSourceTailActive dispatches to the per-agent tail-state classifier in
// the forkconvo package. Returns true when the source agent is mid-work
// with an unresolved non-chat tool_use (bash/edit/grep/...), in which case
// /api/fork refuses the request with 409. Agent-chat parks (send_message
// waiting on user) do NOT count as active -- those are the natural safe
// fork point. Unsupported assistants return (false, nil); the caller has
// already filtered them earlier.
func forkSourceTailActive(assistant, agentJsonl string) (bool, error) {
	switch assistant {
	case "claude":
		return forkconvo.ClaudeIsTailActive(agentJsonl)
	case "codex":
		return forkconvo.CodexIsTailActive(agentJsonl)
	}
	return false, nil
}

// sessionTailBusy is the best-effort listing variant of the /api/fork
// ACTIVE-tail guard: nil means unknown (assistant has no tail classifier,
// no agent session id was captured, or the log couldn't be read). Unlike
// the fork path it never falls back to fingerprint/mtime recovery -- a
// listing shouldn't pay that cost, and only a definite answer should gate
// a shutdown.
func sessionTailBusy(assistant, workDir, agentSessionID string) *bool {
	if agentSessionID == "" {
		return nil
	}
	switch assistant {
	case "claude", "codex":
	default:
		return nil
	}
	path, err := agentSessionFilePath(assistant, workDir, agentSessionID)
	if err != nil {
		return nil
	}
	busy, err := forkSourceTailActive(assistant, path)
	if err != nil {
		return nil
	}
	return &busy
}

// findLatestClaudeSessionInWorkDir returns the session id (filename minus
// .jsonl) of the most recently modified Claude .jsonl in the project
// directory that corresponds to workDir. Claude stores sessions under
// ~/.claude/projects/<workDir-with-slashes-replaced-by-dashes>/.
//
// The mtime heuristic is fine for the MVP because we only have one active
// claude per workdir during the e2e flow. The Session struct should grow
// an AgentSessionID field once we get to it.
func findLatestClaudeSessionInWorkDir(workDir string) (string, error) {
	if workDir == "" {
		return "", fmt.Errorf("empty workdir")
	}
	encoded := encodeClaudeProjectDir(workDir)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "projects", encoded)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var latestName string
	var latestMtime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMtime) {
			latestMtime = info.ModTime()
			latestName = e.Name()
		}
	}
	if latestName == "" {
		return "", fmt.Errorf("no claude .jsonl in %s", dir)
	}
	return strings.TrimSuffix(latestName, ".jsonl"), nil
}

// copyFile is a small helper used by /api/fork to clone the source chat
// event log into the new session's location before the agent-chat MCP
// sidecar opens the file at boot.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
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

	// Parse UUID from path: /api/session/{uuid}/browser/start
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	path = strings.TrimSuffix(path, "/browser/start")
	sessionUUID := path

	if sessionUUID == "" {
		http.Error(w, "Missing session UUID", http.StatusBadRequest)
		return
	}

	// Per-session key auth: the caller's key must belong to this very
	// session, so one session can never start another's browser.
	if !sessionKeyMatchesPath(r, sessionUUID) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

	status, err := startSessionAgentView(sess)
	if err != nil {
		log.Printf("Failed to start Agent View for session %s: %v", sessionUUID, err)
		http.Error(w, fmt.Sprintf("Failed to start browser: %v", err), http.StatusInternalServerError)
		return
	}

	// Push the updated status (browserStarted / agentViewAvailable) to clients.
	sess.BroadcastStatus()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":%q}`, status)
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

	// Remote backend: probe the remote websockify -- the same target the VNC
	// proxy dials (sess.VNCPort is the local pool port; unused in remote mode).
	target := fmt.Sprintf("localhost:%d", sess.VNCPort)
	if sess.RemoteVNCTarget != "" {
		target = sess.RemoteVNCTarget
	} else if sess.VNCPort == 0 {
		http.Error(w, "VNC not configured", http.StatusServiceUnavailable)
		return
	}

	// TCP connect to websockify to check if it's listening
	conn, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
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

// handleFilesReadyAPI handles GET /api/session/{uuid}/files-ready
// Returns 200 if the per-session md-serve is listening on the session's
// FilesPort, 503 otherwise. Same-origin probe endpoint so the Files pane can
// check a real status code instead of an opaque cross-origin no-cors response,
// and defer loading its iframe until md-serve has finished its (slow, npx
// @latest) cold start -- otherwise the pane renders blank until a manual reload.
func handleFilesReadyAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionUUID := strings.TrimSuffix(path, "/files-ready")

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

	if sess.FilesPort == 0 {
		http.Error(w, "Files not configured", http.StatusServiceUnavailable)
		return
	}

	// TCP connect to md-serve to check if it's listening yet.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", sess.FilesPort), 500*time.Millisecond)
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
		Description: "List all active agent sessions. busy=true means the agent is mid-work on an unresolved tool call (ending or forking it would truncate in-flight work); busy absent means unknown (agent has no tail classifier or no session id captured). recordingUUID feeds /api/fork/<recordingUUID>, which keeps working after the session ends -- post those as resume links before a planned shutdown.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		type sessionInfo struct {
			UUID          string `json:"uuid"`
			Name          string `json:"name"`
			Assistant     string `json:"assistant"`
			ClientCount   int    `json:"clientCount"`
			Duration      string `json:"duration"`
			WorkDir       string `json:"workDir"`
			BranchName    string `json:"branchName,omitempty"`
			PreviewPort   int    `json:"previewPort"`
			PublicPort    int    `json:"publicPort"`
			RecordingUUID string `json:"recordingUUID,omitempty"`
			Busy          *bool  `json:"busy,omitempty"`
		}
		var result []sessionInfo
		var agentSessionIDs []string
		sessionsMu.RLock()
		for _, sess := range sessions {
			if sess.Cmd.ProcessState != nil {
				continue
			}
			sess.mu.RLock()
			result = append(result, sessionInfo{
				UUID:          sess.UUID,
				Name:          sess.Name,
				Assistant:     sess.Assistant,
				ClientCount:   len(sess.wsClients),
				Duration:      formatDuration(time.Since(sess.CreatedAt)),
				WorkDir:       sess.WorkDir,
				BranchName:    sess.BranchName,
				PreviewPort:   sess.PreviewPort,
				PublicPort:    sess.PublicPort,
				RecordingUUID: sess.RecordingUUID,
			})
			agentSessionIDs = append(agentSessionIDs, sess.AgentSessionID)
			sess.mu.RUnlock()
		}
		sessionsMu.RUnlock()
		// Busy classification reads each agent's session log, so it runs
		// after both locks are released.
		for i := range result {
			result[i].Busy = sessionTailBusy(result[i].Assistant, result[i].WorkDir, agentSessionIDs[i])
		}
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
		RepoPath  string `json:"repo_path" jsonschema:"required,Repository path for worktree creation"`
		ExtraArgs string `json:"extra_args,omitempty" jsonschema:"Extra CLI flags appended to the agent command, e.g. --channels server:agent-chat"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_session",
		Description: "Create a new agent session",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createSessionArgs) (*mcp.CallToolResult, any, error) {
		if args.Assistant == "" {
			return nil, nil, fmt.Errorf("assistant is required")
		}
		if args.RepoPath == "" {
			return nil, nil, fmt.Errorf("repo_path is required")
		}
		// The calling session is identified by its per-session MCP auth key
		// (injected by mcpAuthMiddleware). Hard-fail when absent: without a
		// trusted caller identity we cannot safely inherit credentials, and
		// an unauthenticated /mcp request should never reach here.
		parentUUID := callerSessionFromContext(ctx)
		if parentUUID == "" {
			return nil, nil, fmt.Errorf("unauthenticated: missing calling session identity")
		}
		sessionUUID := uuid.New().String()
		// MCP create_session is an explicit, authenticated creation action: it
		// mints the UUID and creates eagerly (allowCreate=true). The browser
		// later attaches to this now-live session via the WS live path.
		sess, _, err := getOrCreateSession(SessionParams{
			UUID:             sessionUUID,
			Assistant:        args.Assistant,
			Name:             args.Name,
			Branch:           args.Branch,
			RepoPath:         args.RepoPath,
			SessionMode:      "chat",
			ExtraArgs:        args.ExtraArgs,
			InheritCredsFrom: parentUUID,
		}, true)
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

	// set_session_name
	type setSessionNameArgs struct {
		Name string `json:"name" jsonschema:"required,New display name; allowed chars: letters digits space - _ / . @ (max 256); recommended format: {short task title} {owner}/{repo}@{branch}"`
		UUID string `json:"uuid,omitempty" jsonschema:"Session UUID to rename; defaults to the calling session"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_session_name",
		Description: "Set a session's display name (shown in the session list and browser tab). Call it once the task at hand is clear, so the user can tell sessions apart; use '{short task title} {owner}/{repo}@{branch}'. Without uuid it renames the calling session.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args setSessionNameArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Name) == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		targetUUID := args.UUID
		if targetUUID == "" {
			targetUUID = callerSessionFromContext(ctx)
		}
		if targetUUID == "" {
			return nil, nil, fmt.Errorf("no uuid provided and missing calling session identity")
		}
		sessionsMu.RLock()
		sess, exists := sessions[targetUUID]
		sessionsMu.RUnlock()
		if !exists {
			return nil, nil, fmt.Errorf("session not found")
		}
		if err := renameSession(sess, args.Name); err != nil {
			return nil, nil, err
		}
		sess.mu.RLock()
		newName := sess.Name
		sess.mu.RUnlock()
		info := map[string]string{"uuid": targetUUID, "name": newName}
		data, _ := json.Marshal(info)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
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
			if err := sess.WriteInput([]byte(text)); err != nil {
				return nil, nil, fmt.Errorf("write failed: %w", err)
			}
		}
		if hasTrailingNewline {
			time.Sleep(300 * time.Millisecond)
			if err := sess.WriteInput([]byte{'\r'}); err != nil {
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
		// Optional HTTPS credentials for cloning a private repo. The token is
		// wired through the broker only for the duration of the clone and is
		// never persisted or embedded in the URL.
		CredHost     string `json:"credHost,omitempty" jsonschema:"HTTPS host for private clone credentials (for clone mode)"`
		CredUsername string `json:"credUsername,omitempty" jsonschema:"HTTPS username for private clone (defaults to x-access-token)"`
		CredToken    string `json:"credToken,omitempty" jsonschema:"HTTPS token/PAT for a private clone (for clone mode)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_repo",
		Description: "Clone, create, or prepare a repository for use",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args prepareRepoArgs) (*mcp.CallToolResult, any, error) {
		switch args.Mode {
		case "workspace":
			workDir := workspaceDir
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
				if out, err := runGitWithTransientCred(args.CredHost, args.CredUsername, args.CredToken, "-C", repoPath, "fetch", "--all"); err != nil {
					return nil, nil, fmt.Errorf("git fetch failed: %s", string(out))
				}
			} else {
				if err := os.MkdirAll(repoBase, 0755); err != nil {
					return nil, nil, fmt.Errorf("failed to create directory: %w", err)
				}
				if out, err := runGitWithTransientCred(args.CredHost, args.CredUsername, args.CredToken, "clone", args.URL, repoPath); err != nil {
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
		if exists && sess.AgentChatPort != 0 {
			result, err := callAgentChatOrchestrator(sess.AgentChatPort, "get_chat_history", map[string]int64{"cursor": args.Cursor})
			if err != nil {
				return nil, nil, fmt.Errorf("agent chat error: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result}}}, nil, nil
		}
		// Fallback: read chat events from the ended recording's .events.jsonl.
		// This lets get_chat_history work for sessions shown in list_recordings
		// (hasChat: true) after they've ended.
		path := findChatEventsFile(args.UUID)
		if path == "" {
			if exists {
				return nil, nil, fmt.Errorf("session has no agent chat (terminal-only session)")
			}
			return nil, nil, fmt.Errorf("session not found")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read chat events: %w", err)
		}
		events := []json.RawMessage{}
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			if args.Cursor > 0 {
				var hdr struct {
					Seq int64 `json:"seq"`
				}
				if err := json.Unmarshal(line, &hdr); err == nil && hdr.Seq <= args.Cursor {
					continue
				}
			}
			events = append(events, json.RawMessage(append([]byte(nil), line...)))
		}
		out, err := json.Marshal(events)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal events: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(out)}}}, nil, nil
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

// testGitCredentials attempts an authenticated GET against the forge host
// using HTTP Basic auth. Returns (ok, human-readable message) for the
// settings panel "Test connection" button. Does not persist anything.
//
// For github.com we hit api.github.com/user, which returns the
// authenticated user's login on success and 401 on a bad token.
// For other hosts we hit https://{host}/ which only confirms the host is
// reachable and what status the server returns for our Basic-auth request --
// we do not assume any particular API surface.
func testGitCredentials(ctx context.Context, host, username, token string) (bool, string) {
	if username == "" {
		username = "x-access-token"
	}
	// GitLab's site root returns 404 for an authenticated Basic-auth GET,
	// so the generic path below would mis-report a valid token as "Not
	// found". Probe the GitLab API first for non-github hosts; it gives a
	// definitive 200/401 on a GitLab instance, and we fall through to the
	// generic GET when the host is clearly not GitLab.
	if host != "github.com" {
		if handled, ok, msg := testGitLabCredentials(ctx, host, token); handled {
			return ok, msg
		}
	}
	var reqURL string
	if host == "github.com" {
		reqURL = "https://api.github.com/user"
	} else {
		reqURL = "https://" + host + "/"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, "request error: " + err.Error()
	}
	req.SetBasicAuth(username, token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "swe-swe/test-credentials")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, "Cannot reach " + host + ": " + err.Error()
	}
	defer resp.Body.Close()

	if host == "github.com" && resp.StatusCode == http.StatusOK {
		var body struct {
			Login string `json:"login"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Login != "" {
			return true, "Connected as @" + body.Login
		}
		return true, "Connected"
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return true, "Reached " + host + " (HTTP 200) -- token accepted"
	case http.StatusUnauthorized:
		return false, "Invalid credentials (HTTP 401)"
	case http.StatusForbidden:
		return false, "Forbidden (HTTP 403) -- token may lack scope"
	case http.StatusNotFound:
		return false, "Not found (HTTP 404)"
	}
	return false, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, host)
}

// testGitLabCredentials probes {host}/api/v4/user with the PAT in the
// PRIVATE-TOKEN header (how GitLab authenticates personal access tokens).
// Returns handled=false to mean "this does not look like a GitLab API
// surface, fall back to the generic check" -- distinct from a definitive
// 401/403. Used because GitLab's site root 404s a Basic-auth GET, which
// the generic path would mis-report.
func testGitLabCredentials(ctx context.Context, host, token string) (handled, ok bool, msg string) {
	return testGitLabAPI(ctx, "https://"+host+"/api/v4/user", token)
}

// testGitLabAPI is the host-agnostic core of testGitLabCredentials, taking
// the full API URL so it can be exercised against an httptest server.
func testGitLabAPI(ctx context.Context, reqURL, token string) (handled, ok bool, msg string) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, false, ""
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("User-Agent", "swe-swe/test-credentials")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network error -> let the generic path report the unreachable host.
		return false, false, ""
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Username string `json:"username"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Username != "" {
			return true, true, "Connected as @" + body.Username + " (GitLab)"
		}
		return true, true, "Connected (GitLab)"
	case http.StatusUnauthorized:
		return true, false, "Invalid credentials (HTTP 401)"
	case http.StatusForbidden:
		return true, false, "Forbidden (HTTP 403) -- token may lack scope"
	}
	// 404 or anything else: not a GitLab API surface. Fall back.
	return false, false, ""
}
