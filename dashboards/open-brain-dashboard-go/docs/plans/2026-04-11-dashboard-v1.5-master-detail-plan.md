# Dashboard v1.5 Master/Detail Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the open-brain-dashboard-go Go binary from four flat pages (home/browse/search/detail) into a single Gmail-style master/detail shell driven entirely by URL query params, and wire bulk-delete end-to-end.

**Architecture:** One root route `/` renders a three-pane CSS grid shell (sidebar · list · detail), with htmx partial swaps targeting individual panes. A floating Gmail-style `<dialog>` compose panel sits as a sibling of the shell for new-thought capture. Search collapses to one input with automatic semantic→text fallback. Bulk delete uses CSS `:has()` for the toolbar state machine.

**Tech Stack:** Go 1.25+, chi v5 router, templ for type-safe HTML, htmx for partial swaps (vendored), pgx/v5 against Postgres + pgvector, Ollama `/v1/embeddings` for semantic search. Single static binary built with `./build.sh`.

**Design source of truth:** See `dashboards/open-brain-dashboard-go/docs/plans/2026-04-11-dashboard-v1.5-master-detail-design.md` for the full shape. This plan executes that design task by task. The design doc's principles (in `dashboards/open-brain-dashboard-go/CLAUDE.md`) are binding.

**Branch:** Work happens on `self-hosted-ui`. A separate `self-hosted-ui-v1.5` branch is optional — skip it if you want linear history. All commits should prefix `[dashboards]` per repo convention.

**Testing philosophy:** TDD where there's real logic (query builders, handler branching, HX-Trigger emission). Visual verification in the browser where the code is pure markup (templ templates, CSS). Tests use `t.TempDir()` and the `pgxtest`-style "build the SQL, assert on the string" pattern — no test database required. Full integration is verified manually against the live brain at `home-server:5432`.

---

## Task 0: Pre-flight sanity check

**Purpose:** Verify the v1.1 build still works on `self-hosted-ui` before touching anything.

**Step 1: Build the current code**

```bash
cd /home/jim/projects/open-brain/dashboards/open-brain-dashboard-go
./build.sh build
```

**Expected:** Builds cleanly, produces `./open-brain-dashboard-go` binary.

**Step 2: Start the server against the live brain**

```bash
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

**Expected:** Listens on `127.0.0.1:8080`, home page loads, recent captures render. Ctrl-C to stop.

**Step 3: Confirm branch state**

```bash
git status
git log --oneline -5
```

**Expected:** Clean working tree, recent commit is the design doc (`9dd7b71` or later).

**No commit in this task.** It's just a gate.

---

## Task 1: Light-mode palette in tokens.css

**Purpose:** Ship both dark and light palettes in v1.5 so the theme toggle has something to toggle.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/static/tokens.css`

**Step 1: Add the light palette**

