package usecase

import (
	"context"
	"time"
)

// KVS is a store of TTL'd string sets, used to hold per-timeline removed-item
// advertisements (written by record on delete, served by chunkline).
// Implemented by internal/infra/kvs.Redis; nil in offline tooling/tests.
type KVS interface {
	SetAdd(ctx context.Context, key string, value string, ttl time.Duration) error
	SetMembers(ctx context.Context, key string) ([]string, error)
}
