package main

import (
	"errors"
	"net/http"
	"strconv"

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
	s.router.Get("/thought/{id}", s.handleDetail)

	// Static files (tokens.css, app.css, vendor/htmx.min.js, ...) served flat
	// under /static/ to match the thought-store convention.
	fs := http.FileServer(http.Dir("static"))
	s.router.Handle("/static/*", http.StripPrefix("/static/", fs))

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
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
