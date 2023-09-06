//go:generate go run go.uber.org/mock/mockgen -source=repository.go -destination=mock/repository.go
package userkv

import (
	"context"
	"github.com/redis/go-redis/v9"
)

// Repository is the interface for userkv repository
type Repository interface {
	Get(ctx context.Context, key string) (string, error)
	Upsert(ctx context.Context, key string, value string) error
}

type repository struct {
	rdb *redis.Client
}

// NewRepository creates a new userkv repository
func NewRepository(rdb *redis.Client) Repository {
	return &repository{rdb: rdb}
}

// Get returns a userkv by ID
func (r *repository) Get(ctx context.Context, key string) (string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	key = "userkv:" + key
	return r.rdb.Get(ctx, key).Result()
}

// Upsert updates a userkv
func (r *repository) Upsert(ctx context.Context, key string, value string) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	key = "userkv:" + key
	return r.rdb.Set(ctx, key, value, 0).Err()
}
