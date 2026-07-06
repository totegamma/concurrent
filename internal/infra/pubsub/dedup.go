package pubsub

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisDeduper claims keys via SET NX, used to suppress duplicate side effects
// (e.g. web push) across replicas.
type RedisDeduper struct {
	rdb *redis.Client
}

func NewRedisDeduper(rdb *redis.Client) *RedisDeduper {
	return &RedisDeduper{rdb: rdb}
}

func (d *RedisDeduper) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return d.rdb.SetNX(ctx, key, 1, ttl).Result()
}

// Release gives a claim back (e.g. after the claimed side effect failed) so
// another replica may retry it.
func (d *RedisDeduper) Release(ctx context.Context, key string) error {
	return d.rdb.Del(ctx, key).Err()
}
