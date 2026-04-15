package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
)

// serveMCP builds the MCP tool registry and starts the StreamableHTTP server.
// Called by runMCP (standalone) and runSystem (shared with dashboard).
func serveMCP(cfg *Config, db *DB, ollama *OllamaClient) error {
	if cfg.MCP.AccessKey == "" {
		return fmt.Errorf("mcp.access_key is required — set it in the config or pass --key")
	}

	s := mcpserver.NewMCPServer("open-brain", "1.0.0")
	registerMCPTools(s, db, ollama)

	httpHandler := mcpserver.NewStreamableHTTPServer(s)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(mcpCORSHeaders)
	r.Use(claudeDesktopAcceptPatch)
	r.Use(mcpAuth(cfg.MCP.AccessKey))
	r.Mount("/", httpHandler)

	addr := cfg.MCP.Listen
	if addr == "" {
		addr = "127.0.0.1:8001"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("open-brain MCP listening on http://%s", addr)
	return srv.ListenAndServe()
}

// mcpCORSHeaders sets permissive CORS headers for MCP clients (browsers,
// Electron, Claude Desktop) that originate from a different port or host.
func mcpCORSHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, x-brain-key, mcp-session-id, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// claudeDesktopAcceptPatch injects the Accept header that StreamableHTTP
// requires when the client (Claude Desktop) omits it. See OB1#33.
func claudeDesktopAcceptPatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			r = r.Clone(r.Context())
			r.Header.Set("Accept", "application/json, text/event-stream")
		}
		next.ServeHTTP(w, r)
	})
}

