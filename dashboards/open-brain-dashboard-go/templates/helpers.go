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
