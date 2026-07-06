package usecase

import (
	"context"
	"slices"
	"testing"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/testutil"
)

type stubEnsurer struct{}

func (stubEnsurer) EnsureSubscriptions(ctx context.Context, prefixes []string) {}

type stubPubSub struct{}

func (stubPubSub) Publish(ctx context.Context, channel string, event concrnt.Event) error {
	return nil
}

func (stubPubSub) Subscribe(ctx context.Context, prefixes []string, response chan<- concrnt.Event) error {
	return nil
}

func (stubPubSub) SubscribeAll(ctx context.Context, response chan<- concrnt.Event) error {
	return nil
}

// Two concurrent realtime sessions must both contribute to the demand set;
// before per-session tracking, the last writer overwrote the whole set.
func TestCurrentSubscriptionsUnionsSessions(t *testing.T) {
	uc := NewSubscriptionUsecase(stubEnsurer{}, stubPubSub{})

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	req1 := make(chan []string)
	req2 := make(chan []string)
	resp := make(chan concrnt.Event, 16)

	go uc.Realtime(ctx1, req1, resp)
	go uc.Realtime(ctx2, req2, resp)

	req1 <- []string{"cckv://alice/home"}
	req2 <- []string{"cckv://bob/home"}

	testutil.WaitFor(t, func() bool {
		subs := uc.CurrentSubscriptions()
		return slices.Contains(subs, "cckv://alice/home") && slices.Contains(subs, "cckv://bob/home")
	})

	// closing one session must drop only its prefixes
	cancel1()
	testutil.WaitFor(t, func() bool {
		subs := uc.CurrentSubscriptions()
		return !slices.Contains(subs, "cckv://alice/home") && slices.Contains(subs, "cckv://bob/home")
	})
}
