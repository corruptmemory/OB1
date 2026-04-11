package templates

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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

// ItoA64 formats an int64 for template output. templ can print primitives
// directly but this keeps intent explicit at call sites.
func ItoA64(n int64) string {
	return fmt.Sprintf("%d", n)
}

// ItoA formats an int for template output.
func ItoA(n int) string {
	return strconv.Itoa(n)
}

// SimilarityPct formats a 0..1 similarity score as a percentage string
// ("84%"). Used for the score badge on semantic-search results.
func SimilarityPct(s float64) string {
	return fmt.Sprintf("%d%%", int(s*100+0.5))
}

// BrowseURL builds a /browse?... URL from a filter plus target page. Empty
// filter fields are omitted; page == 1 is omitted (that's the default).
// This is the single source of truth for the browse URL shape, used by the
// filter form's "clear filters" link, the pagination prev/next, the type
// chip row in the filter, and the topic/person tag anchors on the detail
// page.
func BrowseURL(f BrowseFilter, page int) string {
	q := url.Values{}
	if f.Type != "" {
		q.Set("type", f.Type)
	}
	if f.Topic != "" {
		q.Set("topic", f.Topic)
	}
	if f.Person != "" {
		q.Set("person", f.Person)
	}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	if f.Days > 0 {
		q.Set("days", strconv.Itoa(f.Days))
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/browse"
	}
	return "/browse?" + q.Encode()
}

// TypeChipURL returns a /browse URL that sets the type filter to `typ`
// (or clears it when typ == ""), preserving all other filters but resetting
// page to 1 since the result set changes.
func TypeChipURL(f BrowseFilter, typ string) string {
	f.Type = typ
	return BrowseURL(f, 1)
}

// TopicURL returns a /browse URL filtered to a specific topic. Used by the
// clickable topic tags on the detail page.
func TopicURL(topic string) string {
	return BrowseURL(BrowseFilter{Topic: topic}, 1)
}

// PersonURL returns a /browse URL filtered to a specific person. Used by
// the clickable person tags on the detail page.
func PersonURL(person string) string {
	return BrowseURL(BrowseFilter{Person: person}, 1)
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

// SearchURL builds a /search?... URL preserving query, mode, and page.
// Used by pagination and by mode-toggle links on the search page.
func SearchURL(query, mode string, page int) string {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	if mode != "" && mode != "semantic" {
		q.Set("mode", mode)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/search"
	}
	return "/search?" + q.Encode()
}
