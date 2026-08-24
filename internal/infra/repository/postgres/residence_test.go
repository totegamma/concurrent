package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/internal/testutil"
)

// UpdateMetaInfo must touch only the info column (SaveMeta's upsert would
// clobber inviter) and report NotFound for unregistered ccids.
func TestResidenceUpdateMetaInfo(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := &ResidenceRepository{db: db}

	ccid := "con1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	inviter := "con1pppppppppppppppppppppppppppppppppppppppp"

	err := repo.UpdateMetaInfo(ctx, ccid, `{"email":"b@example.com"}`)
	require.True(t, errors.Is(err, domain.ErrNotFound), "expected not found, got %v", err)

	require.NoError(t, repo.SaveMeta(ctx, domain.EntityMeta{
		ID:      ccid,
		Inviter: &inviter,
		Info:    `{"email": "a@example.com"}`,
	}))

	require.NoError(t, repo.UpdateMetaInfo(ctx, ccid, `{"email":"b@example.com"}`))

	meta, err := repo.GetMeta(ctx, ccid)
	require.NoError(t, err)
	require.Equal(t, `{"email": "b@example.com"}`, meta.Info)
	require.NotNil(t, meta.Inviter)
	require.Equal(t, inviter, *meta.Inviter)
}

// Unregister must flag only commits the departing user owns alone: a commit
// co-owned by another user (an ack between two local users) must survive,
// since GCing it would cascade-delete the co-owner's rows.
func TestResidenceMarkCommitLogsGcCandidateByOwner(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := &ResidenceRepository{db: db}

	userA := "con1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	userB := "con1pppppppppppppppppppppppppppppppppppppppp"

	seed := func(id string, owners ...string) {
		t.Helper()
		require.NoError(t, db.Create(&models.CommitLog{ID: id, Document: "{}", Proof: "{}"}).Error)
		for _, owner := range owners {
			require.NoError(t, db.Create(&models.CommitOwner{CommitLogID: id, Owner: owner}).Error)
		}
	}
	seed("log-a-sole", userA)
	seed("log-b-sole", userB)
	seed("log-shared", userA, userB)

	flagged := func() map[string]bool {
		t.Helper()
		var logs []models.CommitLog
		require.NoError(t, db.Find(&logs).Error)
		out := map[string]bool{}
		for _, log := range logs {
			out[log.ID] = log.GcCandidate
		}
		return out
	}

	require.NoError(t, repo.MarkCommitLogsGcCandidateByOwner(ctx, userA))
	want := map[string]bool{"log-a-sole": true, "log-b-sole": false, "log-shared": false}
	require.Equal(t, want, flagged())

	// idempotent: a second run changes nothing
	require.NoError(t, repo.MarkCommitLogsGcCandidateByOwner(ctx, userA))
	require.Equal(t, want, flagged())
}
