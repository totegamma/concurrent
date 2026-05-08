package usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
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

func TestAckCommitOwners(t *testing.T) {
	t.Parallel()

	uc := &RecordUsecase{
		entity: &EntityUsecase{
			config: &domain.Config{FQDN: "local.example"},
		},
	}

	localFrom := domain.Entity{ID: "con1from", Domain: "local.example"}
	localTo := domain.Entity{ID: "con1to", Domain: "local.example"}
	remoteFrom := domain.Entity{ID: "con1from", Domain: "remote.example"}
	remoteTo := domain.Entity{ID: "con1to", Domain: "remote.example"}

	require.Equal(t, []string{"con1from", "con1to"}, uc.ackCommitOwners(t.Context(), localFrom, localTo))
	require.Equal(t, []string{"con1from"}, uc.ackCommitOwners(t.Context(), localFrom, remoteTo))
	require.Equal(t, []string{"con1to"}, uc.ackCommitOwners(t.Context(), remoteFrom, localTo))
	require.Empty(t, uc.ackCommitOwners(t.Context(), remoteFrom, remoteTo))

	sameLocal := domain.Entity{ID: "con1same", Domain: "local.example"}
	require.Equal(t, []string{"con1same"}, uc.ackCommitOwners(t.Context(), sameLocal, sameLocal))
}
