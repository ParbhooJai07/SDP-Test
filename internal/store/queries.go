package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/sdp-test/repo-analysis-tool/internal/domain"
)

// buildCommitCTE constructs a CTE selecting commit IDs matching the filter.
func buildCommitCTE(fp domain.FilterParams) (string, []any) {
	var conds []string
	var args []any

	conds = append(conds, "c.repository_id = ?")
	args = append(args, fp.RepoID)

	if len(fp.CommitHashes) > 0 {
		placeholders := make([]string, len(fp.CommitHashes))
		for i, h := range fp.CommitHashes {
			placeholders[i] = "?"
			args = append(args, h)
		}
		conds = append(conds, fmt.Sprintf("c.hash IN (%s)", strings.Join(placeholders, ",")))
	} else {
		if fp.FromTS != nil {
			conds = append(conds, "c.committer_timestamp >= datetime(?, 'unixepoch')")
			args = append(args, *fp.FromTS)
		}
		if fp.ToTS != nil {
			conds = append(conds, "c.committer_timestamp < datetime(?, 'unixepoch')")
			args = append(args, *fp.ToTS)
		}
	}

	if len(fp.AuthorIDs) > 0 {
		placeholders := make([]string, len(fp.AuthorIDs))
		for i, id := range fp.AuthorIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conds = append(conds, fmt.Sprintf("c.author_id IN (%s)", strings.Join(placeholders, ",")))
	}

	cte := fmt.Sprintf("WITH commit_set AS (SELECT c.id FROM commits c WHERE %s)", strings.Join(conds, " AND "))
	return cte, args
}

func GetCommitSetSize(ctx context.Context, s *Store, fp domain.FilterParams) (int64, error) {
	cte, args := buildCommitCTE(fp)
	var count int64
	err := s.db.QueryRowContext(ctx, cte+` SELECT COUNT(*) FROM commit_set`, args...).Scan(&count)
	return count, err
}

func GetRepositorySummary(ctx context.Context, s *Store, fp domain.FilterParams) (domain.SummaryMetric, error) {
	cte, args := buildCommitCTE(fp)

	query := cte + `
	SELECT
		COALESCE(SUM(ch.added), 0),
		COALESCE(SUM(ch.removed), 0),
		COALESCE(SUM(ch.added - ch.removed), 0),
		COALESCE(SUM(ch.added + ch.removed), 0),
		COUNT(DISTINCT CASE WHEN (ch.added + ch.removed) > 0 THEN ch.commit_id END),
		(SELECT COUNT(*) FROM commit_set)
	FROM changes ch
	JOIN commit_set cs ON cs.id = ch.commit_id
	JOIN objects o ON o.id = ch.object_id
	WHERE o.repository_id = ? AND o.path = '/' AND o.object_type = 'directory'`
	args = append(args, fp.RepoID)

	var m domain.SummaryMetric
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&m.Added, &m.Removed, &m.Growth, &m.Churn, &m.Modifications, &m.CommitCount)
	if err != nil {
		return m, err
	}
	if m.CommitCount > 0 {
		m.ModificationFrequency = float64(m.Modifications) / float64(m.CommitCount)
		m.ChurnRate = float64(m.Churn) / float64(m.CommitCount)
	}
	return m, nil
}

