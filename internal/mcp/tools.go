package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"learning-mcp/internal/db"
	"learning-mcp/internal/engine"
	"learning-mcp/internal/parser"
)

var (
	tokenSecretOnce sync.Once
	tokenSecret     []byte
)

func getTokenSecret() []byte {
	tokenSecretOnce.Do(func() {
		if envSec := os.Getenv("LEARNING_TOKEN_SECRET"); envSec != "" {
			if len(envSec) < 16 {
				panic("critical security failure: LEARNING_TOKEN_SECRET must be at least 16 bytes")
			}
			tokenSecret = []byte(envSec)
		} else {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				panic(fmt.Sprintf("critical security failure: failed generating cryptographic session secret: %v", err))
			}
			tokenSecret = buf
		}
	})
	return tokenSecret
}

func generateApprovalToken(patternID, rawCmd string) string {
	h := hmac.New(sha256.New, getTokenSecret())
	h.Write([]byte(fmt.Sprintf("%s:%s", patternID, rawCmd)))
	return hex.EncodeToString(h.Sum(nil))
}
func validateApprovalToken(patternID, rawCmd, token string) bool {
	if token == "" {
		return false
	}
	expected := generateApprovalToken(patternID, rawCmd)
	return hmac.Equal([]byte(expected), []byte(token))
}

