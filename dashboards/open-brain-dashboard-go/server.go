package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
)

type Server struct {
	router *chi.Mux
	db     *DB
	ollama *OllamaClient
}

func NewServer(db *DB, ollama *OllamaClient) *Server {
	s := &Server{
		router: chi.NewRouter(),
		db:     db,
		ollama: ollama,
	}
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	s.router.Get("/", s.handleHome)
	s.router.Get("/browse", s.handleBrowse)
	s.router.Get("/search", s.handleSearch)
	s.router.Post("/capture", s.handleCapture)
	s.router.Get("/thought/{id}", s.handleDetail)
	s.router.Get("/thought/{id}/edit", s.handleEditForm)
	s.router.Post("/thought/{id}/edit", s.handleUpdate)
	s.router.Post("/thought/{id}/delete", s.handleDelete)
	s.router.Get("/partials/action-item-row", s.handleActionItemRow)
	s.router.Get("/v15", s.handleShell)

	// Static files (tokens.css, app.css, vendor/htmx.min.js, ...) served flat
	// under /static/ to match the thought-store convention.
	fs := http.FileServer(http.Dir("static"))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fs))

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// handleShell is the v1.5 master/detail route. During cutover it lives
// at /v15; Task 14 flips it to / and redirects the v1.1 routes. The
// shell template is agnostic about pane content — this handler wires
// placeholder components into each named slot, and later tasks swap
// them out for real sidebar/list/detail renderers.
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

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	data, err := s.db.HomeData(r.Context())
	if err != nil {
		http.Error(w, "home data: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.Home(*data).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := templates.BrowseFilter{
		Type:   q.Get("type"),
		Topic:  q.Get("topic"),
		Person: q.Get("person"),
		Q:      q.Get("q"),
	}
	if daysStr := q.Get("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 3650 {
			f.Days = d
		}
	}
	page := 1
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	data, err := s.db.Browse(r.Context(), f, page)
	if err != nil {
		http.Error(w, "browse: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.Browse(*data).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	mode := q.Get("mode")
	if mode != "text" {
		mode = "semantic"
	}
	page := 1
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// Empty query — just render the landing form, no queries run.
	if query == "" {
		data := templates.SearchData{Mode: mode}
		if err := templates.Search(data).Render(r.Context(), w); err != nil {
			http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var (
		data *templates.SearchData
		err  error
	)

	if mode == "semantic" {
		embedding, embedErr := s.ollama.Embed(r.Context(), query)
		if embedErr != nil {
			// Don't 500 — show the user a graceful error panel with a
			// text-mode retry link. Their query is still valid; we just
			// can't embed it right now.
			data = &templates.SearchData{
				Query:      query,
				Mode:       "semantic",
				EmbedError: embedErr.Error(),
			}
		} else {
			data, err = s.db.SearchSemantic(r.Context(), query, embedding, page)
			if err != nil {
				http.Error(w, "semantic search: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		data, err = s.db.SearchText(r.Context(), query, page)
		if err != nil {
			http.Error(w, "text search: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := templates.Search(*data).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	typ := strings.TrimSpace(r.FormValue("type"))
	if content == "" {
		http.Error(w, "content cannot be empty", http.StatusBadRequest)
		return
	}
	if typ == "" {
		typ = "observation"
	}

	embedding, err := s.ollama.Embed(r.Context(), content)
	if err != nil {
		http.Error(w, "embed failed: "+err.Error()+" (Ollama unreachable? try again in a moment)", http.StatusServiceUnavailable)
		return
	}

	id, err := s.db.CreateThought(r.Context(), CreateThoughtInput{
		Content:   content,
		Type:      typ,
		Topics:    parseCSVField(r.FormValue("topics")),
		People:    parseCSVField(r.FormValue("people")),
		Embedding: embedding,
	})
	if err != nil {
		http.Error(w, "create: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/thought/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "thought id must be an integer", http.StatusBadRequest)
		return
	}
	detail, err := s.db.ThoughtByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "thought not found", http.StatusNotFound)
			return
		}
		http.Error(w, "detail: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.Detail(*detail).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// parseIDParam pulls {id} out of the chi URL param and parses it as an
// int64. Returns the id and ok=true on success; on failure it writes a
// 400 response and returns ok=false so the caller can just `return`.
func parseIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "thought id must be an integer", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// parseCSVField splits a comma-separated input (topics or people) into a
// trimmed, de-duplicated slice. Empty input returns an empty slice, not
// nil, so the JSON patch stays an explicit empty array rather than null.
func parseCSVField(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

// parseActionItems pulls the parallel action_item_description /
// action_item_priority arrays out of a parsed form and pairs them by
// index, dropping any row with an empty description. Returns an empty
// (non-nil) slice when no action items were submitted so the metadata
// jsonb patch writes an explicit empty array rather than null.
func parseActionItems(form url.Values) []templates.ActionItem {
	descriptions := form["action_item_description"]
	priorities := form["action_item_priority"]
	out := []templates.ActionItem{}
	for i, d := range descriptions {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		p := ""
		if i < len(priorities) {
			p = strings.TrimSpace(priorities[i])
		}
		out = append(out, templates.ActionItem{Description: d, Priority: p})
	}
	return out
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	detail, err := s.db.ThoughtByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "thought not found", http.StatusNotFound)
			return
		}
		http.Error(w, "load thought: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := templates.EditFormData{
		Thought:      *detail,
		ContentInput: detail.Content,
		TypeInput:    detail.Type,
		TopicsInput:  templates.RenderableTopics(detail.Topics),
		PeopleInput:  templates.RenderableTopics(detail.People),
	}
	if err := templates.DetailEdit(data).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	existing, err := s.db.ThoughtByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "thought not found", http.StatusNotFound)
			return
		}
		http.Error(w, "load thought: "+err.Error(), http.StatusInternalServerError)
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	typ := strings.TrimSpace(r.FormValue("type"))
	topicsRaw := r.FormValue("topics")
	peopleRaw := r.FormValue("people")
	actionItems := parseActionItems(r.Form)

	renderEditError := func(msg string) {
		// Echo the user-edited thought back with ActionItems updated from
		// the POST body so the re-rendered form shows what they just
		// typed, not the pre-edit state.
		echo := *existing
		echo.ActionItems = actionItems
		data := templates.EditFormData{
			Thought:      echo,
			Error:        msg,
			ContentInput: content,
			TypeInput:    typ,
			TopicsInput:  topicsRaw,
			PeopleInput:  peopleRaw,
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = templates.DetailEdit(data).Render(r.Context(), w)
	}

	if content == "" {
		renderEditError("content cannot be empty")
		return
	}

	input := UpdateThoughtInput{
		Content:     content,
		Type:        typ,
		Topics:      parseCSVField(topicsRaw),
		People:      parseCSVField(peopleRaw),
		ActionItems: actionItems,
	}

	// Re-embed only when content actually changed. Metadata-only edits
	// skip Ollama entirely, which keeps edit-save latency close to a
	// single round-trip when you're just retagging.
	if content != existing.Content {
		embedding, embedErr := s.ollama.Embed(r.Context(), content)
		if embedErr != nil {
			renderEditError("embedding failed: " + embedErr.Error() + " (try again in a moment, don't save stale embeddings)")
			return
		}
		input.Embedding = embedding
	}

	if err := s.db.UpdateThought(r.Context(), id, input); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "thought not found", http.StatusNotFound)
			return
		}
		renderEditError(err.Error())
		return
	}

	http.Redirect(w, r, "/thought/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// handleActionItemRow returns a single blank ActionItemRow HTML fragment
// used by htmx to append a new row to the action-items section of the
// edit form. No layout wrapper — the response is swapped into an existing
// list via hx-swap="beforeend".
func (s *Server) handleActionItemRow(w http.ResponseWriter, r *http.Request) {
	if err := templates.ActionItemRow(templates.ActionItem{}).Render(r.Context(), w); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteThought(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "thought not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
