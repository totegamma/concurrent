package kvs

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a small general-purpose key-value store over redis: set-with-TTL
// and existence checks. Higher layers build specific stores (e.g. deletion
// tombstones) on top of it.
type Redis struct {
	rdb *redis.Client
}

func NewRedis(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb}
}

func (r *Redis) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return r.rdb.Set(ctx, key, value, ttl).Err()
}

func (r *Redis) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
