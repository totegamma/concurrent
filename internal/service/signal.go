package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/totegamma/concrnt-playground"
)

type SignalService struct {
	rdb         *redis.Client
	currentSubs map[int64][]string

	mu sync.RWMutex
	id atomic.Int64
}

func NewSignalService(redisClient *redis.Client) *SignalService {
	return &SignalService{
		rdb:         redisClient,
		currentSubs: make(map[int64][]string),
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

func (s *SignalService) Realtime(ctx context.Context, request <-chan []string, response chan<- concrnt.Event) {

	var cancel context.CancelFunc
	events := make(chan concrnt.Event)

	for {
		select {
		case prefixes := <-request:
			if cancel != nil {
				cancel()
			}

			patterns := make([]string, len(prefixes))
			for i, prefix := range prefixes {
				patterns[i] = prefix + "*"
			}

			var subctx context.Context
			subctx, cancel = context.WithCancel(ctx)
			go s.subscribe(subctx, patterns, events)

		case event := <-events:
			response <- event

		case <-ctx.Done():
			if cancel != nil {
				cancel()
			}
			return
		}
	}
}

func (s *SignalService) subscribe(ctx context.Context, patterns []string, event chan<- concrnt.Event) error {

	if len(patterns) == 0 {
		return nil
	}

	id := func() int64 {
		s.mu.Lock()
		defer s.mu.Unlock()

		id := s.id.Add(1)
		s.currentSubs[id] = patterns

		return id
	}()

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		delete(s.currentSubs, id)
	}()

	pubsub := s.rdb.PSubscribe(ctx, patterns...)
	defer pubsub.Close()

	psch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-psch:
			var item concrnt.Event
			err := json.Unmarshal([]byte(msg.Payload), &item)
			if err != nil {
				fmt.Println("failed to unmarshal event:", err)
				continue
			}
			event <- item
		}
	}
}

func (s *SignalService) GetCurrentSubscriptions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uniquePatterns := make(map[string]struct{})
	for _, patterns := range s.currentSubs {
		for _, pattern := range patterns {
			uniquePatterns[pattern] = struct{}{}
		}
	}

	patterns := make([]string, 0, len(uniquePatterns))
	for pattern := range uniquePatterns {
		patterns = append(patterns, pattern)
	}

	return patterns
}
