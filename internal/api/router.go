package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sdp-test/repo-analysis-tool/internal/analysis"
	"github.com/sdp-test/repo-analysis-tool/internal/domain"
	"github.com/sdp-test/repo-analysis-tool/internal/gitservice"
	"github.com/sdp-test/repo-analysis-tool/internal/store"
)

type Dependencies struct {
	Store   *store.Store
	Logger  *slog.Logger
	DataDir string
	Git     gitservice.Runner
}

const maxUploadSize = 500 << 20 // 500 MB

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(corsMiddleware)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/repositories", func(r chi.Router) {
		r.Get("/", listRepos(deps))
		r.Post("/clone", cloneRepo(deps))
		r.Post("/upload", uploadRepo(deps))

		r.Route("/{repoID}", func(r chi.Router) {
			r.Get("/", getRepo(deps))
			r.Delete("/", deleteRepo(deps))
			r.Post("/reanalyze", reanalyzeRepo(deps))

			r.Get("/jobs", listJobs(deps))
			r.Get("/jobs/{jobID}", getJob(deps))

			r.Get("/authors", listAuthors(deps))
			r.Post("/authors/merge", mergeAuthors(deps))
			r.Post("/authors/unmerge", unmergeAuthors(deps))

			r.Get("/objects", objectTree(deps))
			r.Get("/commits", listCommits(deps))

			r.Get("/metrics/summary", metricsSummary(deps))
			r.Get("/metrics/objects", metricsObjects(deps))
			r.Get("/metrics/authors", metricsAuthors(deps))
			r.Get("/metrics/timeseries", metricsTimeSeries(deps))
		})
	})

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Repository endpoints ──

func listRepos(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repos, err := deps.Store.ListRepositories(r.Context())
		if err != nil {
			deps.Logger.Error("list repositories", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not list repositories.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"repositories": repos})
	}
}

func getRepo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, err := getRepoByParam(r, deps)
		if err != nil {
			writeError(w, http.StatusNotFound, "Repository not found.")
			return
		}
		writeJSON(w, http.StatusOK, repo)
	}
}

func deleteRepo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, err := getRepoByParam(r, deps)
		if err != nil {
			writeError(w, http.StatusNotFound, "Repository not found.")
			return
		}
		deps.Store.DeleteRepoData(r.Context(), repo.ID)
		deps.Store.DeleteRepository(r.Context(), repo.ID)
		if repo.LocalPath != "" {
			os.RemoveAll(repo.LocalPath)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func cloneRepo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if body.URL == "" {
			writeError(w, http.StatusBadRequest, "URL is required.")
			return
		}
		if !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") && !strings.HasPrefix(body.URL, "git@") {
			writeError(w, http.StatusBadRequest, "Invalid repository URL.")
			return
		}
		if body.Name == "" {
			parts := strings.Split(strings.TrimSuffix(body.URL, ".git"), "/")
			body.Name = parts[len(parts)-1]
		}

		repo, err := deps.Store.CreateRepository(r.Context(), body.Name, "remote", body.URL, "")
		if err != nil {
			deps.Logger.Error("create repository", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not create repository.")
			return
		}

		job, err := deps.Store.CreateJob(r.Context(), repo.ID, "HEAD")
		if err != nil {
			deps.Logger.Error("create job", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not create analysis job.")
			return
		}

		// Background clone + analysis
		go func() {
			ctx := context.Background()
			destPath := filepath.Join(deps.DataDir, "repos", fmt.Sprintf("%d", repo.ID))
			os.MkdirAll(filepath.Dir(destPath), 0o755)

			if err := deps.Git.CloneRepo(ctx, body.URL, destPath); err != nil {
				deps.Logger.Error("clone failed", "repoID", repo.ID, "error", err)
				deps.Store.FailJob(ctx, job.ID, "Clone failed.")
				deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusFailed, "Clone failed.")
				return
			}

			deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusAnalyzing, "")
			analysis.AnalyzeRepository(ctx, repo.ID, destPath, "HEAD", deps.Store, job.ID, deps.Git, deps.Logger)
		}()

		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, http.StatusAccepted, map[string]any{"repository": repo, "jobID": job.ID})
	}
}

