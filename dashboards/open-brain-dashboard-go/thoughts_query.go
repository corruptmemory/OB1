package main

import (
	"fmt"
	"strings"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
)

// buildListQuery produces the SELECT for the list pane given a filter
// set and whether the caller has a vector on hand for semantic ranking.
// Returns the full SQL and the positional args in order.
//
// Contract with the caller:
//
// When withVector is true, the caller must also have set Q to a non-empty
// string AND must prepend a pgvector.Vector as args[0] before calling
// pool.Query. The placeholder $1 is reserved for the vector. Filters then
// take $2..$N and LIMIT/OFFSET occupy the final two slots. The returned
// args slice does NOT include the vector — the caller prepends it.
//
// When withVector is false, filters take $1..$N and LIMIT/OFFSET take the
// final two slots. If Q is set in non-semantic mode, an ILIKE clause on
// content is added at $N.
//
// Passing withVector=true with Q=="" is a caller bug. The SQL will still
// be well-formed but the similarity score will be meaningless. Search()
// (in thoughts.go, Task 4+) is the sole caller and enforces the Q!=""
// precondition.
func buildListQuery(f templates.ListFilters, withVector bool) (string, []any) {
	var b strings.Builder
	args := []any{}
	n := 0
	next := func() int { n++; return n }

	// Reserve $1 for the embedding vector so the WHERE placeholders
	// line up regardless of branch. The vector itself is prepended by
	// the caller, not tracked here.
	var vectorPlaceholder int
	if withVector {
		vectorPlaceholder = next()
	}

	if withVector {
		fmt.Fprintf(&b, `SELECT id, content, metadata, created_at, 1 - (embedding <=> $%d) AS similarity
FROM thoughts
WHERE embedding IS NOT NULL`, vectorPlaceholder)
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
	if !withVector && f.Q != "" {
		fmt.Fprintf(&b, " AND content ILIKE '%%' || $%d || '%%'", next())
		args = append(args, f.Q)
	}

	if withVector {
		fmt.Fprintf(&b, " ORDER BY embedding <=> $%d", vectorPlaceholder)
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
//
// buildCountQuery doesn't need the vector in semantic mode — the count
// of "rows eligible for ranking" is just the count of rows with a
// non-null embedding matched against the other filters, and similarity
// isn't a WHERE clause. If a similarity threshold is ever introduced
// in buildListQuery, the count here will need to match.
func buildCountQuery(f templates.ListFilters, withVector bool) (string, []any) {
	var b strings.Builder
	args := []any{}
	n := 0
	next := func() int { n++; return n }

	if withVector {
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
	if !withVector && f.Q != "" {
		fmt.Fprintf(&b, " AND content ILIKE '%%' || $%d || '%%'", next())
		args = append(args, f.Q)
	}
	return b.String(), args
}

// buildBulkDeleteQuery returns the SQL and args for deleting a batch
// of thoughts by ID. Empty or nil ids returns empty strings so the
// caller can short-circuit without hitting the DB.
func buildBulkDeleteQuery(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	return "DELETE FROM thoughts WHERE id = ANY($1)", []any{ids}
}
