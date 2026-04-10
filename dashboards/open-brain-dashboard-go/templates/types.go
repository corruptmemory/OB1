package templates

import "time"

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