Replace the single `:root { ... }` block with a dark default + a light override that activates when `<html data-theme="light">` is set. The type-chip colors stay the same across themes (they're semantic, not decorative).

```css
/* Dark theme tokens for open-brain-dashboard-go.
   Dark is the default. Light is active when <html data-theme="light">. */

:root {
	--bg: #0f1419;
	--bg-elevated: #161b22;
	--bg-raised: #1c2128;
	--fg: #e6edf3;
	--fg-muted: #8b949e;
	--fg-subtle: #6e7681;
	--accent: #58a6ff;
	--accent-dim: #388bfd33;
	--border: #30363d;
	--border-muted: #21262d;
	--danger: #f85149;
	--success: #3fb950;
	--warning: #d29922;

	--font-sans: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	--font-mono: ui-monospace, "Cascadia Code", "JetBrains Mono", Menlo, monospace;

	--radius: 6px;
	--radius-lg: 10px;

	--space-1: 0.25rem;
	--space-2: 0.5rem;
	--space-3: 0.75rem;
	--space-4: 1rem;
	--space-5: 1.5rem;
	--space-6: 2rem;
	--space-8: 3rem;

	--max-content-width: 1100px;
}

:root[data-theme="light"] {
	--bg: #ffffff;
	--bg-elevated: #f6f8fa;
	--bg-raised: #eaeef2;
	--fg: #1f2328;
	--fg-muted: #59636e;
	--fg-subtle: #818b98;
	--accent: #0969da;
	--accent-dim: #0969da1a;
	--border: #d1d9e0;
	--border-muted: #e4e8ed;
	--danger: #cf222e;
	--success: #1a7f37;
	--warning: #9a6700;
}
```

**Step 2: Verify build**

```bash
./build.sh build
```

**Expected:** Clean build. CSS isn't validated at build time, so this just confirms nothing else broke.

**Step 3: Commit**

```bash
git add dashboards/open-brain-dashboard-go/static/tokens.css
git commit -m "$(cat <<'EOF'
[dashboards] Add light palette to dashboard-go tokens

Light palette activates via :root[data-theme="light"] so the upcoming
theme toggle in v1.5 has both sides to swap between. Dark stays default.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Minimal `app.js` scaffold

**Purpose:** Create the tiny client-JS file that will hold theme toggle, sidebar collapse, compose mode switchers, and clear-selection. Ship it empty-but-wired so later tasks can drop in handlers without touching build/embed plumbing.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/static/js/app.js`
- Modify: `dashboards/open-brain-dashboard-go/static/embed.go` (if it exists — check first)

**Step 1: Inspect the embed setup**

```bash
cat dashboards/open-brain-dashboard-go/static/embed.go 2>/dev/null || echo "no embed.go"
```

If there's no `embed.go` at `static/`, the embed is likely in `server.go` via `//go:embed static/*`. Check `server.go` for the embed directive and confirm it uses `static/*` or `static/**`; if it's `static/*.css` or similar narrow, widen it.

**Step 2: Create the initial js file**

```bash
mkdir -p dashboards/open-brain-dashboard-go/static/js
```

```javascript
// dashboards/open-brain-dashboard-go/static/js/app.js
//
// Tiny client-side glue for open-brain-dashboard-go.
//
// The dashboard's source of truth is the URL. This file only handles
// ephemeral interactions that genuinely need client state: theme
// preference (localStorage), sidebar collapse (localStorage), compose
// dialog mode switching (native <dialog>), and a handful of
// single-line event handlers called from inline onclick attributes.
//
// If you're adding anything here that describes "what's on the
// screen," stop and put it in the URL instead.

(function () {
	"use strict";

	// Theme: read localStorage and apply on first load, before htmx
	// touches the DOM.
	const savedTheme = localStorage.getItem("ob-theme");
	if (savedTheme === "light") {
		document.documentElement.setAttribute("data-theme", "light");
	}

	window.obToggleTheme = function () {
		const current = document.documentElement.getAttribute("data-theme");
		if (current === "light") {
			document.documentElement.removeAttribute("data-theme");
			localStorage.setItem("ob-theme", "dark");
		} else {
			document.documentElement.setAttribute("data-theme", "light");
			localStorage.setItem("ob-theme", "light");
		}
	};
})();
```

**Step 3: Verify the server serves it**

Start the server (`./build.sh run`) and hit `http://127.0.0.1:8080/static/js/app.js`. Expect the file body. If you get a 404, the embed directive in `server.go` is too narrow — widen to `//go:embed static` and rebuild.

**Step 4: Stop the server and commit**

```bash
git add dashboards/open-brain-dashboard-go/static/js/app.js
# + any server.go embed widening from step 3
git commit -m "$(cat <<'EOF'
[dashboards] Scaffold static/js/app.js for v1.5 client glue

Initial file contains only the theme-toggle helper (called from inline
onclick later). Subsequent v1.5 tasks add sidebar collapse, compose mode
switching, and clear-selection. Kept deliberately small — the URL is the
source of truth, this file is for ephemeral client state only.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Extract the unified list-query builder (TDD)

**Purpose:** The v1.5 `/` handler needs one query that combines type/topic/person/days filters AND optional semantic ranking AND text fallback. Extract this logic into a pure `buildListQuery` function that returns `(sql string, args []any)` so it can be unit-tested without a database. Table-driven test as per the user's test preferences.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/thoughts_query.go`
- Create: `dashboards/open-brain-dashboard-go/thoughts_query_test.go`
- Modify: `dashboards/open-brain-dashboard-go/thoughts.go` (will use `buildListQuery` in later tasks; don't modify yet)

**Step 1: Write the failing test**

```go
// dashboards/open-brain-dashboard-go/thoughts_query_test.go
package main

import (
	"strings"
	"testing"
)

func TestBuildListQuery(t *testing.T) {
	cases := []struct {
		name        string
		filters     ListFilters
		hasEmbedding bool
		wantSubstr  []string
		wantArgLen  int
	}{
		{
			name:       "no filters, no query",
			filters:    ListFilters{Page: 1, PerPage: 50},
			wantSubstr: []string{"FROM thoughts", "ORDER BY created_at DESC", "LIMIT $1 OFFSET $2"},
			wantArgLen: 2,
		},
		{
			name:       "type filter only",
			filters:    ListFilters{Type: "task", Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->>'type' = $1", "LIMIT $2 OFFSET $3"},
			wantArgLen: 3,
		},
		{
			name:       "topic filter uses jsonb array membership",
			filters:    ListFilters{Topic: "open-brain", Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->'topics' ? $1", "jsonb_typeof(metadata->'topics') = 'array'"},
			wantArgLen: 3,
		},
		{
			name:       "q triggers ILIKE in non-semantic mode",
			filters:    ListFilters{Q: "deployment", Page: 1, PerPage: 50},
			hasEmbedding: false,
			wantSubstr: []string{"content ILIKE '%' || $1 || '%'"},
			wantArgLen: 3,
		},
		{
			name:         "q with embedding uses pgvector distance",
			filters:      ListFilters{Q: "deployment", Page: 1, PerPage: 50},
			hasEmbedding: true,
			wantSubstr:   []string{"1 - (embedding <=> $1)", "ORDER BY embedding <=> $1", "embedding IS NOT NULL"},
			wantArgLen:   3,
		},
		{
			name:       "all filters combine with AND",
			filters:    ListFilters{Type: "task", Topic: "open-brain", Person: "Jim", Days: 7, Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->>'type' = $1", "metadata->'topics' ? $2", "metadata->'people' ? $3", "interval '7 days'"},
			wantArgLen: 5,
		},
		{
			name:       "pagination offset math",
			filters:    ListFilters{Page: 3, PerPage: 50},
			wantSubstr: []string{"OFFSET $2"},
			wantArgLen: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := buildListQuery(tc.filters, tc.hasEmbedding)
			for _, sub := range tc.wantSubstr {
				if !strings.Contains(sql, sub) {
					t.Errorf("sql missing %q\nactual:\n%s", sub, sql)
				}
			}
			if len(args) != tc.wantArgLen {
				t.Errorf("args length = %d, want %d (args=%v)", len(args), tc.wantArgLen, args)
			}
		})
	}
}

func TestBuildCountQuery(t *testing.T) {
	// Count query mirrors the list query's WHERE clause but returns count(*)
	// instead of rows and skips ORDER/LIMIT/OFFSET.
	sql, args := buildCountQuery(ListFilters{Type: "task", Topic: "open-brain"}, false)
	if !strings.HasPrefix(sql, "SELECT count(*)") {
		t.Errorf("count query should start with SELECT count(*), got:\n%s", sql)
	}
	if strings.Contains(sql, "ORDER BY") || strings.Contains(sql, "LIMIT") {
		t.Errorf("count query must not have ORDER BY or LIMIT, got:\n%s", sql)
	}
	if len(args) != 2 {
		t.Errorf("count args = %d, want 2", len(args))
	}
}
```

**Step 2: Run the test to verify it fails**

```bash
./build.sh test 2>&1 | grep -A2 buildListQuery
```

**Expected:** Compile error — `ListFilters`, `buildListQuery`, `buildCountQuery` undefined.

**Step 3: Implement the query builder**

```go
// dashboards/open-brain-dashboard-go/thoughts_query.go
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
// When hasEmbedding is true, the caller must pass the pgvector.Vector as
// args[0]; the placeholder $1 is reserved for it. Filters then take
// $2..$N, and LIMIT/OFFSET occupy the final two slots.
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
	// line up regardless of branch.
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

	// Put the embedding placeholder value placeholder in slot 0 — actual
	// value is injected by the caller in Search(). Here we only build
	// the SQL and track non-embedding args; the caller prepends the
	// vector.
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
```

Note the test's `wantArgLen` expectations: non-semantic includes the two LIMIT/OFFSET args in the count; re-read the test and verify it matches. If it doesn't, adjust either the test expectations or the builder — there's one source of truth. The test expects non-semantic `hasEmbedding=false, no filters, Page=1, PerPage=50` to produce args length 2 (the LIMIT + OFFSET pair). Ensure the builder matches.

**Also:** the semantic variant reserves `$1` for the embedding vector in the SQL. The embedding itself isn't in the args slice returned by `buildListQuery` — the caller prepends it before calling `pool.Query`. Document this contract in the comment and make sure the test's `hasEmbedding` cases pass with that understanding (`wantArgLen=3` = filter arg + limit + offset, vector not counted).

**Step 4: Run the test to verify it passes**

```bash
./build.sh test 2>&1 | tail -30
```

**Expected:** `PASS` for all subtests. If not, read the failure — likely a wantSubstr or wantArgLen off-by-one.

**Step 5: Commit**

```bash
git add dashboards/open-brain-dashboard-go/thoughts_query.go dashboards/open-brain-dashboard-go/thoughts_query_test.go
git commit -m "$(cat <<'EOF'
[dashboards] Extract unified list-query builder for v1.5

buildListQuery returns (sql, args) for the unified / handler's list
pane, combining type/topic/person/days filters with optional pgvector
semantic ranking. Table-driven test covers each filter axis plus the
semantic branch. No DB required for tests.

The embedding vector (when present) is caller-supplied at $1; the
builder reserves the placeholder but doesn't include the vector in
the returned args slice.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Unified `Search` method and `BulkDelete` (TDD + integration)

**Purpose:** Wrap `buildListQuery` in a high-level `thoughts.Search(ctx, filters, embedding)` that handles the three-path fallback (semantic → semantic zero → text fallback → ollama fail → text fallback). Also add `BulkDelete(ctx, ids)` as a single-statement delete.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/thoughts.go`
- Modify: `dashboards/open-brain-dashboard-go/thoughts_query_test.go` (add BulkDelete SQL test)

**Step 1: Write the failing test for `BulkDelete` SQL shape**

Add to `thoughts_query_test.go`:

```go
func TestBuildBulkDeleteQuery(t *testing.T) {
	sql, args := buildBulkDeleteQuery([]int64{1, 2, 3})
	if !strings.Contains(sql, "DELETE FROM thoughts") {
		t.Errorf("expected DELETE FROM thoughts, got:\n%s", sql)
	}
	if !strings.Contains(sql, "id = ANY($1)") {
		t.Errorf("expected id = ANY($1), got:\n%s", sql)
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg (the ids slice), got %d", len(args))
	}
	ids, ok := args[0].([]int64)
	if !ok || len(ids) != 3 {
		t.Errorf("expected args[0] = []int64{1,2,3}, got %v", args[0])
	}
}

func TestBuildBulkDeleteQueryEmpty(t *testing.T) {
	sql, args := buildBulkDeleteQuery(nil)
	if sql != "" || args != nil {
		t.Errorf("empty ids should return empty sql + nil args, got sql=%q args=%v", sql, args)
	}
}
```

**Step 2: Run to verify it fails**

```bash
./build.sh test
```

**Expected:** `buildBulkDeleteQuery undefined`.

**Step 3: Implement `buildBulkDeleteQuery` in `thoughts_query.go`**

```go
// buildBulkDeleteQuery returns the SQL and args for deleting a batch of
// thoughts by ID. Empty or nil ids returns empty strings so the caller
// can short-circuit without hitting the DB.
func buildBulkDeleteQuery(ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	return "DELETE FROM thoughts WHERE id = ANY($1)", []any{ids}
}
```

**Step 4: Run the tests**

```bash
./build.sh test
```

**Expected:** All pass.

**Step 5: Add `Search` and `BulkDelete` methods to `thoughts.go`**

Add to `thoughts.go`:

```go
// SearchResultMode describes which path the unified Search method took.
// Consumed by the handler to render the appropriate banner.
type SearchResultMode int

const (
	SearchModeNone     SearchResultMode = iota // no q, pure filter list
	SearchModeSemantic                         // q + semantic results found
	SearchModeFallback                         // q + semantic returned zero, fell back to ILIKE
	SearchModeTextOnly                         // q + Ollama was down entirely
)

// ListResult bundles the rows returned by Search with pagination info
// and the mode the caller used, so the handler can render a banner
// and the pagination footer without re-deriving either.
type ListResult struct {
	Filter     ListFilters
	Mode       SearchResultMode
	Results    []templates.ThoughtData
	Total      int64
	TotalPages int
}

// Search is the one list-pane query entry point for the v1.5 handler.
// It combines filters with optional semantic ranking, and automatically
// falls back to ILIKE substring search if:
//   - the caller passed a nil embedding (Ollama down), or
//   - semantic ranking returned zero results above threshold
//
// The caller is responsible for calling ollama.Embed separately and
// passing the result (nil on failure). This keeps thoughts.go free of
// HTTP concerns.
func (d *DB) Search(ctx context.Context, f ListFilters, embedding []float32) (*ListResult, error) {
	// Path 1: no query, pure filter list.
	if f.Q == "" {
		return d.listQuery(ctx, f, nil, SearchModeNone)
	}

	// Path 2: query present + embedding available. Try semantic first.
	if embedding != nil {
		result, err := d.listQuery(ctx, f, embedding, SearchModeSemantic)
		if err != nil {
			return nil, err
		}
		if result.Total > 0 {
			return result, nil
		}
		// Fall through: semantic returned zero matches above threshold.
		fallback, err := d.listQuery(ctx, f, nil, SearchModeFallback)
		if err != nil {
			return nil, err
		}
		return fallback, nil
	}

	// Path 3: query present, Ollama was down. Text-only.
	return d.listQuery(ctx, f, nil, SearchModeTextOnly)
}

// listQuery runs one pass against the database using buildListQuery and
// buildCountQuery. Pulled out of Search so the fallback paths share the
// same scan logic.
func (d *DB) listQuery(ctx context.Context, f ListFilters, embedding []float32, mode SearchResultMode) (*ListResult, error) {
	hasEmbedding := embedding != nil

	countSQL, countArgs := buildCountQuery(f, hasEmbedding)
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

	listSQL, listArgs := buildListQuery(effective, hasEmbedding)

	var finalArgs []any
	if hasEmbedding {
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
		if hasEmbedding {
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

	return &ListResult{
		Filter:     effective,
		Mode:       mode,
		Results:    results,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// BulkDelete removes a batch of thoughts by ID in a single statement.
// Returns the number of rows affected. Empty ids is a no-op.
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
```

**Step 6: Build and run tests**

```bash
./build.sh build && ./build.sh test
```

**Expected:** Clean build, all tests pass. The compiler will catch any type-mismatch between `ListResult.Filter` and the existing `templates.BrowseFilter` — you may need to import `templates` or keep `ListFilters` local depending on how the handler later consumes it.

**Step 7: Commit**

```bash
git add dashboards/open-brain-dashboard-go/thoughts.go dashboards/open-brain-dashboard-go/thoughts_query.go dashboards/open-brain-dashboard-go/thoughts_query_test.go
git commit -m "$(cat <<'EOF'
[dashboards] Unified Search with auto-fallback + BulkDelete method

Search(ctx, filters, embedding) is the single list-pane query entry
point for the v1.5 / handler. It combines filters with optional
semantic ranking via pgvector, and automatically falls back to ILIKE
substring search when the caller passed nil embedding (Ollama down)
or when semantic returned zero matches. Returns a SearchResultMode
so the handler can render the correct banner.

BulkDelete runs a single DELETE FROM thoughts WHERE id = ANY($1) and
returns rows-affected. Empty ids is a no-op, not a DB call.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Three-pane shell template + base `/` handler

**Purpose:** Stand up the CSS-grid shell with sidebar/list/detail named areas, plus a working `/` handler that renders the shell with placeholder content in each pane. Every subsequent task fills in one pane.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/templates/shell.templ`
- Modify: `dashboards/open-brain-dashboard-go/static/app.css` (add grid shell rules — old styles stay for now)
- Modify: `dashboards/open-brain-dashboard-go/server.go` (add handleShell; leave old handlers in place)

**Step 1: Write the shell template**

```go
// dashboards/open-brain-dashboard-go/templates/shell.templ
package templates

templ Shell(vm ShellViewModel) {
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8"/>
		<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
		<title>Open Brain</title>
		<link rel="icon" type="image/svg+xml" href="/static/favicon.svg"/>
		<script>try{if(localStorage.getItem('ob-theme')==='light')document.documentElement.setAttribute('data-theme','light')}catch(e){}</script>
		<link rel="stylesheet" href="/static/tokens.css"/>
		<link rel="stylesheet" href="/static/app.css"/>
		<script src="/static/vendor/htmx.min.js"></script>
		<script src="/static/js/app.js" defer></script>
	</head>
	<body>
		<div class="app">
			<nav class="sidebar" id="sidebar-pane">
				@vm.Sidebar
			</nav>
			<main class="list" id="list-pane">
				@vm.List
			</main>
			<aside class="detail" id="detail-pane">
				@vm.Detail
			</aside>
		</div>
		<div id="compose-slot"></div>
	</body>
	</html>
}

templ PlaceholderSidebar() {
	<div class="placeholder">sidebar placeholder</div>
}

templ PlaceholderList() {
	<div class="placeholder">list placeholder</div>
}

templ PlaceholderDetail() {
	<div class="placeholder">detail placeholder — no thought selected</div>
}
```

**Step 2: Add `ShellViewModel` to `templates/types.go`**

```go
// Append to templates/types.go
type ShellViewModel struct {
	Sidebar templ.Component
	List    templ.Component
	Detail  templ.Component
}
```

You'll also need `import "github.com/a-h/templ"` in types.go.

**Step 3: Add grid-shell CSS to `app.css`**

Append (don't delete anything yet — old v1.1 styles stay until Task 14):

```css
/* v1.5 grid shell. Desktop-first, mobile-viable via a single media
   query below. CSS grid with named areas sized in fr + min-content
   so the phone fallback is a one-column override. */
.app {
	display: grid;
	grid-template-columns: min-content 1fr min-content;
	grid-template-areas: "sidebar list detail";
	min-height: 100vh;
	background: var(--bg);
	color: var(--fg);
	font-family: var(--font-sans);
}

.sidebar {
	grid-area: sidebar;
	width: 18rem;
	border-right: 1px solid var(--border);
	background: var(--bg-elevated);
	padding: var(--space-4);
	overflow-y: auto;
}

.list {
	grid-area: list;
	min-width: 26rem;
	border-right: 1px solid var(--border);
	overflow-y: auto;
}

.detail {
	grid-area: detail;
	width: 32rem;
	background: var(--bg-elevated);
	padding: var(--space-5);
	overflow-y: auto;
}

.placeholder {
	padding: var(--space-5);
	color: var(--fg-muted);
	font-style: italic;
}

/* Single-column fallback for small screens. Phone variant refinement
   is deferred but this gives mobile a usable default. */
@media (max-width: 860px) {
	.app {
		grid-template-columns: 1fr;
		grid-template-areas: "list";
	}
	.sidebar,
	.detail {
		display: none;
	}
}
```

**Step 4: Add the shell handler to `server.go`**

Find where routes are declared and add a route **before** the existing `/` handler (chi takes the first match for duplicated paths, so insertion order matters — or use a version flag, but first-match is simpler for incremental cutover).

For the cutover: temporarily mount the new shell at `/v15` so the v1.1 home page keeps working. Once the shell is fleshed out, Task 14 will flip `/v15` to `/` and redirect the old routes. For now:

```go
// In the route registration block in server.go
r.Get("/v15", s.handleShell)
```

And add the handler:

```go
// handleShell is the v1.5 master/detail route. During cutover it lives
// at /v15; Task 14 flips it to / and redirects the v1.1 routes.
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	vm := templates.ShellViewModel{
		Sidebar: templates.PlaceholderSidebar(),
		List:    templates.PlaceholderList(),
		Detail:  templates.PlaceholderDetail(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Shell(vm).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

**Step 5: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `http://127.0.0.1:8080/v15` in a browser. Expect to see three placeholder strings in a three-column layout. The v1.1 home page at `/` still works.

Toggle the theme manually from DevTools: `localStorage.setItem('ob-theme', 'light'); location.reload()`. Expect light palette. Reset: `localStorage.setItem('ob-theme', 'dark'); location.reload()`.

Stop the server.

**Step 6: Commit**

```bash
git add dashboards/open-brain-dashboard-go/templates/shell.templ dashboards/open-brain-dashboard-go/templates/types.go dashboards/open-brain-dashboard-go/static/app.css dashboards/open-brain-dashboard-go/server.go
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 shell template + /v15 handler

Three-pane CSS grid shell with named areas (sidebar/list/detail),
sized in fr and min-content so the single-column phone media query
is a later 40-line override. Mounted at /v15 during cutover so the
v1.1 home page keeps working while later tasks fill in each pane.

Theme toggle scaffolding reads localStorage in a tiny head script
and applies data-theme="light" before htmx/app.js load, so the
initial paint matches the saved preference.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Filter sidebar — type chips, time windows, topics, people, footer

**Purpose:** Build the left rail. Reads top topics and top people from the DB, renders chips with counts, wires each interaction as `hx-get` targeting `#list-pane` with `hx-push-url="true"`.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/templates/sidebar.templ`
- Modify: `dashboards/open-brain-dashboard-go/thoughts.go` (add `SidebarCounts` query)
- Modify: `dashboards/open-brain-dashboard-go/server.go` (wire sidebar into `handleShell`)

**Step 1: Add `SidebarCounts` method to `thoughts.go`**

```go
// SidebarCounts is the one query set that powers the filter rail: total,
// week count, by-type counts, top-10 topics, top-10 people. Each query
// is scoped to the current filters so counts reflect what's actually
// selectable next, not the global corpus.
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
```

**Step 2: Add `PersonCount` and `SidebarData` to `templates/types.go`**

```go
type PersonCount struct {
	Person string
	Count  int64
}

// SidebarData is the view-model for sidebar.templ. It bundles the
// counts from the DB with the currently-active filter so the template
// can highlight the active chip.
type SidebarData struct {
	Total     int64
	WeekCount int64
	Types     []TypeCount
	Topics    []TopicCount
	People    []PersonCount
	Active    ListFilters
}
```

You may need to move `ListFilters` from `package main` to `package templates`, or create a duplicate for the template's view-model. Cleanest approach: move it. Then `thoughts_query.go` imports it from `templates`.

Move: cut `ListFilters` from `thoughts_query.go` and paste into `templates/types.go`, add the `package templates` and `ListFilters` type with the same fields. Update all references.

**Step 3: Write the sidebar template**

```go
// dashboards/open-brain-dashboard-go/templates/sidebar.templ
package templates

import "fmt"

templ Sidebar(sd SidebarData) {
	<div class="sidebar-inner">
		<button class="btn-new"
			hx-get="/partials/compose"
			hx-target="#compose-slot"
			hx-swap="innerHTML"
		>
			+ New thought
		</button>

		<section class="filter-section">
			<h3 class="filter-heading">Type</h3>
			<ul class="chip-list">
				@typeChip("", "All", totalCount(sd.Types), sd.Active)
				for _, tc := range sd.Types {
					@typeChip(tc.Type, tc.Type, tc.Count, sd.Active)
				}
			</ul>
		</section>

		<section class="filter-section">
			<h3 class="filter-heading">Time</h3>
			<ul class="chip-list chip-list--row">
				@daysChip(0, "All", sd.Active)
				@daysChip(7, "7d", sd.Active)
				@daysChip(30, "30d", sd.Active)
				@daysChip(90, "90d", sd.Active)
			</ul>
		</section>

		if len(sd.Topics) > 0 {
			<section class="filter-section">
				<h3 class="filter-heading">Topics</h3>
				<ul class="filter-list">
					for _, tc := range sd.Topics {
						@topicRow(tc.Topic, tc.Count, sd.Active)
					}
				</ul>
			</section>
		}

		if len(sd.People) > 0 {
			<section class="filter-section">
				<h3 class="filter-heading">People</h3>
				<ul class="filter-list">
					for _, pc := range sd.People {
						@personRow(pc.Person, pc.Count, sd.Active)
					}
				</ul>
			</section>
		}

		<footer class="sidebar-footer">
			<div class="stats-line">
				{ fmt.Sprintf("%d thoughts · %d this week", sd.Total, sd.WeekCount) }
			</div>
			<div class="footer-actions">
				<button class="btn-icon" onclick="obToggleTheme()" title="Toggle theme">☾</button>
				<button class="btn-icon" title="Settings (coming soon)" disabled>⚙</button>
			</div>
		</footer>
	</div>
}

templ typeChip(value, label string, count int64, active ListFilters) {
	<li>
		if active.Type == value {
			<a class="chip chip--active"
				hx-get={ "/?" + filtersToURL(clearType(active)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				{ label } <span class="chip-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		} else {
			<a class="chip"
				hx-get={ "/?" + filtersToURL(withType(active, value)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				{ label } <span class="chip-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		}
	</li>
}

templ daysChip(days int, label string, active ListFilters) {
	<li>
		if active.Days == days {
			<a class="chip chip--active"
				hx-get={ "/?" + filtersToURL(withDays(active, 0)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				{ label }
			</a>
		} else {
			<a class="chip"
				hx-get={ "/?" + filtersToURL(withDays(active, days)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				{ label }
			</a>
		}
	</li>
}

templ topicRow(topic string, count int64, active ListFilters) {
	<li>
		if active.Topic == topic {
			<a class="filter-row filter-row--active"
				hx-get={ "/?" + filtersToURL(withTopic(active, "")) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				<span class="filter-label">{ topic }</span>
				<span class="filter-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		} else {
			<a class="filter-row"
				hx-get={ "/?" + filtersToURL(withTopic(active, topic)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				<span class="filter-label">{ topic }</span>
				<span class="filter-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		}
	</li>
}

templ personRow(person string, count int64, active ListFilters) {
	<li>
		if active.Person == person {
			<a class="filter-row filter-row--active"
				hx-get={ "/?" + filtersToURL(withPerson(active, "")) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				<span class="filter-label">{ person }</span>
				<span class="filter-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		} else {
			<a class="filter-row"
				hx-get={ "/?" + filtersToURL(withPerson(active, person)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>
				<span class="filter-label">{ person }</span>
				<span class="filter-count">{ fmt.Sprintf("%d", count) }</span>
			</a>
		}
	</li>
}
```

**Step 4: Add the filter URL helpers to `templates/helpers.go`**

```go
func totalCount(types []TypeCount) int64 {
	var total int64
	for _, t := range types {
		total += t.Count
	}
	return total
}

func clearType(f ListFilters) ListFilters { f.Type = ""; return f }
func withType(f ListFilters, t string) ListFilters { f.Type = t; return f }
func withDays(f ListFilters, d int) ListFilters { f.Days = d; return f }
func withTopic(f ListFilters, t string) ListFilters { f.Topic = t; return f }
func withPerson(f ListFilters, p string) ListFilters { f.Person = p; return f }

// filtersToURL renders a ListFilters as a URL query string, skipping
// zero values. Used by the sidebar chips and the list-pane pagination
// to construct navigation URLs that preserve sibling filters.
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
```

Add `import "net/url"` and `import "fmt"` if not already present.

**Step 5: Wire the sidebar into `handleShell`**

Update `handleShell` in `server.go`:

```go
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters from query params.
	filters := parseListFilters(r)

	sidebarCounts, err := s.db.SidebarCounts(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sidebarData := templates.SidebarData{
		Total:     sidebarCounts.Total,
		WeekCount: sidebarCounts.WeekCount,
		Types:     sidebarCounts.Types,
		Topics:    sidebarCounts.Topics,
		People:    sidebarCounts.People,
		Active:    filters,
	}

	vm := templates.ShellViewModel{
		Sidebar: templates.Sidebar(sidebarData),
		List:    templates.PlaceholderList(),
		Detail:  templates.PlaceholderDetail(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Shell(vm).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// parseListFilters reads the query-string filters into a ListFilters
// struct. Invalid ints silently become 0; invalid page becomes 1.
func parseListFilters(r *http.Request) templates.ListFilters {
	q := r.URL.Query()
	f := templates.ListFilters{
		Type:    q.Get("type"),
		Topic:   q.Get("topic"),
		Person:  q.Get("person"),
		Q:       q.Get("q"),
		PerPage: 50,
	}
	if d, err := strconv.Atoi(q.Get("days")); err == nil && d > 0 {
		f.Days = d
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		f.Page = p
	} else {
		f.Page = 1
	}
	return f
}
```

Add `import "strconv"` if needed.

**Step 6: Add sidebar CSS to `app.css`**

Append:

```css
.sidebar-inner { display: flex; flex-direction: column; gap: var(--space-4); height: 100%; }
.btn-new {
	background: var(--accent);
	color: var(--bg);
	border: none;
	border-radius: var(--radius);
	padding: var(--space-3) var(--space-4);
	font-family: var(--font-sans);
	font-size: 0.95rem;
	font-weight: 600;
	cursor: pointer;
}
.btn-new:hover { filter: brightness(1.1); }

.filter-section { border-top: 1px solid var(--border-muted); padding-top: var(--space-3); }
.filter-section:first-of-type { border-top: none; padding-top: 0; }
.filter-heading { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fg-subtle); margin: 0 0 var(--space-2); font-weight: 600; }

.chip-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: var(--space-1); }
.chip-list--row { flex-direction: row; flex-wrap: wrap; }
.chip {
	display: inline-flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-1) var(--space-3);
	background: var(--bg-raised);
	border: 1px solid var(--border-muted);
	border-radius: var(--radius);
	color: var(--fg);
	text-decoration: none;
	font-size: 0.875rem;
	cursor: pointer;
}
.chip:hover { background: var(--bg); border-color: var(--border); }
.chip--active { background: var(--accent-dim); border-color: var(--accent); color: var(--fg); }
.chip-count { color: var(--fg-subtle); font-variant-numeric: tabular-nums; font-size: 0.75rem; }

.filter-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; }
.filter-row {
	display: flex;
	justify-content: space-between;
	padding: var(--space-1) var(--space-2);
	color: var(--fg-muted);
	text-decoration: none;
	font-size: 0.875rem;
	cursor: pointer;
	border-radius: var(--radius);
}
.filter-row:hover { background: var(--bg-raised); color: var(--fg); }
.filter-row--active { background: var(--accent-dim); color: var(--fg); }
.filter-count { color: var(--fg-subtle); font-variant-numeric: tabular-nums; font-size: 0.75rem; }

.sidebar-footer { margin-top: auto; padding-top: var(--space-4); border-top: 1px solid var(--border-muted); }
.stats-line { font-size: 0.75rem; color: var(--fg-subtle); margin-bottom: var(--space-2); }
.footer-actions { display: flex; gap: var(--space-2); }
.btn-icon {
	background: transparent;
	border: 1px solid var(--border-muted);
	color: var(--fg-muted);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius);
	cursor: pointer;
}
.btn-icon:hover:not(:disabled) { color: var(--fg); border-color: var(--border); }
.btn-icon:disabled { opacity: 0.5; cursor: not-allowed; }
```

**Step 7: Build, run, smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `http://127.0.0.1:8080/v15`. Expect the sidebar to render with real type/topic/people counts from the live brain. Clicking a chip should push the URL but the list pane stays "list placeholder" — that's expected until Task 7. The URL should reflect the clicked filter.

Stop the server.

**Step 8: Commit**

```bash
git add dashboards/open-brain-dashboard-go/templates/sidebar.templ dashboards/open-brain-dashboard-go/templates/helpers.go dashboards/open-brain-dashboard-go/templates/types.go dashboards/open-brain-dashboard-go/thoughts.go dashboards/open-brain-dashboard-go/thoughts_query.go dashboards/open-brain-dashboard-go/server.go dashboards/open-brain-dashboard-go/static/app.css
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 sidebar filter rail

Three filter sections (type chips, time window, topic/person lists)
plus a sidebar footer with total/week stats and theme toggle. All
interactions hx-get to /?... targeting #list-pane with hx-push-url.

SidebarCounts query returns total, week count, by-type counts, top-10
topics, top-10 people in a single method call. ListFilters moved to
package templates so the view-model can import it.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: List pane — toolbar, rows, pagination, wired to unified Search

**Purpose:** Render the middle pane: the toolbar (normal state only — bulk + confirm land in Task 12), dense two-line rows, numbered pagination, empty state. Wire `handleShell` to call `db.Search` and pass results into the list template.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/templates/list.templ`
- Modify: `dashboards/open-brain-dashboard-go/server.go` — add `GET /partials/list` handler and flesh out `handleShell` to populate the list pane
- Modify: `dashboards/open-brain-dashboard-go/templates/helpers.go` — add list-url helper
- Modify: `dashboards/open-brain-dashboard-go/static/app.css` — list styles

**Step 1: Write `list.templ`**

```go
// dashboards/open-brain-dashboard-go/templates/list.templ
package templates

import "fmt"

// ListData is the view-model for the list pane. SelectedID is the
// currently-open thought's id (or 0 if none) so rows can render the
// .selected class.
type ListData struct {
	Result     *ListResult
	SelectedID int64
}

templ List(ld ListData) {
	<div class="list-inner">
		<div class="list-toolbar">
			<form class="search-form"
				hx-get="/partials/list"
				hx-target="#list-pane"
				hx-push-url="true"
				hx-trigger="input changed delay:300ms, keyup[key=='Enter'], search"
				hx-include="[name=type],[name=topic],[name=person],[name=days]"
			>
				<input type="search" name="q" class="search-input"
					value={ ld.Result.Filter.Q }
					placeholder="Search…"
					autocomplete="off"
				/>
				@hiddenFilters(ld.Result.Filter)
			</form>
			<div class="list-count">
				{ fmt.Sprintf("%d thoughts", ld.Result.Total) }
				if ld.Result.Filter.Q != "" {
					{ fmt.Sprintf(" for %q", ld.Result.Filter.Q) }
				}
			</div>
		</div>

		@searchBanner(ld.Result.Mode)

		if len(ld.Result.Results) == 0 {
			<div class="empty-state">
				<p>No thoughts match these filters.</p>
				<a class="link" hx-get="/" hx-target="#list-pane" hx-push-url="true">Clear filters</a>
			</div>
		} else {
			<ul class="list-rows">
				for _, t := range ld.Result.Results {
					@listRow(t, ld.SelectedID, ld.Result.Filter)
				}
			</ul>
			if ld.Result.TotalPages > 1 {
				@pagination(ld.Result)
			}
		}
	</div>
}

templ hiddenFilters(f ListFilters) {
	if f.Type != "" {
		<input type="hidden" name="type" value={ f.Type }/>
	}
	if f.Topic != "" {
		<input type="hidden" name="topic" value={ f.Topic }/>
	}
	if f.Person != "" {
		<input type="hidden" name="person" value={ f.Person }/>
	}
	if f.Days > 0 {
		<input type="hidden" name="days" value={ fmt.Sprintf("%d", f.Days) }/>
	}
}

templ searchBanner(mode SearchResultMode) {
	switch mode {
	case SearchModeFallback:
		<div class="search-banner">no semantic matches — showing text matches</div>
	case SearchModeTextOnly:
		<div class="search-banner">text-only — embeddings unavailable</div>
	}
}

templ listRow(t ThoughtData, selectedID int64, current ListFilters) {
	<li>
		<a class={ rowClass(t, selectedID) }
			hx-get={ "/partials/detail/" + fmt.Sprintf("%d", t.ID) + "?" + filtersToURL(current) }
			hx-target="#detail-pane"
			hx-push-url="true"
		>
			<div class="row-line1">
				<span class={ "type-chip", typeClass(t.Type) }>{ t.Type }</span>
				<span class="row-time">{ HumanTime(t.CreatedAt) }</span>
				<span class="row-id">{ fmt.Sprintf("%d", t.ID) }</span>
			</div>
			<div class="row-line2">
				{ Truncate(t.Content, 120) }
			</div>
		</a>
	</li>
}

templ pagination(r *ListResult) {
	<nav class="pagination">
		if r.Filter.Page > 1 {
			<a hx-get={ "/partials/list?" + filtersToURL(pageFilter(r.Filter, r.Filter.Page-1)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>« Prev</a>
		} else {
			<span class="disabled">« Prev</span>
		}
		<span class="page-indicator">{ fmt.Sprintf("Page %d of %d", r.Filter.Page, r.TotalPages) }</span>
		if r.Filter.Page < r.TotalPages {
			<a hx-get={ "/partials/list?" + filtersToURL(pageFilter(r.Filter, r.Filter.Page+1)) }
				hx-target="#list-pane"
				hx-push-url="true"
			>Next »</a>
		} else {
			<span class="disabled">Next »</span>
		}
	</nav>
}
```

**Step 2: Add `rowClass` and `pageFilter` helpers to `templates/helpers.go`**

```go
func rowClass(t ThoughtData, selectedID int64) string {
	if t.ID == selectedID {
		return "list-row list-row--selected"
	}
	return "list-row"
}

func pageFilter(f ListFilters, page int) ListFilters {
	f.Page = page
	return f
}
```

**Step 3: Add list CSS**

Append to `app.css`:

```css
.list-inner { display: flex; flex-direction: column; height: 100%; }
.list-toolbar {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--border);
	background: var(--bg-elevated);
}
.search-form { flex: 1; }
.search-input {
	width: 100%;
	padding: var(--space-2) var(--space-3);
	background: var(--bg-raised);
	border: 1px solid var(--border);
	border-radius: var(--radius);
	color: var(--fg);
	font-family: var(--font-sans);
	font-size: 0.9rem;
}
.search-input:focus { outline: none; border-color: var(--accent); }
.list-count { color: var(--fg-subtle); font-size: 0.8rem; white-space: nowrap; }

.search-banner {
	padding: var(--space-2) var(--space-4);
	background: var(--bg-raised);
	border-bottom: 1px solid var(--border-muted);
	color: var(--fg-muted);
	font-size: 0.8rem;
}

.list-rows { list-style: none; padding: 0; margin: 0; overflow-y: auto; flex: 1; }
.list-row {
	display: block;
	padding: var(--space-3) var(--space-4);
	border-bottom: 1px solid var(--border-muted);
	text-decoration: none;
	color: var(--fg);
	cursor: pointer;
	border-left: 3px solid transparent;
}
.list-row:hover { background: var(--bg-raised); }
.list-row--selected {
	background: var(--accent-dim);
	border-left-color: var(--accent);
}
.row-line1 {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin-bottom: var(--space-1);
}
.type-chip {
	display: inline-flex;
	padding: 0 var(--space-2);
	border-radius: 9999px;
	font-size: 0.7rem;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.05em;
}
.type-chip--observation { background: #4c4fcc33; color: #8a8cfc; }
.type-chip--task { background: #d2992233; color: #e8b74d; }
.type-chip--idea { background: #1f929433; color: #52c4c5; }
.type-chip--reference { background: #8957e533; color: #b48ef0; }
.type-chip--person_note { background: #d83d7a33; color: #e874a6; }
.type-chip--unknown { background: var(--bg-raised); color: var(--fg-subtle); }
.row-time { font-size: 0.75rem; color: var(--fg-muted); }
.row-id { margin-left: auto; font-size: 0.75rem; color: var(--fg-subtle); font-family: var(--font-mono); }
.row-line2 {
	color: var(--fg-muted);
	font-size: 0.875rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.pagination {
	display: flex;
	justify-content: center;
	align-items: center;
	gap: var(--space-4);
	padding: var(--space-3);
	border-top: 1px solid var(--border-muted);
}
.pagination a {
	color: var(--accent);
	text-decoration: none;
	cursor: pointer;
}
.pagination a:hover { text-decoration: underline; }
.pagination .disabled { color: var(--fg-subtle); }
.page-indicator { color: var(--fg-muted); font-size: 0.85rem; }

.empty-state { padding: var(--space-6); text-align: center; color: var(--fg-muted); }
.link { color: var(--accent); cursor: pointer; text-decoration: underline; }
```

**Step 4: Check `TypeClass` helper exists and matches**

`templates/helpers.go` already has `TypeClass` from v1.1. Verify it returns `"type-chip--observation"` style and matches the CSS class names above. If it returns `"type-observation"` style, either update the helper or update the CSS to match. Do NOT have two inconsistent conventions.

Example if the helper needs updating:

```go
func typeClass(t string) string {
	switch t {
	case "observation":
		return "type-chip--observation"
	case "task":
		return "type-chip--task"
	case "idea":
		return "type-chip--idea"
	case "reference":
		return "type-chip--reference"
	case "person_note":
		return "type-chip--person_note"
	default:
		return "type-chip--unknown"
	}
}
```

Note: the templ class binding I used in `listRow` uses `class={ "type-chip", typeClass(t.Type) }` which emits both classes. Confirm that's the right templ syntax for your version — if not, change to `class={ "type-chip " + typeClass(t.Type) }`.

**Step 5: Add the `/partials/list` handler and wire `handleShell`**

In `server.go`:

```go
func (s *Server) handlePartialList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filters := parseListFilters(r)

	var embedding []float32
	if filters.Q != "" {
		// Try to embed; nil on failure triggers text-only fallback.
		emb, err := s.ollama.Embed(ctx, filters.Q)
		if err == nil {
			embedding = emb
		}
	}

	result, err := s.db.Search(ctx, filters, embedding)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	selectedID := int64(0)
	if idStr := r.URL.Query().Get("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			selectedID = id
		}
	}

	vm := templates.ListData{
		Result:     result,
		SelectedID: selectedID,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.List(vm).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

And update `handleShell` to populate the list pane (replacing the `PlaceholderList`):

```go
// In handleShell, after building sidebarData:
filters := parseListFilters(r)

var embedding []float32
if filters.Q != "" {
	if emb, err := s.ollama.Embed(ctx, filters.Q); err == nil {
		embedding = emb
	}
}

result, err := s.db.Search(ctx, filters, embedding)
if err != nil {
	http.Error(w, err.Error(), http.StatusInternalServerError)
	return
}

selectedID := int64(0)
if idStr := r.URL.Query().Get("id"); idStr != "" {
	if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		selectedID = id
	}
}

vm := templates.ShellViewModel{
	Sidebar: templates.Sidebar(sidebarData),
	List:    templates.List(templates.ListData{Result: result, SelectedID: selectedID}),
	Detail:  templates.PlaceholderDetail(),
}
```

And register the route:

```go
r.Get("/partials/list", s.handlePartialList)
```

**Step 6: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `http://127.0.0.1:8080/v15`. Expect a dense list of recent 50 thoughts. Click a type chip in the sidebar — list pane re-renders with the filter, URL updates. Type in the search box — semantic re-ranking kicks in after 300ms (assuming Ollama is up). Click a topic chip — list filters accordingly. Clicking a row doesn't do anything yet (that's Task 8).

Stop the server.

**Step 7: Commit**

```bash
git add dashboards/open-brain-dashboard-go/templates/list.templ dashboards/open-brain-dashboard-go/templates/helpers.go dashboards/open-brain-dashboard-go/server.go dashboards/open-brain-dashboard-go/static/app.css
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 list pane with unified search and pagination

Dense two-line rows, numbered pagination, in-toolbar search input
with 300ms debounce and hx-include to preserve sibling filters.
Semantic search hits the unified Search method with auto-fallback
banner rendering for the text-only and zero-semantic-matches paths.

Row click targets #detail-pane but detail is still placeholder —
Task 8 fills it in.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Detail pane — read state

**Purpose:** Render the right-pane read view when `?id=N` is in the URL. Reuse v1.1's content rendering (full content, topic/person chips, action items, raw metadata disclosure) inside a new `DetailRead` templ component. Wire row click to hx-get into `#detail-pane`.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/templates/detail_pane.templ`
- Modify: `dashboards/open-brain-dashboard-go/server.go` — add `GET /partials/detail/{id}`, wire `handleShell` to render detail when `?id=N`
- Modify: `dashboards/open-brain-dashboard-go/static/app.css` — detail pane styles

**Step 1: Write `detail_pane.templ` with `DetailRead` + `DetailEmpty`**

```go
// dashboards/open-brain-dashboard-go/templates/detail_pane.templ
package templates

import "fmt"

templ DetailEmpty() {
	<div class="detail-empty">
		<p>Select a thought or create a new one.</p>
		<button class="btn-secondary"
			hx-get="/partials/compose"
			hx-target="#compose-slot"
		>+ New thought</button>
	</div>
}

templ DetailRead(d *ThoughtDetail, current ListFilters) {
	<div class="detail-inner">
		<div class="detail-toolbar">
			<a class="btn-primary"
				hx-get={ fmt.Sprintf("/partials/detail/%d/edit?%s", d.ID, filtersToURL(current)) }
				hx-target="#detail-pane"
				hx-push-url="true"
			>Edit</a>
			<form class="inline-form"
				hx-post={ fmt.Sprintf("/thought/%d/delete", d.ID) }
				hx-target="#detail-pane"
				hx-confirm="Delete this thought?"
			>
				<button class="btn-danger" type="submit">Delete</button>
			</form>
		</div>

		<header class="detail-header">
			<span class={ "type-chip", typeClass(d.Type) }>{ d.Type }</span>
			<span class="detail-time">captured { HumanTime(d.CreatedAt) }</span>
			if d.UpdatedAt != nil {
				<span class="detail-edited">edited { HumanTime(*d.UpdatedAt) }</span>
			}
		</header>

		<div class="detail-content">{ d.Content }</div>

		if len(d.Topics) > 0 {
			<div class="detail-row">
				<span class="detail-label">Topics:</span>
				for _, topic := range d.Topics {
					<a class="inline-tag"
						hx-get={ "/?topic=" + topic }
						hx-target="#list-pane"
						hx-push-url="true"
					>{ topic }</a>
				}
			</div>
		}

		if len(d.People) > 0 {
			<div class="detail-row">
				<span class="detail-label">People:</span>
				for _, person := range d.People {
					<a class="inline-tag"
						hx-get={ "/?person=" + person }
						hx-target="#list-pane"
						hx-push-url="true"
					>@{ person }</a>
				}
			</div>
		}

		if len(d.ActionItems) > 0 {
			<div class="detail-section">
				<h4 class="detail-section-heading">Action items</h4>
				<ul class="action-items-read">
					for _, ai := range d.ActionItems {
						<li>
							<span class={ "priority-chip", "priority-" + ai.Priority }>{ ai.Priority }</span>
							<span>{ ai.Description }</span>
						</li>
					}
				</ul>
			</div>
		}

		if len(d.DatesMentioned) > 0 {
			<div class="detail-row">
				<span class="detail-label">Dates mentioned:</span>
				for _, dt := range d.DatesMentioned {
					<span class="inline-tag">{ dt }</span>
				}
			</div>
		}

		<details class="detail-raw">
			<summary>Raw metadata</summary>
			<pre>{ string(d.RawMetadata) }</pre>
		</details>
	</div>
}
```

**Step 2: Add detail CSS**

Append:

```css
.detail-inner { display: flex; flex-direction: column; gap: var(--space-4); }
.detail-toolbar { display: flex; gap: var(--space-2); padding-bottom: var(--space-3); border-bottom: 1px solid var(--border-muted); }
.detail-empty { color: var(--fg-muted); text-align: center; padding: var(--space-8); }
.detail-empty p { margin-bottom: var(--space-4); }

.btn-primary { background: var(--accent); color: var(--bg); padding: var(--space-2) var(--space-4); border-radius: var(--radius); border: none; text-decoration: none; cursor: pointer; font-weight: 600; }
.btn-primary:hover { filter: brightness(1.1); }
.btn-secondary { background: var(--bg-raised); color: var(--fg); padding: var(--space-2) var(--space-4); border-radius: var(--radius); border: 1px solid var(--border); cursor: pointer; }
.btn-danger { background: var(--danger); color: white; padding: var(--space-2) var(--space-4); border-radius: var(--radius); border: none; cursor: pointer; }
.inline-form { display: inline; }

.detail-header { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; }
.detail-time, .detail-edited { color: var(--fg-muted); font-size: 0.8rem; }
.detail-edited { color: var(--warning); }

.detail-content { white-space: pre-wrap; line-height: 1.5; max-width: 72ch; color: var(--fg); }

.detail-row { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
.detail-label { color: var(--fg-subtle); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }

.inline-tag {
	background: var(--bg-raised);
	border: 1px solid var(--border-muted);
	padding: 0 var(--space-2);
	border-radius: var(--radius);
	font-size: 0.75rem;
	color: var(--fg);
	text-decoration: none;
	cursor: pointer;
}
.inline-tag:hover { background: var(--bg); }

.detail-section { padding-top: var(--space-3); border-top: 1px solid var(--border-muted); }
.detail-section-heading { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fg-subtle); margin: 0 0 var(--space-2); }
.action-items-read { list-style: none; padding: 0; display: flex; flex-direction: column; gap: var(--space-2); }
.action-items-read li { display: flex; gap: var(--space-2); align-items: center; }

.priority-chip { display: inline-block; padding: 0 var(--space-2); border-radius: var(--radius); font-size: 0.7rem; text-transform: uppercase; font-weight: 600; }
.priority-high { background: #f8514933; color: var(--danger); }
.priority-medium, .priority-med { background: #d2992233; color: var(--warning); }
.priority-low { background: #3fb95033; color: var(--success); }

.detail-raw { margin-top: var(--space-4); border-top: 1px solid var(--border-muted); padding-top: var(--space-3); }
.detail-raw summary { cursor: pointer; color: var(--fg-muted); font-size: 0.8rem; }
.detail-raw pre { background: var(--bg-raised); padding: var(--space-3); border-radius: var(--radius); font-size: 0.75rem; color: var(--fg-muted); overflow-x: auto; }
```

**Step 3: Add `handlePartialDetail` handler**

```go
// In server.go
func (s *Server) handlePartialDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	detail, err := s.db.ThoughtByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	filters := parseListFilters(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.DetailRead(detail, filters).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Register:

```go
r.Get("/partials/detail/{id}", s.handlePartialDetail)
```

Add imports: `errors`, `github.com/jackc/pgx/v5`, and ensure `github.com/go-chi/chi/v5` is already imported.

**Step 4: Update `handleShell` to render detail when `?id=N`**

```go
// After computing `result` but before building vm:
var detailComponent templ.Component = templates.DetailEmpty()
if selectedID > 0 {
	detail, err := s.db.ThoughtByID(ctx, selectedID)
	if err == nil {
		detailComponent = templates.DetailRead(detail, filters)
	}
	// On err, fall through to DetailEmpty rather than 500.
}

vm := templates.ShellViewModel{
	Sidebar: templates.Sidebar(sidebarData),
	List:    templates.List(templates.ListData{Result: result, SelectedID: selectedID}),
	Detail:  detailComponent,
}
```

**Step 5: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

`http://127.0.0.1:8080/v15` — click a list row. Detail pane should render the read view, the clicked row gets a selected highlight, and the URL should update to `/v15?id=142` (or whatever). Click another row — detail updates in place. Click a topic chip in the detail pane — list pane filters. Hit browser Back — returns to the previous state.

Direct navigation: open `http://127.0.0.1:8080/v15?id=142` directly — expect the shell to render with the detail already populated.

Stop the server.

**Step 6: Commit**

```bash
git add dashboards/open-brain-dashboard-go/templates/detail_pane.templ dashboards/open-brain-dashboard-go/server.go dashboards/open-brain-dashboard-go/static/app.css
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 detail pane read state + row click wiring

DetailRead renders a selected thought with type chip, timestamps,
content, topic/person tags, action items (read-only), dates, raw
metadata disclosure, and an edit/delete toolbar. DetailEmpty is the
placeholder for the no-id case.

Row click in the list pane hx-gets /partials/detail/{id} into
#detail-pane with hx-push-url, and the shell handler renders the
detail state directly when the incoming URL has ?id=N.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Detail pane — edit state + save + row refresh

**Purpose:** Add `DetailEdit` with the edit form (content, type, topics, people, action items), the partial route that serves it, the POST handler that saves and emits `HX-Trigger: refresh-row-{id}`, and the `GET /partials/row/{id}` endpoint that the trigger re-fetches.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/templates/detail_pane.templ` — add `DetailEdit` + `ActionItemRow`
- Modify: `dashboards/open-brain-dashboard-go/server.go` — `handlePartialDetailEdit`, update `handleUpdate`, add `handlePartialRow`
- Modify: `dashboards/open-brain-dashboard-go/static/js/app.js` — action item add/remove handlers
- Modify: `dashboards/open-brain-dashboard-go/static/app.css` — edit form styles

**Step 1: Add `DetailEdit` to `detail_pane.templ`**

Copy v1.1's edit form content from the existing `detail_edit.templ` (do not delete it yet). Adapt the form's wrapping to post to `/thought/{id}/edit` and use htmx targeting:

```go
templ DetailEdit(d *ThoughtDetail, current ListFilters, formErr string) {
	<div class="detail-inner">
		<div class="detail-toolbar">
			<h3 class="detail-heading">Edit thought {  fmt.Sprintf("#%d", d.ID) }</h3>
			<div class="toolbar-spacer"></div>
			<a class="btn-secondary"
				hx-get={ fmt.Sprintf("/partials/detail/%d?%s", d.ID, filtersToURL(current)) }
				hx-target="#detail-pane"
				hx-push-url="true"
			>Cancel</a>
		</div>

		if formErr != "" {
			<div class="form-error">{ formErr }</div>
		}

		<form class="edit-form"
			hx-post={ fmt.Sprintf("/thought/%d/edit", d.ID) }
			hx-target="#detail-pane"
			hx-swap="innerHTML"
		>
			<input type="hidden" name="return_filters" value={ filtersToURL(current) }/>

			<label class="field-label">Content</label>
			<textarea name="content" class="field-textarea" rows="8">{ d.Content }</textarea>

			<label class="field-label">Type</label>
			<select name="type" class="field-select">
				@typeOption("observation", d.Type)
				@typeOption("task", d.Type)
				@typeOption("idea", d.Type)
				@typeOption("reference", d.Type)
				@typeOption("person_note", d.Type)
			</select>

			<label class="field-label">Topics (comma-separated)</label>
			<input type="text" name="topics" class="field-input"
				value={ joinCSV(d.Topics) }/>

			<label class="field-label">People (comma-separated)</label>
			<input type="text" name="people" class="field-input"
				value={ joinCSV(d.People) }/>

			<label class="field-label">Action items</label>
			<ul class="action-items-edit" id="action-items-list">
				for _, ai := range d.ActionItems {
					@ActionItemRow(ai)
				}
			</ul>
			<button type="button" class="btn-secondary"
				hx-get="/partials/action-item-row"
				hx-target="#action-items-list"
				hx-swap="beforeend"
			>+ Add action item</button>

			<div class="form-actions">
				<button type="submit" class="btn-primary">Save</button>
			</div>
		</form>
	</div>
}

templ ActionItemRow(ai ActionItem) {
	<li class="action-item-row">
		<input type="text" name="action_item_description[]" class="field-input"
			value={ ai.Description } placeholder="description"/>
		<select name="action_item_priority[]" class="field-select-sm">
			@priorityOption("high", ai.Priority)
			@priorityOption("medium", ai.Priority)
			@priorityOption("low", ai.Priority)
		</select>
		<button type="button" class="btn-icon" onclick="this.closest('.action-item-row').remove()">✕</button>
	</li>
}

templ typeOption(value, current string) {
	if value == current {
		<option value={ value } selected>{ value }</option>
	} else {
		<option value={ value }>{ value }</option>
	}
}

templ priorityOption(value, current string) {
	if value == current {
		<option value={ value } selected>{ value }</option>
	} else {
		<option value={ value }>{ value }</option>
	}
}
```

Add `joinCSV` helper if not already in `templates/helpers.go`:

```go
func joinCSV(xs []string) string {
	return strings.Join(xs, ", ")
}
```

**Step 2: Add edit form CSS**

```css
.edit-form { display: flex; flex-direction: column; gap: var(--space-3); }
.detail-heading { margin: 0; font-size: 1rem; color: var(--fg); }
.toolbar-spacer { flex: 1; }
.field-label { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fg-subtle); }
.field-input, .field-select, .field-textarea, .field-select-sm {
	background: var(--bg-raised);
	border: 1px solid var(--border);
	color: var(--fg);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius);
	font-family: var(--font-sans);
	font-size: 0.9rem;
}
.field-textarea { font-family: var(--font-mono); resize: vertical; }
.field-input:focus, .field-select:focus, .field-textarea:focus, .field-select-sm:focus { outline: none; border-color: var(--accent); }
.field-select-sm { padding: var(--space-1) var(--space-2); font-size: 0.8rem; }

.action-items-edit { list-style: none; padding: 0; display: flex; flex-direction: column; gap: var(--space-2); }
.action-item-row { display: flex; gap: var(--space-2); align-items: center; }
.action-item-row .field-input { flex: 1; }

.form-actions { display: flex; gap: var(--space-2); margin-top: var(--space-3); }
.form-error { background: #f8514922; border: 1px solid var(--danger); color: var(--danger); padding: var(--space-2) var(--space-3); border-radius: var(--radius); }
```

**Step 3: Add `/partials/detail/{id}/edit` and `/partials/row/{id}` handlers**

```go
// In server.go
func (s *Server) handlePartialDetailEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	detail, err := s.db.ThoughtByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filters := parseListFilters(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.DetailEdit(detail, filters, "").Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePartialRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	detail, err := s.db.ThoughtByID(ctx, id)
	if err != nil {
		// Soft-fail: return empty so the row just disappears.
		w.WriteHeader(http.StatusOK)
		return
	}
	// Use the ThoughtData minimal shape expected by listRow
	t := templates.ThoughtData{
		ID:        detail.ID,
		Content:   detail.Content,
		Type:      detail.Type,
		Topics:    detail.Topics,
		People:    detail.People,
		CreatedAt: detail.CreatedAt,
	}
	filters := parseListFilters(r)
	// Render just the <li>...</li> element
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	selectedIDStr := r.URL.Query().Get("id")
	var selectedID int64
	if selectedIDStr != "" {
		selectedID, _ = strconv.ParseInt(selectedIDStr, 10, 64)
	}
	templates.ListRowStandalone(t, selectedID, filters).Render(ctx, w)
}
```

Note: you need to expose `listRow` as a public component. Rename or add a wrapper `ListRowStandalone` in `list.templ`:

```go
// in list.templ, add:
templ ListRowStandalone(t ThoughtData, selectedID int64, current ListFilters) {
	@listRow(t, selectedID, current)
}
```

**Step 4: Update the edit POST handler to return fragment + `HX-Trigger`**

Find the existing v1.1 `handleUpdate` (or similar — it may be named `handleEdit` or posted to `/thought/{id}/edit`). It currently responds with a 303. Keep the 303 behavior for JS-off fallback (detect via `HX-Request` header), but when the request is htmx, render the fresh read view inline and add the trigger header:

```go
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Parse form fields (existing v1.1 logic carries over).
	// ...

	// Decide whether embedding needs recomputing.
	// ...

	if err := s.db.UpdateThought(ctx, id, input); err != nil {
		// Existing graceful-degrade: re-render edit form with banner.
		detail, _ := s.db.ThoughtByID(ctx, id)
		filters := parseListFiltersFromForm(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.DetailEdit(detail, filters, err.Error()).Render(ctx, w)
		return
	}

	// htmx request: fragment + trigger
	if r.Header.Get("HX-Request") == "true" {
		detail, err := s.db.ThoughtByID(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		filters := parseListFiltersFromForm(r)
		w.Header().Set("HX-Trigger", fmt.Sprintf("refresh-row-%d", id))
		w.Header().Set("HX-Push-Url", "/?id="+fmt.Sprintf("%d", id)+"&"+filtersToURLValues(filters).Encode())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.DetailRead(detail, filters).Render(ctx, w)
		return
	}

	// Non-htmx: plain 303 redirect (JS-off fallback).
	http.Redirect(w, r, "/?id="+fmt.Sprintf("%d", id), http.StatusSeeOther)
}
```

Add helper `parseListFiltersFromForm` that reads `return_filters` or falls back to the query string, and `filtersToURLValues` (wraps `filtersToURL` into a `url.Values`).

**Step 5: Wire the row's `hx-trigger` listener**

In `list.templ`, add to the `<li>` wrapping `listRow` so the row refreshes itself when a refresh-row event fires:

```go
templ listRow(t ThoughtData, selectedID int64, current ListFilters) {
	<li hx-get={ fmt.Sprintf("/partials/row/%d?%s&id=%d", t.ID, filtersToURL(current), selectedID) }
		hx-trigger={ fmt.Sprintf("refresh-row-%d from:body", t.ID) }
		hx-swap="outerHTML"
	>
		... existing row content ...
	</li>
}
```

The refresh-row handler replaces the `<li>` with a freshly-rendered one.

**Step 6: Register new routes**

```go
r.Get("/partials/detail/{id}/edit", s.handlePartialDetailEdit)
r.Get("/partials/row/{id}", s.handlePartialRow)
// handleUpdate is already mounted at POST /thought/{id}/edit from v1.1
```

**Step 7: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `/v15`, click a row, hit Edit. Expect the edit form to render into the detail pane, URL becomes `/v15?id=142&mode=edit`. Change the content, hit Save. Expect the read view to render back into the detail pane AND the corresponding list row to update in place without a full list reload. Hit Cancel from edit mode — read view returns, no save.

Stop the server.

**Step 8: Commit**

```bash
git add -u dashboards/open-brain-dashboard-go/
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 detail pane edit state with row refresh trigger

DetailEdit is a templ component rendered into the same #detail-pane
that holds DetailRead — no modal, no separate page. Save posts to the
existing /thought/{id}/edit handler, which now returns the read view
fragment plus HX-Trigger: refresh-row-{id} and HX-Push-Url for the
correct URL when the request is htmx, or a plain 303 redirect for JS-off.

/partials/row/{id} renders a single <li> list row so the hx-trigger
listener on each row can re-fetch itself when refresh-row-N fires.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Compose panel — compact mode + capture + focus-thought trigger

**Purpose:** Build the Gmail-style floating compose panel as a native `<dialog>`, default open in compact (non-modal). Wire the "+ New thought" button, POST /capture, and the HX-Trigger chain that refreshes the list and opens the new thought in the detail pane.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/templates/compose.templ`
- Modify: `dashboards/open-brain-dashboard-go/server.go` — `handlePartialCompose`, `handlePartialComposeClose`, update existing `handleCapture`
- Modify: `dashboards/open-brain-dashboard-go/static/js/app.js` — dialog mode handlers + focus-thought trigger listener
- Modify: `dashboards/open-brain-dashboard-go/static/app.css` — compose styles

**Step 1: Write `compose.templ` (compact mode only for now; modal added in Task 10b)**

```go
// dashboards/open-brain-dashboard-go/templates/compose.templ
package templates

templ Compose() {
	<dialog class="compose-dialog" id="compose-dialog" open>
		<div class="compose-header">
			<span class="compose-title">New thought</span>
			<div class="compose-controls">
				<button class="btn-icon" onclick="obComposeExpand()" title="Expand">↕</button>
				<button class="btn-icon" onclick="obComposeMinimize()" title="Minimize">–</button>
				<button class="btn-icon"
					hx-delete="/partials/compose"
					hx-target="#compose-slot"
					hx-swap="innerHTML"
					title="Close">×</button>
			</div>
		</div>
		<form class="compose-form"
			action="/capture"
			method="POST"
			hx-post="/capture"
			hx-target="#compose-slot"
			hx-swap="innerHTML"
		>
			<textarea name="content" class="field-textarea" rows="10" placeholder="Capture a thought…" required></textarea>
			<select name="type" class="field-select">
				<option value="observation">observation</option>
				<option value="task">task</option>
				<option value="idea">idea</option>
				<option value="reference">reference</option>
				<option value="person_note">person_note</option>
			</select>
			<input type="text" name="topics" class="field-input" placeholder="topics (comma-separated)"/>
			<input type="text" name="people" class="field-input" placeholder="people (comma-separated)"/>
			<div class="form-actions">
				<button type="submit" class="btn-primary">Save</button>
				<button type="button" class="btn-secondary"
					hx-delete="/partials/compose"
					hx-target="#compose-slot"
				>Cancel</button>
			</div>
		</form>
	</dialog>
}
```

**Step 2: Add compose CSS**

```css
.compose-dialog {
	position: fixed;
	bottom: 0;
	right: var(--space-5);
	margin: 0;
	width: 34rem;
	max-height: 34rem;
	padding: 0;
	background: var(--bg-elevated);
	border: 1px solid var(--border);
	border-radius: var(--radius-lg) var(--radius-lg) 0 0;
	box-shadow: 0 -4px 16px rgba(0, 0, 0, 0.3);
	color: var(--fg);
	font-family: var(--font-sans);
	display: flex;
	flex-direction: column;
}
.compose-dialog::backdrop { background: rgba(0, 0, 0, 0.4); }
.compose-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: var(--space-2) var(--space-4);
	background: var(--bg-raised);
	border-bottom: 1px solid var(--border);
}
.compose-title { font-weight: 600; font-size: 0.9rem; }
.compose-controls { display: flex; gap: var(--space-1); }
.compose-form { display: flex; flex-direction: column; gap: var(--space-3); padding: var(--space-4); overflow-y: auto; }
.compose-form .field-textarea { flex: 1; min-height: 12rem; }

/* Modal mode — added in Task 10b */
.compose-dialog.compose-modal {
	bottom: auto;
	right: auto;
	top: 50%;
	left: 50%;
	transform: translate(-50%, -50%);
	width: 50rem;
	max-height: 90vh;
	border-radius: var(--radius-lg);
}

/* Mobile */
@media (max-width: 860px) {
	.compose-dialog {
		width: 100vw;
		right: 0;
		border-radius: 0;
	}
	.compose-dialog.compose-modal {
		top: 0;
		left: 0;
		width: 100vw;
		height: 100vh;
		max-height: 100vh;
		transform: none;
		border-radius: 0;
	}
}
```

**Step 3: Add the partial handlers**

```go
func (s *Server) handlePartialCompose(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Compose().Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePartialComposeClose(w http.ResponseWriter, r *http.Request) {
	// Respond empty: clears the #compose-slot.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}
```

Register:

```go
r.Get("/partials/compose", s.handlePartialCompose)
r.Delete("/partials/compose", s.handlePartialComposeClose)
```

**Step 4: Update `handleCapture` to emit the right headers**

```go
func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// ... parse form content/type/topics/people (existing logic) ...

	embedding, err := s.ollama.Embed(ctx, content)
	if err != nil {
		// Re-render the compose panel with an error banner.
		// (Add an error field to Compose() or wrap in a ComposeWithError helper.)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Quick inline error rendering; expand in Task 10b if needed.
		fmt.Fprintf(w, `<div class="form-error">Embedding failed: %s</div>`, err.Error())
		return
	}

	id, err := s.db.CreateThought(ctx, CreateThoughtInput{
		Content:   content,
		Type:      thoughtType,
		Topics:    topics,
		People:    people,
		Embedding: embedding,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// Clear the compose slot AND trigger refresh-list + focus on new.
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"refresh-list": {}, "focus-thought": {"id": %d}}`, id))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK) // empty body clears #compose-slot
		return
	}

	// Non-htmx: plain 303 to the new thought.
	http.Redirect(w, r, fmt.Sprintf("/?id=%d", id), http.StatusSeeOther)
}
```

**Step 5: Add trigger listeners in `app.js`**

```javascript
// Append to app.js
window.obComposeExpand = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.close();
	d.classList.add("compose-modal");
	d.showModal();
};
window.obComposeContract = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.close();
	d.classList.remove("compose-modal");
	d.show();
};
window.obComposeMinimize = function () {
	const d = document.getElementById("compose-dialog");
	if (!d) return;
	d.classList.toggle("compose-minimized");
};