func GetObjectMetrics(ctx context.Context, s *Store, fp domain.FilterParams, sortCol, sortDir string, page, limit int) ([]domain.ObjectMetric, int64, error) {
	cte, args := buildCommitCTE(fp)

	var objConds []string
	objConds = append(objConds, "o.repository_id = ?")
	args = append(args, fp.RepoID)

	if fp.ObjectPath != "" {
		objConds = append(objConds, "o.path LIKE ?")
		args = append(args, fp.ObjectPath+"%")
	}
	if fp.ObjectType != "" {
		objConds = append(objConds, "o.object_type = ?")
		args = append(args, fp.ObjectType)
	}

	objWhere := strings.Join(objConds, " AND ")

	// Count total
	countQuery := cte + fmt.Sprintf(`
	SELECT COUNT(DISTINCT o.id)
	FROM objects o
	LEFT JOIN changes ch ON ch.object_id = o.id
	LEFT JOIN commit_set cs ON cs.id = ch.commit_id
	WHERE %s`, objWhere)

	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Commit set size for rates
	cteSize, sizeArgs := buildCommitCTE(fp)
	var commitCount int64
	s.db.QueryRowContext(ctx, cteSize+` SELECT COUNT(*) FROM commit_set`, sizeArgs...).Scan(&commitCount)

	// Allowed sort columns
	allowedSort := map[string]string{
		"path": "o.path", "type": "o.object_type", "added": "total_added", "removed": "total_removed",
		"growth": "total_growth", "churn": "total_churn", "modifications": "mods", "modFrequency": "mods", "churnRate": "total_churn",
	}
	orderExpr := "total_churn DESC"
	if col, ok := allowedSort[sortCol]; ok {
		dir := "ASC"
		if strings.EqualFold(sortDir, "desc") {
			dir = "DESC"
		}
		orderExpr = col + " " + dir
	}

	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := 0
	if page > 1 {
		offset = (page - 1) * limit
	}

	query := cte + fmt.Sprintf(`
	SELECT
		o.path,
		o.object_type,
		COALESCE(SUM(ch.added), 0) AS total_added,
		COALESCE(SUM(ch.removed), 0) AS total_removed,
		COALESCE(SUM(ch.added - ch.removed), 0) AS total_growth,
		COALESCE(SUM(ch.added + ch.removed), 0) AS total_churn,
		COUNT(DISTINCT CASE WHEN (ch.added + ch.removed) > 0 THEN ch.commit_id END) AS mods
	FROM objects o
	LEFT JOIN changes ch ON ch.object_id = o.id AND ch.commit_id IN (SELECT id FROM commit_set)
	WHERE %s
	GROUP BY o.id
	ORDER BY %s
	LIMIT ? OFFSET ?`, objWhere, orderExpr)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var metrics []domain.ObjectMetric
	for rows.Next() {
		var m domain.ObjectMetric
		if err := rows.Scan(&m.Path, &m.ObjectType, &m.Added, &m.Removed, &m.Growth, &m.Churn, &m.Modifications); err != nil {
			return nil, 0, err
		}
		if commitCount > 0 {
			m.ModificationFrequency = float64(m.Modifications) / float64(commitCount)
			m.ChurnRate = float64(m.Churn) / float64(commitCount)
		}
		metrics = append(metrics, m)
	}
	return metrics, total, rows.Err()
}

