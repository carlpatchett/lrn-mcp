package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStore_InitAndCRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "learning_db_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// 1. Verify default tools seeded
	tool, err := store.GetTool("kubectl")
	if err != nil {
		t.Fatalf("unexpected error getting tool: %v", err)
	}
	if tool == nil || !tool.Enabled {
		t.Fatalf("expected kubectl tool to be seeded and enabled")
	}

	// 2. Verify seeded flag
	flag, err := store.FindFlag("kubectl", "", "-n")
	if err != nil {
		t.Fatalf("unexpected error finding flag: %v", err)
	}
	if flag == nil || flag.FlagName != "-n" {
		t.Fatalf("expected flag -n to be found, got %+v", flag)
	}

	// 3. Upsert pattern & fetch
	pat := &CommandPattern{
		ID:              uuid.NewString(),
		ToolID:          "kubectl",
		PatternTemplate: "kubectl get pods -n <NAMESPACE>",
		CanonicalTokens: `["kubectl", "get", "pods", "-n", "<NAMESPACE>"]`,
		Summary:         "List Kubernetes pods in a specific namespace",
		SafetyLevel:     "read_only",
	}
	if err := store.UpsertPattern(pat); err != nil {
		t.Fatalf("failed to upsert pattern: %v", err)
	}

	fetchedPat, err := store.GetPattern(pat.ID)
	if err != nil {
		t.Fatalf("failed to get pattern: %v", err)
	}
	if fetchedPat.Summary != pat.Summary {
		t.Fatalf("expected summary %q, got %q", pat.Summary, fetchedPat.Summary)
	}

	// 4. Upsert repetition item & check due
	rep := &RepetitionItem{
		ID:           pat.ID,
		ItemType:     "pattern",
		TargetID:     pat.ID,
		Repetitions:  0,
		IntervalDays: 0,
		EaseFactor:   2.5,
		NextReviewAt: time.Now().Add(-1 * time.Hour), // due now
		MasteryScore: 0.1,
		Status:       "learning",
	}
	if err := store.UpsertRepetitionItem(rep); err != nil {
		t.Fatalf("failed to upsert repetition item: %v", err)
	}

	due, err := store.GetDueRepetitionItems(10, "")
	if err != nil {
		t.Fatalf("failed to get due items: %v", err)
	}
	if len(due) == 0 {
		t.Fatalf("expected at least 1 due item, got 0")
	}

	// 5. Test Snooze
	snooze := &SnoozeRule{
		ID:          uuid.NewString(),
		Scope:       "tool",
		TargetID:    "kubectl",
		Reason:      "Rushing production debug",
		SnoozeUntil: time.Now().Add(1 * time.Hour),
	}
	if err := store.AddSnoozeRule(snooze); err != nil {
		t.Fatalf("failed to add snooze: %v", err)
	}

	isSnoozed, activeRule, err := store.IsSnoozed("kubectl", pat.ID)
	if err != nil {
		t.Fatalf("failed checking snooze: %v", err)
	}
	if !isSnoozed || activeRule == nil {
		t.Fatalf("expected tool kubectl to be snoozed")
	}

	snoozes, err := store.ListSnoozes()
	if err != nil || len(snoozes) != 1 {
		t.Fatalf("expected 1 active snooze rule, got %d (err: %v)", len(snoozes), err)
	}

	// 6. Test Exclusions
	ex := &KnownExclusion{
		ID:           uuid.NewString(),
		Scope:        "exact_command",
		Target:       "git status",
		MarkedReason: "Trivial standard command",
	}
	if err := store.AddKnownExclusion(ex); err != nil {
		t.Fatalf("failed to add exclusion: %v", err)
	}

	isKnown, foundEx, err := store.IsKnown("git", "git_status", "git status")
	if err != nil {
		t.Fatalf("failed checking exclusion: %v", err)
	}
	if !isKnown || foundEx == nil {
		t.Fatalf("expected 'git status' to be marked as known")
	}

	exclusions, err := store.ListExclusions()
	if err != nil || len(exclusions) != 1 {
		t.Fatalf("expected 1 exclusion, got %d (err: %v)", len(exclusions), err)
	}

	// 7. Test Stats
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("failed getting stats: %v", err)
	}
	if stats.TotalPatternsTracked != 1 {
		t.Fatalf("expected 1 pattern tracked, got %d", stats.TotalPatternsTracked)
	}
}
