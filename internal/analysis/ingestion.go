package analysis

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/sdp-test/repo-analysis-tool/internal/domain"
	"github.com/sdp-test/repo-analysis-tool/internal/gitservice"
	"github.com/sdp-test/repo-analysis-tool/internal/store"
)

func AnalyzeRepository(ctx context.Context, repoID int64, repoPath string, ref string, db *store.Store, jobID int64, git gitservice.Runner, logger *slog.Logger) {
	// 1. Resolve ref
	hash, err := git.ResolveRef(ctx, repoPath, ref)
	if err != nil {
		fail(ctx, db, repoID, jobID, fmt.Sprintf("resolve ref %q: %v", ref, err), logger)
		return
	}

	// 2. List commits
	commits, err := git.ListCommits(ctx, repoPath, ref)
	if err != nil {
		fail(ctx, db, repoID, jobID, fmt.Sprintf("list commits: %v", err), logger)
		return
	}

	total := len(commits)
	if err := db.UpdateJobProgress(ctx, jobID, 0, total); err != nil {
		logger.Error("update job progress", "error", err)
	}
	db.UpdateRepositoryStatus(ctx, repoID, domain.RepositoryStatusAnalyzing, "")

	// 3. Build author map: mailmap-resolved identity -> author ID
	authorCache := make(map[string]int64) // "email|name" -> authorID

	for _, c := range commits {
		resolvedKey := strings.ToLower(c.MailmapEmail) + "|" + c.MailmapName
		if _, ok := authorCache[resolvedKey]; !ok {
			source := "raw"
			if c.MailmapEmail != c.AuthorEmail || c.MailmapName != c.AuthorName {
				source = "mailmap"
			}
			authorID, err := db.UpsertAuthor(ctx, repoID, c.MailmapName, c.MailmapEmail, source)
			if err != nil {
				fail(ctx, db, repoID, jobID, fmt.Sprintf("upsert author: %v", err), logger)
				return
			}
			authorCache[resolvedKey] = authorID
			// Create alias
			db.UpsertAlias(ctx, repoID, authorID, c.AuthorName, c.AuthorEmail, c.MailmapName, c.MailmapEmail, source)
		}
	}

	// 4. Process commits in batches
	const batchSize = 500
	tx, err := db.BeginTx(ctx)
	if err != nil {
		fail(ctx, db, repoID, jobID, fmt.Sprintf("begin tx: %v", err), logger)
		return
	}

	// Ensure root directory object exists
	rootObjID, err := db.UpsertObject(ctx, tx, repoID, "/", "directory")
	if err != nil {
		tx.Rollback()
		fail(ctx, db, repoID, jobID, fmt.Sprintf("upsert root: %v", err), logger)
		return
	}
	_ = rootObjID

	for i, c := range commits {
		if ctx.Err() != nil {
			tx.Rollback()
			fail(ctx, db, repoID, jobID, "context cancelled", logger)
			return
		}

		resolvedKey := strings.ToLower(c.MailmapEmail) + "|" + c.MailmapName
		authorID := authorCache[resolvedKey]

		commitID, err := db.InsertCommit(ctx, tx, repoID, c.Hash, c.ParentHash, authorID, c.CommitterTS, c.Subject)
		if err != nil {
			tx.Rollback()
			fail(ctx, db, repoID, jobID, fmt.Sprintf("insert commit %s: %v", c.Hash, err), logger)
			return
		}

		// Get diffs
		deltas, err := git.DiffCommit(ctx, repoPath, c.Hash, c.ParentHash)
		if err != nil {
			logger.Warn("diff failed, skipping commit", "hash", c.Hash, "error", err)
			continue
		}

		// Track directory aggregates for this commit
		dirChanges := make(map[string][2]int64) // path -> [added, removed]

		for _, d := range deltas {
			if d.IsBinary {
				continue
			}

			filePath := d.Path
			if filePath == "" {
				continue
			}

			// Upsert file object
			fileObjID, err := db.UpsertObject(ctx, tx, repoID, filePath, "file")
			if err != nil {
				logger.Error("upsert file object", "path", filePath, "error", err)
				continue
			}

			// Insert file change
			if err := db.InsertChange(ctx, tx, repoID, commitID, fileObjID, d.Added, d.Removed); err != nil {
				logger.Error("insert file change", "path", filePath, "error", err)
				// Still accumulate directory changes even if file row failed
			}

			// Walk ancestors regardless of file insert outcome
			walkAncestors(filePath, d.Added, d.Removed, dirChanges)
		}

		// Insert directory changes for this commit
		for dirPath, counts := range dirChanges {
			dirObjID, err := db.UpsertObject(ctx, tx, repoID, dirPath, "directory")
			if err != nil {
				logger.Error("upsert dir object", "path", dirPath, "error", err)
				continue
			}
			if err := db.InsertChange(ctx, tx, repoID, commitID, dirObjID, counts[0], counts[1]); err != nil {
				logger.Error("insert dir change", "path", dirPath, "error", err)
			}
		}

		// Batch commit every batchSize
		if (i+1)%batchSize == 0 {
			if err := tx.Commit(); err != nil {
				fail(ctx, db, repoID, jobID, fmt.Sprintf("commit batch: %v", err), logger)
				return
			}
			tx, err = db.BeginTx(ctx)
			if err != nil {
				fail(ctx, db, repoID, jobID, fmt.Sprintf("begin tx: %v", err), logger)
				return
			}
		}

		// Update progress every 100 commits
		if (i+1)%100 == 0 || i == total-1 {
			db.UpdateJobProgress(ctx, jobID, i+1, total)
		}
	}

	// Final commit
	if err := tx.Commit(); err != nil {
		fail(ctx, db, repoID, jobID, fmt.Sprintf("final commit: %v", err), logger)
		return
	}

	// Mark complete
	db.UpdateRepositoryHead(ctx, repoID, ref, hash, repoPath)
	db.UpdateRepositoryStatus(ctx, repoID, domain.RepositoryStatusReady, "")
	db.CompleteJob(ctx, jobID, hash)
	logger.Info("analysis complete", "repoID", repoID, "commits", total)
}

func walkAncestors(filePath string, added, removed int64, dirChanges map[string][2]int64) {
	dir := path.Dir(filePath)
	for {
		if dir == "." || dir == "" {
			dir = "/"
		}
		prev := dirChanges[dir]
		dirChanges[dir] = [2]int64{prev[0] + added, prev[1] + removed}
		if dir == "/" {
			break
		}
		dir = path.Dir(dir)
	}
}

func fail(ctx context.Context, db *store.Store, repoID, jobID int64, msg string, logger *slog.Logger) {
	logger.Error("analysis failed", "repoID", repoID, "jobID", jobID, "error", msg)
	db.FailJob(ctx, jobID, msg)
	db.UpdateRepositoryStatus(ctx, repoID, domain.RepositoryStatusFailed, msg)
}
