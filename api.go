package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type APIServer struct {
	db  *sql.DB
	mux *http.ServeMux
}

func NewAPIServer(db *sql.DB) *APIServer {
	s := &APIServer{db: db}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/api/feeds", s.authMiddleware(s.handleFeeds))
	s.mux.HandleFunc("/api/feeds/", s.authMiddleware(s.handleFeedByID))
	s.mux.HandleFunc("/api/articles/search", s.authMiddleware(s.handleArticleSearch))
	s.mux.HandleFunc("/api/articles", s.authMiddleware(s.handleArticles))
	return s
}

func (s *APIServer) Start(addr string) error {
	log.Printf("API server listening on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *APIServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("API_TOKEN")
		if token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

func (s *APIServer) handleFeeds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		feeds, err := ListFeeds(s.db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"feeds": feeds})

	case http.MethodPost:
		var req struct {
			URL       string `json:"url"`
			Title     string `json:"title"`
			ChannelID string `json:"channel_id"`
			Format    string `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.URL == "" {
			jsonError(w, http.StatusBadRequest, "url is required")
			return
		}
		if req.Title == "" {
			req.Title = req.URL
		}
		id, err := AddFeed(s.db, req.URL, req.Title, req.ChannelID, req.Format)
		if err != nil {
			jsonError(w, http.StatusConflict, err.Error())
			return
		}
		jsonResponse(w, http.StatusCreated, map[string]any{"id": id})

	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) handleFeedByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/feeds/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid feed ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		feed, err := GetFeed(s.db, id)
		if err != nil {
			jsonError(w, http.StatusNotFound, "feed not found")
			return
		}
		jsonResponse(w, http.StatusOK, feed)

	case http.MethodPut:
		var req struct {
			URL    string `json:"url"`
			Title  string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.URL == "" {
			jsonError(w, http.StatusBadRequest, "url is required")
			return
		}
		if req.Title == "" {
			req.Title = req.URL
		}
		if err := EditFeed(s.db, id, req.URL, req.Title); err != nil {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "updated"})

	case http.MethodDelete:
		if err := DeleteFeed(s.db, id); err != nil {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) handleArticles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	articles, total, err := ListArticles(s.db, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"articles": articles, "total": total})
}

func (s *APIServer) handleArticleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		jsonError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	articles, total, err := SearchArticles(s.db, q, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"articles": articles, "total": total})
}
