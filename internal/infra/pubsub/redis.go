package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/concrnt/concrnt"
)

type RedisPubsub struct {
	rdb *redis.Client
}

func NewRedisPubsub(redisClient *redis.Client) *RedisPubsub {
	return &RedisPubsub{
		rdb: redisClient,
	}
}

func (s *RedisPubsub) Publish(ctx context.Context, channel string, event concrnt.Event) error {

	event.Source = channel

	jsonstr, err := json.Marshal(event)
	if err != nil {
		return err
	}

	err = s.rdb.Publish(ctx, channel, jsonstr).Err()
	if err != nil {
		return err

	}

	return nil
}

func (s *RedisPubsub) Subscribe(ctx context.Context, prefixes []string, response chan<- concrnt.Event) error {
	if len(prefixes) == 0 {
		return nil
	}

	fmt.Println("Subscribing to prefixes:", prefixes)

	patterns := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		patterns[i] = prefix + "*"
	}

	pubsub := s.rdb.PSubscribe(ctx, patterns...)

	psch := pubsub.Channel()

	go func() {
		defer pubsub.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-psch:
				if !ok {
					return
				}
				var item concrnt.Event
				err := json.Unmarshal([]byte(msg.Payload), &item)
				if err != nil {
					slog.Error("failed to unmarshal event", slog.String("error", err.Error()))
					continue
				}
				select {
				case response <- item:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return nil
}

func (s *RedisPubsub) SubscribeAll(ctx context.Context, response chan<- concrnt.Event) error {
	pubsub := s.rdb.PSubscribe(ctx, "*")

	psch := pubsub.Channel()

	go func() {
		defer pubsub.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-psch:
				if !ok {
					return
				}
				var item concrnt.Event
				err := json.Unmarshal([]byte(msg.Payload), &item)
				if err != nil {
					slog.Error("failed to unmarshal event", slog.String("error", err.Error()))
					continue
				}
				select {
				case response <- item:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return nil
}
