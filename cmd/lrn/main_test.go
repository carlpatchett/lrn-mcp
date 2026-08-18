package main

import (
	"os"
	"path/filepath"
	"testing"

	"learning-mcp/internal/db"
	"learning-mcp/internal/engine"
)

func TestLRN_Commands(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lrn_test_*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening db: %v", err)
	}
	defer store.Close()

	evaluator := engine.NewEvaluator(store)

	t.Run("runExplain smoke test", func(t *testing.T) {
		runExplain(store, "kubectl get pods -n prod -l app=web")
	})

	t.Run("runStats smoke test", func(t *testing.T) {
		runStats(store)
	})

	t.Run("printHelp smoke test", func(t *testing.T) {
		printHelp()
	})

	t.Run("runReview empty queue test", func(t *testing.T) {
		runReview(store, evaluator, "")
	})
}
