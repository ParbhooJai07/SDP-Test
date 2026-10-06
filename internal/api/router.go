package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sdp-test/repo-analysis-tool/internal/store"
)

type Dependencies struct {
	Store  *store.Store
	Logger *slog.Logger
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/api/repositories", func(w http.ResponseWriter, r *http.Request) {
		repos, err := deps.Store.ListRepositories(r.Context())
		if err != nil {
			deps.Logger.Error("list repositories", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not list repositories.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"repositories": repos})
	})

	return r
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
