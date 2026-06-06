package worker

import (
	"context"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestSubscriberApplyDesiredSubscriptionsTracksRequestedAndClosesStale(t *testing.T) {
	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{},
	}

	changed, closed := subscriber.applyDesiredSubscriptions(map[string][]string{
		"remote.example": []string{"cc://remote/b", "cc://remote/a", "cc://remote/a"},
	})

	require.Equal(t, []string{"remote.example"}, changed)
	require.Empty(t, closed)
	require.Equal(t, []string{"cc://remote/a", "cc://remote/b"}, subscriber.Subscriptions["remote.example"].RequestedPrefixes)
	require.Empty(t, subscriber.Subscriptions["remote.example"].SubscribedPrefixes)

	changed, closed = subscriber.applyDesiredSubscriptions(map[string][]string{
		"remote.example": []string{"cc://remote/a", "cc://remote/b"},
	})

	require.Equal(t, []string{"remote.example"}, changed)
	require.Empty(t, closed)

	changed, closed = subscriber.applyDesiredSubscriptions(map[string][]string{})

	require.Empty(t, changed)
	require.Equal(t, []string{"remote.example"}, closed)
	require.Empty(t, subscriber.Subscriptions)
}

func TestSubscriberSetSubscribedPrefixesIntersectsRequested(t *testing.T) {
	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{
			"remote.example": {
				RequestedPrefixes: []string{"cc://remote/a"},
			},
		},
	}

	subscriber.setSubscribedPrefixes("remote.example", []string{"cc://remote/b", "cc://remote/a"})

	require.Equal(t, []string{"cc://remote/a"}, subscriber.Subscriptions["remote.example"].SubscribedPrefixes)
}

func TestSubscriberNotifiesWhenSubscribedPrefixesChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{
			"remote.example": {
				RequestedPrefixes: []string{"cc://remote/a"},
			},
		},
	}
	updates := subscriber.WatchSubscriptionChanges(ctx)

	subscriber.setSubscribedPrefixes("remote.example", []string{"cc://remote/a"})

	select {
	case <-updates:
	default:
		t.Fatal("expected subscription change notification")
	}

	subscriber.setSubscribedPrefixes("remote.example", []string{"cc://remote/a"})

	select {
	case <-updates:
		t.Fatal("unexpected duplicate subscription change notification")
	default:
	}
}

func TestSubscriberRetainsRemoteSubscriptionsWhenCacheUpdaterExists(t *testing.T) {
	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{
			"remote.example": {
				RequestedPrefixes: []string{"cc://remote/a"},
				Connection:        &websocket.Conn{},
			},
		},
		CacheUpdater: &fakeChunklineCacheUpdater{exists: true},
	}

	changed, closed := subscriber.applyDesiredSubscriptions(map[string][]string{})

	require.Empty(t, changed)
	require.Empty(t, closed)
	require.Contains(t, subscriber.Subscriptions, "remote.example")
	require.Empty(t, subscriber.Subscriptions["remote.example"].RequestedPrefixes)
	require.Equal(t, []string{"cc://remote/a"}, subscriber.Subscriptions["remote.example"].RetainedPrefixes)
}

func TestSubscriberCleanupReleasesRetainedSubscriptionAfterRepeatedCacheMisses(t *testing.T) {
	updater := &fakeChunklineCacheUpdater{}
	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{
			"remote.example": {
				RetainedPrefixes: []string{"cc://remote/a"},
			},
		},
		CacheUpdater: updater,
	}

	subscriber.cleanupRetainedSubscriptions(context.Background())
	require.Contains(t, subscriber.Subscriptions, "remote.example")
	require.Equal(t, 0, updater.deleteCount)

	subscriber.cleanupRetainedSubscriptions(context.Background())
	require.Empty(t, subscriber.Subscriptions)
	require.Equal(t, 1, updater.deleteCount)
}

func TestSubscriberPromotesRetainedSubscriptionWhenRequestedAgain(t *testing.T) {
	subscriber := &Subscriber{
		Subscriptions: map[string]*SubState{
			"remote.example": {
				RetainedPrefixes: []string{"cc://remote/a"},
			},
		},
		CacheUpdater: &fakeChunklineCacheUpdater{exists: true},
	}

	changed, closed := subscriber.applyDesiredSubscriptions(map[string][]string{
		"remote.example": []string{"cc://remote/a"},
	})

	require.Empty(t, changed)
	require.Empty(t, closed)
	require.Empty(t, subscriber.Subscriptions["remote.example"].RetainedPrefixes)
	require.Equal(t, []string{"cc://remote/a"}, subscriber.Subscriptions["remote.example"].SubscribedPrefixes)
}

type fakeChunklineCacheUpdater struct {
	exists      bool
	stale       bool
	deleteCount int
}

func (u *fakeChunklineCacheUpdater) CacheCreatedEvent(ctx context.Context, event concrnt.Event) error {
	return nil
}

func (u *fakeChunklineCacheUpdater) EnsureLatestCache(ctx context.Context, timeline string) error {
	u.exists = true
	return nil
}

func (u *fakeChunklineCacheUpdater) LatestCacheExists(ctx context.Context, timeline string) (bool, error) {
	return u.exists, nil
}

func (u *fakeChunklineCacheUpdater) LatestChunkStaleForRetention(ctx context.Context, timeline string, now time.Time) (bool, error) {
	return u.stale, nil
}

func (u *fakeChunklineCacheUpdater) DeleteLatestCache(ctx context.Context, timeline string) error {
	u.deleteCount++
	return nil
}
