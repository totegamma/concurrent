package userkv

import (
    "context"
    "github.com/redis/go-redis/v9"
)


// Repository is userkv repository
type Repository struct {
    rdb *redis.Client
    ctx context.Context
}

// NewRepository is for wire.go
func NewRepository(rdb *redis.Client) *Repository {
    return &Repository{rdb: rdb, ctx: context.Background()}
}

// Get returns a userkv by ID
func (r *Repository) Get(ctx context.Context, key string) (string, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    key = "userkv:" + key
    return r.rdb.Get(r.ctx, key).Result()
}

// Upsert updates a userkv
func (r *Repository) Upsert(ctx context.Context, key string, value string) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsert")
    defer childSpan.End()

    key = "userkv:" + key
    return r.rdb.Set(r.ctx, key, value, 0).Err()
}

