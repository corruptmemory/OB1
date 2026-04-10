package templates

import (
	"fmt"
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
