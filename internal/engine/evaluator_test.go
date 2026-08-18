package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"learning-mcp/internal/db"
	"learning-mcp/internal/parser"
)

func setupTestStore(t *testing.T) (*db.Store, func()) {
	tempDir, err := os.MkdirTemp("", "evaluator_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tempDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	return store, func() {
		store.Close()
		os.RemoveAll(tempDir)
	}
}

func TestEvaluator_Actions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	evaluator := NewEvaluator(store)

	t.Run("untracked command returns ActionUntracked", func(t *testing.T) {
		res, err := evaluator.Evaluate("curl https://example.com", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Action != ActionUntracked {
			t.Errorf("expected ActionUntracked, got %s", res.Action)
		}
	})

	t.Run("new tracked command returns ActionInteractiveChallenge", func(t *testing.T) {
		res, err := evaluator.Evaluate("kubectl get pods -n prod", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Action != ActionInteractiveChallenge {
			t.Errorf("expected ActionInteractiveChallenge, got %s", res.Action)
		}
		if res.ChallengeText == "" {
			t.Errorf("expected challenge text, got empty string")
		}
	})

	t.Run("non-interactive mode auto-approves for automation", func(t *testing.T) {
		res, err := evaluator.Evaluate("kubectl get pods -n prod", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Action != ActionAutoExecute {
			t.Errorf("expected ActionAutoExecute for non-interactive, got %s", res.Action)
		}
	})

	t.Run("snoozed tool returns ActionSnoozed", func(t *testing.T) {
		_, err := evaluator.Snooze("tool", "git", "Testing outage", 1*time.Hour)
		if err != nil {
			t.Fatalf("failed to snooze: %v", err)
		}

		res, err := evaluator.Evaluate("git commit -m 'test'", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Action != ActionSnoozed {
			t.Errorf("expected ActionSnoozed, got %s", res.Action)
		}
	})

	t.Run("known exclusion returns ActionKnownBypass", func(t *testing.T) {
		_, err := evaluator.MarkKnown("exact_command", "docker ps", "Simple query")
		if err != nil {
			t.Fatalf("failed to mark known: %v", err)
		}

		res, err := evaluator.Evaluate("docker ps", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Action != ActionKnownBypass {
			t.Errorf("expected ActionKnownBypass, got %s", res.Action)
		}
	})

	t.Run("submit score updates repetition state", func(t *testing.T) {
		parsed, _ := parser.Parse("jj describe -m 'wip'", store)
		item, err := evaluator.SubmitScore(parsed.PatternID, 5)
		if err != nil {
			t.Fatalf("unexpected error submitting score: %v", err)
		}
		if item.Repetitions != 1 {
			t.Errorf("expected 1 repetition, got %d", item.Repetitions)
		}
		if item.IntervalDays != 1.0 {
			t.Errorf("expected 1.0 interval, got %f", item.IntervalDays)
		}
	})
}

func TestEvaluator_ExecuteSafe(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	evaluator := NewEvaluator(store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	t.Run("execute standard echo command", func(t *testing.T) {
		parsed, err := parser.Parse("echo hello-learning-mcp", store)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		out, err := evaluator.ExecuteSafe(ctx, parsed, "")
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if out.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", out.ExitCode)
		}
	})

	t.Run("execute nonexistent binary reports stderr error", func(t *testing.T) {
		parsed, err := parser.Parse("nonexistent_binary_xyz_123 -a", store)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		out, err := evaluator.ExecuteSafe(ctx, parsed, "")
		if err != nil {
			t.Fatalf("unexpected go error: %v", err)
		}
		if out.ExitCode == 0 {
			t.Errorf("expected nonzero exit code for nonexistent binary")
		}
		if out.Stderr == "" {
			t.Errorf("expected stderr error message, got empty string")
		}
	})
}