func uploadRepo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
		if err := r.ParseMultipartForm(maxUploadSize); err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "Upload exceeds 500 MB limit.")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "Missing file upload.")
			return
		}
		defer file.Close()

		if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
			writeError(w, http.StatusBadRequest, "Only .zip files are accepted.")
			return
		}

		name := r.FormValue("name")
		if name == "" {
			name = strings.TrimSuffix(header.Filename, ".zip")
		}

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "rat-upload-*.zip")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not save upload.")
			return
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := io.Copy(tmpFile, file); err != nil {
			tmpFile.Close()
			writeError(w, http.StatusInternalServerError, "Could not save upload.")
			return
		}
		tmpFile.Close()

		repo, err := deps.Store.CreateRepository(r.Context(), name, "upload", "", "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not create repository.")
			return
		}
		job, err := deps.Store.CreateJob(r.Context(), repo.ID, "HEAD")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not create analysis job.")
			return
		}

		// Copy tmpFile for background processing
		bgPath := filepath.Join(deps.DataDir, "uploads", fmt.Sprintf("%d.zip", repo.ID))
		os.MkdirAll(filepath.Dir(bgPath), 0o755)
		copyFile(tmpPath, bgPath)

		go func() {
			ctx := context.Background()
			defer os.Remove(bgPath)

			destPath := filepath.Join(deps.DataDir, "repos", fmt.Sprintf("%d", repo.ID))

			// Detect GitHub source archives BEFORE extraction: they have no git
			// history and cannot be analysed. Return a targeted error immediately.
			if hint := detectGitHubSourceZip(bgPath); hint != "" {
				msg := "This looks like a GitHub source download (no git history). " +
					"Use \"Clone from URL\" with the repository URL instead. " + hint
				deps.Store.FailJob(ctx, job.ID, msg)
				deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusFailed, msg)
				return
			}

			if err := extractZip(bgPath, destPath); err != nil {
				deps.Logger.Error("extract zip failed", "repoID", repo.ID, "error", err)
				deps.Store.FailJob(ctx, job.ID, "Failed to extract ZIP: "+err.Error())
				deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusFailed, "Failed to extract ZIP.")
				return
			}

			// Find usable git repository inside the extracted archive.
			// Supports:
			//   • Working trees with a .git subdirectory or file
			//   • Bare / mirror repos where git objects live at the top level
			repoRoot, err := findRepoRoot(ctx, destPath, deps.Git)
			if err != nil {
				msg := "No git repository found in archive. " +
					"Upload a ZIP created from \"git clone\", not a GitHub source download. " +
					"Use \"Clone from URL\" to import directly from GitHub."
				deps.Store.FailJob(ctx, job.ID, msg)
				deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusFailed, msg)
				return
			}

			deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusAnalyzing, "")
			analysis.AnalyzeRepository(ctx, repo.ID, repoRoot, "HEAD", deps.Store, job.ID, deps.Git, deps.Logger)
		}()

		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, http.StatusAccepted, map[string]any{"repository": repo, "jobID": job.ID})
	}
}

func reanalyzeRepo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, err := getRepoByParam(r, deps)
		if err != nil {
			writeError(w, http.StatusNotFound, "Repository not found.")
			return
		}

		running, _ := deps.Store.HasRunningJob(r.Context(), repo.ID)
		if running {
			writeError(w, http.StatusConflict, "Analysis is already running.")
			return
		}

		// Clear existing data
		deps.Store.DeleteRepoData(r.Context(), repo.ID)

		job, err := deps.Store.CreateJob(r.Context(), repo.ID, "HEAD")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not create job.")
			return
		}

		repoPath := repo.LocalPath
		if repoPath == "" {
			repoPath = filepath.Join(deps.DataDir, "repos", fmt.Sprintf("%d", repo.ID))
		}

		go func() {
			ctx := context.Background()
			// For uploaded repos the LocalPath may point to the raw extraction
			// directory. Re-run findRepoRoot so we never accidentally traverse up
			// to the host project's .git.
			actualPath := repoPath
			if repo.SourceType == "upload" {
				if found, err := findRepoRoot(ctx, repoPath, deps.Git); err == nil {
					actualPath = found
				} else {
					deps.Store.FailJob(ctx, job.ID, "No git repository found: "+err.Error())
					deps.Store.UpdateRepositoryStatus(ctx, repo.ID, domain.RepositoryStatusFailed, "No git repository found in archive.")
					return
				}
			}
			analysis.AnalyzeRepository(ctx, repo.ID, actualPath, "HEAD", deps.Store, job.ID, deps.Git, deps.Logger)
		}()

		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, http.StatusAccepted, map[string]any{"jobID": job.ID})
	}
}

// ── Job endpoints ──

func listJobs(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		jobs, err := deps.Store.ListJobs(r.Context(), repoID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list jobs.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	}
}

func getJob(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, _ := parseRepoID(r)
		jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid job ID.")
			return
		}
		job, err := deps.Store.GetJob(r.Context(), repoID, jobID)
		if err != nil {
			writeError(w, http.StatusNotFound, "Job not found.")
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}

