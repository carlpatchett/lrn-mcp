package db

import (
	"time"
)

// Tool represents a tracked CLI tool
type Tool struct {
	ID         string    `json:"id"`
	Enabled    bool      `json:"enabled"`
	Strictness string    `json:"strictness"` // lenient, standard, draconian
	CreatedAt  time.Time `json:"created_at"`
}

// CommandPattern represents a normalized command template
type CommandPattern struct {
	ID              string    `json:"id"`
	ToolID          string    `json:"tool_id"`
	PatternTemplate string    `json:"pattern_template"` // e.g. "kubectl get pods -n <NAMESPACE>"
	CanonicalTokens string    `json:"canonical_tokens"` // JSON token array
	Summary         string    `json:"summary"`          // Human description
	SafetyLevel     string    `json:"safety_level"`     // read_only, mutating, destructive, privileged
	CreatedAt       time.Time `json:"created_at"`
}

// FlagDefinition contains flag knowledge and explanations
type FlagDefinition struct {
	ID          string `json:"id"`
	ToolID      string `json:"tool_id"`
	Subcommand  string `json:"subcommand"`
	FlagName    string `json:"flag_name"`
	FlagType    string `json:"flag_type"` // string, boolean, int, array
	Explanation string `json:"explanation"`
	IsCommon    bool   `json:"is_common"`
}

// RepetitionItem represents SM-2 spaced repetition state
type RepetitionItem struct {
	ID             string     `json:"id"`
	ItemType       string     `json:"item_type"` // pattern, flag, concept
	TargetID       string     `json:"target_id"`
	Repetitions    int        `json:"repetitions"`
	IntervalDays   float64    `json:"interval_days"`
	EaseFactor     float64    `json:"ease_factor"`
	LastReviewedAt *time.Time `json:"last_reviewed_at"`
	NextReviewAt   time.Time  `json:"next_review_at"`
	MasteryScore   float64    `json:"mastery_score"`
	Status         string     `json:"status"` // learning, reviewing, mastered, ignored
}

// ExecutionLog records command runs and quiz responses
type ExecutionLog struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	RawCommand     string    `json:"raw_command"`
	PatternID      string    `json:"pattern_id"`
	Mode           string    `json:"mode"` // agent_proxy, hook, cli_shim, quiz
	IsInteractive  bool      `json:"is_interactive"`
	ActionTaken    string    `json:"action_taken"` // auto_run, challenge_passed, challenge_failed, snoozed, bypassed, ignored
	QualityScore   *int      `json:"quality_score"`
	ResponseTimeMs int       `json:"response_time_ms"`
	ExecutedAt     time.Time `json:"executed_at"`
}

// SnoozeRule manages temporary learning bypasses
type SnoozeRule struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"` // tool, pattern, global
	TargetID    string    `json:"target_id"`
	Reason      string    `json:"reason"`
	SnoozeUntil time.Time `json:"snooze_until"`
	CreatedAt   time.Time `json:"created_at"`
}

// KnownExclusion manages permanently excluded commands/patterns
type KnownExclusion struct {
	ID           string    `json:"id"`
	Scope        string    `json:"scope"` // tool, pattern, exact_command
	Target       string    `json:"target"`
	MarkedReason string    `json:"marked_reason"`
	CreatedAt    time.Time `json:"created_at"`
}

// LearningStats represents overview progress
type LearningStats struct {
	TotalPatternsTracked int            `json:"total_patterns_tracked"`
	DueReviewsCount      int            `json:"due_reviews_count"`
	MasteredCount        int            `json:"mastered_count"`
	LearningCount        int            `json:"learning_count"`
	TotalExecutions      int            `json:"total_executions"`
	ToolBreakdown        map[string]int `json:"tool_breakdown"`
	ActiveSnoozesCount   int            `json:"active_snoozes_count"`
}
