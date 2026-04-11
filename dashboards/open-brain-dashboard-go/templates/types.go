package templates

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/a-h/templ"
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
// page renders. UpdatedAt is nil for thoughts that have never been edited
// — it lives inside the metadata jsonb blob (as metadata.updated_at) so
// the stock OB1 schema stays untouched.
type ThoughtDetail struct {
	ThoughtData
	ActionItems    []ActionItem
	DatesMentioned []string
	Source         string
	UpdatedAt      *time.Time
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

// ShellViewModel is the payload for the v1.5 three-pane shell. Each pane
// is a pre-rendered templ.Component so the shell template stays agnostic
// about what lives inside it — handlers decide which component to pass
// for each slot based on the URL's query string.
type ShellViewModel struct {
	Sidebar templ.Component
	List    templ.Component
	Detail  templ.Component
}

// ListFilters is the full set of query-string-driven selectors that the
// unified /?... handler combines into a single list query. Empty strings
// and zero values are skipped. Lives in templates so the sidebar view
// model can reference it without creating a cycle from package main.
type ListFilters struct {
	Type   string // metadata->>'type' equality
	Topic  string // metadata->'topics' array membership
	Person string // metadata->'people' array membership
	Days   int    // created_at > now() - interval '$ days'
	// Q is the user's search text. When the caller has embedded Q into a
	// vector and passes withVector=true, Q drives pgvector ranking.
	// Otherwise Q falls back to an ILIKE substring match on content.
	Q       string
	Page    int
	PerPage int
}

// PersonCount is the view model for a row in the sidebar's People list.
type PersonCount struct {
	Person string
	Count  int64
}

// SearchResultMode describes which path the unified DB.Search method
// took. Consumed by the list template to render the appropriate
// auto-fallback banner. Lives in templates because the view model
// carries it end-to-end and the template branches on it directly.
type SearchResultMode int

const (
	SearchModeNone     SearchResultMode = iota // no q, pure filter list
	SearchModeSemantic                         // q + semantic results found
	SearchModeFallback                         // q + semantic returned zero, fell back to ILIKE
	SearchModeTextOnly                         // q + Ollama was down entirely
)

// ListResult bundles the rows returned by DB.Search with pagination
// info and the mode the caller used so the list template can render
// the banner and pagination footer without re-deriving either. Lives
// in templates alongside ListFilters so the view model stays in one
// place and the main package doesn't own a rendering type.
type ListResult struct {
	Filter     ListFilters
	Mode       SearchResultMode
	Results    []ThoughtData
	Total      int64
	TotalPages int
}

// ListData is the view-model for the list pane. SelectedID is the
// currently-open thought's id (or 0 if none) so rows can render the
// selected state. OllamaStatus is "green"/"amber"/"grey" — Task 11
// adds real state tracking; Task 7 always renders "grey".
type ListData struct {
	Result       *ListResult
	SelectedID   int64
	OllamaStatus string
}

// SidebarData is the view-model for sidebar.templ. Bundles counts
// from the DB with the currently-active filter so the template can
// highlight active chips.
type SidebarData struct {
	Total     int64
	WeekCount int64
	Types     []TypeCount
	Topics    []TopicCount
	People    []PersonCount
	Active    ListFilters
}
