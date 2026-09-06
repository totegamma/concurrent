package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/stretchr/testify/require"
)

// Ack state at the repository layer: the acker's server keeps it in acks
// (anchored to the ack commit), the target's server in ackeds (anchored to
// the acked mirror commit), and /acknowledges serves whichever side this
// server holds.

// The target-side acked/unacked state follows the same accept-if-newer
// transitions as the acker-side ack state (CIP-10 §4 / §5.2): only a strictly
// newer document moves the (from, to, schema) row, older replays are no-ops,
// and the acks table stays untouched.
func TestAckedRepositoryAcceptIfNewer(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	from, to := "con1remote", "con1owner"
	schema := "https://schema.example/follow.json"
	associate := "cckv://" + to
	t1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	mirror := func(kind string, createdAt time.Time) concrnt.SignedDocument {
		return repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      kind,
			Value:     map[string]string{"context": "follow"},
			Author:    from,
			Schema:    schema,
			CreatedAt: createdAt,
			Associate: &associate,
		})
	}
	state := func() models.Acked {
		t.Helper()
		var row models.Acked
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, from, to, schema).Take(&row).Error)
		return row
	}

	withRepositoryTx(t, ctx, repo, "acked-1-on", "127.0.0.1", mirror("acked", t1), to, func(tx usecase.RepositoryTx) error {
		applied, err := repo.Acknowledged(ctx, tx, "acked-1-on", from, to, schema, t1)
		if err != nil {
			return err
		}
		require.True(t, applied)
		return nil
	})
	row := state()
	require.True(t, row.Valid)
	require.Equal(t, "acked-1-on", row.DocumentID)
	require.True(t, row.CreatedAt.Equal(t1))
	requireCommitOwner(t, db, "acked-1-on", to)

	withRepositoryTx(t, ctx, repo, "acked-2-off", "127.0.0.1", mirror("unacked", t1), to, func(tx usecase.RepositoryTx) error {
		applied, err := repo.UnAcknowledged(ctx, tx, "acked-2-off", from, to, schema, t1)
		if err != nil {
			return err
		}
		require.True(t, applied)
		return nil
	})
	row = state()
	require.False(t, row.Valid)
	require.Equal(t, "acked-2-off", row.DocumentID)

	// an older acked replayed after the unacked must not resurrect it
	tx, err := repo.BeginTx(ctx)
	require.NoError(t, err)
	applied, err := repo.Acknowledged(ctx, tx, "acked-0-stale", from, to, schema, t1)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, "acked-2-off", state().DocumentID)

	// a newer acked wins, and an unacked from before it is a no-op
	withRepositoryTx(t, ctx, repo, "acked-3-on", "127.0.0.1", mirror("acked", t2), to, func(tx usecase.RepositoryTx) error {
		applied, err := repo.Acknowledged(ctx, tx, "acked-3-on", from, to, schema, t2)
		if err != nil {
			return err
		}
		require.True(t, applied)
		return nil
	})
	tx, err = repo.BeginTx(ctx)
	require.NoError(t, err)
	applied, err = repo.UnAcknowledged(ctx, tx, "acked-2x-stale", from, to, schema, t1)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, tx.Rollback(ctx))
	row = state()
	require.True(t, row.Valid)
	require.Equal(t, "acked-3-on", row.DocumentID)
	require.True(t, row.CreatedAt.Equal(t2))

	var ackCount int64
	require.NoError(t, db.Model(&models.Ack{}).Count(&ackCount).Error)
	require.EqualValues(t, 0, ackCount, "target-side state must not leak into the acker-side acks table")
}

// /acknowledges returns the side of each relationship this server holds
// (CIP-10 §6): the ack commit for acks made by local users, the acked mirror
// for acks received by local users. A to-filtered listing must therefore
// include the ackeds rows, and the counts must add them up.
func TestGetAcknowledgeRecordsIncludesAcked(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	schema := "https://schema.example/follow.json"
	local, localAcker, remoteAcker := "con1owner", "con1author", "con1remote"
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	doc := func(kind, author string) concrnt.SignedDocument {
		associate := "cckv://" + local
		return repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      kind,
			Value:     map[string]string{"context": "follow"},
			Author:    author,
			Schema:    schema,
			CreatedAt: at,
			Associate: &associate,
		})
	}

	// a local user acking the local target: this server holds the ack
	withRepositoryTx(t, ctx, repo, "ack-local", "127.0.0.1", doc("ack", localAcker), localAcker, func(tx usecase.RepositoryTx) error {
		_, err := repo.Acknowledge(ctx, tx, "ack-local", localAcker, local, schema, at)
		return err
	})
	// a remote user acking the local target: this server holds the mirror
	withRepositoryTx(t, ctx, repo, "acked-remote", "127.0.0.1", doc("acked", remoteAcker), local, func(tx usecase.RepositoryTx) error {
		_, err := repo.Acknowledged(ctx, tx, "acked-remote", remoteAcker, local, schema, at.Add(time.Hour))
		return err
	})

	kinds := func(rows []usecase.QueryRow) []string {
		t.Helper()
		out := make([]string, len(rows))
		for i, row := range rows {
			parsed, err := row.Row.ParsedDocument()
			require.NoError(t, err)
			out[i] = parsed.Kind
		}
		return out
	}

	rows, err := repo.GetAcknowledgeRecords(ctx, "", local, "", nil, nil, 0, "desc")
	require.NoError(t, err)
	require.Equal(t, []string{"acked", "ack"}, kinds(rows), "to-filtered listing must include the acked mirror, newest first")

	rows, err = repo.GetAcknowledgeRecords(ctx, remoteAcker, "", "", nil, nil, 0, "desc")
	require.NoError(t, err)
	require.Equal(t, []string{"acked"}, kinds(rows))

	rows, err = repo.GetAcknowledgeRecords(ctx, localAcker, "", "", nil, nil, 0, "desc")
	require.NoError(t, err)
	require.Equal(t, []string{"ack"}, kinds(rows))

	counts, err := repo.GetAcknowledgeRecordCounts(ctx, "", local, "")
	require.NoError(t, err)
	require.EqualValues(t, 2, counts[schema])
}
