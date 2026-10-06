package analysis

import "github.com/sdp-test/repo-analysis-tool/internal/domain"

func FromLineDelta(added int64, removed int64) domain.Metrics {
	return domain.Metrics{
		Added:   added,
		Removed: removed,
		Growth:  added - removed,
		Churn:   added + removed,
	}
}

func AddMetrics(left domain.Metrics, right domain.Metrics) domain.Metrics {
	return domain.Metrics{
		Added:         left.Added + right.Added,
		Removed:       left.Removed + right.Removed,
		Growth:        left.Growth + right.Growth,
		Churn:         left.Churn + right.Churn,
		Modifications: left.Modifications + right.Modifications,
	}
}

func FinalizeRates(metrics domain.Metrics, commitCount int64) domain.Metrics {
	if commitCount <= 0 {
		metrics.ModificationFrequency = 0
		metrics.ChurnRate = 0
		return metrics
	}
	metrics.ModificationFrequency = float64(metrics.Modifications) / float64(commitCount)
	metrics.ChurnRate = float64(metrics.Churn) / float64(commitCount)
	return metrics
}
