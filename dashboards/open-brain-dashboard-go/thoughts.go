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

// CreateThoughtInput is the set of fields the dashboard's quick-capture
// form writes through to a new thoughts row. Embedding is always required
// because capture always runs ollama.Embed before the DB write — there's
// no "skip embedding" path on create. ActionItems and DatesMentioned are
// populated by the AI extraction step and may be nil when extraction fails
// or the chat model isn't configured.
type CreateThoughtInput struct {
	Content        string
	Type           string
	Topics         []string
	People         []string
	ActionItems    []string
	DatesMentioned []string
	Embedding      []float32
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
	actionItems := in.ActionItems
	if actionItems == nil {
		actionItems = []string{}
	}
	datesMentioned := in.DatesMentioned
	if datesMentioned == nil {
		datesMentioned = []string{}
	}
	metadata := map[string]any{
		"type":            in.Type,
		"topics":          topics,
		"people":          people,
		"action_items":    actionItems,
		"dates_mentioned": datesMentioned,
		"source":          "dashboard",
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
	var countFinalArgs []any
	if withVector {
		vec := pgvector.NewVector(embedding)
		countFinalArgs = append([]any{vec}, countArgs...)
	} else {
		countFinalArgs = countArgs
	}
	var total int64
	if err := d.pool.QueryRow(ctx, countSQL, countFinalArgs...).Scan(&total); err != nil {
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

// CoalesceThoughts atomically inserts a new thought and deletes the
// originals in a single transaction. Returns the new thought's id.
// If any step fails, the transaction is rolled back and originals
// are untouched.
func (d *DB) CoalesceThoughts(ctx context.Context, in CreateThoughtInput, deleteIDs []int64) (int64, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin coalesce tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Build metadata for the new thought.
	topics := in.Topics
	if topics == nil {
		topics = []string{}
	}
	people := in.People
	if people == nil {
		people = []string{}
	}
	actionItems := in.ActionItems
	if actionItems == nil {
		actionItems = []string{}
	}
	datesMentioned := in.DatesMentioned
	if datesMentioned == nil {
		datesMentioned = []string{}
	}
	metadata := map[string]any{
		"type":            in.Type,
		"topics":          topics,
		"people":          people,
		"action_items":    actionItems,
		"dates_mentioned": datesMentioned,
		"source":          "dashboard-coalesce",
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal coalesce metadata: %w", err)
	}

	// Insert the new coalesced thought.
	var newID int64
	vec := pgvector.NewVector(in.Embedding)
	err = tx.QueryRow(ctx, `
		INSERT INTO thoughts (content, metadata, embedding)
		VALUES ($1, $2::jsonb, $3)
		RETURNING id
	`, in.Content, metaJSON, vec).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("insert coalesced thought: %w", err)
	}

	// Delete the originals.
	delSQL, delArgs := buildBulkDeleteQuery(deleteIDs)
	if delSQL != "" {
		if _, err := tx.Exec(ctx, delSQL, delArgs...); err != nil {
			return 0, fmt.Errorf("delete originals in coalesce: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit coalesce tx: %w", err)
	}
	return newID, nil
}

// FetchContents retrieves the content text for a list of thought IDs.
// Used by coalesce to gather the source material before synthesis.
func (d *DB) FetchContents(ctx context.Context, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Build a parameterized IN clause.
	args := make([]any, len(ids))
	params := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	query := fmt.Sprintf(
		"SELECT content FROM thoughts WHERE id IN (%s) ORDER BY created_at ASC",
		strings.Join(params, ", "),
	)
	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch contents: %w", err)
	}
	defer rows.Close()

	var contents []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		contents = append(contents, c)
	}
	return contents, rows.Err()
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
		LIMIT 50
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
		LIMIT 50
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
