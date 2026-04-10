package templates

import (
	"bytes"
	"encoding/json"
	"time"
)

// ThoughtData is the view model for a single thought as rendered on the
// home page's recent list and browse/search pages. Fields are flattened
// out of the `thoughts.metadata` jsonb blob by the main package's query
// layer so templates can render without re-parsing JSON. Similarity is
// populated only by the semantic-search path and ignored elsewhere — a
// zero value tells ThoughtCard to skip the score badge.
type ThoughtData struct {
	ID         int64
	Content    string
	Type       string
	Topics     []string
	People     []string
	CreatedAt  time.Time
	Similarity float64
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

// BrowseFilter captures every query parameter the browse page accepts.
// Zero values mean "no filter on this field". Days == 0 means all time.
type BrowseFilter struct {
	Type   string
	Topic  string
	Person string
	Q      string
	Days   int
}

// Active reports whether any filter is currently applied. The template uses
// this to decide whether to show the "clear filters" link.
func (f BrowseFilter) Active() bool {
	return f.Type != "" || f.Topic != "" || f.Person != "" || f.Q != "" || f.Days > 0
}

// BrowseData is the payload the browse page renders — the filtered result
// set plus pagination metadata and the filter struct echoed back so the
// form can preselect its current values.
type BrowseData struct {
	Filter     BrowseFilter
	Results    []ThoughtData
	Total      int64
	Page       int
	PerPage    int
	TotalPages int
}

// SearchData is the payload the search page renders. Mode is either
// "semantic" or "text". EmbedError, if non-empty, indicates that semantic
// mode tried to call Ollama and failed — the template renders a graceful
// error panel with a text-mode retry link instead of returning 500.
type SearchData struct {
	Query      string
	Mode       string
	Results    []ThoughtData
	Total      int64
	Page       int
	PerPage    int
	TotalPages int
	EmbedError string
}
