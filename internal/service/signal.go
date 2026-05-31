package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/redis/go-redis/v9"

	"github.com/concrnt/concrnt"
)

type SignalService struct {
	rdb *redis.Client
}

func NewSignalService(redisClient *redis.Client) *SignalService {
	return &SignalService{
		rdb: redisClient,
	}
}

func (s *SignalService) Publish(ctx context.Context, channel string, event concrnt.Event) error {

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

func (s *SignalService) Subscribe(ctx context.Context, prefixes []string) (<-chan concrnt.Event, error) {
	prefixes = uniqueStrings(prefixes)

	if len(prefixes) == 0 {
		ch := make(chan concrnt.Event)
		close(ch)
		return ch, nil
	}

	patterns := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		patterns[i] = prefix + "*"
	}

	pubsub := s.rdb.PSubscribe(ctx, patterns...)

	if err := waitPSubscribe(ctx, pubsub, patterns); err != nil {
		pubsub.Close()
		return nil, err
	}

	psch := pubsub.Channel()
	events := make(chan concrnt.Event)

	go func() {
		defer close(events)
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
				case events <- item:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, nil
}

func waitPSubscribe(ctx context.Context, pubsub *redis.PubSub, patterns []string) error {
	waiting := make(map[string]struct{}, len(patterns))
	for _, pattern := range patterns {
		waiting[pattern] = struct{}{}
	}

	for len(waiting) > 0 {
		msg, err := pubsub.Receive(ctx)
		if err != nil {
			return err
		}

		sub, ok := msg.(*redis.Subscription)
		if !ok || sub.Kind != "psubscribe" {
			continue
		}

		delete(waiting, sub.Channel)
	}

	return nil
}

func uniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}

	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)

	return result
}
