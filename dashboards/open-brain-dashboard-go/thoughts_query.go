package main

import (
	"fmt"
	"strings"
)

// ListFilters is the full set of query-string-driven selectors that the
// unified /?... handler combines into a single list query. Empty strings
// and zero values are skipped.
type ListFilters struct {
	Type    string // metadata->>'type' equality
	Topic   string // metadata->'topics' array membership
	Person  string // metadata->'people' array membership
	Days    int    // created_at > now() - interval '$ days'
	Q       string // when paired with a non-nil embedding in Search(),
	// triggers pgvector ranking; otherwise drops to ILIKE
	Page    int
	PerPage int
}

// buildListQuery produces the SELECT for the list pane given a filter
// set and whether the caller has an embedding on hand for semantic
// ranking. Returns the full SQL and the positional args in order.
//
// When hasEmbedding is true, the caller must prepend a pgvector.Vector
// as args[0] before calling pool.Query; the placeholder $1 is reserved
// for it. Filters then take $2..$N, and LIMIT/OFFSET occupy the final
// two slots. The returned args slice does NOT include the vector —
// Search() prepends it.
//
// When hasEmbedding is false, filters take $1..$N and LIMIT/OFFSET take
// the final two slots. If Q is set in non-semantic mode, an ILIKE clause
// on content is added.
func buildListQuery(f ListFilters, hasEmbedding bool) (string, []any) {
	var b strings.Builder
	args := []any{}
	n := 0
	next := func() int { n++; return n }

	// Reserve $1 for the embedding vector so the WHERE placeholders
	// line up regardless of branch. The vector itself is prepended by
	// the caller, not tracked here.
	var embeddingPlaceholder int
	if hasEmbedding {
		embeddingPlaceholder = next()
	}

	if hasEmbedding {
		fmt.Fprintf(&b, `SELECT id, content, metadata, created_at, 1 - (embedding <=> $%d) AS similarity
FROM thoughts
WHERE embedding IS NOT NULL`, embeddingPlaceholder)
	} else {
		b.WriteString(`SELECT id, content, metadata, created_at
FROM thoughts
WHERE 1=1`)
	}

	if f.Type != "" {
		fmt.Fprintf(&b, " AND metadata->>'type' = $%d", next())
		args = append(args, f.Type)
	}
	if f.Topic != "" {
		fmt.Fprintf(&b, " AND jsonb_typeof(metadata->'topics') = 'array' AND metadata->'topics' ? $%d", next())
		args = append(args, f.Topic)
	}
	if f.Person != "" {
		fmt.Fprintf(&b, " AND jsonb_typeof(metadata->'people') = 'array' AND metadata->'people' ? $%d", next())
		args = append(args, f.Person)
	}
	if f.Days > 0 {
		// Days is a validated int; no injection risk.
		fmt.Fprintf(&b, " AND created_at > now() - interval '%d days'", f.Days)
	}
	if !hasEmbedding && f.Q != "" {
		fmt.Fprintf(&b, " AND content ILIKE '%%' || $%d || '%%'", next())
		args = append(args, f.Q)
	}

	if hasEmbedding {
		fmt.Fprintf(&b, " ORDER BY embedding <=> $%d", embeddingPlaceholder)
	} else {
		b.WriteString(" ORDER BY created_at DESC")
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	perPage := f.PerPage
	if perPage < 1 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	fmt.Fprintf(&b, " LIMIT $%d OFFSET $%d", next(), next())
	args = append(args, perPage, offset)

	return b.String(), args
}

// buildCountQuery mirrors buildListQuery's WHERE clause but returns
// count(*) and omits ORDER/LIMIT/OFFSET. Used for pagination total.
func buildCountQuery(f ListFilters, hasEmbedding bool) (string, []any) {
	var b strings.Builder
	args := []any{}
	n := 0
	next := func() int { n++; return n }

	if hasEmbedding {
		b.WriteString("SELECT count(*) FROM thoughts WHERE embedding IS NOT NULL")
	} else {
		b.WriteString("SELECT count(*) FROM thoughts WHERE 1=1")
	}

	if f.Type != "" {
		fmt.Fprintf(&b, " AND metadata->>'type' = $%d", next())
		args = append(args, f.Type)
	}
	if f.Topic != "" {
		fmt.Fprintf(&b, " AND jsonb_typeof(metadata->'topics') = 'array' AND metadata->'topics' ? $%d", next())
		args = append(args, f.Topic)
	}
	if f.Person != "" {
		fmt.Fprintf(&b, " AND jsonb_typeof(metadata->'people') = 'array' AND metadata->'people' ? $%d", next())
		args = append(args, f.Person)
	}
	if f.Days > 0 {
		fmt.Fprintf(&b, " AND created_at > now() - interval '%d days'", f.Days)
	}
	if !hasEmbedding && f.Q != "" {
		fmt.Fprintf(&b, " AND content ILIKE '%%' || $%d || '%%'", next())
		args = append(args, f.Q)
	}
	return b.String(), args
}
