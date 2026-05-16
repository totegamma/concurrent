package usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
)

func TestPolicyActionSelection(t *testing.T) {
	t.Parallel()

	recordDoc := concrnt.Document[any]{
		Key: "cckv://con1user/timeline/post-1",
	}
	associationDoc := concrnt.Document[any]{
		Associate: ptr("cckv://con1user/timeline/post-1"),
	}

	require.Equal(t, "record:create", policyCreateAction(recordDoc))
	require.Equal(t, "record:read", policyReadAction(recordDoc))
	require.Equal(t, "record:delete", policyDeleteAction(recordDoc))

	require.Equal(t, "association:create", policyCreateAction(associationDoc))
	require.Equal(t, "association:read", policyReadAction(associationDoc))
	require.Equal(t, "association:delete", policyDeleteAction(associationDoc))
}

func ptr[T any](v T) *T {
	return &v
}
