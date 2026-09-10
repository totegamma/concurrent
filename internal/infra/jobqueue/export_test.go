package jobqueue

import "github.com/prometheus/client_golang/prometheus"

// JobsProcessedForTest exposes the jobs_total counter to the external test
// package.
func JobsProcessedForTest(jobType, result string) prometheus.Counter {
	return jobsProcessed.WithLabelValues(jobType, result)
}
