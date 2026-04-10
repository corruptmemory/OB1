package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
	"github.com/pgvector/pgvector-go"
)

const (
	recentLimit    = 20
	topTopicsLimit = 10
	browsePerPage  = 20
	searchPerPage  = 20
)

// HomeData runs the four queries the home page needs — total count, counts
// by type, top topics, and the most recent N thoughts — against the single
// `thoughts` table, reading type/topics/people out of the metadata jsonb blob
// with coalesce+typeof guards so malformed or missing fields don't error.
func (d *DB) HomeData(ctx context.Context) (*templates.HomeData, error) {
	hd := &templates.HomeData{}

	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM thoughts`).Scan(&hd.Total); err != nil {
		return nil, fmt.Errorf("count thoughts: %w", err)
	}

	if err := d.pool.QueryRow(ctx, `
		SELECT count(*) FROM thoughts
		WHERE created_at > now() - interval '7 days'
	`).Scan(&hd.WeekCount); err != nil {
		return nil, fmt.Errorf("count week: %w", err)
	}

	typeRows, err := d.pool.Query(ctx, `
		SELECT coalesce(metadata->>'type', 'unknown') AS t, count(*) AS n
		FROM thoughts
		GROUP BY t
		ORDER BY n DESC, t ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query types: %w", err)
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var tc templates.TypeCount
		if err := typeRows.Scan(&tc.Type, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan type row: %w", err)
		}
		hd.Types = append(hd.Types, tc)
	}
	if err := typeRows.Err(); err != nil {
		return nil, fmt.Errorf("type rows: %w", err)
	}

	topicRows, err := d.pool.Query(ctx, `
		SELECT topic, count(*) AS n
		FROM thoughts, jsonb_array_elements_text(metadata->'topics') AS topic
		WHERE jsonb_typeof(metadata->'topics') = 'array'
		GROUP BY topic
		ORDER BY n DESC, topic ASC
		LIMIT $1
	`, topTopicsLimit)
	if err != nil {
		return nil, fmt.Errorf("query topics: %w", err)
	}
	defer topicRows.Close()
	for topicRows.Next() {
		var tc templates.TopicCount
		if err := topicRows.Scan(&tc.Topic, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan topic row: %w", err)
		}
		hd.Topics = append(hd.Topics, tc)
	}
	if err := topicRows.Err(); err != nil {
		return nil, fmt.Errorf("topic rows: %w", err)
	}

	recentRows, err := d.pool.Query(ctx, `
		SELECT id, content, metadata, created_at
		FROM thoughts
		ORDER BY created_at DESC
		LIMIT $1
	`, recentLimit)
	if err != nil {
		return nil, fmt.Errorf("query recent: %w", err)
	}
	defer recentRows.Close()
	for recentRows.Next() {
		var (
			t    templates.ThoughtData
			meta []byte
		)
		if err := recentRows.Scan(&t.ID, &t.Content, &meta, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent row: %w", err)
		}
		var m struct {
			Type   string   `json:"type"`
			Topics []string `json:"topics"`
			People []string `json:"people"`
		}
		// qwen2.5:3b's metadata shape is best-effort; missing fields and
		// type mismatches are normal, so ignore unmarshal errors and let
		// the zero values stand in.
		_ = json.Unmarshal(meta, &m)
		if m.Type == "" {
			m.Type = "unknown"
		}
		t.Type = m.Type
		t.Topics = m.Topics
		t.People = m.People
		hd.Recent = append(hd.Recent, t)
	}
	if err := recentRows.Err(); err != nil {
		return nil, fmt.Errorf("recent rows: %w", err)
	}

	return hd, nil
}

