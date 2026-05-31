package worker

import (
	"testing"

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
