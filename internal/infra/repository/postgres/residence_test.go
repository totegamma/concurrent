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

// Unregister flags every commit the departing user owns — since every commit
// has exactly one owner (an ack between two local users is two commits: the
// ack owned by the acker plus its mirror owned by the ackee), the whole
// repository is eligible and nothing lingers. Unowned pass-through commits
// and other users' commits are untouched.
func TestResidenceMarkCommitLogsGcCandidateByOwner(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := &ResidenceRepository{db: db}

	userA := "con1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	userB := "con1pppppppppppppppppppppppppppppppppppppppp"

	seed := func(id string, owner *string) {
		t.Helper()
		require.NoError(t, db.Create(&models.CommitLog{ID: id, Owner: owner, Document: "{}", Proof: "{}"}).Error)
	}
	seed("log-a-ack", &userA)    // A's side of an A<->B ack
	seed("log-b-mirror", &userB) // B's mirror of the same ack
	seed("log-unowned", nil)     // pass-through commit nobody local owns

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
	want := map[string]bool{"log-a-ack": true, "log-b-mirror": false, "log-unowned": false}
	require.Equal(t, want, flagged())

	// idempotent: a second run changes nothing
	require.NoError(t, repo.MarkCommitLogsGcCandidateByOwner(ctx, userA))
	require.Equal(t, want, flagged())
}
