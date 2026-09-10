package subscription

import (
	"context"
	"log/slog"
	"sync"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/worker"
)

// Ensurer requests that peer subscriptions cover the given
// prefixes. Implemented by *worker.SubscriberManager, which runs the request
// locally on the leader or forwards it to the current cluster leader.
type Ensurer interface {
	EnsureSubscriptions(ctx context.Context, prefixes []string)
}

type Usecase struct {
	ensurer Ensurer
	pubsub  worker.PubSub

	mu       sync.Mutex
	nextID   uint64
	sessions map[uint64][]string
}

func New(
	ensurer Ensurer,
	pubsub worker.PubSub,
) *Usecase {
	return &Usecase{
		ensurer:  ensurer,
		pubsub:   pubsub,
		sessions: make(map[uint64][]string),
	}
}

// CurrentSubscriptions returns the union of prefixes wanted by all realtime
// sessions on this replica.
func (uc *Usecase) CurrentSubscriptions() []string {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	subscriptionSet := make(map[string]bool)
	for _, prefixes := range uc.sessions {
		for _, prefix := range prefixes {
			subscriptionSet[prefix] = true
		}
	}

	subscriptions := make([]string, 0, len(subscriptionSet))
	for prefix := range subscriptionSet {
		subscriptions = append(subscriptions, prefix)
	}

	return subscriptions
}

// SessionCount reports the number of realtime websocket sessions currently
// open on this replica.
func (uc *Usecase) SessionCount() int {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	return len(uc.sessions)
}

func (uc *Usecase) openSession() uint64 {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.nextID++
	id := uc.nextID
	uc.sessions[id] = []string{}
	return id
}

func (uc *Usecase) updateSession(id uint64, prefixes []string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.sessions[id] = prefixes
}

func (uc *Usecase) closeSession(id uint64) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	delete(uc.sessions, id)
}

func (uc *Usecase) Realtime(ctx context.Context, request <-chan []string, response chan<- concrnt.Event) {
	id := uc.openSession()
	defer uc.closeSession(id)

	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case newSubscriptions := <-request:
			if cancel != nil {
				cancel()
			}

			subctx, subcancel := context.WithCancel(ctx)
			cancel = subcancel

			uc.updateSession(id, newSubscriptions)

			err := uc.pubsub.Subscribe(subctx, newSubscriptions, response)
			if err != nil {
				slog.Error("failed to subscribe", "subscriptions", newSubscriptions, "error", err)
			}
			uc.ensurer.EnsureSubscriptions(ctx, newSubscriptions)
		}
	}
}
