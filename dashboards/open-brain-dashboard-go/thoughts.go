package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
	"github.com/jackc/pgx/v5"
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

// CreateThoughtInput is the set of fields the dashboard's quick-capture
// form writes through to a new thoughts row. Embedding is always required
// because capture always runs ollama.Embed before the DB write — there's
// no "skip embedding" path on create.
type CreateThoughtInput struct {
	Content   string
	Type      string
	Topics    []string
	People    []string
	Embedding []float32
}

// CreateThought inserts a new thoughts row and returns its id. Metadata
// is built as a fresh jsonb object with type/topics/people/source; the
// MCP capture path writes source="mcp", so the dashboard writes
// source="dashboard" to keep provenance visible in the raw metadata blob
// on the detail page.
func (d *DB) CreateThought(ctx context.Context, in CreateThoughtInput) (int64, error) {
	topics := in.Topics
	if topics == nil {
		topics = []string{}
	}
	people := in.People
	if people == nil {
		people = []string{}
	}
	metadata := map[string]any{
		"type":   in.Type,
		"topics": topics,
		"people": people,
		"source": "dashboard",
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal metadata: %w", err)
	}

	var id int64
	vec := pgvector.NewVector(in.Embedding)
	err = d.pool.QueryRow(ctx, `
		INSERT INTO thoughts (content, metadata, embedding)
		VALUES ($1, $2::jsonb, $3)
		RETURNING id
	`, in.Content, metaJSON, vec).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert thought: %w", err)
	}
	return id, nil
}

// UpdateThoughtInput is the set of editable fields carried from the edit
// form's POST body through to the SQL update. Embedding is optional — nil
// means "content didn't change, preserve the existing embedding unchanged."
// ActionItems is always written as the always-object shape
// [{description, priority}, ...], regardless of whether the existing row
// used the string-array shape. This is a per-thought forward migration on
// first edit.
type UpdateThoughtInput struct {
	Content     string
	Type        string
	Topics      []string
	People      []string
	ActionItems []templates.ActionItem
	Embedding   []float32
}

