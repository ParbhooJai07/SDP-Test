package domain

import "time"

type RepositoryStatus string

const (
	RepositoryStatusPending   RepositoryStatus = "pending"
	RepositoryStatusAnalyzing RepositoryStatus = "analyzing"
	RepositoryStatusReady     RepositoryStatus = "ready"
	RepositoryStatusFailed    RepositoryStatus = "failed"
)

type Repository struct {
	ID           int64            `json:"id"`
	Name         string           `json:"name"`
	SourceType   string           `json:"sourceType"`
	RemoteURL    string           `json:"remoteUrl,omitempty"`
	LocalPath    string           `json:"-"`
	StorageKey   string           `json:"-"`
	DefaultRef   string           `json:"defaultRef"`
	HeadCommit   string           `json:"headCommit"`
	Status       RepositoryStatus `json:"status"`
	Failure      string           `json:"failure,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	LastAnalyzed *time.Time       `json:"lastAnalyzed,omitempty"`
}

type AnalysisJob struct {
	ID             int64      `json:"id"`
	RepositoryID   int64      `json:"repositoryId"`
	RequestedRef   string     `json:"requestedRef"`
	ResolvedCommit string     `json:"resolvedCommit"`
	Status         string     `json:"status"`
	TotalCommits   int        `json:"totalCommits"`
	DoneCommits    int        `json:"doneCommits"`
	Failure        string     `json:"failure,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

type Metrics struct {
	Added                 int64   `json:"added"`
	Removed               int64   `json:"removed"`
	Growth                int64   `json:"growth"`
	Churn                 int64   `json:"churn"`
	Modifications         int64   `json:"modifications"`
	ModificationFrequency float64 `json:"modificationFrequency"`
	ChurnRate             float64 `json:"churnRate"`
}

type Author struct {
	ID             int64          `json:"id"`
	RepositoryID   int64          `json:"repositoryId"`
	CanonicalName  string         `json:"canonicalName"`
	CanonicalEmail string         `json:"canonicalEmail"`
	Source         string         `json:"source"`
	Aliases        []AuthorAlias  `json:"aliases,omitempty"`
}

type AuthorAlias struct {
	ID            int64  `json:"id"`
	AuthorID      int64  `json:"authorId"`
	RawName       string `json:"rawName"`
	RawEmail      string `json:"rawEmail"`
	ResolvedName  string `json:"resolvedName"`
	ResolvedEmail string `json:"resolvedEmail"`
	Source        string `json:"source"`
}

type Commit struct {
	ID                 int64  `json:"id"`
	RepositoryID       int64  `json:"repositoryId"`
	Hash               string `json:"hash"`
	ParentHash         string `json:"parentHash"`
	AuthorID           int64  `json:"authorId"`
	CommitterTimestamp int64  `json:"committerTimestamp"`
	Subject            string `json:"subject"`
	AuthorName         string `json:"authorName,omitempty"`
	AuthorEmail        string `json:"authorEmail,omitempty"`
}

type ObjectInfo struct {
	ID         int64  `json:"id"`
	Path       string `json:"path"`
	ObjectType string `json:"type"`
}

type FilterParams struct {
	RepoID       int64
	AuthorIDs    []int64
	ObjectPath   string
	ObjectType   string
	FromTS       *int64
	ToTS         *int64
	CommitHashes []string
}

type ObjectMetric struct {
	Path                  string  `json:"path"`
	ObjectType            string  `json:"type"`
	Added                 int64   `json:"added"`
	Removed               int64   `json:"removed"`
	Growth                int64   `json:"growth"`
	Churn                 int64   `json:"churn"`
	Modifications         int64   `json:"modifications"`
	ModificationFrequency float64 `json:"modificationFrequency"`
	ChurnRate             float64 `json:"churnRate"`
}

type AuthorMetric struct {
	AuthorID      int64   `json:"authorId"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	Modifications int64   `json:"modifications"`
	Churn         int64   `json:"churn"`
	Ownership     float64 `json:"ownership"`
}

type TimePoint struct {
	Date         string `json:"date"`
	TotalAdded   int64  `json:"totalAdded"`
	TotalRemoved int64  `json:"totalRemoved"`
	TotalChurn   int64  `json:"totalChurn"`
}

type SummaryMetric struct {
	Added                 int64   `json:"added"`
	Removed               int64   `json:"removed"`
	Growth                int64   `json:"growth"`
	Churn                 int64   `json:"churn"`
	Modifications         int64   `json:"modifications"`
	ModificationFrequency float64 `json:"modificationFrequency"`
	ChurnRate             float64 `json:"churnRate"`
	CommitCount           int64   `json:"commitCount"`
}
