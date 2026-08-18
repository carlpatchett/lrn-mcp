package mcp

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
)

func TestMCPServer_FullRoundtrip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "learning_mcp_test_*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening store: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		DBPath: dbPath,
	}

	server := NewServer(cfg, store)

	// Create request stream
	requests := []string{
		// 1. Initialize
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`,
		// 2. Initialized Notification (no ID)
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		// 3. List Tools
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		// 4. Call explain_command
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"explain_command","arguments":{"command":"kubectl get pods -n prod -l app=web"}}}`,
		// 5. Call learn_exec (evaluate_only default)
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"learn_exec","arguments":{"command":"kubectl get pods -n prod -l app=web","action":"evaluate_only"}}}`,
		// 6. Call learn_exec (user_completed)
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"learn_exec","arguments":{"command":"kubectl get pods -n prod -l app=web","action":"user_completed","quality_score":4}}}`,
		// 7. Snooze learning
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"snooze_learning","arguments":{"scope":"tool","target":"kubectl","duration_minutes":60,"reason":"Debug outage"}}}`,
		// 8. Mark known
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mark_known","arguments":{"scope":"exact_command","target":"git status","reason":"Trivial"}}}`,
		// 9. List Resources
		`{"jsonrpc":"2.0","id":8,"method":"resources/list","params":{}}`,
		// 10. Read Resource
		`{"jsonrpc":"2.0","id":9,"method":"resources/read","params":{"uri":"learning://stats/overview"}}`,
		// 11. List Prompts
		`{"jsonrpc":"2.0","id":10,"method":"prompts/list","params":{}}`,
		// 12. Get Prompt
		`{"jsonrpc":"2.0","id":11,"method":"prompts/get","params":{"name":"spaced_review_session","arguments":{"limit":"3"}}}`,
	}

	inputBuffer := bytes.NewBufferString(strings.Join(requests, "\n") + "\n")
	outputBuffer := &bytes.Buffer{}
	server.SetOutput(outputBuffer)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := server.Run(ctx, inputBuffer); err != nil {
		t.Fatalf("server run error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(outputBuffer.String()), "\n")
	if len(lines) < 11 {
		t.Fatalf("expected at least 11 responses, got %d:\n%s", len(lines), outputBuffer.String())
	}

	for idx, line := range lines {
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Errorf("line %d is not valid JSON-RPC response: %v\ncontent: %s", idx, err, line)
		}
		if resp.Error != nil {
			t.Errorf("line %d returned error: %+v", idx, resp.Error)
		}
	}
}

func TestApprovalTokenValidation(t *testing.T) {
	patternID := "kubectl_get_pods_123"
	rawCmd := "kubectl get pods -n prod"

	token := generateApprovalToken(patternID, rawCmd)
	if len(token) != 64 {
		t.Fatalf("expected 64-character full SHA-256 HMAC approval token, got length %d (%s)", len(token), token)
	}

	if !validateApprovalToken(patternID, rawCmd, token) {
		t.Errorf("expected valid token to pass validation")
	}

	if validateApprovalToken(patternID, rawCmd, "invalid_token_xyz") {
		t.Errorf("expected forged token to fail validation")
	}

	if validateApprovalToken(patternID, rawCmd, "") {
		t.Errorf("expected empty token to fail validation")
	}

	if validateApprovalToken("different_pattern", rawCmd, token) {
		t.Errorf("expected token for different pattern to fail validation")
	}
}
