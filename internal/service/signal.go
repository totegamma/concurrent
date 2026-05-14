package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/concrnt/concrnt"
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
		case prefixes, ok := <-request:
			if !ok {
				if cancel != nil {
					cancel()
				}
				return
			}
			if cancel != nil {
				cancel()
			}

			patterns := make([]string, len(prefixes))
			for i, prefix := range prefixes {
				patterns[i] = prefix
				if !strings.HasSuffix(patterns[i], "*") {
					patterns[i] += "*"
				}
			}

			var subctx context.Context
			subctx, cancel = context.WithCancel(ctx)
			go s.subscribe(subctx, patterns, events)

		case event := <-events:
			select {
			case response <- event:
			case <-ctx.Done():
				if cancel != nil {
					cancel()
				}
				return
			}

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
		select {
		case event <- concrnt.Event{Type: "subscribed"}:
		case <-ctx.Done():
		}
		return nil
	}

	pubsub := s.rdb.PSubscribe(ctx, patterns...)
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		slog.Error("failed to subscribe realtime patterns", slog.String("error", err.Error()))
		return err
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

	select {
	case event <- concrnt.Event{Type: "subscribed", Prefixes: patterns}:
	case <-ctx.Done():
		return nil
	}

	psch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-psch:
			if !ok {
				return nil
			}
			var item concrnt.Event
			err := json.Unmarshal([]byte(msg.Payload), &item)
			if err != nil {
				slog.Error("failed to unmarshal event", slog.String("error", err.Error()))
				continue
			}
			select {
			case event <- item:
			case <-ctx.Done():
				return nil
			}
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
