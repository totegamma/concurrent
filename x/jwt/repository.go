package jwt

import (
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

type Repository interface {
	CheckJTI(ctx context.Context, jti string) (bool, error)
	InvalidateJTI(ctx context.Context, jti string, exp time.Time) error
}

type repository struct {
	rdb *redis.Client
}

func NewRepository(rdb *redis.Client) Repository {
	return &repository{
		rdb: rdb,
	}
}

func (r *repository) CheckJTI(ctx context.Context, jti string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Jwt.Repository.CheckJTI")
	defer span.End()

	// check if jti exists
	exists, err := r.rdb.Exists(ctx, "jti:"+jti).Result()
	if err != nil {
		span.RecordError(err)
		return false, err
	}

	if exists == 0 {
		return false, nil
	}

	return true, nil
}

func (r *repository) InvalidateJTI(ctx context.Context, jti string, exp time.Time) error {
	ctx, span := tracer.Start(ctx, "Jwt.Repository.InvalidateJTI")
	defer span.End()

	// set jti with expiration
	expiration := time.Until(exp)
	err := r.rdb.Set(ctx, "jti:"+jti, "1", expiration).Err()
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}
