package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/concrnt/concrnt"
)

type SubscriptionProvider interface {
	GetCurrentSubscriptions() []string
}

type SignalService struct {
	rdb                         *redis.Client
	currentSubscriptionRequests map[int64][]string
	currentSubscriptions        map[int64][]string
	subscriptionProvider        SubscriptionProvider

	mu sync.RWMutex
	id atomic.Int64
}

func NewSignalService(redisClient *redis.Client) *SignalService {
	return &SignalService{
		rdb:                         redisClient,
		currentSubscriptionRequests: make(map[int64][]string),
		currentSubscriptions:        make(map[int64][]string),
	}
}

func (s *SignalService) SetSubscriptionProvider(provider SubscriptionProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subscriptionProvider = provider
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

			var subctx context.Context
			subctx, cancel = context.WithCancel(ctx)
			go func() {
				err := s.subscribe(subctx, prefixes, events)
				if err != nil && subctx.Err() == nil {
					slog.Error("failed to subscribe signal", slog.String("error", err.Error()))
				}
			}()

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

func (s *SignalService) subscribe(ctx context.Context, prefixes []string, event chan<- concrnt.Event) error {
	prefixes = uniqueStrings(prefixes)

	id := func() int64 {
		s.mu.Lock()
		defer s.mu.Unlock()

		id := s.id.Add(1)
		s.currentSubscriptionRequests[id] = cloneStrings(prefixes)

		return id
	}()

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		delete(s.currentSubscriptionRequests, id)
		delete(s.currentSubscriptions, id)
	}()

	if len(prefixes) == 0 {
		return sendSubscribed(ctx, event, prefixes)
	}

	patterns := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		patterns[i] = prefix + "*"
	}

	pubsub := s.rdb.PSubscribe(ctx, patterns...)
	defer pubsub.Close()

	if err := waitPSubscribe(ctx, pubsub, patterns); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentSubscriptions[id] = cloneStrings(prefixes)
	s.mu.Unlock()

	if err := sendSubscribed(ctx, event, prefixes); err != nil {
		return err
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
	provider := s.subscriptionProvider
	s.mu.RUnlock()

	if provider != nil {
		return provider.GetCurrentSubscriptions()
	}

	return s.GetLocalCurrentSubscriptions()
}

func (s *SignalService) GetCurrentSubscriptionRequests() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return collectUniqueStrings(s.currentSubscriptionRequests)
}

func (s *SignalService) GetLocalCurrentSubscriptions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return collectUniqueStrings(s.currentSubscriptions)
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

func sendSubscribed(ctx context.Context, event chan<- concrnt.Event, prefixes []string) error {
	select {
	case event <- concrnt.Event{
		Type:     "subscribed",
		Prefixes: cloneStrings(prefixes),
	}:
		return nil
	case <-ctx.Done():
		return nil
	}
}

func collectUniqueStrings(values map[int64][]string) []string {
	unique := make(map[string]struct{})
	for _, items := range values {
		for _, item := range items {
			unique[item] = struct{}{}
		}
	}

	result := make([]string, 0, len(unique))
	for item := range unique {
		result = append(result, item)
	}
	sort.Strings(result)

	return result
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

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	result := make([]string, len(values))
	copy(result, values)
	return result
}
