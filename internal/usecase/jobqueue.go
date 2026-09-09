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

// parseJobPayload restores the typed payload a handler was registered for.
func parseJobPayload[T any](payload json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(payload, &value)
	return value, err
}
