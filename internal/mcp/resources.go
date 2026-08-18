package mcp

import (
	"encoding/json"
	"fmt"

	"learning-mcp/internal/db"
)

func getResourcesManifest() []Resource {
	return []Resource{
		{
			URI:         "learning://stats/overview",
			Name:        "Learning Statistics",
			Description: "Summary of tracked commands, mastery progress, active snoozes, and due spaced reviews.",
			MimeType:    "application/json",
		},
		{
			URI:         "learning://config/exclusions",
			Name:        "Known Exclusions & Active Snoozes",
			Description: "Active snooze rules and permanently excluded commands.",
			MimeType:    "application/json",
		},
	}
}

type ResourceHandler struct {
	store *db.Store
}

func NewResourceHandler(store *db.Store) *ResourceHandler {
	return &ResourceHandler{store: store}
}

func (h *ResourceHandler) HandleRead(uri string) (*ReadResourceResult, error) {
	switch uri {
	case "learning://stats/overview":
		stats, err := h.store.GetStats()
		if err != nil {
			return nil, fmt.Errorf("failed fetching stats: %w", err)
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	case "learning://config/exclusions":
		snoozes, _ := h.store.ListSnoozes()
		exclusions, _ := h.store.ListExclusions()

		payload := map[string]interface{}{
			"active_snoozes": snoozes,
			"exclusions":     exclusions,
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("resource not found: %s", uri)
	}
}
