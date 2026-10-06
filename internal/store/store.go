package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sdp-test/repo-analysis-tool/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Migrate(ctx context.Context) error {
	// Create migration tracking table
	s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (name TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)`)

	paths, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, p := range paths {
		name := filepath.Base(p)
		var exists int
		s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _migrations WHERE name=?`, name).Scan(&exists)
		if exists > 0 {
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		s.db.ExecContext(ctx, `INSERT INTO _migrations (name, applied_at) VALUES (?, datetime('now'))`, name)
	}
	return nil
}

// ── Repository CRUD ──

func (s *Store) ListRepositories(ctx context.Context) ([]domain.Repository, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, source_type, remote_url, local_path, storage_key, default_ref, head_commit, status, failure, created_at, updated_at, last_analyzed_at
		FROM repositories ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	repos := []domain.Repository{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *Store) GetRepository(ctx context.Context, id int64) (domain.Repository, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, source_type, remote_url, local_path, storage_key, default_ref, head_commit, status, failure, created_at, updated_at, last_analyzed_at
		FROM repositories WHERE id = ?`, id)
	return scanRepoRow(row)
}

func (s *Store) CreateRepository(ctx context.Context, name, sourceType, remoteURL, localPath string) (domain.Repository, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO repositories (name, source_type, remote_url, local_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, name, sourceType, remoteURL, localPath, domain.RepositoryStatusPending, now, now)
	if err != nil {
		return domain.Repository{}, err
	}
	id, _ := res.LastInsertId()
	return domain.Repository{ID: id, Name: name, SourceType: sourceType, RemoteURL: remoteURL, LocalPath: localPath, Status: domain.RepositoryStatusPending, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) UpdateRepositoryStatus(ctx context.Context, id int64, status domain.RepositoryStatus, failure string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE repositories SET status=?, failure=?, updated_at=? WHERE id=?`,
		status, failure, time.Now().UTC(), id)
	return err
}

func (s *Store) UpdateRepositoryHead(ctx context.Context, id int64, ref, hash, localPath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE repositories SET default_ref=?, head_commit=?, local_path=?, updated_at=?, last_analyzed_at=? WHERE id=?`,
		ref, hash, localPath, time.Now().UTC(), time.Now().UTC(), id)
	return err
}

func (s *Store) DeleteRepository(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id=?`, id)
	return err
}

// ── Analysis Jobs ──

func (s *Store) CreateJob(ctx context.Context, repoID int64, ref string) (domain.AnalysisJob, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO analysis_jobs (repository_id, requested_ref, status, created_at) VALUES (?, ?, 'queued', ?)`,
		repoID, ref, now)
	if err != nil {
		return domain.AnalysisJob{}, err
	}
	id, _ := res.LastInsertId()
	return domain.AnalysisJob{ID: id, RepositoryID: repoID, RequestedRef: ref, Status: "queued", CreatedAt: now}, nil
}

func (s *Store) UpdateJobProgress(ctx context.Context, jobID int64, done, total int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE analysis_jobs SET done_commits=?, total_commits=?, status='running', started_at=COALESCE(started_at, ?) WHERE id=?`,
		done, total, time.Now().UTC(), jobID)
	return err
}

func (s *Store) CompleteJob(ctx context.Context, jobID int64, resolvedCommit string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE analysis_jobs SET status='succeeded', resolved_commit=?, finished_at=? WHERE id=?`,
		resolvedCommit, time.Now().UTC(), jobID)
	return err
}

func (s *Store) FailJob(ctx context.Context, jobID int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE analysis_jobs SET status='failed', failure=?, finished_at=? WHERE id=?`,
		errMsg, time.Now().UTC(), jobID)
	return err
}

func (s *Store) ListJobs(ctx context.Context, repoID int64) ([]domain.AnalysisJob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, repository_id, requested_ref, resolved_commit, status, total_commits, done_commits, failure, created_at, started_at, finished_at
		FROM analysis_jobs WHERE repository_id=? ORDER BY id DESC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.AnalysisJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, repoID, jobID int64) (domain.AnalysisJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository_id, requested_ref, resolved_commit, status, total_commits, done_commits, failure, created_at, started_at, finished_at
		FROM analysis_jobs WHERE id=? AND repository_id=?`, jobID, repoID)
	return scanJobRow(row)
}

func (s *Store) HasRunningJob(ctx context.Context, repoID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analysis_jobs WHERE repository_id=? AND status IN ('queued','running')`, repoID).Scan(&count)
	return count > 0, err
}

// ── Authors ──

