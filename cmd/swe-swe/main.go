package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alvinchoong/go-httphandler"
	"golang.org/x/sync/errgroup"
)

// Config holds the server configuration
type Config struct {
	Port            int
	Timeout         time.Duration
	AgentCLI        string
	AgentCLIResume  string
	PrefixPath      string
	DeferStdinClose bool
	JSONOutput      bool
}

func main() {
	if err := errmain(); err != nil {
		log.Fatal(err)
	}
}

func errmain() error {
	// Parse command line flags
	config := Config{}
	flag.IntVar(&config.Port, "port", 7000, "Port to listen on")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "Server timeout")
	flag.StringVar(&config.AgentCLI, "agent-cli", "goose run --resume --debug --text ?", "Agent CLI command template (use ? as placeholder for prompt, include the resume session flag)")
	flag.StringVar(&config.AgentCLIResume, "agent-cli-resume", "--resume", "Resume flag to remove from -agent-cli on first execution")
	flag.StringVar(&config.PrefixPath, "prefix-path", "", "URL prefix path for serving assets (e.g., /myapp)")
	flag.BoolVar(&config.DeferStdinClose, "defer-stdin-close", true, "Defer closing stdin until process ends (true for goose, false for claude)")
	flag.BoolVar(&config.JSONOutput, "json-output", false, "Parse JSON stream output (true for claude with stream-json format)")
	flag.Parse()

	// Ensure prefix path starts with / if provided
	if config.PrefixPath != "" && !strings.HasPrefix(config.PrefixPath, "/") {
		config.PrefixPath = "/" + config.PrefixPath
	}
	// Remove trailing slash
	config.PrefixPath = strings.TrimSuffix(config.PrefixPath, "/")

	// Setup context with signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create chat service
	chatsvc := NewChatService(config.AgentCLI, config.AgentCLIResume, config.DeferStdinClose, config.JSONOutput)

	// Run services concurrently
	return runCtxFuncs(ctx,
		runChatService(chatsvc),
		runWebServer(config, chatsvc),
	)
}

func runChatService(chatsvc *ChatService) func(context.Context) error {
	return func(ctx context.Context) error {
		return chatsvc.Run(ctx)
	}
}

func runWebServer(config Config, chatsvc *ChatService) func(context.Context) error {
	return func(ctx context.Context) error {
		// Load static assets with hashes
		assets, err := getStaticAssets()
		if err != nil {
			return fmt.Errorf("failed to load static assets: %w", err)
		}

		// Create server mux
		mux := http.NewServeMux()

		// Set up routes with prefix
		if config.PrefixPath != "" {
			// Serve index at prefix path
			mux.Handle(config.PrefixPath+"/", httphandler.Handle(indexHandler(config, assets)))

			// Serve static files
			mux.HandleFunc(config.PrefixPath+"/css/", staticHandler(config, assets))
			mux.HandleFunc(config.PrefixPath+"/js/", staticHandler(config, assets))

			// Set up websocket handler
			mux.Handle(config.PrefixPath+"/ws", httphandler.Handle(chatWebsocketHandler(chatsvc)))
		} else {
			// Serve at root
			mux.Handle("/", httphandler.Handle(indexHandler(config, assets)))

			// Serve static files
			mux.HandleFunc("/css/", staticHandler(config, assets))
			mux.HandleFunc("/js/", staticHandler(config, assets))

			// Set up websocket handler
			mux.Handle("/ws", httphandler.Handle(chatWebsocketHandler(chatsvc)))
		}

		// Configure server
		server := &http.Server{
			Addr:         fmt.Sprintf(":%d", config.Port),
			Handler:      mux,
			ReadTimeout:  config.Timeout,
			WriteTimeout: config.Timeout,
		}

		// Start server in a goroutine
		serverErr := make(chan error, 1)
		go func() {
			log.Printf("Server starting on http://localhost:%d%s", config.Port, config.PrefixPath)
			serverErr <- server.ListenAndServe()
		}()

		// Wait for context cancellation or server error
		select {
		case <-ctx.Done():
			log.Println("Shutting down server gracefully...")
			return performGracefulShutdown(server)

		case err := <-serverErr:
			return fmt.Errorf("server error: %w", err)
		}
	}
}

// performGracefulShutdown performs a graceful shutdown of the HTTP server
func performGracefulShutdown(server *http.Server) error {
	ctx, cancel := context.WithTimeout(
		context.Background(), // if we use main's ctx, it might already be cancelled
		10*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}
	return nil
}

// Give me a list of `func(context.Context) error`. That. Is. All.
// Preferred.
func runCtxFuncs(parentCtx context.Context, services ...func(context.Context) error) error {
	g, ctx := errgroup.WithContext(parentCtx)

	for i := range services {
		service := services[i]
		g.Go(func() error {
			// if any service returns error, the shared `ctx` will be cancelled
			// which auto stops other services
			return service(ctx)
		})
	}

	// blocks until all [service func] have returned, then returns the first non-nil error (if any) from them.
	// https://godoc.org/golang.org/x/sync/errgroup#Group.Wait
	return g.Wait()
}
