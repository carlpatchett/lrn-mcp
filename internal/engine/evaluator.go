package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"learning-mcp/internal/db"
	"learning-mcp/internal/parser"
)

// Action types
const (
	ActionAutoExecute          = "auto_execute"
	ActionInteractiveChallenge = "interactive_challenge"
	ActionSnoozed              = "snoozed"
	ActionKnownBypass          = "known_bypass"
	ActionUntracked            = "untracked"
)

// EvaluationResult contains decision and presentation payload
type EvaluationResult struct {
	Action         string                `json:"action"` // ActionAutoExecute, ActionInteractiveChallenge, etc.
	Parsed         *parser.ParsedCommand `json:"parsed"`
	RepetitionItem *db.RepetitionItem    `json:"repetition_item,omitempty"`
	SnoozeRule     *db.SnoozeRule        `json:"snooze_rule,omitempty"`
	KnownExclusion *db.KnownExclusion    `json:"known_exclusion,omitempty"`
	ChallengeText  string                `json:"challenge_text,omitempty"`
	Explanation    string                `json:"explanation,omitempty"`
}

// ExecutionOutput holds command run output
type ExecutionOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Duration int64  `json:"duration_ms"`
}

// Evaluator coordinates learning evaluations and execution
type Evaluator struct {
	store *db.Store
}

// NewEvaluator creates a new learning evaluator
func NewEvaluator(store *db.Store) *Evaluator {
	return &Evaluator{store: store}
}