// ── Author endpoints ──

func listAuthors(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		authors, err := deps.Store.ListAuthors(r.Context(), repoID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list authors.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authors": authors})
	}
}

func mergeAuthors(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		var body struct {
			TargetID  int64   `json:"targetID"`
			SourceIDs []int64 `json:"sourceIDs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if body.TargetID == 0 || len(body.SourceIDs) == 0 {
			writeError(w, http.StatusBadRequest, "targetID and sourceIDs are required.")
			return
		}
		if err := deps.Store.MergeAuthors(r.Context(), repoID, body.TargetID, body.SourceIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "Merge failed.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "merged"})
	}
}

func unmergeAuthors(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		var body struct {
			AuthorID int64   `json:"authorID"`
			AliasIDs []int64 `json:"aliasIDs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if len(body.AliasIDs) == 0 {
			writeError(w, http.StatusBadRequest, "aliasIDs is required.")
			return
		}
		if err := deps.Store.UnmergeAuthor(r.Context(), repoID, body.AliasIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "Unmerge failed.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "unmerged"})
	}
}

// ── Object/Commit endpoints ──

func objectTree(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		parentPath := r.URL.Query().Get("path")
		objs, err := store.GetObjectTree(r.Context(), deps.Store, repoID, parentPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list objects.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"objects": objs})
	}
}

func listCommits(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID, err := parseRepoID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid repository ID.")
			return
		}
		page := parseIntParam(r, "page", 1)
		limit := parseIntParam(r, "limit", 50)
		fromTS := parseOptionalInt64(r, "from")
		toTS := parseOptionalInt64(r, "to")

		commits, total, err := store.ListCommits(r.Context(), deps.Store, repoID, fromTS, toTS, page, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list commits.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"commits": commits, "total": total, "page": page, "limit": limit})
	}
}

// ── Metric endpoints ──

func metricsSummary(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, err := parseFilterParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		summary, err := store.GetRepositorySummary(r.Context(), deps.Store, fp)
		if err != nil {
			deps.Logger.Error("metrics summary", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not compute summary.")
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func metricsObjects(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, err := parseFilterParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		page := parseIntParam(r, "page", 1)
		limit := parseIntParam(r, "limit", 20)
		sortCol := r.URL.Query().Get("sort")
		sortDir := r.URL.Query().Get("dir")

		metrics, total, err := store.GetObjectMetrics(r.Context(), deps.Store, fp, sortCol, sortDir, page, limit)
		if err != nil {
			deps.Logger.Error("metrics objects", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not compute object metrics.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics, "total": total, "page": page, "limit": limit})
	}
}

func metricsAuthors(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, err := parseFilterParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		metrics, err := store.GetAuthorMetrics(r.Context(), deps.Store, fp)
		if err != nil {
			deps.Logger.Error("metrics authors", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not compute author metrics.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authors": metrics})
	}
}

func metricsTimeSeries(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, err := parseFilterParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		points, err := store.GetCommitTimeSeries(r.Context(), deps.Store, fp)
		if err != nil {
			deps.Logger.Error("metrics timeseries", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not compute time series.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"series": points})
	}
}

// ── Helpers ──

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseRepoID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "repoID"), 10, 64)
}

func getRepoByParam(r *http.Request, deps Dependencies) (domain.Repository, error) {
	id, err := parseRepoID(r)
	if err != nil {
		return domain.Repository{}, err
	}
	return deps.Store.GetRepository(r.Context(), id)
}

func parseFilterParams(r *http.Request) (domain.FilterParams, error) {
	repoID, err := parseRepoID(r)
	if err != nil {
		return domain.FilterParams{}, fmt.Errorf("invalid repository ID")
	}

	fp := domain.FilterParams{
		RepoID:     repoID,
		ObjectPath: r.URL.Query().Get("path"),
		ObjectType: r.URL.Query().Get("type"),
	}

	if ids := r.URL.Query().Get("authorIDs"); ids != "" {
		for _, s := range strings.Split(ids, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return fp, fmt.Errorf("invalid authorID: %s", s)
			}
			fp.AuthorIDs = append(fp.AuthorIDs, id)
		}
	}

	if from := r.URL.Query().Get("from"); from != "" {
		v, err := strconv.ParseInt(from, 10, 64)
		if err != nil {
			return fp, fmt.Errorf("invalid from timestamp")
		}
		fp.FromTS = &v
	}
	if to := r.URL.Query().Get("to"); to != "" {
		v, err := strconv.ParseInt(to, 10, 64)
		if err != nil {
			return fp, fmt.Errorf("invalid to timestamp")
		}
		fp.ToTS = &v
	}
	if hashes := r.URL.Query().Get("commits"); hashes != "" {
		for _, h := range strings.Split(hashes, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				fp.CommitHashes = append(fp.CommitHashes, h)
			}
		}
	}

	return fp, nil
}