// UpdateThought writes the edit-form values back to a single row. Type,
// topics, people, and updated_at are merged into the existing metadata
// jsonb blob via the || operator, so fields we don't touch (action_items,
// dates_mentioned, source, content_fingerprint, etc.) carry over untouched.
// When Embedding is non-nil we also update the embedding column in the same
// statement, keeping content and its vector atomically consistent.
func (d *DB) UpdateThought(ctx context.Context, id int64, in UpdateThoughtInput) error {
	// Build the metadata patch. jsonb's || operator merges keys at the top
	// level with the right side winning, which is exactly the semantics we
	// want: metadata || {"type": "idea", ...} leaves everything else alone.
	patch := map[string]any{
		"type":         in.Type,
		"topics":       in.Topics,
		"people":       in.People,
		"action_items": in.ActionItems,
		"updated_at":   time.Now().UTC().Format(time.RFC3339),
	}
	// JSON nil slices become "null" which overwrites as null in jsonb; force
	// empty arrays so the metadata shape stays consistent with captures.
	if in.Topics == nil {
		patch["topics"] = []string{}
	}
	if in.People == nil {
		patch["people"] = []string{}
	}
	if in.ActionItems == nil {
		patch["action_items"] = []templates.ActionItem{}
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal metadata patch: %w", err)
	}

	if in.Embedding != nil {
		vec := pgvector.NewVector(in.Embedding)
		tag, err := d.pool.Exec(ctx, `
			UPDATE thoughts
			SET content = $2,
			    metadata = metadata || $3::jsonb,
			    embedding = $4
			WHERE id = $1
		`, id, in.Content, patchJSON, vec)
		if err != nil {
			return fmt.Errorf("update thought with embedding: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	}

	tag, err := d.pool.Exec(ctx, `
		UPDATE thoughts
		SET content = $2,
		    metadata = metadata || $3::jsonb
		WHERE id = $1
	`, id, in.Content, patchJSON)
	if err != nil {
		return fmt.Errorf("update thought: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteThought removes a row by id. Returns pgx.ErrNoRows when the id
// doesn't exist so the handler can 404 instead of 500.
func (d *DB) DeleteThought(ctx context.Context, id int64) error {
	tag, err := d.pool.Exec(ctx, `DELETE FROM thoughts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete thought: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Search is the one list-pane query entry point for the v1.5 handler.
// It combines filters with optional semantic ranking and automatically
// falls back to ILIKE substring search when:
//   - the caller passed a nil embedding (Ollama down), or
//   - the semantic query returned zero rows (happens only when the
//     non-Q filters exclude every row with a non-null embedding;
//     pgvector itself has no threshold applied yet, so this fallback
//     path is rare in practice and will become more meaningful if a
//     similarity cutoff is introduced).
//
// The caller is responsible for calling ollama.Embed separately and
// passing the result (nil on failure). This keeps thoughts.go free of
// HTTP concerns.
//
// Preconditions:
//   - If embedding is non-nil, filters.Q must also be non-empty.
//     (Enforces the buildListQuery withVector contract.)
//
// Cost note: the fallback path runs a second (count, list) query pair
// when it fires. Given how rare the fallback currently is, this is
// acceptable; if a similarity threshold is added later and fallback
// becomes hot, revisit.
func (d *DB) Search(ctx context.Context, f templates.ListFilters, embedding []float32) (*templates.ListResult, error) {
	// Path 1: no query, pure filter list.
	if f.Q == "" {
		return d.listQuery(ctx, f, nil, templates.SearchModeNone)
	}

	// Path 2: query present + embedding available. Try semantic first.
	if embedding != nil {
		result, err := d.listQuery(ctx, f, embedding, templates.SearchModeSemantic)
		if err != nil {
			return nil, err
		}
		if result.Total > 0 {
			return result, nil
		}
		// Fall through: semantic returned zero rows for these filters.
		fallback, err := d.listQuery(ctx, f, nil, templates.SearchModeFallback)
		if err != nil {
			return nil, err
		}
		return fallback, nil
	}

	// Path 3: query present, Ollama was down. Text-only.
	return d.listQuery(ctx, f, nil, templates.SearchModeTextOnly)
}

// listQuery runs one pass against the database using buildListQuery
// and buildCountQuery. Pulled out of Search so the fallback paths
// share the same scan logic.
func (d *DB) listQuery(ctx context.Context, f templates.ListFilters, embedding []float32, mode templates.SearchResultMode) (*templates.ListResult, error) {
	withVector := embedding != nil

	countSQL, countArgs := buildCountQuery(f, withVector)
	var total int64
	if err := d.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("list count (%d): %w", mode, err)
	}

	perPage := f.PerPage
	if perPage < 1 {
		perPage = 50
	}
	totalPages := int((total + int64(perPage) - 1) / int64(perPage))
	if totalPages == 0 {
		totalPages = 1
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	effective := f
	effective.Page = page
	effective.PerPage = perPage

	listSQL, listArgs := buildListQuery(effective, withVector)

	var finalArgs []any
	if withVector {
		vec := pgvector.NewVector(embedding)
		finalArgs = append([]any{vec}, listArgs...)
	} else {
		finalArgs = listArgs
	}

	rows, err := d.pool.Query(ctx, listSQL, finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("list query (%d): %w", mode, err)
	}
	defer rows.Close()

	var results []templates.ThoughtData
	for rows.Next() {
		var t templates.ThoughtData
		if withVector {
			if err := scanThoughtRow(rows, &t, &t.Similarity); err != nil {
				return nil, fmt.Errorf("list scan (%d): %w", mode, err)
			}
		} else {
			if err := scanThoughtRow(rows, &t); err != nil {
				return nil, fmt.Errorf("list scan (%d): %w", mode, err)
			}
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rows (%d): %w", mode, err)
	}

	return &templates.ListResult{
		Filter:     effective,
		Mode:       mode,
		Results:    results,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// BulkDelete removes a batch of thoughts by ID in a single statement.
// Returns the number of rows affected. Empty ids is a no-op that
// returns (0, nil) without hitting the DB.
func (d *DB) BulkDelete(ctx context.Context, ids []int64) (int64, error) {
	sql, args := buildBulkDeleteQuery(ids)
	if sql == "" {
		return 0, nil
	}
	tag, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("bulk delete: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SidebarCounts is the one query set that powers the filter rail:
// total, week count, by-type counts, top-10 topics, top-10 people.
type SidebarCounts struct {
	Total     int64
	WeekCount int64
	Types     []templates.TypeCount
	Topics    []templates.TopicCount
	People    []templates.PersonCount
}

func (d *DB) SidebarCounts(ctx context.Context) (*SidebarCounts, error) {
	sc := &SidebarCounts{}

	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM thoughts`).Scan(&sc.Total); err != nil {
		return nil, fmt.Errorf("sidebar total: %w", err)
	}
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*) FROM thoughts WHERE created_at > now() - interval '7 days'
	`).Scan(&sc.WeekCount); err != nil {
		return nil, fmt.Errorf("sidebar week: %w", err)
	}

	typeRows, err := d.pool.Query(ctx, `
		SELECT coalesce(metadata->>'type', 'unknown') AS t, count(*) AS n
		FROM thoughts
		GROUP BY t
		ORDER BY n DESC, t ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("sidebar types: %w", err)
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var tc templates.TypeCount
		if err := typeRows.Scan(&tc.Type, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan type: %w", err)
		}
		sc.Types = append(sc.Types, tc)
	}

	topicRows, err := d.pool.Query(ctx, `
		SELECT topic, count(*) AS n
		FROM thoughts, jsonb_array_elements_text(metadata->'topics') AS topic
		WHERE jsonb_typeof(metadata->'topics') = 'array'
		GROUP BY topic
		ORDER BY n DESC, topic ASC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("sidebar topics: %w", err)
	}
	defer topicRows.Close()
	for topicRows.Next() {
		var tc templates.TopicCount
		if err := topicRows.Scan(&tc.Topic, &tc.Count); err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}
		sc.Topics = append(sc.Topics, tc)
	}

	peopleRows, err := d.pool.Query(ctx, `
		SELECT person, count(*) AS n
		FROM thoughts, jsonb_array_elements_text(metadata->'people') AS person
		WHERE jsonb_typeof(metadata->'people') = 'array'
		GROUP BY person
		ORDER BY n DESC, person ASC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("sidebar people: %w", err)
	}
	defer peopleRows.Close()
	for peopleRows.Next() {
		var pc templates.PersonCount
		if err := peopleRows.Scan(&pc.Person, &pc.Count); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		sc.People = append(sc.People, pc)
	}

	return sc, nil
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
		Type           string                 `json:"type"`
		Topics         []string               `json:"topics"`
		People         []string               `json:"people"`
		ActionItems    []templates.ActionItem `json:"action_items"`
		DatesMentioned []string               `json:"dates_mentioned"`
		Source         string                 `json:"source"`
		UpdatedAt      string                 `json:"updated_at"`
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
	if m.UpdatedAt != "" {
		if ts, err := time.Parse(time.RFC3339, m.UpdatedAt); err == nil {
			detail.UpdatedAt = &ts
		}
	}

	return &detail, nil
}
