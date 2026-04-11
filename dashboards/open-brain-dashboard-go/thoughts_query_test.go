package main

import (
	"strings"
	"testing"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
)

func TestBuildListQuery(t *testing.T) {
	cases := []struct {
		name       string
		filters    templates.ListFilters
		withVector bool
		wantSubstr []string
		wantArgLen int
	}{
		{
			name:       "no filters, no query",
			filters:    templates.ListFilters{Page: 1, PerPage: 50},
			wantSubstr: []string{"FROM thoughts", "ORDER BY created_at DESC", "LIMIT $1 OFFSET $2"},
			wantArgLen: 2,
		},
		{
			name:       "type filter only",
			filters:    templates.ListFilters{Type: "task", Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->>'type' = $1", "LIMIT $2 OFFSET $3"},
			wantArgLen: 3,
		},
		{
			name:       "topic filter uses jsonb array membership",
			filters:    templates.ListFilters{Topic: "open-brain", Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->'topics' ? $1", "jsonb_typeof(metadata->'topics') = 'array'"},
			wantArgLen: 3,
		},
		{
			name:       "q triggers ILIKE in non-semantic mode",
			filters:    templates.ListFilters{Q: "deployment", Page: 1, PerPage: 50},
			withVector: false,
			wantSubstr: []string{"content ILIKE '%' || $1 || '%'"},
			wantArgLen: 3,
		},
		{
			name:       "q with embedding uses pgvector distance",
			filters:    templates.ListFilters{Q: "deployment", Page: 1, PerPage: 50},
			withVector: true,
			wantSubstr: []string{"1 - (embedding <=> $1)", "ORDER BY embedding <=> $1", "embedding IS NOT NULL"},
			// In semantic mode, Q is used by the caller to compute the
			// embedding vector (prepended at $1) and is NOT added as a
			// SQL arg by buildListQuery. The ILIKE branch only fires
			// when !withVector. So args = [LIMIT, OFFSET] = 2.
			wantArgLen: 2,
		},
		{
			name:       "all filters combine with AND",
			filters:    templates.ListFilters{Type: "task", Topic: "open-brain", Person: "Jim", Days: 7, Page: 1, PerPage: 50},
			wantSubstr: []string{"metadata->>'type' = $1", "metadata->'topics' ? $2", "metadata->'people' ? $3", "interval '7 days'"},
			wantArgLen: 5,
		},
		{
			name:       "pagination offset math",
			filters:    templates.ListFilters{Page: 3, PerPage: 50},
			wantSubstr: []string{"OFFSET $2"},
			wantArgLen: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := buildListQuery(tc.filters, tc.withVector)
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
	sql, args := buildCountQuery(templates.ListFilters{Type: "task", Topic: "open-brain"}, false)
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
