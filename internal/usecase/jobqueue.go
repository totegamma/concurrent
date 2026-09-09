// Package usecase holds the ports shared by more than one usecase package
// (job queue, KVS). Each bounded concern lives in its own subpackage
// (record, residence, server, ...) and declares the repository/gateway
// interfaces only it needs.
package usecase

import (
	"context"
	"encoding/json"
)

// JobQueue runs typed background jobs asynchronously with retries. The queue
// carries a (type, serialized payload) envelope and knows nothing about the
// payload: whoever owns a job type registers the handler that interprets it
// and enqueues payloads of that type. Implemented by
// internal/infra/jobqueue.RedisJobQueue; a no-op in offline tooling/tests.
type JobQueue interface {
	Enqueue(ctx context.Context, jobType string, payload any) error
	RegisterHandler(jobType string, handler func(ctx context.Context, payload json.RawMessage) error)
}

// ParseJobPayload restores the typed payload a handler was registered for.
func ParseJobPayload[T any](payload json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(payload, &value)
	return value, err
}
