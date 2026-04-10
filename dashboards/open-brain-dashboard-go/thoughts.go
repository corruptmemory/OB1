package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
)

const (
	recentLimit    = 20
	topTopicsLimit = 10
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
