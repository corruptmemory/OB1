package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	flags "github.com/jessevdk/go-flags"
)

type ServeCmd struct {
	Config string `long:"config" default:"open-brain-dashboard-go.toml" description:"Path to config file"`
	Listen string `long:"listen" description:"Override the config's listen address (host:port)"`
}

type MCPCmd struct {
	Config string `long:"config" default:"open-brain-dashboard-go.toml" description:"Path to config file"`
	Listen string `long:"listen" description:"Override the config's MCP listen address (host:port)"`
	Key    string `long:"key" description:"Override the config's MCP access key"`
}

type SystemCmd struct {
	Config string `long:"config" default:"open-brain-dashboard-go.toml" description:"Path to config file"`
}

type GenConfigCmd struct {
	Output string `long:"output" default:"open-brain-dashboard-go.toml" description:"Destination path for the generated config"`
	Force  bool   `long:"force" description:"Overwrite an existing file at --output"`
}

type Options struct {
	Serve     ServeCmd     `command:"serve" description:"Run the dashboard HTTP server"`
	MCP       MCPCmd       `command:"mcp" description:"Run the MCP server"`
	System    SystemCmd    `command:"system" description:"Run the dashboard and MCP server together (shared DB and Ollama)"`
	GenConfig GenConfigCmd `command:"gen-config" description:"Write a default config file to --output"`
}

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	parser.Name = "open-brain-dashboard-go"
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if parser.Active == nil {
		parser.WriteHelp(os.Stderr)
		os.Exit(1)
	}

	var err error
	switch parser.Active.Name {
	case "serve":
		err = runServe(opts.Serve)
	case "mcp":
		err = runMCP(opts.MCP)
	case "system":
		err = runSystem(opts.System)
	case "gen-config":
		err = runGenConfig(opts.GenConfig)
	default:
		err = fmt.Errorf("unknown command: %s", parser.Active.Name)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, parser.Active.Name+":", err)
		os.Exit(1)
	}
}

// runServe is the `serve` subcommand: dashboard only.
func runServe(cmd ServeCmd) error {
	cfg, err := LoadConfig(cmd.Config)
	if err != nil {
		return err
	}
	if cmd.Listen != "" {
		cfg.Server.Listen = cmd.Listen
	}

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	dbCtx, cancelDB := context.WithTimeout(serverCtx, 10*time.Second)
	defer cancelDB()

	db, err := NewDB(dbCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	ollama := NewOllamaClient(cfg.Ollama.URL, cfg.Ollama.EmbeddingModel, cfg.Ollama.ChatModel)
	health := NewOllamaHealth(serverCtx)

	return serveDashboard(cfg, db, ollama, health)
}

// runMCP is the `mcp` subcommand: MCP server only.
func runMCP(cmd MCPCmd) error {
	cfg, err := LoadConfig(cmd.Config)
	if err != nil {
		return err
	}
	if cmd.Listen != "" {
		cfg.MCP.Listen = cmd.Listen
	}
	if cmd.Key != "" {
		cfg.MCP.AccessKey = cmd.Key
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()

	dbCtx, cancelDB := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDB()

	db, err := NewDB(dbCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	ollama := NewOllamaClient(cfg.Ollama.URL, cfg.Ollama.EmbeddingModel, cfg.Ollama.ChatModel)

	return serveMCP(cfg, db, ollama)
}

// runSystem is the `system` subcommand: dashboard + MCP sharing a single DB
// pool and Ollama client. Returns when either server exits.
func runSystem(cmd SystemCmd) error {
	cfg, err := LoadConfig(cmd.Config)
	if err != nil {
		return err
	}

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	dbCtx, cancelDB := context.WithTimeout(serverCtx, 10*time.Second)
	defer cancelDB()

	db, err := NewDB(dbCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	ollama := NewOllamaClient(cfg.Ollama.URL, cfg.Ollama.EmbeddingModel, cfg.Ollama.ChatModel)
	health := NewOllamaHealth(serverCtx)

	errCh := make(chan error, 2)
	go func() { errCh <- serveDashboard(cfg, db, ollama, health) }()
	go func() { errCh <- serveMCP(cfg, db, ollama) }()

	// Return on the first error (or clean exit). The deferred cancelServer
	// will signal the remaining goroutine to unblock, and deferred db.Close
	// will tear down the pool.
	return <-errCh
}

// serveDashboard is the inner dashboard server — extracted so runSystem
// can run it concurrently with serveMCP on shared deps.
func serveDashboard(cfg *Config, db *DB, ollama *OllamaClient, health *OllamaHealth) error {
	srv := NewServer(db, ollama, health)
	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("open-brain dashboard listening on http://%s", cfg.Server.Listen)
	return httpSrv.ListenAndServe()
}

func runGenConfig(cmd GenConfigCmd) error {
	if !cmd.Force {
		if _, err := os.Stat(cmd.Output); err == nil {
			return fmt.Errorf("refusing to overwrite existing file %q (use --force to replace)", cmd.Output)
		}
	}
	if err := WriteConfig(cmd.Output, DefaultConfig()); err != nil {
		return err
	}
	fmt.Printf("Wrote default config to %s\n", cmd.Output)
	return nil
}
