package templates

import (
	"bytes"
	"encoding/json"
	"time"
)

// ThoughtData is the view model for a single thought as rendered on the
// home page's recent list (and, later, browse/detail pages). Fields are
// flattened out of the `thoughts.metadata` jsonb blob by the main package's
// query layer so templates can render without re-parsing JSON.
type ThoughtData struct {
	ID        int64
	Content   string
	Type      string
	Topics    []string
	People    []string
	CreatedAt time.Time
}

type TypeCount struct {
	Type  string
	Count int64
}

type TopicCount struct {
	Topic string
	Count int64
}

// HomeData is the aggregate payload the home page renders.
type HomeData struct {
	Total     int64
	WeekCount int64
	Types     []TypeCount
	Topics    []TopicCount
	Recent    []ThoughtData
}

// ActionItem normalises qwen2.5:3b's two observed shapes for
// metadata.action_items — plain strings like "fix X" and objects like
// {"description": "fix X", "priority": "high"}. Either shape unmarshals
// into this struct with the string form leaving Priority empty.
type ActionItem struct {
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

func (a *ActionItem) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	// Object shape: {"description": "...", "priority": "..."}
	if trimmed[0] == '{' {
		var obj struct {
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return err
		}
		a.Description = obj.Description
		a.Priority = obj.Priority
		return nil
	}
	// String shape: "fix X"
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return err
	}
	a.Description = s
	return nil
}

// ThoughtDetail is the full view model for the detail page. It embeds
// ThoughtData so every place that already takes a ThoughtData (ThoughtCard,
// list rows, etc.) works unchanged, and adds the heavier fields the detail
// page renders.
type ThoughtDetail struct {
	ThoughtData
	ActionItems    []ActionItem
	DatesMentioned []string
	Source         string
	RawMetadata    []byte
}
