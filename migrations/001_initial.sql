CREATE TABLE IF NOT EXISTS repositories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN ('remote', 'upload', 'fixture')),
    storage_key TEXT NOT NULL DEFAULT '',
    default_ref TEXT NOT NULL DEFAULT 'HEAD',
    head_commit TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending', 'analyzing', 'ready', 'failed')),
    failure TEXT NOT NULL DEFAULT '',
    analysis_version INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    last_analyzed_at DATETIME
);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    requested_ref TEXT NOT NULL DEFAULT 'HEAD',
    resolved_commit TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    total_commits INTEGER NOT NULL DEFAULT 0,
    done_commits INTEGER NOT NULL DEFAULT 0,
    failure TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    finished_at DATETIME
);

CREATE TABLE IF NOT EXISTS authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    canonical_name TEXT NOT NULL,
    canonical_email TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('raw', 'mailmap', 'manual')),
    UNIQUE(repository_id, canonical_name, canonical_email)
);

CREATE TABLE IF NOT EXISTS author_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    raw_name TEXT NOT NULL,
    raw_email TEXT NOT NULL,
    resolved_name TEXT NOT NULL,
    resolved_email TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('raw', 'mailmap', 'manual')),
    UNIQUE(repository_id, raw_name, raw_email)
);

CREATE TABLE IF NOT EXISTS commits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    parent_hash TEXT NOT NULL DEFAULT '',
    author_id INTEGER NOT NULL REFERENCES authors(id),
    committer_timestamp DATETIME NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    UNIQUE(repository_id, hash)
);

CREATE TABLE IF NOT EXISTS objects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    object_type TEXT NOT NULL CHECK (object_type IN ('file', 'directory')),
    deleted_at_commit TEXT NOT NULL DEFAULT '',
    UNIQUE(repository_id, path, object_type)
);

CREATE TABLE IF NOT EXISTS changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit_id INTEGER NOT NULL REFERENCES commits(id) ON DELETE CASCADE,
    object_id INTEGER NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    added INTEGER NOT NULL,
    removed INTEGER NOT NULL,
    growth INTEGER NOT NULL,
    churn INTEGER NOT NULL,
    UNIQUE(repository_id, commit_id, object_id)
);

CREATE INDEX IF NOT EXISTS idx_commits_repository_time ON commits(repository_id, committer_timestamp);
CREATE INDEX IF NOT EXISTS idx_commits_repository_author ON commits(repository_id, author_id);
CREATE INDEX IF NOT EXISTS idx_objects_repository_path ON objects(repository_id, path, object_type);
CREATE INDEX IF NOT EXISTS idx_changes_repository_object ON changes(repository_id, object_id);
CREATE INDEX IF NOT EXISTS idx_changes_repository_commit ON changes(repository_id, commit_id);