// Browse runs a filtered + paginated list query against the thoughts
// table. The filters combine with AND; empty fields are skipped. Topic and
// person use the jsonb `?` operator to check for array membership, type
// uses a scalar text match on metadata->>'type', q is a plain ILIKE
// substring match on content, and days is a time window from now.
//
// pgx's numbered placeholders ($1, $2, ...) carry every user-supplied
// value. The only value interpolated as raw SQL is the days interval,
// which is a validated int and therefore injection-safe.
func (d *DB) Browse(ctx context.Context, f templates.BrowseFilter, page int) (*templates.BrowseData, error) {
	if page < 1 {
		page = 1
	}

	var where strings.Builder
	where.WriteString(" WHERE 1=1")
	args := []any{}
	n := 0
	next := func() int { n++; return n }

	if f.Type != "" {
		fmt.Fprintf(&where, " AND metadata->>'type' = $%d", next())
		args = append(args, f.Type)
	}
	if f.Topic != "" {
		fmt.Fprintf(&where, " AND jsonb_typeof(metadata->'topics') = 'array' AND metadata->'topics' ? $%d", next())
		args = append(args, f.Topic)
	}
	if f.Person != "" {
		fmt.Fprintf(&where, " AND jsonb_typeof(metadata->'people') = 'array' AND metadata->'people' ? $%d", next())
		args = append(args, f.Person)
	}
	if f.Q != "" {
		fmt.Fprintf(&where, " AND content ILIKE '%%' || $%d || '%%'", next())
		args = append(args, f.Q)
	}
	if f.Days > 0 {
		// Days is an int from the handler's validated strconv.Atoi; no
		// injection risk. pgx interval binding is awkward so we go direct.
		fmt.Fprintf(&where, " AND created_at > now() - interval '%d days'", f.Days)
	}

	var total int64
	countSQL := "SELECT count(*) FROM thoughts" + where.String()
	if err := d.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("browse count: %w", err)
	}

	totalPages := int((total + int64(browsePerPage) - 1) / int64(browsePerPage))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * browsePerPage

	listSQL := "SELECT id, content, metadata, created_at FROM thoughts" + where.String() +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", next(), next())
	listArgs := append(args, browsePerPage, offset)

	rows, err := d.pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("browse query: %w", err)
	}
	defer rows.Close()

	var results []templates.ThoughtData
	for rows.Next() {
		var (
			t    templates.ThoughtData
			meta []byte
		)
		if err := rows.Scan(&t.ID, &t.Content, &meta, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("browse scan: %w", err)
		}
		var m struct {
			Type   string   `json:"type"`
			Topics []string `json:"topics"`
			People []string `json:"people"`
		}
		_ = json.Unmarshal(meta, &m)
		if m.Type == "" {
			m.Type = "unknown"
		}
		t.Type = m.Type
		t.Topics = m.Topics
		t.People = m.People
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse rows: %w", err)
	}

	return &templates.BrowseData{
		Filter:     f,
		Results:    results,
		Total:      total,
		Page:       page,
		PerPage:    browsePerPage,
		TotalPages: totalPages,
	}, nil
}

// scanRowsIntoThoughts is a helper used by the browse, search, and home
// query paths that all do the same "scan id+content+metadata+created_at
// into a ThoughtData slice" work. The extraScan slice holds optional extra
// pointers (e.g. a similarity float) that the caller wants appended to the
// Scan argument list, in the order they appear in the SELECT.
func scanThoughtRow(row interface {
	Scan(dest ...any) error
}, t *templates.ThoughtData, extras ...any) error {
	var meta []byte
	base := []any{&t.ID, &t.Content, &meta, &t.CreatedAt}
	if err := row.Scan(append(base, extras...)...); err != nil {
		return err
	}
	var m struct {
		Type   string   `json:"type"`
		Topics []string `json:"topics"`
		People []string `json:"people"`
	}
	_ = json.Unmarshal(meta, &m)
	if m.Type == "" {
		m.Type = "unknown"
	}
	t.Type = m.Type
	t.Topics = m.Topics
	t.People = m.People
	return nil
}