// mcpAuth validates the x-brain-key header or ?key= query param against
// the configured access key.
func mcpAuth(accessKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("x-brain-key")
			if key == "" {
				key = r.URL.Query().Get("key")
			}
			if key != accessKey {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// registerMCPTools registers all MCP tools on the server. Broken out so
// the list is easy to scan without scrolling past middleware wiring.
func registerMCPTools(s *mcpserver.MCPServer, db *DB, ollama *OllamaClient) {
	// --- Parity tools (match server/index.ts) ---
	s.AddTool(toolSearchThoughts(), makeSearchThoughtsHandler(db, ollama))
	s.AddTool(toolListThoughts(), makeListThoughtsHandler(db))
	s.AddTool(toolThoughtStats(), makeThoughtStatsHandler(db))
	s.AddTool(toolCaptureThought(), makeCaptureThoughtHandler(db, ollama))

	// --- Toolbox tools (beyond parity, enabled by domain layer) ---
	s.AddTool(toolDeleteThought(), makeDeleteThoughtHandler(db))
	s.AddTool(toolFindSimilar(), makeFindSimilarHandler(db))
	s.AddTool(toolCoalesceThoughts(), makeCoalesceThoughtsHandler(db, ollama))
}

// ---- Tool definitions ----

func toolSearchThoughts() mcp.Tool {
	return mcp.NewTool("search_thoughts",
		mcp.WithDescription("Semantic search over stored thoughts. Returns thoughts ranked by similarity to the query, falling back to substring search when Ollama is unavailable."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query text"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum results to return (default 20)"),
		),
	)
}

func toolListThoughts() mcp.Tool {
	return mcp.NewTool("list_thoughts",
		mcp.WithDescription("List stored thoughts, optionally filtered by type, topic, person, or recency. Returns newest-first."),
		mcp.WithString("type",
			mcp.Description("Filter by thought type: observation, task, idea, reference, person_note"),
		),
		mcp.WithString("topic",
			mcp.Description("Filter to thoughts whose topics array contains this value"),
		),
		mcp.WithString("person",
			mcp.Description("Filter to thoughts whose people array contains this value"),
		),
		mcp.WithNumber("days",
			mcp.Description("Limit to thoughts created within this many days"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum results to return (default 50)"),
		),
	)
}

func toolThoughtStats() mcp.Tool {
	return mcp.NewTool("thought_stats",
		mcp.WithDescription("Return aggregate counts: total thoughts, week count, breakdown by type, top topics, and top people."),
	)
}

func toolCaptureThought() mcp.Tool {
	return mcp.NewTool("capture_thought",
		mcp.WithDescription("Save a new thought. Embeds the content and extracts metadata (type, topics, people, action items) automatically via Ollama."),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("The thought content to capture"),
		),
	)
}

func toolDeleteThought() mcp.Tool {
	return mcp.NewTool("delete_thought",
		mcp.WithDescription("Permanently delete a thought by ID."),
		mcp.WithNumber("id",
			mcp.Required(),
			mcp.Description("Thought ID to delete"),
		),
	)
}

func toolFindSimilar() mcp.Tool {
	return mcp.NewTool("find_similar_thoughts",
		mcp.WithDescription("Find thoughts semantically similar to a given thought, ranked by cosine similarity. Useful for detecting near-duplicates before coalescing."),
		mcp.WithNumber("id",
			mcp.Required(),
			mcp.Description("ID of the source thought"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum results to return (default 10)"),
		),
	)
}

func toolCoalesceThoughts() mcp.Tool {
	return mcp.NewTool("coalesce_thoughts",
		mcp.WithDescription("Atomically merge a set of thoughts into one, deleting the originals in the same transaction. If synthesized_content is omitted, Ollama synthesizes the originals automatically."),
		mcp.WithArray("ids",
			mcp.Required(),
			mcp.Description("IDs of the thoughts to merge and delete"),
			mcp.WithNumberItems(),
		),
		mcp.WithString("synthesized_content",
			mcp.Description("Pre-written merged content. Omit to let Ollama synthesize from the originals."),
		),
	)
}

// ---- Handlers ----

// thoughtOut is the JSON shape returned for individual thoughts by the
// search, list, and find-similar tools. Kept flat for easy parsing.
type thoughtOut struct {
	ID         int64    `json:"id"`
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Topics     []string `json:"topics"`
	People     []string `json:"people,omitempty"`
	Similarity float64  `json:"similarity,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

func makeSearchThoughtsHandler(db *DB, ollama *OllamaClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 20)

		// Best-effort embed; nil signals Search to use text fallback.
		embedding, _ := ollama.Embed(ctx, query)

		f := templates.ListFilters{Q: query, PerPage: limit, Page: 1}
		result, err := db.Search(ctx, f, embedding)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
		}

		out := make([]thoughtOut, len(result.Results))
		for i, t := range result.Results {
			out[i] = thoughtOut{
				ID:         t.ID,
				Content:    t.Content,
				Type:       t.Type,
				Topics:     t.Topics,
				People:     t.People,
				Similarity: t.Similarity,
				CreatedAt:  t.CreatedAt.Format(time.RFC3339),
			}
		}
		return jsonResult(out)
	}
}

func makeListThoughtsHandler(db *DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := req.GetInt("limit", 50)
		f := templates.ListFilters{
			Type:    req.GetString("type", ""),
			Topic:   req.GetString("topic", ""),
			Person:  req.GetString("person", ""),
			Days:    req.GetInt("days", 0),
			PerPage: limit,
			Page:    1,
		}

		result, err := db.Search(ctx, f, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list: %v", err)), nil
		}

		out := make([]thoughtOut, len(result.Results))
		for i, t := range result.Results {
			out[i] = thoughtOut{
				ID:        t.ID,
				Content:   t.Content,
				Type:      t.Type,
				Topics:    t.Topics,
				People:    t.People,
				CreatedAt: t.CreatedAt.Format(time.RFC3339),
			}
		}
		return jsonResult(out)
	}
}

func makeThoughtStatsHandler(db *DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sc, err := db.SidebarCounts(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stats: %v", err)), nil
		}

		byType := make(map[string]int64, len(sc.Types))
		for _, tc := range sc.Types {
			byType[tc.Type] = tc.Count
		}
		byTopic := make(map[string]int64, len(sc.Topics))
		for _, tc := range sc.Topics {
			byTopic[tc.Topic] = tc.Count
		}
		byPerson := make(map[string]int64, len(sc.People))
		for _, pc := range sc.People {
			byPerson[pc.Person] = pc.Count
		}

		out := map[string]any{
			"total":     sc.Total,
			"week":      sc.WeekCount,
			"by_type":   byType,
			"by_topic":  byTopic,
			"by_person": byPerson,
		}
		return jsonResult(out)
	}
}

func makeCaptureThoughtHandler(db *DB, ollama *OllamaClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Embed and extract in parallel — two independent Ollama calls.
		var (
			embedding []float32
			meta      ExtractedMetadata
			embedErr  error
			wg        sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			embedding, embedErr = ollama.Embed(ctx, content)
		}()
		go func() {
			defer wg.Done()
			meta = ollama.Extract(ctx, content)
		}()
		wg.Wait()

		if embedErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embed: %v", embedErr)), nil
		}

		in := CreateThoughtInput{
			Content:        content,
			Type:           meta.Type,
			Topics:         meta.Topics,
			People:         meta.People,
			ActionItems:    meta.ActionItems,
			DatesMentioned: meta.DatesMentioned,
			Embedding:      embedding,
			Source:         "mcp",
		}
		id, err := db.CreateThought(ctx, in)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("capture: %v", err)), nil
		}

		out := map[string]any{
			"id":      id,
			"type":    meta.Type,
			"topics":  meta.Topics,
			"people":  meta.People,
			"content": content,
		}
		return jsonResult(out)
	}
}

func makeDeleteThoughtHandler(db *DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireInt("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := db.DeleteThought(ctx, int64(id)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return mcp.NewToolResultError(fmt.Sprintf("thought %d not found", id)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("delete: %v", err)), nil
		}

		out := map[string]any{"deleted": true, "id": id}
		return jsonResult(out)
	}
}

func makeFindSimilarHandler(db *DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireInt("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 10)

		results, err := db.FindSimilar(ctx, int64(id), limit)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return mcp.NewToolResultError(fmt.Sprintf("thought %d not found", id)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("find similar: %v", err)), nil
		}

		out := make([]thoughtOut, len(results))
		for i, r := range results {
			out[i] = thoughtOut{
				ID:         r.ID,
				Content:    r.Content,
				Type:       r.Type,
				Topics:     r.Topics,
				Similarity: r.Similarity,
				CreatedAt:  r.CreatedAt.Format(time.RFC3339),
			}
		}
		return jsonResult(out)
	}
}

func makeCoalesceThoughtsHandler(db *DB, ollama *OllamaClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawIDs, err := req.RequireIntSlice("ids")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(rawIDs) < 2 {
			return mcp.NewToolResultError("coalesce requires at least 2 thought IDs"), nil
		}
		ids := make([]int64, len(rawIDs))
		for i, v := range rawIDs {
			ids[i] = int64(v)
		}

		synthesized := req.GetString("synthesized_content", "")
		if synthesized == "" {
			contents, err := db.FetchContents(ctx, ids)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("fetch contents: %v", err)), nil
			}
			synthesized, err = ollama.Synthesize(ctx, contents)
			if err != nil {
				// Fallback: concatenate with separators rather than failing.
				synthesized = strings.Join(contents, "\n\n---\n\n")
			}
		}

		// Embed and extract the synthesized content in parallel.
		var (
			embedding []float32
			meta      ExtractedMetadata
			embedErr  error
			wg        sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			embedding, embedErr = ollama.Embed(ctx, synthesized)
		}()
		go func() {
			defer wg.Done()
			meta = ollama.Extract(ctx, synthesized)
		}()
		wg.Wait()

		if embedErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embed synthesized: %v", embedErr)), nil
		}

		in := CreateThoughtInput{
			Content:        synthesized,
			Type:           meta.Type,
			Topics:         meta.Topics,
			People:         meta.People,
			ActionItems:    meta.ActionItems,
			DatesMentioned: meta.DatesMentioned,
			Embedding:      embedding,
			Source:         "mcp-coalesce",
		}

		newID, err := db.CoalesceThoughts(ctx, in, ids)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("coalesce: %v", err)), nil
		}

		out := map[string]any{
			"id":      newID,
			"type":    meta.Type,
			"topics":  meta.Topics,
			"content": synthesized,
			"merged":  ids,
		}
		return jsonResult(out)
	}
}

// jsonResult marshals v and returns it as an MCP text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
