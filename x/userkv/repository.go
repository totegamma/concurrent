package userkv

import (
    "context"
    "github.com/redis/go-redis/v9"
)


// Repository is userkv repository
type Repository struct {
    rdb *redis.Client
}

// NewRepository is for wire.go
func NewRepository(rdb *redis.Client) *Repository {
    return &Repository{rdb: rdb}
}

// Get returns a userkv by ID
func (r *Repository) Get(ctx context.Context, key string) (string, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    key = "userkv:" + key
    return r.rdb.Get(ctx, key).Result()
}

// Upsert updates a userkv
func (r *Repository) Upsert(ctx context.Context, key string, value string) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsert")
    defer childSpan.End()

    key = "userkv:" + key
    return r.rdb.Set(ctx, key, value, 0).Err()
}

