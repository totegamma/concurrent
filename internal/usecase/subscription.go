package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/worker"
)

type SubscriptionUsecase struct {
	worker *worker.Subscriber
	pubsub worker.PubSub

	currentSubscriptions []string
}

func NewSubscriptionUsecase(
	worker *worker.Subscriber,
	pubsub worker.PubSub,
) *SubscriptionUsecase {
	return &SubscriptionUsecase{
		worker:               worker,
		pubsub:               pubsub,
		currentSubscriptions: []string{},
	}
}

func (uc *SubscriptionUsecase) CurrentSubscriptions() []string {
	return uc.currentSubscriptions
}

func (uc *SubscriptionUsecase) Realtime(ctx context.Context, request <-chan []string, response chan<- concrnt.Event) {
	var cancel context.CancelFunc

	uc.worker.RegisterClient(uc)

	for {
		select {
		case <-ctx.Done():
			if cancel != nil {
				cancel()
			}
			return
		case newSubscriptions := <-request:
			if cancel != nil {
				cancel()
			}

			fmt.Println("recreating subscription with new prefixes:", newSubscriptions)

			subctx, subcancel := context.WithCancel(ctx)
			cancel = subcancel

			uc.currentSubscriptions = newSubscriptions

			err := uc.pubsub.Subscribe(subctx, newSubscriptions, response)
			if err != nil {
				slog.Error("failed to subscribe", "subscriptions", newSubscriptions, "error", err)
			}
		}
	}
}
