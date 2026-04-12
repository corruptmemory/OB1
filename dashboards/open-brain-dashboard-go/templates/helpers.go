package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// joinCSV joins a slice of strings with ", " for display in a form
// CSV input. Nil slices return empty string. Used by the v1.5 in-pane
// edit form's topics and people inputs.
func joinCSV(xs []string) string {
	return strings.Join(xs, ", ")
}

// Truncate shortens s to at most max runes, appending an ellipsis when it
// had to cut. Used by the home page's recent-thoughts list so long captures
// don't blow up the layout.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// HumanTime formats a timestamp relative to now for recent values and falls
// back to an absolute date for older ones. Keeps the home page feeling live
// without making yesterday's captures illegible.
func HumanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}

// TypeClass maps a thought type to the CSS class suffix used by the chip
// colors in app.css. Canonical types get their own color; anything else
// (including qwen2.5:3b's occasional wildcard outputs) gets a neutral gray.
func TypeClass(typ string) string {
	switch typ {
	case "observation", "task", "idea", "reference", "person_note":
		return "type-chip type-" + typ
	default:
		return "type-chip type-other"
	}
}

// totalCount sums the counts in a by-type slice so the sidebar "All"
// chip can render a cumulative number without a separate query.
func totalCount(types []TypeCount) int64 {
	var total int64
	for _, t := range types {
		total += t.Count
	}
	return total
}

// clearType returns a copy of f with Type cleared. Used by the
// sidebar's active-chip affordance: clicking an already-active chip
// drops the filter.
func clearType(f ListFilters) ListFilters { f.Type = ""; return f }

// withType / withDays / withTopic / withPerson return a copy of f with
// one field replaced. The sidebar chips build their hx-get URLs by
// composing these on top of the currently-active ListFilters so
// siblings are preserved across clicks.
func withType(f ListFilters, t string) ListFilters   { f.Type = t; return f }
func withDays(f ListFilters, d int) ListFilters      { f.Days = d; return f }
func withTopic(f ListFilters, t string) ListFilters  { f.Topic = t; return f }
func withPerson(f ListFilters, p string) ListFilters { f.Person = p; return f }

// resetPage returns a copy of f with Page cleared. Filter changes from
// the sidebar should always reset pagination because the filtered result
// set is usually smaller than the page we were on.
func resetPage(f ListFilters) ListFilters { f.Page = 0; return f }

// ollamaTooltip returns user-facing hover text for the list toolbar's
// health dot. Avoids leaking the CSS class name (e.g. "embeddings:
// grey") into the user-visible tooltip.
func ollamaTooltip(status string) string {
	switch status {
	case "green":
		return "embeddings available"
	case "amber":
		return "embeddings unavailable"
	default:
		return "embeddings: not yet checked"
	}
}

// rowClass returns the list-row class string, adding --selected when
// the row's id matches the currently-open detail pane.
func rowClass(id int64, selectedID int64) string {
	if id == selectedID {
		return "list-row list-row--selected"
	}
	return "list-row"
}

// pageFilter returns a copy of f with Page set to the given value.
// Used by pagination to build the URL for prev/next/numbered links.
func pageFilter(f ListFilters, page int) ListFilters {
	f.Page = page
	return f
}

// rowHref builds the pretty shell URL for a list row: /?<filters>&id=N,
// collapsing to /?id=N when the filter set is empty (so we never produce
// a "/?&id=N" eyesore). Used by both the row's href and its
// hx-push-url so the address bar tracks the shell route instead of the
// /partials/detail/N endpoint htmx actually fetches.
func rowHref(f ListFilters, id int64) string {
	qs := filtersToURL(f)
	if qs == "" {
		return fmt.Sprintf("/?id=%d", id)
	}
	return fmt.Sprintf("/?%s&id=%d", qs, id)
}

// filtersToURL renders a ListFilters as a URL query string, skipping
// zero values. Used by the sidebar chips and list-pane pagination to
// construct navigation URLs that preserve sibling filters.
func filtersToURL(f ListFilters) string {
	v := url.Values{}
	if f.Type != "" {
		v.Set("type", f.Type)
	}
	if f.Topic != "" {
		v.Set("topic", f.Topic)
	}
	if f.Person != "" {
		v.Set("person", f.Person)
	}
	if f.Days > 0 {
		v.Set("days", fmt.Sprintf("%d", f.Days))
	}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	if f.Page > 1 {
		v.Set("page", fmt.Sprintf("%d", f.Page))
	}
	return v.Encode()
}

// PrettyJSON reformats a raw JSON byte slice with two-space indentation.
// Returns the original string unchanged if the input is not valid JSON.
func PrettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

