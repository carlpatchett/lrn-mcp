package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"learning-mcp/internal/config"
	"learning-mcp/internal/db"
	"learning-mcp/internal/mcp"
)

func TestLearningMCPEntrypoint_StdioPipeline(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "entrypoint_test_*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	cfg := &config.Config{DBPath: dbPath}
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening store: %v", err)
	}
	defer store.Close()

	server := mcp.NewServer(cfg, store)

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n"
	inBuf := bytes.NewBufferString(input)
	outBuf := &bytes.Buffer{}
	server.SetOutput(outBuf)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := server.Run(ctx, inBuf); err != nil {
		t.Fatalf("unexpected server run error: %v", err)
	}

	resStr := strings.TrimSpace(outBuf.String())
	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(resStr), &resp); err != nil {
		t.Fatalf("invalid json-rpc response: %v\noutput: %s", err, resStr)
	}
	if resp.Error != nil {
		t.Fatalf("server returned error: %+v", resp.Error)
	}
}
