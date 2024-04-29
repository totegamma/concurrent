package store

import (
	"context"
	"github.com/redis/go-redis/v9"

	"github.com/totegamma/concurrent/x/core"
)

type Repository interface {
	Log(ctx context.Context, owner, entry string) error
	Since(ctx context.Context, since string) ([]core.CommitLog, error)
}

type repository struct {
	rdb *redis.Client
}

func NewRepository(rdb *redis.Client) Repository {
	return &repository{rdb}
}

func (r *repository) Log(ctx context.Context, owner, entry string) error {

	err := r.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "repository-all",
		Values: map[string]interface{}{
			"owner": owner,
			"entry": entry,
		},
	}).Err()

	return err
}

func (r *repository) Since(ctx context.Context, since string) ([]core.CommitLog, error) {

	result, err := r.rdb.XRead(ctx, &redis.XReadArgs{
		Streams: []string{
			"repository-all",
			since,
		},
		Count: 64,
		Block: 0,
	}).Result()

	if err != nil {
		return nil, err
	}

	var entries []core.CommitLog
	for _, messages := range result {
		for _, message := range messages.Messages {

			owner, ok := message.Values["owner"].(string)
			if !ok {
				continue
			}

			content, ok := message.Values["entry"].(string)
			if !ok {
				continue
			}

			entries = append(entries, core.CommitLog{
				ID:      message.ID,
				Owner:   owner,
				Content: content,
			})
		}
	}

	return entries, nil
}
