package kvs

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a small store of TTL'd string sets over redis. Higher layers build
// specific stores (e.g. removed-item advertisements) on top of it.
type Redis struct {
	rdb *redis.Client
}

func NewRedis(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb}
}

// SetAdd adds a member to a set and refreshes the whole set's TTL.
func (r *Redis) SetAdd(ctx context.Context, key string, value string, ttl time.Duration) error {
	if err := r.rdb.SAdd(ctx, key, value).Err(); err != nil {
		return err
	}
	return r.rdb.Expire(ctx, key, ttl).Err()
}

func (r *Redis) SetMembers(ctx context.Context, key string) ([]string, error) {
	return r.rdb.SMembers(ctx, key).Result()
}
