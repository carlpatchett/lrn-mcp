package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"learning-mcp/internal/db"
)

func getPromptsManifest() []Prompt {
	return []Prompt{
		{
			Name:        "spaced_review_session",
			Description: "Starts an interactive spaced repetition session quizzing you on due CLI commands and flags.",
			Arguments: []PromptArgument{
				{
					Name:        "tool",
					Description: "Filter review items by tool (e.g. 'kubectl', 'git', 'jj')",
					Required:    false,
				},
				{
					Name:        "limit",
					Description: "Number of questions to quiz (default: 5)",
					Required:    false,
				},
			},
		},
		{
			Name:        "explain_pattern",
			Description: "Deep dive anatomy and breakdown of a specific command pattern or syntax.",
			Arguments: []PromptArgument{
				{
					Name:        "pattern_id",
					Description: "The ID or template of the pattern to explain",
					Required:    true,
				},
			},
		},
	}
}

type PromptHandler struct {
	store *db.Store
}

func NewPromptHandler(store *db.Store) *PromptHandler {
	return &PromptHandler{store: store}
}

func (h *PromptHandler) HandleGet(params *GetPromptParams) (*GetPromptResult, error) {
	switch params.Name {
	case "spaced_review_session":
		limit := 5
		if lStr, ok := params.Arguments["limit"]; ok {
			if parsedL, err := strconv.Atoi(lStr); err == nil && parsedL > 0 {
				limit = parsedL
			}
		}
		toolFilter := params.Arguments["tool"]

		dueItems, err := h.store.GetDueRepetitionItems(limit, toolFilter)
		if err != nil {
			return nil, fmt.Errorf("failed fetching due items: %w", err)
		}

		var promptText strings.Builder
		promptText.WriteString("Please conduct an interactive flashcard review for the following due CLI commands:\n\n")

		if len(dueItems) == 0 {
			promptText.WriteString("All caught up! No spaced repetition reviews currently due. Ask if I'd like to practice any specific tool.")
		} else {
			data, _ := json.MarshalIndent(dueItems, "", "  ")
			promptText.WriteString(fmt.Sprintf("Due Items JSON:\n```json\n%s\n```\n\n", string(data)))
			promptText.WriteString("For each item, present the user with the intent and ask them to recall or type the canonical command/flags before revealing the answer.")
		}

		return &GetPromptResult{
			Description: "Spaced Repetition Review Session",
			Messages: []PromptMessage{
				{
					Role: "user",
					Content: PromptMessageContent{
						Type: "text",
						Text: promptText.String(),
					},
				},
			},
		}, nil

	case "explain_pattern":
		patternID := params.Arguments["pattern_id"]
		if patternID == "" {
			return nil, fmt.Errorf("missing required argument 'pattern_id'")
		}
		pattern, err := h.store.GetPattern(patternID)
		if err != nil {
			return nil, fmt.Errorf("error fetching pattern: %w", err)
		}
		if pattern == nil {
			return nil, fmt.Errorf("pattern not found: %s", patternID)
		}

		data, _ := json.MarshalIndent(pattern, "", "  ")
		return &GetPromptResult{
			Description: fmt.Sprintf("Command Pattern Deep-dive: %s", patternID),
			Messages: []PromptMessage{
				{
					Role: "user",
					Content: PromptMessageContent{
						Type: "text",
						Text: fmt.Sprintf("Explain the anatomy, flags, and common real-world use cases for this command pattern:\n```json\n%s\n```", string(data)),
					},
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown prompt: %s", params.Name)
	}
}
