package main

import (
	"strings"
	"testing"
)

func TestBuildListQuery(t *testing.T) {
	cases := []struct {
		name       string
		filters    ListFilters
		withVector bool
		wantSubstr []string
		wantArgLen int
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
			withVector: false,
			wantSubstr: []string{"content ILIKE '%' || $1 || '%'"},
			wantArgLen: 3,
		},
		{
			name:       "q with embedding uses pgvector distance",
			filters:    ListFilters{Q: "deployment", Page: 1, PerPage: 50},
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