document.body.addEventListener("refresh-list", function () {
	htmx.ajax("GET", "/partials/list" + window.location.search, "#list-pane");
});
document.body.addEventListener("focus-thought", function (e) {
	const id = e.detail.id;
	htmx.ajax("GET", "/partials/detail/" + id, "#detail-pane");
	// Push URL with the new id
	const url = new URL(window.location);
	url.searchParams.set("id", id);
	window.history.pushState({}, "", url);
});
document.body.addEventListener("refresh-row", function (e) {
	// Fired by edit save; the row's own hx-trigger handles the fetch.
	// No-op here — listener exists so htmx doesn't warn on unknown event.
});
```

Also add a minimize CSS rule:

```css
.compose-dialog.compose-minimized .compose-form { display: none; }
```

**Step 6: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `/v15`, click "+ New thought" in the sidebar. Expect the compose panel to appear bottom-right, non-blocking (you can still click list rows). Type content, pick type, save. Expect the panel to disappear, the list to refresh with the new thought at the top, and the detail pane to show the new thought.

Click "+ New thought" again. Click minimize — form hides, title bar remains. Click minimize again — form returns. Click close — panel disappears. Click "+ New thought" again, click expand — panel goes fullscreen-ish with backdrop. Click contract — back to compact. Click the dimmed backdrop — expected behavior (Task 10b): contracts to compact. For now may actually close the dialog; that's fine for this task.

Stop the server.

**Step 7: Commit**

```bash
git add dashboards/open-brain-dashboard-go/templates/compose.templ dashboards/open-brain-dashboard-go/server.go dashboards/open-brain-dashboard-go/static/js/app.js dashboards/open-brain-dashboard-go/static/app.css
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 compose panel (compact mode) + capture chain