func parseIntParam(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return def
	}
	return v
}

func parseOptionalInt64(r *http.Request, key string) *int64 {
	s := r.URL.Query().Get(key)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// ── ZIP extraction ──

// detectGitHubSourceZip returns a non-empty hint string when zipPath looks like
// a GitHub "Download ZIP" source archive (no git history). GitHub source ZIPs:
//   - have a single top-level directory named "<repo>-<branch>" or "<repo>-<sha>"
//   - contain no .git entry
//   - optionally carry a ZIP comment that is a 40-char commit SHA
//
// Returns "" when the ZIP may contain a real git repository.
func detectGitHubSourceZip(zipPath string) string {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ""
	}
	defer r.Close()

	hasGit := false
	topDirs := map[string]struct{}{}
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, ".git") {
			hasGit = true
			break
		}
		// Collect unique top-level directory names
		parts := strings.SplitN(name, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			topDirs[parts[0]] = struct{}{}
		}
	}

	if hasGit {
		return ""
	}

	// GitHub source ZIPs always have exactly one top-level directory.
	if len(topDirs) != 1 {
		return ""
	}
	for topDir := range topDirs {
		// GitHub names it "<repo>-<branch>" e.g. "cJSON-master"
		// Extract the repo name (everything before the last dash).
		if idx := strings.LastIndex(topDir, "-"); idx > 0 {
			repoName := topDir[:idx]
			return fmt.Sprintf(
				"Suggested URL: https://github.com/<username>/%s.git", repoName,
			)
		}
	}
	return "This appears to be a source-only archive with no git history."
}

func extractZip(zipPath, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	os.MkdirAll(destDir, 0o755)

	for _, f := range reader.File {
		// Sanitize path
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
			continue // Skip path traversal attempts
		}

		target := filepath.Join(destDir, cleanName)
		// Verify target is within destDir
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}

		os.MkdirAll(filepath.Dir(target), 0o755)

		rc, err := f.Open()
		if err != nil {
			return err
		}
		outFile, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findRepoRoot locates a usable git repository inside the extracted archive root.
// IMPORTANT: we must NOT call git rev-parse without constraining the search
// directory, because git traverses upward and would find the host project's
// .git when the data directory is nested inside it.
// Instead we use pure filesystem checks that never leave the candidate path.
func findRepoRoot(ctx context.Context, archiveRoot string, git gitservice.Runner) (string, error) {
	// Pass 1: look for a .git entry anywhere in the tree (working-tree ZIPs).
	var dotGit string
	filepath.Walk(archiveRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || dotGit != "" {
			return nil
		}
		if info.Name() == ".git" {
			dotGit = path
			return filepath.SkipAll
		}
		return nil
	})
	if dotGit != "" {
		repoRoot := filepath.Dir(dotGit)
		fi, err := os.Stat(dotGit)
		if err == nil {
			if fi.IsDir() {
				// .git directory — classic working tree
				return repoRoot, nil
			}
			// .git file — gitlink (submodule / worktree pointer)
			// Trust it; the real git-dir is elsewhere but the work-tree is here.
			return repoRoot, nil
		}
	}

	// Pass 2: archive root itself is a bare repo.
	if looksLikeBareRepo(archiveRoot) {
		return archiveRoot, nil
	}

	// Pass 3: a single immediate subdirectory is a bare repo
	// (e.g. zip -r repo.zip repo.git/).
	entries, err := os.ReadDir(archiveRoot)
	if err != nil {
		return "", fmt.Errorf("cannot list archive contents: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(archiveRoot, e.Name())
		if looksLikeBareRepo(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("the archive does not contain a git repository (.git directory not found and no bare-repo structure detected). Please upload a ZIP that was created from a git clone, not a GitHub source download")
}

// looksLikeBareRepo checks whether dir contains the core files of a bare git
// repository WITHOUT invoking git (which would traverse parent directories).
// A bare repo always has: HEAD, objects/, refs/
func looksLikeBareRepo(dir string) bool {
	for _, name := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	// Make sure HEAD actually looks like a git HEAD file.
	head, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(head))
	return strings.HasPrefix(s, "ref: ") || (len(s) == 40 || len(s) == 64) // ref or detached SHA
}


func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