// SearchSemantic runs a pgvector similarity search. The embedding is
// expected to be a 1024-dim float32 slice produced by mxbai-embed-large
// via the Ollama client. Results are ordered by ascending cosine distance
// (<=>), and Similarity is populated as (1 - distance) so 1.0 means
// identical and 0.0 means orthogonal.
func (d *DB) SearchSemantic(ctx context.Context, query string, embedding []float32, page int) (*templates.SearchData, error) {
	if page < 1 {
		page = 1
	}
	vec := pgvector.NewVector(embedding)

	var total int64
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*) FROM thoughts WHERE embedding IS NOT NULL
	`).Scan(&total); err != nil {
		return nil, fmt.Errorf("semantic count: %w", err)
	}

	totalPages := int((total + int64(searchPerPage) - 1) / int64(searchPerPage))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * searchPerPage

	rows, err := d.pool.Query(ctx, `
		SELECT id, content, metadata, created_at,
		       1 - (embedding <=> $1) AS similarity
		FROM thoughts
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $2 OFFSET $3
	`, vec, searchPerPage, offset)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	defer rows.Close()

	var results []templates.ThoughtData
	for rows.Next() {
		var t templates.ThoughtData
		if err := scanThoughtRow(rows, &t, &t.Similarity); err != nil {
			return nil, fmt.Errorf("semantic scan: %w", err)
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic rows: %w", err)
	}

	return &templates.SearchData{
		Query:      query,
		Mode:       "semantic",
		Results:    results,
		Total:      total,
		Page:       page,
		PerPage:    searchPerPage,
		TotalPages: totalPages,
	}, nil
}

// SearchText runs a plain ILIKE substring match on the content column,
// ordered by recency. Dedicated to the /search page's text mode — browse
// also exposes an ILIKE match via its `q` filter, but search is a
// standalone "find me thoughts about X" flow with its own UX.
func (d *DB) SearchText(ctx context.Context, query string, page int) (*templates.SearchData, error) {
	if page < 1 {
		page = 1
	}

	var total int64
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*) FROM thoughts WHERE content ILIKE '%' || $1 || '%'
	`, query).Scan(&total); err != nil {
		return nil, fmt.Errorf("text count: %w", err)
	}

	totalPages := int((total + int64(searchPerPage) - 1) / int64(searchPerPage))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * searchPerPage

	rows, err := d.pool.Query(ctx, `
		SELECT id, content, metadata, created_at
		FROM thoughts
		WHERE content ILIKE '%' || $1 || '%'
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, query, searchPerPage, offset)
	if err != nil {
		return nil, fmt.Errorf("text query: %w", err)
	}
	defer rows.Close()

	var results []templates.ThoughtData
	for rows.Next() {
		var t templates.ThoughtData
		if err := scanThoughtRow(rows, &t); err != nil {
			return nil, fmt.Errorf("text scan: %w", err)
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("text rows: %w", err)
	}

	return &templates.SearchData{
		Query:      query,
		Mode:       "text",
		Results:    results,
		Total:      total,
		Page:       page,
		PerPage:    searchPerPage,
		TotalPages: totalPages,
	}, nil
}

// ThoughtByID fetches a single thought and its full metadata for the detail
// page. Returns pgx.ErrNoRows unchanged when the id doesn't exist so the
// handler can branch to a 404 via errors.Is.
func (d *DB) ThoughtByID(ctx context.Context, id int64) (*templates.ThoughtDetail, error) {
	var (
		detail templates.ThoughtDetail
		meta   []byte
	)
	err := d.pool.QueryRow(ctx, `
		SELECT id, content, metadata, created_at
		FROM thoughts
		WHERE id = $1
	`, id).Scan(&detail.ID, &detail.Content, &meta, &detail.CreatedAt)
	if err != nil {
		return nil, err
	}

	detail.RawMetadata = meta

	// All metadata fields are best-effort — missing keys and shape
	// mismatches (action_items can be strings or objects) are handled by
	// ActionItem.UnmarshalJSON and the jsonb_typeof guards in the SQL.
	var m struct {
		Type           string              `json:"type"`
		Topics         []string            `json:"topics"`
		People         []string            `json:"people"`
		ActionItems    []templates.ActionItem `json:"action_items"`
		DatesMentioned []string            `json:"dates_mentioned"`
		Source         string              `json:"source"`
	}
	_ = json.Unmarshal(meta, &m)
	if m.Type == "" {
		m.Type = "unknown"
	}
	detail.Type = m.Type
	detail.Topics = m.Topics
	detail.People = m.People
	detail.ActionItems = m.ActionItems
	detail.DatesMentioned = m.DatesMentioned
	detail.Source = m.Source

	return &detail, nil
}