Gmail-style floating compose panel implemented as a native <dialog>.
Opens bottom-right in compact mode via dialog.show() — non-blocking,
list and detail panes stay fully interactive. Minimize toggles a
class that hides the form but keeps the title bar.

Save path posts to /capture, which emits HX-Trigger with refresh-list
and focus-thought events. Client-side listeners call htmx.ajax() to
re-fetch the list pane and load the new thought into the detail pane,
then pushState the updated URL. JS-off fallback is a plain 303 redirect.

Expand/contract mode switching is wired via inline onclick handlers.
Backdrop-click-contracts and Esc-contracts polish lands in Task 10b.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10b: Compose panel polish — backdrop contract, Esc handling, error re-render

**Purpose:** Add the polish items from the design doc: clicking the modal backdrop contracts rather than closes, Esc in modal contracts, Esc in compact minimizes, embed-failure re-renders the compose form with an error banner (preserving typed input).

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/static/js/app.js` (backdrop click, Esc)
- Modify: `dashboards/open-brain-dashboard-go/templates/compose.templ` (error variant)
- Modify: `dashboards/open-brain-dashboard-go/server.go` (error re-render with preserved form state)

**Step 1: Backdrop and Esc handlers**

Add to `app.js`:

```javascript
document.addEventListener("click", function (e) {
	const d = document.getElementById("compose-dialog");
	if (!d || !d.open) return;
	if (!d.classList.contains("compose-modal")) return;
	// Click was on the dialog's backdrop (target === dialog, not inside).
	const rect = d.getBoundingClientRect();
	if (
		e.clientX < rect.left || e.clientX > rect.right ||
		e.clientY < rect.top || e.clientY > rect.bottom
	) {
		obComposeContract();
	}
});

