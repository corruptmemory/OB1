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

type GenConfigCmd struct {
	Output string `long:"output" default:"open-brain-dashboard-go.toml" description:"Destination path for the generated config"`
	Force  bool   `long:"force" description:"Overwrite an existing file at --output"`
}

type Options struct {
	Serve     ServeCmd     `command:"serve" description:"Run the dashboard HTTP server"`
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

func runServe(cmd ServeCmd) error {
	cfg, err := LoadConfig(cmd.Config)
	if err != nil {
		return err
	}
	if cmd.Listen != "" {
		cfg.Server.Listen = cmd.Listen
	}

	// Server-lifetime context used by background goroutines (like the
	// OllamaHealth actor) so they exit cleanly when runServe returns.
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

	srv := NewServer(db, ollama, health)

	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("open-brain-dashboard-go listening on http://%s", cfg.Server.Listen)
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
