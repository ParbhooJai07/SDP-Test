package domain

import "time"

type RepositoryStatus string

const (
	RepositoryStatusPending  RepositoryStatus = "pending"
	RepositoryStatusAnalyzing RepositoryStatus = "analyzing"
	RepositoryStatusReady     RepositoryStatus = "ready"
	RepositoryStatusFailed    RepositoryStatus = "failed"
)

type Repository struct {
	ID           int64            `json:"id"`
	Name         string           `json:"name"`
	SourceType   string           `json:"sourceType"`
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