func (s *Store) UpsertAuthor(ctx context.Context, repoID int64, name, email, source string) (int64, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO authors (repository_id, canonical_name, canonical_email, source)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repository_id, canonical_name, canonical_email) DO UPDATE SET source=excluded.source`,
		repoID, name, email, source)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM authors WHERE repository_id=? AND canonical_name=? AND canonical_email=?`,
		repoID, name, email).Scan(&id)
	return id, err
}

func (s *Store) UpsertAlias(ctx context.Context, repoID, authorID int64, rawName, rawEmail, resolvedName, resolvedEmail, source string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO author_aliases (repository_id, author_id, raw_name, raw_email, resolved_name, resolved_email, source)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, raw_name, raw_email) DO UPDATE SET author_id=excluded.author_id, resolved_name=excluded.resolved_name, resolved_email=excluded.resolved_email, source=excluded.source`,
		repoID, authorID, rawName, rawEmail, resolvedName, resolvedEmail, source)
	return err
}

func (s *Store) ListAuthors(ctx context.Context, repoID int64) ([]domain.Author, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, repository_id, canonical_name, canonical_email, source FROM authors WHERE repository_id=? ORDER BY canonical_name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var authors []domain.Author
	for rows.Next() {
		var a domain.Author
		if err := rows.Scan(&a.ID, &a.RepositoryID, &a.CanonicalName, &a.CanonicalEmail, &a.Source); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Load aliases
	for i := range authors {
		aliases, err := s.ListAliases(ctx, authors[i].ID)
		if err != nil {
			return nil, err
		}
		authors[i].Aliases = aliases
	}
	return authors, nil
}

func (s *Store) ListAliases(ctx context.Context, authorID int64) ([]domain.AuthorAlias, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, author_id, raw_name, raw_email, resolved_name, resolved_email, source FROM author_aliases WHERE author_id=?`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var aliases []domain.AuthorAlias
	for rows.Next() {
		var a domain.AuthorAlias
		if err := rows.Scan(&a.ID, &a.AuthorID, &a.RawName, &a.RawEmail, &a.ResolvedName, &a.ResolvedEmail, &a.Source); err != nil {
			return nil, err
		}
		aliases = append(aliases, a)
	}
	return aliases, rows.Err()
}

func (s *Store) MergeAuthors(ctx context.Context, repoID, targetID int64, sourceIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, srcID := range sourceIDs {
		if srcID == targetID {
			continue
		}
		// Move aliases
		if _, err := tx.ExecContext(ctx, `UPDATE author_aliases SET author_id=? WHERE author_id=?`, targetID, srcID); err != nil {
			return err
		}
		// Move commits
		if _, err := tx.ExecContext(ctx, `UPDATE commits SET author_id=? WHERE author_id=?`, targetID, srcID); err != nil {
			return err
		}
		// Create alias for the source author identity
		var name, email string
		tx.QueryRowContext(ctx, `SELECT canonical_name, canonical_email FROM authors WHERE id=?`, srcID).Scan(&name, &email)
		if name != "" {
			tx.ExecContext(ctx, `INSERT OR IGNORE INTO author_aliases (repository_id, author_id, raw_name, raw_email, resolved_name, resolved_email, source) VALUES (?, ?, ?, ?, ?, ?, 'manual')`,
				repoID, targetID, name, email, name, email)
		}
		// Delete source author
		tx.ExecContext(ctx, `DELETE FROM authors WHERE id=?`, srcID)
	}
	// Mark target as manual
	tx.ExecContext(ctx, `UPDATE authors SET source='manual' WHERE id=?`, targetID)

	return tx.Commit()
}

func (s *Store) UnmergeAuthor(ctx context.Context, repoID int64, aliasIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, aliasID := range aliasIDs {
		var rawName, rawEmail string
		var currentAuthorID int64
		err := tx.QueryRowContext(ctx, `SELECT author_id, raw_name, raw_email FROM author_aliases WHERE id=?`, aliasID).Scan(&currentAuthorID, &rawName, &rawEmail)
		if err != nil {
			return err
		}
		// Create new author
		res, err := tx.ExecContext(ctx, `INSERT INTO authors (repository_id, canonical_name, canonical_email, source) VALUES (?, ?, ?, 'raw')`,
			repoID, rawName, rawEmail)
		if err != nil {
			return err
		}
		newAuthorID, _ := res.LastInsertId()
		// Move alias
		tx.ExecContext(ctx, `UPDATE author_aliases SET author_id=?, source='raw' WHERE id=?`, newAuthorID, aliasID)
		// Move matching commits
		tx.ExecContext(ctx, `UPDATE commits SET author_id=? WHERE author_id=? AND repository_id=? AND hash IN (
			SELECT c.hash FROM commits c
			JOIN author_aliases aa ON aa.author_id=? AND aa.raw_email=? AND aa.raw_name=?
			WHERE c.repository_id=?
		)`, newAuthorID, currentAuthorID, repoID, newAuthorID, rawEmail, rawName, repoID)
	}

	return tx.Commit()
}

