package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSubscriptionProvider struct {
	subscriptions []string
}

func (p fakeSubscriptionProvider) GetCurrentSubscriptions() []string {
	return p.subscriptions
}

func TestSignalSubscriptionListsAreSeparated(t *testing.T) {
	signal := NewSignalService(nil)

	signal.currentSubscriptionRequests[1] = []string{"cc://remote/a", "cc://local/a"}
	signal.currentSubscriptionRequests[2] = []string{"cc://local/a"}
	signal.currentSubscriptions[1] = []string{"cc://local/a"}

	require.Equal(t, []string{"cc://local/a", "cc://remote/a"}, signal.GetCurrentSubscriptionRequests())
	require.Equal(t, []string{"cc://local/a"}, signal.GetLocalCurrentSubscriptions())
	require.Equal(t, []string{"cc://local/a"}, signal.GetCurrentSubscriptions())
}

func TestSignalGetCurrentSubscriptionsDelegatesToProvider(t *testing.T) {
	signal := NewSignalService(nil)
	signal.currentSubscriptions[1] = []string{"cc://local/a"}
	signal.SetSubscriptionProvider(fakeSubscriptionProvider{
		subscriptions: []string{"cc://remote/a"},
	})

	require.Equal(t, []string{"cc://remote/a"}, signal.GetCurrentSubscriptions())
}
