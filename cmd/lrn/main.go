package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"learning-mcp/internal/config"
	"learning-mcp/internal/db"
	"learning-mcp/internal/engine"
	"learning-mcp/internal/parser"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database at %s: %v\n", cfg.DBPath, err)
		os.Exit(1)
	}
	defer store.Close()

	evaluator := engine.NewEvaluator(store)

	subcommand := os.Args[1]
	switch subcommand {
	case "stats":
		runStats(store)
	case "explain":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: lrn explain \"<command>\"")
			os.Exit(1)
		}
		runExplain(store, strings.Join(os.Args[2:], " "))
	case "review":
		toolFilter := ""
		if len(os.Args) >= 3 {
			toolFilter = os.Args[2]
		}
		runReview(store, evaluator, toolFilter)
	case "snooze":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: lrn snooze <scope: global|tool|pattern> [target] <duration_minutes> [reason]")
			os.Exit(1)
		}
		scope := os.Args[2]
		var target string
		var mins float64
		var reason string

		if scope == "global" {
			if len(os.Args) < 4 {
				fmt.Fprintln(os.Stderr, "Usage: lrn snooze global <duration_minutes> [reason]")
				os.Exit(1)
			}
			target = "global"
			var err error
			mins, err = strconv.ParseFloat(os.Args[3], 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid duration minutes: %v\n", err)
				os.Exit(1)
			}
			if len(os.Args) >= 5 {
				reason = strings.Join(os.Args[4:], " ")
			} else {
				reason = "Global CLI snooze"
			}
		} else {
			if len(os.Args) < 5 {
				fmt.Fprintf(os.Stderr, "Usage: lrn snooze %s <target> <duration_minutes> [reason]\n", scope)
				os.Exit(1)
			}
			target = os.Args[3]
			var err error
			mins, err = strconv.ParseFloat(os.Args[4], 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid duration minutes: %v\n", err)
				os.Exit(1)
			}
			if len(os.Args) >= 6 {
				reason = strings.Join(os.Args[5:], " ")
			} else {
				reason = fmt.Sprintf("CLI snooze for %s", target)
			}
		}

		dur := time.Duration(mins) * time.Minute
		rule, err := evaluator.Snooze(scope, target, reason, dur)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to snooze: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Snoozed learning for '%s' (scope: %s) until %s\n", target, scope, rule.SnoozeUntil.Format(time.RFC3339))

	case "known":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: lrn known <scope: exact_command|pattern|tool> <target> [reason]")
			os.Exit(1)
		}
		scope := os.Args[2]
		target := os.Args[3]
		reason := "Marked known via CLI"
		if len(os.Args) >= 5 {
			reason = strings.Join(os.Args[4:], " ")
		}
		_, err := evaluator.MarkKnown(scope, target, reason)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to mark known: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Marked '%s' as known (scope: %s)\n", target, scope)

	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", subcommand)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("lrn - CLI companion for Learning & Spaced Repetition MCP")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  lrn stats                              Show mastery & review stats")
	fmt.Println("  lrn explain \"<command>\"                Analyze command anatomy & flags")
	fmt.Println("  lrn review [tool]                      Start interactive flashcard quiz")
	fmt.Println("  lrn snooze <scope> <target> <minutes>  Pause learning prompts")
	fmt.Println("  lrn known <scope> <target> [reason]    Permanently exclude a command")
	fmt.Println("  lrn help                               Show this help message")
}

func runStats(store *db.Store) {
	stats, err := store.GetStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching stats: %v\n", err)
		return
	}

	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("           Learning & Mastery Overview             ")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("  Total Command Patterns Tracked : %d\n", stats.TotalPatternsTracked)
	fmt.Printf("  Reviews Currently Due          : %d\n", stats.DueReviewsCount)
	fmt.Printf("  Patterns in Learning Stage     : %d\n", stats.LearningCount)
	fmt.Printf("  Patterns Fully Mastered        : %d\n", stats.MasteredCount)
	fmt.Printf("  Total Executions Logged        : %d\n", stats.TotalExecutions)
	fmt.Printf("  Active Snooze Timers           : %d\n", stats.ActiveSnoozesCount)
	fmt.Println("───────────────────────────────────────────────────")
	fmt.Println("  Tool Breakdown:")
	for tool, count := range stats.ToolBreakdown {
		fmt.Printf("    • %-12s : %d patterns\n", tool, count)
	}
	fmt.Println("═══════════════════════════════════════════════════")
}

func runExplain(store *db.Store, rawCmd string) {
	parsed, err := parser.Parse(rawCmd, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed parsing command: %v\n", err)
		return
	}

	fmt.Println("───────────────────────────────────────────────────")
	fmt.Printf("Command  : %s\n", parsed.RawCommand)
	fmt.Printf("Tool     : %s\n", parsed.BaseTool)
	fmt.Printf("Template : %s\n", parsed.PatternTemplate)
	fmt.Printf("Safety   : %s\n", parsed.SafetyLevel)
	fmt.Printf("Summary  : %s\n", parsed.Summary)
	if len(parsed.Flags) > 0 {
		fmt.Println("Flags:")
		for _, f := range parsed.Flags {
			if f.IsBool {
				fmt.Printf("  • %-16s : %s\n", f.Name, f.Explanation)
			} else {
				fmt.Printf("  • %-16s : %s\n", fmt.Sprintf("%s %s", f.Name, f.Value), f.Explanation)
			}
		}
	}
	fmt.Println("───────────────────────────────────────────────────")
}

func runReview(store *db.Store, evaluator *engine.Evaluator, toolFilter string) {
	dueItems, err := store.GetDueRepetitionItems(10, toolFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching due items: %v\n", err)
		return
	}

	if len(dueItems) == 0 {
		fmt.Println("No reviews due right now! All caught up.")
		return
	}

	fmt.Printf("Found %d due command pattern(s) for review.\n\n", len(dueItems))
	scanner := bufio.NewScanner(os.Stdin)

	for idx, due := range dueItems {
		fmt.Printf("[%d/%d] Tool: %s | Safety: %s\n", idx+1, len(dueItems), due.Pattern.ToolID, due.Pattern.SafetyLevel)
		fmt.Printf("Intent: %s\n", due.Pattern.Summary)
		fmt.Printf("Template: %s\n", due.Pattern.PatternTemplate)
		fmt.Println()
		fmt.Print("Press ENTER to reveal answer & flags (or 'q' to quit)... ")
		if !scanner.Scan() {
			break
		}
		if strings.TrimSpace(scanner.Text()) == "q" {
			break
		}

		fmt.Printf("\nCanonical Pattern: %s\n", due.Pattern.PatternTemplate)
		fmt.Print("Rate your recall quality (0=blackout, 3=good with hesitation, 5=instant recall) [0-5]: ")
		if !scanner.Scan() {
			break
		}

		scoreStr := strings.TrimSpace(scanner.Text())
		score, err := strconv.Atoi(scoreStr)
		if err != nil || score < 0 || score > 5 {
			fmt.Println("Invalid score, recording default score 3 (good).")
			score = 3
		}

		updated, err := evaluator.SubmitScore(due.Pattern.ID, score)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating score: %v\n", err)
		} else {
			fmt.Printf("Saved! Next review in %.1f days (Mastery: %.0f%%)\n\n", updated.IntervalDays, updated.MasteryScore*100)
		}
	}

	fmt.Println("Review session completed!")
}
