package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sdp-test/repo-analysis-tool/internal/api"
	"github.com/sdp-test/repo-analysis-tool/internal/config"
	"github.com/sdp-test/repo-analysis-tool/internal/gitservice"
	"github.com/sdp-test/repo-analysis-tool/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.FromEnv()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	git := gitservice.NewRunner()

	apiRouter := api.NewRouter(api.Dependencies{
		Store:   db,
		Logger:  logger,
		DataDir: cfg.DataDir,
		Git:     git,
	})

	// Compose: API routes + static file serving for React SPA
	mux := http.NewServeMux()

	// Try to serve web/dist as static files
	staticDir := "web/dist"
	if _, err := os.Stat(staticDir); err == nil {
		spa := spaHandler{staticDir: http.Dir(staticDir), apiHandler: apiRouter}
		mux.Handle("/", spa)
	} else {
		// No static build; just serve API
		mux.Handle("/", apiRouter)
	}

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server listening", "addr", cfg.ListenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
}

type spaHandler struct {
	staticDir  http.FileSystem
	apiHandler http.Handler
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// API routes go to the API handler
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.apiHandler.ServeHTTP(w, r)
		return
	}

	// Try to serve the file
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	f, err := h.staticDir.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// SPA fallback: serve index.html
			r.URL.Path = "/"
			http.FileServer(h.staticDir).ServeHTTP(w, r)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	f.Close()
	http.FileServer(h.staticDir).ServeHTTP(w, r)
}