// Evaluate analyzes a raw command and returns the learning decision
func (e *Evaluator) Evaluate(rawCmd string, interactive bool) (*EvaluationResult, error) {
	parsed, err := parser.Parse(rawCmd, e.store)
	if err != nil {
		return nil, fmt.Errorf("parsing failed: %w", err)
	}

	// 1. Check if tool is tracked
	tool, err := e.store.GetTool(parsed.BaseTool)
	if err != nil {
		return nil, err
	}
	if tool == nil || !tool.Enabled {
		return &EvaluationResult{
			Action: ActionUntracked,
			Parsed: parsed,
		}, nil
	}

	// 2. Check known exclusions (exact command, pattern, or tool)
	isKnown, exclusion, err := e.store.IsKnown(parsed.BaseTool, parsed.PatternID, parsed.RawCommand)
	if err != nil {
		return nil, err
	}
	if isKnown {
		return &EvaluationResult{
			Action:         ActionKnownBypass,
			Parsed:         parsed,
			KnownExclusion: exclusion,
			Explanation:    fmt.Sprintf("Command pattern '%s' is marked as known.", parsed.PatternID),
		}, nil
	}

	// 3. Check snooze rules (global, tool, or pattern)
	isSnoozed, snoozeRule, err := e.store.IsSnoozed(parsed.BaseTool, parsed.PatternID)
	if err != nil {
		return nil, err
	}
	if isSnoozed {
		return &EvaluationResult{
			Action:      ActionSnoozed,
			Parsed:      parsed,
			SnoozeRule:  snoozeRule,
			Explanation: fmt.Sprintf("Learning is snoozed until %s (reason: %s).", snoozeRule.SnoozeUntil.Format(time.RFC3339), snoozeRule.Reason),
		}, nil
	}

	// 4. Ensure pattern is recorded in SQLite
	canonicalBytes, _ := json.Marshal(parsed.Subcommands)
	patternRec := &db.CommandPattern{
		ID:              parsed.PatternID,
		ToolID:          parsed.BaseTool,
		PatternTemplate: parsed.PatternTemplate,
		CanonicalTokens: string(canonicalBytes),
		Summary:         parsed.Summary,
		SafetyLevel:     parsed.SafetyLevel,
	}
	_ = e.store.UpsertPattern(patternRec)

	// 5. Fetch repetition item
	repItem, err := e.store.GetRepetitionItem(parsed.PatternID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	isDue := false

	if repItem == nil {
		// First time seeing this pattern
		repItem = &db.RepetitionItem{
			ID:           parsed.PatternID,
			ItemType:     "pattern",
			TargetID:     parsed.PatternID,
			Repetitions:  0,
			IntervalDays: 0.0,
			EaseFactor:   2.5,
			NextReviewAt: now,
			MasteryScore: 0.1,
			Status:       "learning",
		}
		_ = e.store.UpsertRepetitionItem(repItem)
		isDue = true
	} else {
		isDue = repItem.Status != "mastered" && (repItem.NextReviewAt.Before(now) || repItem.NextReviewAt.Equal(now))
	}

	if !isDue || !interactive {
		return &EvaluationResult{
			Action:         ActionAutoExecute,
			Parsed:         parsed,
			RepetitionItem: repItem,
			Explanation:    parsed.Summary,
		}, nil
	}

	// 6. Generate interactive challenge
	challengeText := generateChallenge(parsed)
	return &EvaluationResult{
		Action:         ActionInteractiveChallenge,
		Parsed:         parsed,
		RepetitionItem: repItem,
		ChallengeText:  challengeText,
		Explanation:    formatExplanation(parsed),
	}, nil
}

// SubmitScore updates repetition item SM-2 interval
func (e *Evaluator) SubmitScore(patternID string, qualityScore int) (*db.RepetitionItem, error) {
	item, err := e.store.GetRepetitionItem(patternID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sm2 := CalculateSM2(item, qualityScore, now)

	if item == nil {
		item = &db.RepetitionItem{
			ID:       patternID,
			ItemType: "pattern",
			TargetID: patternID,
		}
	}

	item.Repetitions = sm2.Repetitions
	item.IntervalDays = sm2.IntervalDays
	item.EaseFactor = sm2.EaseFactor
	item.LastReviewedAt = &now
	item.NextReviewAt = sm2.NextReviewAt
	item.MasteryScore = sm2.MasteryScore
	item.Status = sm2.Status

	if err := e.store.UpsertRepetitionItem(item); err != nil {
		return nil, err
	}

	return item, nil
}

// Snooze creates a snooze rule
func (e *Evaluator) Snooze(scope, targetID, reason string, duration time.Duration) (*db.SnoozeRule, error) {
	rule := &db.SnoozeRule{
		ID:          uuid.NewString(),
		Scope:       scope,
		TargetID:    targetID,
		Reason:      reason,
		SnoozeUntil: time.Now().UTC().Add(duration),
	}
	if err := e.store.AddSnoozeRule(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

// MarkKnown creates a known exclusion rule
func (e *Evaluator) MarkKnown(scope, target, reason string) (*db.KnownExclusion, error) {
	ex := &db.KnownExclusion{
		ID:           uuid.NewString(),
		Scope:        scope,
		Target:       target,
		MarkedReason: reason,
	}
	if err := e.store.AddKnownExclusion(ex); err != nil {
		return nil, err
	}
	return ex, nil
}

// ExecuteSafe safely executes the command without passing through an arbitrary raw shell unless necessary
func (e *Evaluator) ExecuteSafe(ctx context.Context, cmd *parser.ParsedCommand, workingDir string) (*ExecutionOutput, error) {
	start := time.Now()

	var execCmd *exec.Cmd
	tokens := parser.Tokenize(cmd.RawCommand)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("invalid command")
	}

	// Direct execution with explicit argv
	execCmd = exec.CommandContext(ctx, tokens[0], tokens[1:]...)
	if workingDir != "" {
		execCmd.Dir = workingDir
	}

	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			if stderr.Len() > 0 {
				stderr.WriteString("\n")
			}
			stderr.WriteString(err.Error())
		}
	}

	return &ExecutionOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

func generateChallenge(p *parser.ParsedCommand) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎓 **Command Learning Opportunity: `%s`**\n\n", p.BaseTool))
	sb.WriteString(fmt.Sprintf("**Intent:** %s\n", p.Summary))
	sb.WriteString(fmt.Sprintf("**Template:** `%s`\n", p.PatternTemplate))
	sb.WriteString(fmt.Sprintf("**Safety Level:** %s\n\n", p.SafetyLevel))

	if len(p.Flags) > 0 {
		sb.WriteString("**Flags Anatomy:**\n")
		for _, f := range p.Flags {
			if f.IsBool {
				sb.WriteString(fmt.Sprintf("  • `%s` : %s\n", f.Name, f.Explanation))
			} else {
				sb.WriteString(fmt.Sprintf("  • `%s %s` : %s\n", f.Name, f.Value, f.Explanation))
			}
		}
		sb.WriteString("\n")
	}

	// Add contextual Pro-Tip / Hint
	hint := getToolHint(p)
	if hint != "" {
		sb.WriteString(fmt.Sprintf("💡 **Pro-Tip & Hint:** %s\n\n", hint))
	}

	sb.WriteString("👉 **Action (Build Muscle Memory):** Copy & run this command in your terminal:\n")
	sb.WriteString(fmt.Sprintf("```bash\n%s\n```\n\n", p.RawCommand))
	sb.WriteString("**Reply Options to LLM:**\n")
	sb.WriteString("- `done` or `ran it` : Records successful practice & schedules next spaced review.\n")
	sb.WriteString("- `skip` : Runs the command automatically this one time.\n")
	sb.WriteString("- `snooze 1h` / `snooze 1d` : Pauses learning challenges for this tool for X duration.\n")
	sb.WriteString("- `known` : Permanently marks this command pattern as known (never prompts again).\n")

	return sb.String()
}

func getToolHint(p *parser.ParsedCommand) string {
	sub := strings.Join(p.Subcommands, " ")
	switch p.BaseTool {
	case "kubectl":
		switch {
		case strings.Contains(sub, "get"):
			return "Combine with `-o wide` for node/IP details, or `-w` to watch live updates without polling."
		case strings.Contains(sub, "apply"):
			return "Add `--dry-run=client -o yaml` to test and preview the manifest before cluster application."
		case strings.Contains(sub, "logs"):
			return "Use `--tail=50` to restrict initial history, or `--previous` to inspect logs from a crashed container."
		case strings.Contains(sub, "describe"):
			return "Look at the 'Events:' table at the bottom of describe output for scheduling or image pull errors."
		default:
			return "Use `kubectl explain <resource>.<field>` to read API documentation directly in your terminal."
		}
	case "git":
		switch {
		case strings.Contains(sub, "commit"):
			return "Use `git commit --amend --no-edit` to quickly fold staged changes into the latest commit."
		case strings.Contains(sub, "add"):
			return "Use `git add -p` to interactively stage specific hunks rather than whole files."
		case strings.Contains(sub, "rebase"):
			return "Interactive rebase (`git rebase -i HEAD~N`) lets you squash, reword, or reorder commits easily."
		case strings.Contains(sub, "push"):
			return "Always prefer `--force-with-lease` over raw `--force` to avoid clobbering unseen remote updates."
		default:
			return "Use `git status -s` for a compact, fast overview of modified/untracked files."
		}
	case "jj":
		switch {
		case strings.Contains(sub, "describe"):
			return "In Jujutsu, every working copy is already a commit. Use `jj describe -m '...'` to set its message."
		case strings.Contains(sub, "squash"):
			return "`jj squash` moves current changes into parent commit, or use `-s <rev>` to target any revision."
		case strings.Contains(sub, "new"):
			return "`jj new` creates a new change on top of current, or `jj new <rev>` on top of any revision."
		default:
			return "`jj undo` instantly rolls back any Jujutsu operation using the operation log."
		}
	case "docker":
		switch {
		case strings.Contains(sub, "run"):
			return "Always add `--rm` for disposable one-off containers so they don't linger in `docker ps -a`."
		case strings.Contains(sub, "ps"):
			return "Use `docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'` for clean tables."
		default:
			return "Use `docker system prune` periodically to reclaim disk space from dangling images."
		}
	}
	return ""
}

func formatExplanation(p *parser.ParsedCommand) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Command: `%s`\n", p.RawCommand))
	sb.WriteString(fmt.Sprintf("Pattern: `%s` (ID: %s)\n", p.PatternTemplate, p.PatternID))
	sb.WriteString(fmt.Sprintf("Summary: %s\n", p.Summary))
	sb.WriteString(fmt.Sprintf("Safety: %s\n", p.SafetyLevel))
	if len(p.Flags) > 0 {
		sb.WriteString("Flags:\n")
		for _, f := range p.Flags {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", f.Name, f.Explanation))
		}
	}
	return sb.String()
}