func getToolsManifest() []Tool {
	return []Tool{
		{
			Name:        "explain_command",
			Description: "Deconstructs and explains the anatomy, flags, and safety implications of a CLI command without executing it.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "The full CLI command to analyze (e.g. 'kubectl get pods -n prod')"
					}
				},
				"required": ["command"]
			}`),
		},
		{
			Name:        "learn_exec",
			Description: "Evaluates a CLI command against active spaced repetition rules. Returns command breakdown and an active learning challenge with an approval token. Requires explicit action / token to execute.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "The CLI command to evaluate"
					},
					"action": {
						"type": "string",
						"enum": ["evaluate_only", "user_completed", "skip_and_execute", "force_execute"],
						"description": "Execution action: 'evaluate_only' (default), 'user_completed' (user ran command manually), 'skip_and_execute' (skip learning for this run), or 'force_execute' (with approval token)"
					},
					"approval_token": {
						"type": "string",
						"description": "Approval token provided by a prior evaluate_only challenge"
					},
					"quality_score": {
						"type": "number",
						"description": "Recall quality score (0 to 5) when action is 'user_completed'"
					},
					"working_dir": {
						"type": "string",
						"description": "Working directory for execution"
					}
				},
				"required": ["command"]
			}`),
		},
		{
			Name:        "snooze_learning",
			Description: "Temporarily pauses interactive learning prompts for a tool, pattern, or globally for a specified duration.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"duration_minutes": {
						"type": "number",
						"description": "Duration to snooze in minutes (e.g. 30, 60, 1440)"
					},
					"scope": {
						"type": "string",
						"enum": ["global", "tool", "pattern"],
						"description": "Snooze scope: 'global', 'tool', or 'pattern'"
					},
					"target": {
						"type": "string",
						"description": "Tool ID or pattern ID (optional if scope is global)"
					},
					"reason": {
						"type": "string",
						"description": "Reason for snoozing (e.g. 'Rushing production outage')"
					}
				},
				"required": ["duration_minutes", "scope"]
			}`),
		},
		{
			Name:        "mark_known",
			Description: "Permanently marks a command, template, or tool as known/mastered to exclude it from future learning prompts.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"target": {
						"type": "string",
						"description": "The exact command, pattern ID, or tool to exclude"
					},
					"scope": {
						"type": "string",
						"enum": ["exact_command", "pattern", "tool"],
						"description": "Scope of exclusion"
					},
					"reason": {
						"type": "string",
						"description": "Why this is excluded (e.g. 'Already familiar')"
					}
				},
				"required": ["target", "scope"]
			}`),
		},
		{
			Name:        "get_due_reviews",
			Description: "Retrieves commands and flag flashcards currently due for spaced repetition revision.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {
						"type": "number",
						"description": "Maximum number of items to return (default 10)"
					},
					"tool": {
						"type": "string",
						"description": "Optional tool filter (e.g. 'kubectl', 'git', 'jj')"
					}
				}
			}`),
		},
		{
			Name:        "submit_review_score",
			Description: "Submits recall quality score (0 to 5) for a command pattern to update its SM-2 spaced repetition schedule.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern_id": {
						"type": "string",
						"description": "Pattern identifier"
					},
					"quality_score": {
						"type": "number",
						"description": "Score from 0 (blackout) to 5 (perfect recall)"
					}
				},
				"required": ["pattern_id", "quality_score"]
			}`),
		},
		{
			Name:        "get_learning_stats",
			Description: "Fetches summary statistics on tracked commands, mastery levels, active snoozes, and due reviews.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {}
			}`),
		},
	}
}

// ToolHandler routes and executes tool calls
type ToolHandler struct {
	store     *db.Store
	evaluator *engine.Evaluator
}

func NewToolHandler(store *db.Store) *ToolHandler {
	return &ToolHandler{
		store:     store,
		evaluator: engine.NewEvaluator(store),
	}
}

func (h *ToolHandler) HandleCall(ctx context.Context, params *CallToolParams) (*CallToolResult, error) {
	switch params.Name {
	case "explain_command":
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Command) == "" {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "Error: 'command' parameter cannot be empty"}},
				IsError: true,
			}, nil
		}

		parsed, err := parser.Parse(args.Command, h.store)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error parsing command: %v", err)}},
				IsError: true,
			}, nil
		}
		data, _ := json.MarshalIndent(parsed, "", "  ")
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		}, nil

	case "learn_exec":
		var args struct {
			Command       string   `json:"command"`
			Action        string   `json:"action"` // evaluate_only, user_completed, skip_and_execute, force_execute
			ApprovalToken string   `json:"approval_token"`
			QualityScore  *float64 `json:"quality_score"`
			WorkingDir    string   `json:"working_dir"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}

		if strings.TrimSpace(args.Command) == "" {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "Error: 'command' parameter is required and cannot be empty"}},
				IsError: true,
			}, nil
		}

		action := args.Action
		if action == "" {
			action = "evaluate_only"
		}

		evalResult, err := h.evaluator.Evaluate(args.Command, true)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Evaluation error: %v", err)}},
				IsError: true,
			}, nil
		}

		token := generateApprovalToken(evalResult.Parsed.PatternID, evalResult.Parsed.RawCommand)

		// 1. User completed the command themselves
		if action == "user_completed" {
			score := 4
			if args.QualityScore != nil {
				score = int(*args.QualityScore)
			}
			_, _ = h.evaluator.SubmitScore(evalResult.Parsed.PatternID, score)
			_ = h.store.LogExecution(&db.ExecutionLog{
				ID:            uuid.NewString(),
				RawCommand:    args.Command,
				PatternID:     evalResult.Parsed.PatternID,
				Mode:          "user_manual",
				IsInteractive: true,
				ActionTaken:   "challenge_passed",
				QualityScore:  &score,
				ExecutedAt:    time.Now().UTC(),
			})

			return &CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: fmt.Sprintf("Great job! Recorded manual completion for '%s' (Pattern: %s). Next revision interval scheduled.", evalResult.Parsed.BaseTool, evalResult.Parsed.PatternID)},
				},
			}, nil
		}

		// 2. Evaluate only (default) or Challenge presentation
		if action == "evaluate_only" {
			_ = h.store.LogExecution(&db.ExecutionLog{
				ID:            uuid.NewString(),
				RawCommand:    args.Command,
				PatternID:     evalResult.Parsed.PatternID,
				Mode:          "agent_proxy",
				IsInteractive: true,
				ActionTaken:   "challenge_presented",
				ExecutedAt:    time.Now().UTC(),
			})

			payload := map[string]interface{}{
				"status":         "challenge_required",
				"approval_token": token,
				"action_options": []string{
					"1. Run command yourself in terminal, then call learn_exec with action='user_completed'",
					"2. Call learn_exec with action='skip_and_execute' and this approval_token to execute once",
					"3. Call snooze_learning to pause challenges",
					"4. Call mark_known to exclude permanently",
				},
				"challenge": evalResult.ChallengeText,
				"parsed":    evalResult.Parsed,
			}
			data, _ := json.MarshalIndent(payload, "", "  ")
			return &CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: string(data)},
				},
			}, nil
		}

		// 3. Execution requested (skip_and_execute or force_execute)
		if action == "skip_and_execute" || action == "force_execute" {
			// Validate approval token if challenge was due
			if evalResult.Action == engine.ActionInteractiveChallenge && !validateApprovalToken(evalResult.Parsed.PatternID, evalResult.Parsed.RawCommand, args.ApprovalToken) {
				return &CallToolResult{
					Content: []ToolContent{
						{Type: "text", Text: "Error: Invalid or missing approval_token. Please evaluate first with action='evaluate_only' to obtain a valid approval token."},
					},
					IsError: true,
				}, nil
			}

			out, execErr := h.evaluator.ExecuteSafe(ctx, evalResult.Parsed, args.WorkingDir)
			if execErr != nil {
				return &CallToolResult{
					Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Execution failed: %v", execErr)}},
					IsError: true,
				}, nil
			}

			_ = h.store.LogExecution(&db.ExecutionLog{
				ID:             uuid.NewString(),
				RawCommand:     args.Command,
				PatternID:      evalResult.Parsed.PatternID,
				Mode:           "agent_proxy",
				IsInteractive:  true,
				ActionTaken:    "executed_with_token",
				ResponseTimeMs: int(out.Duration),
				ExecutedAt:     time.Now().UTC(),
			})

			resBytes, _ := json.MarshalIndent(out, "", "  ")
			return &CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: string(resBytes)},
				},
			}, nil
		}

		return &CallToolResult{
			Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error: Unsupported action '%s'", action)}},
			IsError: true,
		}, nil

	case "snooze_learning":
		var args struct {
			DurationMinutes float64 `json:"duration_minutes"`
			Scope           string  `json:"scope"`
			Target          string  `json:"target"`
			Reason          string  `json:"reason"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if args.DurationMinutes <= 0 {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "Error: 'duration_minutes' must be greater than 0"}},
				IsError: true,
			}, nil
		}
		dur := time.Duration(args.DurationMinutes) * time.Minute
		rule, err := h.evaluator.Snooze(args.Scope, args.Target, args.Reason, dur)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Failed to set snooze: %v", err)}},
				IsError: true,
			}, nil
		}
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: fmt.Sprintf("Snoozed learning for '%s' (scope: %s) until %s", args.Target, args.Scope, rule.SnoozeUntil.Format(time.RFC3339))},
			},
		}, nil

	case "mark_known":
		var args struct {
			Target string `json:"target"`
			Scope  string `json:"scope"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Target) == "" {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "Error: 'target' cannot be empty"}},
				IsError: true,
			}, nil
		}
		_, err := h.evaluator.MarkKnown(args.Scope, args.Target, args.Reason)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Failed to mark known: %v", err)}},
				IsError: true,
			}, nil
		}
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: fmt.Sprintf("Marked '%s' as permanently known (scope: %s).", args.Target, args.Scope)},
			},
		}, nil

	case "get_due_reviews":
		var args struct {
			Limit int    `json:"limit"`
			Tool  string `json:"tool"`
		}
		_ = json.Unmarshal(params.Arguments, &args)
		dueItems, err := h.store.GetDueRepetitionItems(args.Limit, args.Tool)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error fetching due items: %v", err)}},
				IsError: true,
			}, nil
		}
		data, _ := json.MarshalIndent(dueItems, "", "  ")
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		}, nil

	case "submit_review_score":
		var args struct {
			PatternID    string `json:"pattern_id"`
			QualityScore int    `json:"quality_score"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if args.PatternID == "" {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: "Error: 'pattern_id' is required"}},
				IsError: true,
			}, nil
		}
		updated, err := h.evaluator.SubmitScore(args.PatternID, args.QualityScore)
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error submitting score: %v", err)}},
				IsError: true,
			}, nil
		}
		data, _ := json.MarshalIndent(updated, "", "  ")
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: fmt.Sprintf("Updated SM-2 schedule for %s:\n%s", args.PatternID, string(data))},
			},
		}, nil

	case "get_learning_stats":
		stats, err := h.store.GetStats()
		if err != nil {
			return &CallToolResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error getting stats: %v", err)}},
				IsError: true,
			}, nil
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return &CallToolResult{
			Content: []ToolContent{
				{Type: "text", Text: string(data)},
			},
		}, nil

	default:
		return &CallToolResult{
			Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}},
			IsError: true,
		}, nil
	}
}
