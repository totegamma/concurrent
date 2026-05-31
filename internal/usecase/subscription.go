package usecase

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/worker"
)

type Subscriptable interface {
	Subscribe(ctx context.Context, prefixes []string) (<-chan concrnt.Event, error)
}

type SubscriptionUsecase struct {
	signal     Subscriptable
	subscriber *worker.Subscriber

	mu                          sync.RWMutex
	id                          atomic.Int64
	currentSubscriptionRequests map[int64][]string
	currentSubscriptions        map[int64][]string
}

func NewSubscriptionUsecase(
	signal Subscriptable,
	subscriber *worker.Subscriber,
) *SubscriptionUsecase {
	return &SubscriptionUsecase{
		signal:                      signal,
		subscriber:                  subscriber,
		currentSubscriptionRequests: make(map[int64][]string),
		currentSubscriptions:        make(map[int64][]string),
	}
}

func (uc *SubscriptionUsecase) Realtime(ctx context.Context, request <-chan []string, response chan<- concrnt.Event) {
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
				if err := uc.subscribe(subctx, prefixes, events); err != nil && subctx.Err() == nil {
					slog.Error("failed to subscribe realtime", slog.String("error", err.Error()))
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

func (uc *SubscriptionUsecase) GetCurrentSubscriptionRequests() []string {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	return collectUniqueStrings(uc.currentSubscriptionRequests)
}

func (uc *SubscriptionUsecase) GetCurrentSubscriptions() []string {
	localCompleted := uc.subscriber.GetLocalSubscriptions(uc.getLocalCurrentSubscriptions())
	remoteCompleted := uc.subscriber.GetRemoteSubscriptions()

	return uniqueStrings(append(localCompleted, remoteCompleted...))
}

func (uc *SubscriptionUsecase) subscribe(ctx context.Context, prefixes []string, event chan<- concrnt.Event) error {
	prefixes = uniqueStrings(prefixes)

	id := uc.addSubscriptionRequest(prefixes)
	defer func() {
		uc.removeSubscription(id)
		uc.reconcile(context.Background())
	}()

	uc.reconcile(ctx)

	if len(prefixes) == 0 {
		return sendSubscribed(ctx, event, prefixes)
	}

	events, err := uc.signal.Subscribe(ctx, prefixes)
	if err != nil {
		return err
	}

	uc.setSubscriptionCompleted(id, prefixes)

	if err := sendSubscribed(ctx, event, prefixes); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case item, ok := <-events:
			if !ok {
				return nil
			}
			select {
			case event <- item:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (uc *SubscriptionUsecase) addSubscriptionRequest(prefixes []string) int64 {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	id := uc.id.Add(1)
	uc.currentSubscriptionRequests[id] = cloneStrings(prefixes)
	return id
}

func (uc *SubscriptionUsecase) setSubscriptionCompleted(id int64, prefixes []string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.currentSubscriptions[id] = cloneStrings(prefixes)
}

func (uc *SubscriptionUsecase) removeSubscription(id int64) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	delete(uc.currentSubscriptionRequests, id)
	delete(uc.currentSubscriptions, id)
}

func (uc *SubscriptionUsecase) getLocalCurrentSubscriptions() []string {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	return collectUniqueStrings(uc.currentSubscriptions)
}

func (uc *SubscriptionUsecase) reconcile(ctx context.Context) {
	uc.subscriber.ReconcileSubscriptions(ctx, uc.GetCurrentSubscriptionRequests())
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
