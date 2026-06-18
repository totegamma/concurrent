package usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
)

func TestPolicyActionSelection(t *testing.T) {
	t.Parallel()

	recordDoc := concrnt.Document[any]{
		Kind: "record",
		Key:  "cckv://con1user/timeline/post-1",
	}
	associationDoc := concrnt.Document[any]{
		Kind:      "association",
		Associate: ptr("cckv://con1user/timeline/post-1"),
	}
	recordWithAssociateDoc := concrnt.Document[any]{
		Kind:      "record",
		Associate: ptr("cckv://con1user/timeline/post-1"),
	}

	require.Equal(t, "record:create", policyCreateAction(recordDoc))
	require.Equal(t, "record:read", policyReadAction(recordDoc))
	require.Equal(t, "record:delete", policyDeleteAction(recordDoc))

	require.Equal(t, "association:create", policyCreateAction(associationDoc))
	require.Equal(t, "association:read", policyReadAction(associationDoc))
	require.Equal(t, "association:delete", policyDeleteAction(associationDoc))

	require.Equal(t, "record:create", policyCreateAction(recordWithAssociateDoc))
	require.Equal(t, "record:read", policyReadAction(recordWithAssociateDoc))
	require.Equal(t, "record:delete", policyDeleteAction(recordWithAssociateDoc))
}

func ptr[T any](v T) *T {
	return &v
}