// ── Commits ──

func (s *Store) InsertCommit(ctx context.Context, tx *sql.Tx, repoID int64, hash, parentHash string, authorID int64, committerTS int64, subject string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO commits (repository_id, hash, parent_hash, author_id, committer_timestamp, subject)
		VALUES (?, ?, ?, ?, datetime(?, 'unixepoch'), ?)`,
		repoID, hash, parentHash, authorID, committerTS, subject); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM commits WHERE repository_id=? AND hash=?`,
		repoID, hash).Scan(&id)
	return id, err
}

// ── Objects ──

func (s *Store) UpsertObject(ctx context.Context, tx *sql.Tx, repoID int64, path, objType string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO objects (repository_id, path, object_type) VALUES (?, ?, ?)
		ON CONFLICT(repository_id, path, object_type) DO NOTHING`,
		repoID, path, objType); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM objects WHERE repository_id=? AND path=? AND object_type=?`,
		repoID, path, objType).Scan(&id)
	return id, err
}

// ── Changes ──

func (s *Store) InsertChange(ctx context.Context, tx *sql.Tx, repoID, commitID, objectID, added, removed int64) error {
	growth := added - removed
	churn := added + removed
	_, err := tx.ExecContext(ctx, `
		INSERT INTO changes (repository_id, commit_id, object_id, added, removed, growth, churn)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repository_id, commit_id, object_id) DO UPDATE SET added=added+excluded.added, removed=removed+excluded.removed, growth=growth+excluded.growth, churn=churn+excluded.churn`,
		repoID, commitID, objectID, added, removed, growth, churn)
	return err
}

// ── Cleanup ──

func (s *Store) DeleteRepoData(ctx context.Context, repoID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `DELETE FROM changes WHERE repository_id=?`, repoID)
	tx.ExecContext(ctx, `DELETE FROM objects WHERE repository_id=?`, repoID)
	tx.ExecContext(ctx, `DELETE FROM commits WHERE repository_id=?`, repoID)
	tx.ExecContext(ctx, `DELETE FROM author_aliases WHERE repository_id=?`, repoID)
	tx.ExecContext(ctx, `DELETE FROM authors WHERE repository_id=?`, repoID)
	tx.ExecContext(ctx, `DELETE FROM analysis_jobs WHERE repository_id=?`, repoID)
	return tx.Commit()
}

func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// ── Scanners ──

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepo(rows *sql.Rows) (domain.Repository, error) {
	var r domain.Repository
	var la sql.NullTime
	err := rows.Scan(&r.ID, &r.Name, &r.SourceType, &r.RemoteURL, &r.LocalPath, &r.StorageKey, &r.DefaultRef, &r.HeadCommit, &r.Status, &r.Failure, &r.CreatedAt, &r.UpdatedAt, &la)
	if la.Valid {
		r.LastAnalyzed = &la.Time
	}
	return r, err
}

func scanRepoRow(row *sql.Row) (domain.Repository, error) {
	var r domain.Repository
	var la sql.NullTime
	err := row.Scan(&r.ID, &r.Name, &r.SourceType, &r.RemoteURL, &r.LocalPath, &r.StorageKey, &r.DefaultRef, &r.HeadCommit, &r.Status, &r.Failure, &r.CreatedAt, &r.UpdatedAt, &la)
	if la.Valid {
		r.LastAnalyzed = &la.Time
	}
	return r, err
}

func scanJob(rows *sql.Rows) (domain.AnalysisJob, error) {
	var j domain.AnalysisJob
	var sa, fa sql.NullTime
	err := rows.Scan(&j.ID, &j.RepositoryID, &j.RequestedRef, &j.ResolvedCommit, &j.Status, &j.TotalCommits, &j.DoneCommits, &j.Failure, &j.CreatedAt, &sa, &fa)
	if sa.Valid {
		j.StartedAt = &sa.Time
	}
	if fa.Valid {
		j.FinishedAt = &fa.Time
	}
	return j, err
}

func scanJobRow(row *sql.Row) (domain.AnalysisJob, error) {
	var j domain.AnalysisJob
	var sa, fa sql.NullTime
	err := row.Scan(&j.ID, &j.RepositoryID, &j.RequestedRef, &j.ResolvedCommit, &j.Status, &j.TotalCommits, &j.DoneCommits, &j.Failure, &j.CreatedAt, &sa, &fa)
	if sa.Valid {
		j.StartedAt = &sa.Time
	}
	if fa.Valid {
		j.FinishedAt = &fa.Time
	}
	return j, err
}
