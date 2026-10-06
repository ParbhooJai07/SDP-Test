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

func (s *Store) Migrate(ctx context.Context) error {
	paths, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", path, err)
		}
	}
	return nil
}

func (s *Store) ListRepositories(ctx context.Context) ([]domain.Repository, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, source_type, default_ref, head_commit, status, failure, created_at, updated_at, last_analyzed_at
		FROM repositories
		ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repos := []domain.Repository{}
	for rows.Next() {
		var repo domain.Repository
		var lastAnalyzed sql.NullTime
		if err := rows.Scan(&repo.ID, &repo.Name, &repo.SourceType, &repo.DefaultRef, &repo.HeadCommit, &repo.Status, &repo.Failure, &repo.CreatedAt, &repo.UpdatedAt, &lastAnalyzed); err != nil {
			return nil, err
		}
		if lastAnalyzed.Valid {
			repo.LastAnalyzed = &lastAnalyzed.Time
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (s *Store) CreateRepository(ctx context.Context, name string, sourceType string) (domain.Repository, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO repositories (name, source_type, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, name, sourceType, domain.RepositoryStatusPending, now, now)
	if err != nil {
		return domain.Repository{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.Repository{}, err
	}
	return domain.Repository{ID: id, Name: name, SourceType: sourceType, Status: domain.RepositoryStatusPending, CreatedAt: now, UpdatedAt: now}, nil
}
