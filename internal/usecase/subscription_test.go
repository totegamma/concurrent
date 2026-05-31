package usecase

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionUsecaseRequestsAreTrackedSeparatelyFromCompletions(t *testing.T) {
	uc := &SubscriptionUsecase{
		currentSubscriptionRequests: make(map[int64][]string),
		currentSubscriptions:        make(map[int64][]string),
	}

	id := uc.addSubscriptionRequest([]string{"cc://remote/b", "cc://remote/a", "cc://remote/a"})
	uc.setSubscriptionCompleted(id, []string{"cc://remote/a"})

	require.Equal(t, []string{"cc://remote/a", "cc://remote/b"}, uc.GetCurrentSubscriptionRequests())
	require.Equal(t, []string{"cc://remote/a"}, uc.getLocalCurrentSubscriptions())

	uc.removeSubscription(id)

	require.Empty(t, uc.GetCurrentSubscriptionRequests())
	require.Empty(t, uc.getLocalCurrentSubscriptions())
}