document.addEventListener("keydown", function (e) {
	if (e.key !== "Escape") return;
	const d = document.getElementById("compose-dialog");
	if (!d || !d.open) return;
	e.preventDefault();
	if (d.classList.contains("compose-modal")) {
		obComposeContract();
	} else {
		d.classList.toggle("compose-minimized");
	}
});
```

**Step 2: Compose error variant**

Add an optional `ComposeWithInput` variant that takes pre-filled values and an error string:

```go
type ComposeDraft struct {
	Content string
	Type    string
	Topics  string
	People  string
	Error   string
}

templ ComposeWithDraft(d ComposeDraft) {
	<dialog class="compose-dialog" id="compose-dialog" open>
		<div class="compose-header">
			<span class="compose-title">New thought</span>
			<div class="compose-controls">
				<button class="btn-icon" onclick="obComposeExpand()">↕</button>
				<button class="btn-icon" onclick="obComposeMinimize()">–</button>
				<button class="btn-icon" hx-delete="/partials/compose" hx-target="#compose-slot">×</button>
			</div>
		</div>
		if d.Error != "" {
			<div class="form-error">{ d.Error }</div>
		}
		<form class="compose-form" action="/capture" method="POST"
			hx-post="/capture" hx-target="#compose-slot" hx-swap="innerHTML"
		>
			<textarea name="content" class="field-textarea" rows="10" required>{ d.Content }</textarea>
			<select name="type" class="field-select">
				@typeOption("observation", d.Type)
				@typeOption("task", d.Type)
				@typeOption("idea", d.Type)
				@typeOption("reference", d.Type)
				@typeOption("person_note", d.Type)
			</select>
			<input type="text" name="topics" class="field-input" value={ d.Topics } placeholder="topics"/>
			<input type="text" name="people" class="field-input" value={ d.People } placeholder="people"/>
			<div class="form-actions">
				<button type="submit" class="btn-primary">Save</button>
				<button type="button" class="btn-secondary" hx-delete="/partials/compose" hx-target="#compose-slot">Cancel</button>
			</div>
		</form>
	</dialog>
}
```

Then `handleCapture`'s error path:

```go
if err != nil {
	draft := templates.ComposeDraft{
		Content: content,
		Type:    thoughtType,
		Topics:  r.FormValue("topics"),
		People:  r.FormValue("people"),
		Error:   "Embedding failed: " + err.Error(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ComposeWithDraft(draft).Render(ctx, w)
	return
}
```

**Step 3: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Click "+ New thought", click expand to go modal. Click outside the panel — expect contract to compact (not close). In modal, press Esc — expect contract. In compact, press Esc — expect minimize.

To test the error path, you can temporarily stop Ollama (or point config at a dead URL) and try to save. Expect the compose panel to redraw with the typed input still in place and an error banner at the top.

Stop the server.

**Step 4: Commit**

```bash
git add -u dashboards/open-brain-dashboard-go/
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 compose polish: backdrop-contract, Esc, error preservation

Clicking the modal backdrop contracts to compact mode instead of
closing (preserves draft). Esc in modal contracts, Esc in compact
minimizes. Capture embed failure re-renders the compose panel with
the typed input still in place and an error banner at the top.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Ollama health indicator

**Purpose:** Small status dot next to the search box that reflects the last embed call's outcome. Green = recent success, amber = fail or stale, grey = never called this session.

**Files:**
- Create: `dashboards/open-brain-dashboard-go/ollama_health.go` (actor holding health state)
- Modify: `dashboards/open-brain-dashboard-go/server.go` — inject health actor, wire embed wrapper, HX-Trigger on every embed
- Modify: `dashboards/open-brain-dashboard-go/templates/list.templ` — render dot based on server-supplied state
- Modify: `dashboards/open-brain-dashboard-go/static/js/app.js` — listener updates dot on HX-Trigger

**Step 1: Health actor** (use the actor-pattern per user's convention)

```go
// dashboards/open-brain-dashboard-go/ollama_health.go
package main

import (
	"context"
	"time"
)

type healthState struct {
	LastUp   time.Time
	LastDown time.Time
}

type healthCmd struct {
	kind string // "record-up", "record-down", "get"
	resp chan healthState
}

type OllamaHealth struct {
	cmds chan healthCmd
}

func NewOllamaHealth(ctx context.Context) *OllamaHealth {
	h := &OllamaHealth{cmds: make(chan healthCmd, 32)}
	go h.run(ctx)
	return h
}

func (h *OllamaHealth) run(ctx context.Context) {
	state := healthState{}
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-h.cmds:
			switch cmd.kind {
			case "record-up":
				state.LastUp = time.Now()
			case "record-down":
				state.LastDown = time.Now()
			case "get":
				cmd.resp <- state
			}
		}
	}
}

func (h *OllamaHealth) RecordUp()    { h.cmds <- healthCmd{kind: "record-up"} }
func (h *OllamaHealth) RecordDown()  { h.cmds <- healthCmd{kind: "record-down"} }
func (h *OllamaHealth) Get() healthState {
	resp := make(chan healthState, 1)
	h.cmds <- healthCmd{kind: "get", resp: resp}
	return <-resp
}

// Status returns the displayable tri-state: "green", "amber", "grey".
func (h *OllamaHealth) Status() string {
	s := h.Get()
	now := time.Now()
	if s.LastUp.IsZero() && s.LastDown.IsZero() {
		return "grey"
	}
	if !s.LastDown.IsZero() && s.LastDown.After(s.LastUp) {
		return "amber"
	}
	if now.Sub(s.LastUp) > 5*time.Minute {
		return "amber"
	}
	return "green"
}
```

**Step 2: Inject into the server and wrap every embed call**

In `server.go`, add `health *OllamaHealth` to the `Server` struct. Construct it in `main.go` and pass to `NewServer`.

Wrap `ollama.Embed` call sites (in `handleShell`, `handlePartialList`, `handleCapture`, `handleUpdate`) so success calls `health.RecordUp()` and failure calls `health.RecordDown()`. E.g.:

```go
func (s *Server) embed(ctx context.Context, q string) []float32 {
	emb, err := s.ollama.Embed(ctx, q)
	if err != nil {
		s.health.RecordDown()
		return nil
	}
	s.health.RecordUp()
	return emb
}
```

Use `s.embed(ctx, q)` instead of direct `s.ollama.Embed` in every handler.

**Step 3: Render the dot in the list toolbar**

Update `ListData` to include the health status:

```go
type ListData struct {
	Result       *ListResult
	SelectedID   int64
	OllamaStatus string // "green" | "amber" | "grey"
}
```

In `list.templ` toolbar, next to the search form:

```go
<span class={ "ollama-dot", "ollama-dot--" + ld.OllamaStatus } title={ "embeddings: " + ld.OllamaStatus }></span>
```

Update every handler that renders `List` to pass `OllamaStatus: s.health.Status()`.

**Step 4: CSS**

```css
.ollama-dot { display: inline-block; width: 0.6rem; height: 0.6rem; border-radius: 50%; }
.ollama-dot--green { background: var(--success); }
.ollama-dot--amber { background: var(--warning); }
.ollama-dot--grey { background: var(--fg-subtle); }
```

**Step 5: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `/v15`. Expect a grey dot initially. Type in the search box — after the semantic query succeeds, expect the dot to turn green. Stop Ollama on the remote host (or point config at a dead URL temporarily), trigger another search — expect amber.

Stop the server.

**Step 6: Commit**

```bash
git add -u dashboards/open-brain-dashboard-go/
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 Ollama health indicator in list toolbar

Actor-pattern health state holds last-up and last-down timestamps.
Every embed call goes through a Server.embed wrapper that records
the outcome. Status() returns green/amber/grey based on whether the
last outcome was a success within the 5-minute window.

Rendered as a small dot next to the search box, server-side per
render (no polling).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Bulk delete — form wrap, toolbar states, CSS `:has()`, handler

**Purpose:** Wire bulk delete end-to-end. Wrap the list in `<form id="list-form">`, add row checkboxes, render the three-state toolbar (normal/bulk/confirm), use CSS `:has()` to switch between normal and bulk, inline onclick for the confirm toggle, POST /bulk-delete handler with proper HX-Trigger clear-detail when the open id was deleted.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/templates/list.templ`
- Modify: `dashboards/open-brain-dashboard-go/static/app.css`
- Modify: `dashboards/open-brain-dashboard-go/static/js/app.js`
- Modify: `dashboards/open-brain-dashboard-go/server.go`

**Step 1: Wrap the list in a form, add checkboxes**

In `list.templ`, wrap `.list-rows` and its toolbar in a `<form id="list-form">`. Update `listRow` to include a checkbox:

```go
templ listRow(t ThoughtData, selectedID int64, current ListFilters) {
	<li class="list-row-wrapper"
		hx-get={ fmt.Sprintf("/partials/row/%d?%s&id=%d", t.ID, filtersToURL(current), selectedID) }
		hx-trigger={ fmt.Sprintf("refresh-row-%d from:body", t.ID) }
		hx-swap="outerHTML"
	>
		<label class="row-checkbox-wrap" onclick="event.stopPropagation()">
			<input type="checkbox" class="row-checkbox" name="ids" value={ fmt.Sprintf("%d", t.ID) }/>
		</label>
		<a class={ rowClass(t, selectedID) }
			hx-get={ fmt.Sprintf("/partials/detail/%d?%s", t.ID, filtersToURL(current)) }
			hx-target="#detail-pane"
			hx-push-url="true"
		>
			<div class="row-line1">
				<span class={ "type-chip", typeClass(t.Type) }>{ t.Type }</span>
				<span class="row-time">{ HumanTime(t.CreatedAt) }</span>
				<span class="row-id">{ fmt.Sprintf("%d", t.ID) }</span>
			</div>
			<div class="row-line2">{ Truncate(t.Content, 120) }</div>
		</a>
	</li>
}
```

**Step 2: Add the three toolbar states, all rendered, CSS picks**

In `List` template, replace the single toolbar with:

```go
<div class="list-toolbar">
	<label class="select-all-wrap">
		<input type="checkbox" id="select-all" onclick="obSelectAll(this)"/>
	</label>

	<div class="toolbar-normal">
		<form class="search-form" ...existing... >...</form>
		<span class="ollama-dot" ...>...</span>
		<div class="list-count">{ ... }</div>
	</div>

	<div class="toolbar-bulk">
		<span class="bulk-count">selected</span>
		<button type="button" class="btn-danger"
			onclick="this.closest('.list-toolbar').setAttribute('data-confirming','true')">
			Delete selected
		</button>
		<button type="button" class="btn-secondary" onclick="obClearSelection()">Clear</button>
	</div>

	<div class="toolbar-confirm">
		<span>Delete selected thoughts? This is permanent.</span>
		<button type="button" class="btn-danger"
			hx-post="/bulk-delete"
			hx-include="#list-form"
			hx-target="#list-pane"
		>Yes, delete</button>
		<button type="button" class="btn-secondary"
			onclick="this.closest('.list-toolbar').removeAttribute('data-confirming')">
			Cancel
		</button>
	</div>
</div>
```

Wrap the whole list content (toolbar + rows + pagination) inside `<form id="list-form">`. Actually — form can't wrap the search form (which is a separate form). Use a single outer `<div id="list-form" class="list-form-wrap">` that the `hx-include` picks up via the id and input-gathering semantics of htmx (htmx's `hx-include` accepts a selector; it will gather all matching inputs descending from it, even if the "form" is a div).

Actually, htmx's `hx-include` can accept any selector and will include all form-like inputs under it. So:

```go
<div id="list-form" class="list-form-wrap">
	<div class="list-toolbar">...</div>
	<ul class="list-rows">...</ul>
	...
</div>
```

Inside, the real `<form>` is only the search form. Checkboxes are regular `<input>` elements. `hx-include="#list-form"` will pick up everything.

**Step 3: CSS for state transitions**

```css
.list-form-wrap { display: flex; flex-direction: column; height: 100%; }

.list-toolbar { position: relative; }
.toolbar-normal,
.toolbar-bulk,
.toolbar-confirm {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	flex: 1;
}

/* Default: normal shown */
.list-form-wrap:has(.row-checkbox:checked) .toolbar-normal { display: none; }
.list-form-wrap:not(:has(.row-checkbox:checked)) .toolbar-bulk { display: none; }
.list-toolbar .toolbar-confirm { display: none; }
.list-toolbar[data-confirming="true"] .toolbar-bulk { display: none; }
.list-toolbar[data-confirming="true"] .toolbar-normal { display: none; }
.list-toolbar[data-confirming="true"] .toolbar-confirm { display: flex; }

.row-checkbox-wrap { padding: 0 var(--space-2); }
.bulk-count::before { content: attr(data-count) " "; }
```

The bulk-count pseudo-element relies on a JS helper that updates `data-count` on every change — cheaper than htmx roundtrip. Add to `app.js`:

```javascript
window.obSelectAll = function (el) {
	document.querySelectorAll(".row-checkbox").forEach(function (c) { c.checked = el.checked; });
	obUpdateBulkCount();
};
window.obClearSelection = function () {
	document.querySelectorAll(".row-checkbox").forEach(function (c) { c.checked = false; });
	document.getElementById("select-all").checked = false;
	obUpdateBulkCount();
};
window.obUpdateBulkCount = function () {
	const n = document.querySelectorAll(".row-checkbox:checked").length;
	document.querySelectorAll(".bulk-count").forEach(function (el) { el.setAttribute("data-count", n); });
};
document.addEventListener("change", function (e) {
	if (e.target && e.target.matches(".row-checkbox")) obUpdateBulkCount();
});
```

**Step 4: Bulk delete handler**

```go
func (s *Server) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	idStrs := r.Form["ids"]
	var ids []int64
	for _, s := range idStrs {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, n)
		}
	}

	if _, err := s.db.BulkDelete(ctx, ids); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if the currently-open id (from the URL) was in the deleted set.
	selectedID := int64(0)
	if idStr := r.URL.Query().Get("id"); idStr != "" {
		selectedID, _ = strconv.ParseInt(idStr, 10, 64)
	}
	clearDetail := false
	for _, id := range ids {
		if id == selectedID {
			clearDetail = true
			break
		}
	}

	// Re-render the list pane with current filters.
	filters := parseListFilters(r)
	var embedding []float32
	if filters.Q != "" {
		embedding = s.embed(ctx, filters.Q)
	}
	result, err := s.db.Search(ctx, filters, embedding)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if clearDetail {
		w.Header().Set("HX-Trigger", `{"clear-detail": {}}`)
		// Also push URL without id
		newURL := "/"
		if qs := filtersToURL(filters); qs != "" {
			newURL += "?" + qs
		}
		w.Header().Set("HX-Push-Url", newURL)
		selectedID = 0
	}

	vm := templates.ListData{
		Result:       result,
		SelectedID:   selectedID,
		OllamaStatus: s.health.Status(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.List(vm).Render(ctx, w)
}
```

Register:

```go
r.Post("/bulk-delete", s.handleBulkDelete)
```

And add a listener in `app.js` that handles `clear-detail`:

```javascript
document.body.addEventListener("clear-detail", function () {
	htmx.ajax("GET", "/partials/detail/empty", "#detail-pane");
});
```

You'll also need a tiny `/partials/detail/empty` route that just renders `DetailEmpty()`. Add:

```go
func (s *Server) handlePartialDetailEmpty(w http.ResponseWriter, r *http.Request) {
	templates.DetailEmpty().Render(r.Context(), w)
}
r.Get("/partials/detail/empty", s.handlePartialDetailEmpty)
```

**Step 5: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `/v15`. Check a single row's checkbox — expect the toolbar to swap to bulk mode (checkbox + "Delete selected" + "Clear"). Check multiple rows. Click Delete selected — expect the toolbar to swap to confirm mode. Cancel — back to bulk mode, selections preserved. Click Delete selected again, then Yes, delete — expect those rows to disappear from the list, toolbar back to normal.

Test the "deleted open thought" case: click a row to open it in the detail pane, then check its checkbox, delete. Expect the detail pane to clear to the empty state and the URL to lose `?id=N`.

**Step 6: Commit**

```bash
git add -u dashboards/open-brain-dashboard-go/
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 bulk delete end-to-end

Row checkboxes, three-state toolbar (normal/bulk/confirm) switched by
CSS :has() on the list-form-wrap container. Inline onclick toggles
data-confirming for the confirm step. POST /bulk-delete runs a single
DELETE WHERE id = ANY and re-renders the list pane.

When the currently-open thought is in the deleted set, the response
emits HX-Trigger: clear-detail and HX-Push-Url to strip ?id. Client
listener swaps #detail-pane to the empty state.

No undo window — deletion is immediate, consistent with v1.1.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Legacy 303 redirects for v1.1 URLs

**Purpose:** Every old URL that was captured in the brain, bookmarked, or linked elsewhere must keep working via a 303 redirect into the new URL space.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/server.go`

**Step 1: Add redirect handlers**

```go
func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Preserve query string where it makes sense.
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (s *Server) handleLegacyBrowse(w http.ResponseWriter, r *http.Request) {
	// /browse?type=... → /?type=...
	target := "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleLegacySearch(w http.ResponseWriter, r *http.Request) {
	// /search?q=...&mode=... → /?q=...  (mode dropped)
	q := r.URL.Query()
	q.Del("mode")
	target := "/"
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleLegacyThought(w http.ResponseWriter, r *http.Request) {
	// /thought/{id} → /?id={id}
	id := chi.URLParam(r, "id")
	http.Redirect(w, r, "/?id="+id, http.StatusSeeOther)
}

func (s *Server) handleLegacyThoughtEdit(w http.ResponseWriter, r *http.Request) {
	// GET /thought/{id}/edit → /?id={id}&mode=edit
	// (POST /thought/{id}/edit keeps working as the update endpoint)
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	http.Redirect(w, r, "/?id="+id+"&mode=edit", http.StatusSeeOther)
}
```

Register (before flipping to v1.5 at `/` — for now keep them mounted even while the new shell lives at `/v15`):

```go
r.Get("/home", redirectTo("/"))
r.Get("/browse", s.handleLegacyBrowse)
r.Get("/search", s.handleLegacySearch)
r.Get("/thought/{id}", s.handleLegacyThought)
// GET /thought/{id}/edit redirects; POST stays as handleUpdate
```

**Step 2: Build and smoke-test**

```bash
./build.sh build
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Hit `/thought/142` — expect 303 to `/?id=142`. Hit `/browse?type=task` — expect 303 to `/?type=task`. Hit `/search?q=foo&mode=text` — expect 303 to `/?q=foo`. But wait — currently `/` is still the v1.1 home, not the shell. So the redirects go to v1.1 for now. That's fine — Task 14 flips `/` to the shell and the redirects become correct automatically.

Stop the server.

**Step 3: Commit**

```bash
git add -u dashboards/open-brain-dashboard-go/server.go
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 legacy 303 redirects for v1.1 URLs

/home, /browse, /search, /thought/{id}, and GET /thought/{id}/edit
all redirect to the new /?... URL space. POST /thought/{id}/edit keeps
working as the update endpoint.

During cutover the redirects still land on the v1.1 home; Task 14 flips
the / handler to the new shell and the redirects resolve to the right
destinations automatically.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Flip `/` to the shell, delete v1.1 templates

**Purpose:** Make the v1.5 shell the canonical route and remove the v1.1 templates and their handlers.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/server.go` — flip `/v15` to `/`, remove old route handlers
- Delete: all v1.1 `*.templ` files and their generated `*_templ.go` files

**Step 1: Remove old handlers and flip the route**

In `server.go`:

1. Delete the old handlers: `handleHome`, `handleBrowse`, `handleSearch`, `handleDetail`, `handleEditForm`, `handleActionItemRow` (if any are dashboard-home-specific and don't power the new shell).
2. Change `r.Get("/v15", s.handleShell)` to `r.Get("/", s.handleShell)`.
3. Remove any route registrations pointing at the deleted handlers.
4. Keep: `handleCapture` (POST /capture), `handleUpdate` (POST /thought/{id}/edit), `handleDelete` (POST /thought/{id}/delete).

**Step 2: Delete the v1.1 templ files**

```bash
cd /home/jim/projects/open-brain/dashboards/open-brain-dashboard-go/templates
rm home.templ home_templ.go
rm browse.templ browse_templ.go
rm search.templ search_templ.go
rm detail.templ detail_templ.go
rm detail_edit.templ detail_edit_templ.go
rm thought_card.templ thought_card_templ.go
rm partials.templ partials_templ.go
```

**Step 3: Regenerate and build**

```bash
cd /home/jim/projects/open-brain/dashboards/open-brain-dashboard-go
./build.sh build
```

**Expected:** Clean build. The compiler will catch any lingering references to deleted templates — read the error and delete the stale reference (likely in `server.go` where old route handlers were registered). Re-run build after each fix.

**Step 4: Smoke-test against the live brain**

```bash
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Open `http://127.0.0.1:8080/`. Expect the new shell. Walk through:

- Filter by type, topic, person, days combinations
- Search (semantic + fallback — try searching for something not in the corpus to trigger the banner)
- Click rows to open detail
- Edit a thought, save, verify row updates
- Open compose, save a new thought, verify it appears at the top
- Toggle the theme
- Bulk-select a few rows, delete, verify they're gone
- Hit `/thought/142` and `/browse?type=task` — verify redirects
- Hit browser Back and Forward repeatedly — verify URL state is correct throughout

Stop the server.

**Step 5: Commit**

```bash
git add -A dashboards/open-brain-dashboard-go/
git commit -m "$(cat <<'EOF'
[dashboards] v1.5 flipped live: / is now the master/detail shell

Removed the v1.1 home/browse/search/detail templ files and their
generated counterparts. Flipped /v15 → / so the new shell is the
canonical route. Legacy redirects (from Task 13) now resolve to the
right destinations.

Kept: main.go, config.go, db.go, ollama.go, thoughts.go, ollama_health.go,
static assets, POST handlers for capture/edit/delete.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: README update and final verification

**Purpose:** Update `README.md` to describe v1.5, update `AGENTS.md`-style references if any, and do a final end-to-end smoke test before handing back.

**Files:**
- Modify: `dashboards/open-brain-dashboard-go/README.md`

**Step 1: Update the README status and feature sections**

Edit the Status section to reflect v1.5 master/detail. Update the URL Surface block. Update the Layout block to list the new templ files. Mention node-0 deployment as the resume point.

Key edits:
- "v1.1 complete" → "v1.5 complete"
- Feature table: replace Home/Browse/Search/Detail rows with a single "Shell" row describing the master/detail layout
- Add rows for "Compose" (dual-mode floating panel) and "Bulk delete"
- URL surface: replace with the Task 13 / 14 routes (just `/`, `/partials/*`, `/bulk-delete`, `/capture`, `/thought/{id}/edit`, `/thought/{id}/delete` + the legacy redirects)

**Step 2: Final smoke test**

```bash
./build.sh build
./build.sh test
./open-brain-dashboard-go serve --config open-brain-dashboard-go.toml
```

Walk through the full happy path one more time. Check that the Ollama health dot reflects actual embed state. Check that the light-mode toggle works.

**Step 3: Commit**

```bash
git add dashboards/open-brain-dashboard-go/README.md
git commit -m "$(cat <<'EOF'
[docs] Bump dashboard-go README to v1.5 master/detail

Describe the unified / route, three-pane shell, compose dual-mode
panel, unified search with auto-fallback, and bulk delete. Call out
node-0 deployment as the next resume point now that v1.5 is in.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

**Step 4: Final state**

`git log --oneline` should show a clean sequence of 16-ish v1.5 commits on top of the design-doc commit. Working tree clean.

**Do not push** unless Jim asks for it.

---

## Summary

Fifteen tasks plus a pre-flight check. Each has its own commit. Bite-sized enough that each task is ~20-45 minutes of focused work. The critical-path dependencies are:

```
Task 0 (sanity) → Task 1 (tokens) → Task 2 (js) → Task 3 (query builder TDD)
       → Task 4 (Search + BulkDelete) → Task 5 (shell)
       → Task 6 (sidebar) → Task 7 (list pane)
       → Task 8 (detail read) → Task 9 (detail edit)
       → Task 10 (compose compact) → Task 10b (compose polish)
       → Task 11 (health indicator) → Task 12 (bulk delete)
       → Task 13 (legacy redirects) → Task 14 (cutover)
       → Task 15 (README)
```

There is no parallelism — each task depends on the previous. Subagent-driven execution is fine; parallel sessions don't help.

## Reference

- Design doc: `dashboards/open-brain-dashboard-go/docs/plans/2026-04-11-dashboard-v1.5-master-detail-design.md`
- Design context (binding): `dashboards/open-brain-dashboard-go/CLAUDE.md`
- User preferences in memory: terse peer-to-peer communication, table-driven tests with `t.TempDir()`, CLI flags over env vars, anti-bloat philosophy
