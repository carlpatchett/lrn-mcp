package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"learning-mcp/internal/config"
	"learning-mcp/internal/db"
	"learning-mcp/internal/mcp"
)

func main() {
	dbPathFlag := flag.String("db", "", "Path to SQLite database")
	flag.Parse()

	// Direct logs to stderr so stdout remains clean for MCP JSON-RPC protocol
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed loading config: %v", err)
	}

	if *dbPathFlag != "" {
		cfg.DBPath = *dbPathFlag
	}

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed initializing database at %s: %v", cfg.DBPath, err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := mcp.NewServer(cfg, store)
	log.Printf("Starting lrn-mcp server (db: %s)...", cfg.DBPath)
	if err := server.Run(ctx, os.Stdin); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