func GetAuthorMetrics(ctx context.Context, s *Store, fp domain.FilterParams) ([]domain.AuthorMetric, error) {
	cte, args := buildCommitCTE(fp)

	// Get total churn for ownership calculation
	var objCond string
	if fp.ObjectPath != "" {
		objCond = " AND o.path LIKE ?"
		args = append(args, fp.ObjectPath+"%")
	}
	if fp.ObjectType != "" {
		objCond += " AND o.object_type = ?"
		args = append(args, fp.ObjectType)
	}

	query := cte + fmt.Sprintf(`
	SELECT
		a.id,
		a.canonical_name,
		a.canonical_email,
		COUNT(DISTINCT CASE WHEN (ch.added + ch.removed) > 0 THEN c.id END) AS mods,
		COALESCE(SUM(ch.added + ch.removed), 0) AS author_churn
	FROM authors a
	JOIN commits c ON c.author_id = a.id AND c.repository_id = ?
	JOIN commit_set cs ON cs.id = c.id
	LEFT JOIN changes ch ON ch.commit_id = c.id
	LEFT JOIN objects o ON o.id = ch.object_id%s
	WHERE a.repository_id = ?
	GROUP BY a.id
	ORDER BY author_churn DESC`, objCond)
	args = append(args, fp.RepoID, fp.RepoID)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []domain.AuthorMetric
	var totalChurn int64
	for rows.Next() {
		var m domain.AuthorMetric
		if err := rows.Scan(&m.AuthorID, &m.Name, &m.Email, &m.Modifications, &m.Churn); err != nil {
			return nil, err
		}
		totalChurn += m.Churn
		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute ownership
	for i := range metrics {
		if totalChurn > 0 {
			metrics[i].Ownership = float64(metrics[i].Churn) / float64(totalChurn)
		}
	}
	return metrics, nil
}

func GetCommitTimeSeries(ctx context.Context, s *Store, fp domain.FilterParams) ([]domain.TimePoint, error) {
	cte, args := buildCommitCTE(fp)

	var objCond string
	if fp.ObjectPath != "" {
		// Filter to specific path: sum file changes under that path
		objCond = " AND o.path LIKE ?"
		args = append(args, fp.ObjectPath+"%")
	} else {
		// No path filter: use root '/' directory rows which aggregate all file changes
		objCond = " AND o.path = '/' AND o.object_type = 'directory'"
	}

	query := cte + fmt.Sprintf(`
	SELECT
		date(c.committer_timestamp) AS d,
		COALESCE(SUM(ch.added), 0),
		COALESCE(SUM(ch.removed), 0),
		COALESCE(SUM(ch.added + ch.removed), 0)
	FROM commits c
	JOIN commit_set cs ON cs.id = c.id
	LEFT JOIN changes ch ON ch.commit_id = c.id
	LEFT JOIN objects o ON o.id = ch.object_id%s
	GROUP BY d
	ORDER BY d`, objCond)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []domain.TimePoint
	for rows.Next() {
		var p domain.TimePoint
		if err := rows.Scan(&p.Date, &p.TotalAdded, &p.TotalRemoved, &p.TotalChurn); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func GetObjectTree(ctx context.Context, s *Store, repoID int64, parentPath string) ([]domain.ObjectInfo, error) {
	var query string
	var args []any

	if parentPath == "" || parentPath == "/" {
		// Get top-level objects (direct children of root)
		query = `SELECT id, path, object_type FROM objects WHERE repository_id=? AND path NOT LIKE '%/%' AND path != '/' ORDER BY object_type, path`
		args = []any{repoID}
	} else {
		// Get children of a directory
		prefix := strings.TrimSuffix(parentPath, "/") + "/"
		query = `SELECT id, path, object_type FROM objects WHERE repository_id=? AND path LIKE ? AND path NOT LIKE ? AND path != ? ORDER BY object_type, path`
		args = []any{repoID, prefix + "%", prefix + "%/%", parentPath}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var objs []domain.ObjectInfo
	for rows.Next() {
		var o domain.ObjectInfo
		if err := rows.Scan(&o.ID, &o.Path, &o.ObjectType); err != nil {
			return nil, err
		}
		objs = append(objs, o)
	}
	return objs, rows.Err()
}

func ListCommits(ctx context.Context, s *Store, repoID int64, fromTS, toTS *int64, page, limit int) ([]domain.Commit, int64, error) {
	var conds []string
	var args []any
	conds = append(conds, "c.repository_id = ?")
	args = append(args, repoID)
	if fromTS != nil {
		conds = append(conds, "c.committer_timestamp >= datetime(?, 'unixepoch')")
		args = append(args, *fromTS)
	}
	if toTS != nil {
		conds = append(conds, "c.committer_timestamp < datetime(?, 'unixepoch')")
		args = append(args, *toTS)
	}
	where := strings.Join(conds, " AND ")

	var total int64
	s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM commits c WHERE %s", where), args...).Scan(&total)

	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := 0
	if page > 1 {
		offset = (page - 1) * limit
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.repository_id, c.hash, c.parent_hash, c.author_id,
			CAST(strftime('%%s', c.committer_timestamp) AS INTEGER),
			c.subject, a.canonical_name, a.canonical_email
		FROM commits c
		JOIN authors a ON a.id = c.author_id
		WHERE %s
		ORDER BY c.committer_timestamp DESC
		LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var commits []domain.Commit
	for rows.Next() {
		var c domain.Commit
		if err := rows.Scan(&c.ID, &c.RepositoryID, &c.Hash, &c.ParentHash, &c.AuthorID, &c.CommitterTimestamp, &c.Subject, &c.AuthorName, &c.AuthorEmail); err != nil {
			return nil, 0, err
		}
		commits = append(commits, c)
	}
	return commits, total, rows.Err()
}
