package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NateBJones-Projects/OB1/dashboards/open-brain-dashboard-go/templates"
	"github.com/a-h/templ"
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
	s.router.Get("/partials/list", s.handlePartialList)
	s.router.Get("/partials/detail/{id}", s.handlePartialDetail)
	s.router.Get("/partials/detail/{id}/edit", s.handlePartialDetailEdit)
	s.router.Get("/partials/row/{id}", s.handlePartialRow)

	// Static files (tokens.css, app.css, vendor/htmx.min.js, ...) served flat
	// under /static/ to match the thought-store convention.
	fs := http.FileServer(http.Dir("static"))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fs))

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// embed is the dashboard's single entry point to the Ollama embedding
// endpoint. Callers pass the user's search query; the method returns
// the vector on success or nil on any failure (Ollama down, network
// blip, non-200 response, etc.), which is the contract DB.Search
// expects to decide between the semantic and text-only paths. Task 11
// wraps this in an actor-pattern health tracker; for now it's a thin
// pass-through.
func (s *Server) embed(ctx context.Context, q string) []float32 {
	emb, err := s.ollama.Embed(ctx, q)
	if err != nil {
		return nil
	}
	return emb
}

// selectedIDFromQuery parses the ?id=N query parameter into an int64,
// returning 0 when the parameter is missing or malformed. Used by
// handleShell and handlePartialList to render the list row for the
// currently-open thought in its --selected state.
func selectedIDFromQuery(r *http.Request) int64 {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		return 0
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// handleShell is the v1.5 master/detail route. During cutover it lives
// at /v15; Task 14 flips it to / and redirects the v1.1 routes. The
// handler renders the sidebar and list panes with real data; the
// detail pane stays a placeholder until Task 8 lands.
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filters := parseListFilters(r)

	sidebarCounts, err := s.db.SidebarCounts(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var embedding []float32
	if filters.Q != "" {
		embedding = s.embed(ctx, filters.Q)
	}

	result, err := s.db.Search(ctx, filters, embedding)
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

	selectedID := selectedIDFromQuery(r)
	listData := templates.ListData{
		Result:       result,
		SelectedID:   selectedID,
		OllamaStatus: "grey", // Task 11 will populate from an actor
	}

	// Detail pane: when ?id=N is present and the lookup succeeds, render
	// the real DetailRead; otherwise fall back to DetailEmpty. We
	// deliberately do NOT 500 the whole page when the id is stale or
	// malformed — the list row just stops highlighting and the pane
	// goes back to the placeholder. Direct navigation and page refresh
	// stay bookmarkable.
	var detailComponent templ.Component = templates.DetailEmpty()
	if selectedID > 0 {
		detail, derr := s.db.ThoughtByID(ctx, selectedID)
		if derr == nil {
			detailComponent = templates.DetailRead(detail, filters)
		}
	}

	vm := templates.ShellViewModel{
		Sidebar: templates.Sidebar(sidebarData),
		List:    templates.List(listData),
		Detail:  detailComponent,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Shell(vm).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handlePartialDetail renders just the #detail-pane contents for htmx
// row-click swaps. The response is the raw detail-inner HTML — no
// <html> wrapper — so htmx can swap it straight into #detail-pane
// without re-rendering the shell. Filters from the query string are
// threaded through so the Edit link round-trips the active sidebar
// state.
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

// handlePartialDetailEdit renders the v1.5 in-pane edit form into
// #detail-pane. Triggered by the Edit button in DetailRead's toolbar.
// Threads the current sidebar/list filter set through so Cancel
// round-trips back to the same filtered context.
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

// handlePartialRow renders a single list-row <li> so the
// hx-trigger="refresh-row-{id} from:body" listener on that row can
// replace itself with a fresh copy after an edit. Soft-fails to an
// empty 200 response when the thought is gone so the row just
// disappears.
func (s *Server) handlePartialRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	detail, err := s.db.ThoughtByID(ctx, id)
	if err != nil {
		// Soft-fail: empty row makes the <li> vanish from the DOM.
		w.WriteHeader(http.StatusOK)
		return
	}
	// Project ThoughtDetail back down to the ThoughtData view model
	// listRow expects. ThoughtDetail embeds ThoughtData so we can
	// hand it over directly.
	t := detail.ThoughtData
	filters := parseListFilters(r)
	selectedID := selectedIDFromQuery(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ListRowStandalone(t, selectedID, filters).Render(ctx, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handlePartialList renders just the #list-pane contents for htmx
// swaps triggered by sidebar chip clicks, search input, pagination,
// and sort toggles. The response is the raw list-inner HTML — no
// <html> wrapper — so htmx can swap it straight into #list-pane.
func (s *Server) handlePartialList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	vm := templates.ListData{
		Result:       result,
		SelectedID:   selectedIDFromQuery(r),
		OllamaStatus: "grey", // Task 11 will populate from an actor
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.List(vm).Render(ctx, w); err != nil {
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
	if err := templates.DetailEditV11(data).Render(r.Context(), w); err != nil {
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
	isHTMX := r.Header.Get("HX-Request") == "true"
	// v1.5 edit form carries the active sidebar/list filter set as a
	// single URL-encoded string so we can re-render the list context
	// around the saved thought. Non-htmx (v1.1) callers don't set it
	// and that's fine — the 303 fallback lands on the plain detail
	// page where filters don't matter.
	v15Filters := parseFiltersFromReturnQuery(r.FormValue("return_filters"))

	renderEditError := func(msg string) {
		if isHTMX {
			// v1.5 in-pane: re-render DetailEdit with the user's
			// in-progress values echoed back. ActionItems come from
			// the parsed form; content/type/topics/people come from
			// the ThoughtDetail we stamp below.
			echo := *existing
			echo.Content = content
			echo.Type = typ
			echo.Topics = parseCSVField(topicsRaw)
			echo.People = parseCSVField(peopleRaw)
			echo.ActionItems = actionItems
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = templates.DetailEdit(&echo, v15Filters, msg).Render(r.Context(), w)
			return
		}
		// v1.1 full-page fallback.
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
		_ = templates.DetailEditV11(data).Render(r.Context(), w)
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

	if isHTMX {
		// Re-fetch the fresh row and return the DetailRead fragment
		// plus HX-Trigger so the list row re-fetches itself, and
		// HX-Push-Url so the address bar tracks the read view.
		detail, derr := s.db.ThoughtByID(r.Context(), id)
		if derr != nil {
			http.Error(w, "reload thought: "+derr.Error(), http.StatusInternalServerError)
			return
		}
		pushQuery := filtersToURLString(v15Filters)
		pushURL := "/v15?id=" + strconv.FormatInt(id, 10)
		if pushQuery != "" {
			pushURL = "/v15?" + pushQuery + "&id=" + strconv.FormatInt(id, 10)
		}
		w.Header().Set("HX-Trigger", "refresh-row-"+strconv.FormatInt(id, 10))
		w.Header().Set("HX-Push-Url", pushURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if rerr := templates.DetailRead(detail, v15Filters).Render(r.Context(), w); rerr != nil {
			http.Error(w, "render: "+rerr.Error(), http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/thought/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// parseFiltersFromReturnQuery decodes the v1.5 edit form's
// return_filters hidden input (a URL-encoded query string) back into
// a ListFilters. Missing or malformed input returns a zero-value
// struct, which renders as "no active filter".
func parseFiltersFromReturnQuery(raw string) templates.ListFilters {
	f := templates.ListFilters{PerPage: 50, Page: 1}
	if raw == "" {
		return f
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		return f
	}
	f.Type = v.Get("type")
	f.Topic = v.Get("topic")
	f.Person = v.Get("person")
	f.Q = v.Get("q")
	if d, err := strconv.Atoi(v.Get("days")); err == nil && d > 0 {
		f.Days = d
	}
	if p, err := strconv.Atoi(v.Get("page")); err == nil && p > 0 {
		f.Page = p
	}
	return f
}

// filtersToURLString is the exported form of the templates package's
// internal filtersToURL helper — kept here so server.go can assemble
// HX-Push-Url values without exporting the template-internal helper.
func filtersToURLString(f templates.ListFilters) string {
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
		v.Set("days", strconv.Itoa(f.Days))
	}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	return v.Encode()
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
