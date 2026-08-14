package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/schemas"
)

func TestRecordRepositoryWrites(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	key := "cckv://con1owner/timeline/post-1"

	t.Run("create and update record", func(t *testing.T) {
		createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		distributions := []string{"cckv://con1channel/timeline"}
		oldSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:        "record",
			Key:         key,
			Value:       map[string]string{"body": "old"},
			Author:      "con1author",
			Schema:      "https://schema.example/post.json",
			CreatedAt:   createdAt,
			Distributes: &distributions,
		})
		onUpdate := "forget"
		withRepositoryTx(t, ctx, repo, "record-1-old", "127.0.0.1", oldSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.CreateRecord(ctx, tx, "record-1-old", key, "con1owner", "con1owner", "https://schema.example/post.json", &onUpdate, nil, distributions, nil, createdAt)
			require.True(t, applied)
			return err
		})

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "record-1-old").Take(&commit).Error)
		require.Equal(t, oldSD.Document, commit.Document)
		require.JSONEq(t, `{"type":"none"}`, commit.Proof)
		require.False(t, commit.GcCandidate)

		requireCommitOwner(t, db, "record-1-old", "con1owner")

		var record models.Record
		require.NoError(t, db.Where("document_id = ?", "record-1-old").Take(&record).Error)
		require.Equal(t, "con1owner", record.Owner)
		require.Equal(t, "https://schema.example/post.json", record.Schema)
		require.Equal(t, distributions, []string(record.Distributions))
		require.True(t, record.CreatedAt.Equal(createdAt))

		var recordKey models.RecordKey
		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-1-old", *recordKey.RecordID)
		require.NotNil(t, recordKey.RecordCreatedAt)
		require.True(t, recordKey.RecordCreatedAt.Equal(createdAt))

		gotSD, err := repo.GetSignedDocument(ctx, key)
		require.NoError(t, err)
		require.Equal(t, oldSD.Document, gotSD.Document)
		require.Equal(t, oldSD.Proof, gotSD.Proof)

		newCreatedAt := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
		newSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": "new"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.v2.json",
			CreatedAt: newCreatedAt,
		})
		withRepositoryTx(t, ctx, repo, "record-2-new", "127.0.0.1", newSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.CreateRecord(ctx, tx, "record-2-new", key, "con1owner", "con1owner", "https://schema.example/post.v2.json", &onUpdate, nil, []string{}, nil, newCreatedAt)
			require.True(t, applied)
			return err
		})

		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-2-new", *recordKey.RecordID)
		require.NotNil(t, recordKey.RecordCreatedAt)
		require.True(t, recordKey.RecordCreatedAt.Equal(newCreatedAt))

		require.NoError(t, db.Where("id = ?", "record-1-old").Take(&commit).Error)
		require.True(t, commit.GcCandidate)

		var oldRecordCount int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "record-1-old").Count(&oldRecordCount).Error)
		require.Zero(t, oldRecordCount)

		var newRecord models.Record
		require.NoError(t, db.Where("document_id = ?", "record-2-new").Take(&newRecord).Error)
		require.Equal(t, "https://schema.example/post.v2.json", newRecord.Schema)
		require.True(t, newRecord.CreatedAt.Equal(newCreatedAt))

		// accept-if-newer: a replayed older (or identical) document must not
		// roll the key back, and must not tombstone the stored newer version.
		tx, err := repo.BeginTx(ctx)
		require.NoError(t, err)
		applied, err := repo.CreateRecord(ctx, tx, "record-0-stale", key, "con1owner", "con1owner", "https://schema.example/post.json", &onUpdate, nil, distributions, nil, createdAt)
		require.NoError(t, err)
		require.False(t, applied)
		applied, err = repo.CreateRecord(ctx, tx, "record-2-new", key, "con1owner", "con1owner", "https://schema.example/post.v2.json", &onUpdate, nil, []string{}, nil, newCreatedAt)
		require.NoError(t, err)
		require.False(t, applied)
		require.NoError(t, tx.Rollback(ctx))

		var keptRecordKey models.RecordKey
		require.NoError(t, db.Where("uri = ?", key).Take(&keptRecordKey).Error)
		require.Equal(t, "record-2-new", *keptRecordKey.RecordID)
		var keptCommit models.CommitLog
		require.NoError(t, db.Where("id = ?", "record-2-new").Take(&keptCommit).Error)
		require.False(t, keptCommit.GcCandidate)
		var newRecordCount int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "record-2-new").Count(&newRecordCount).Error)
		require.EqualValues(t, 1, newRecordCount)
	})

	// CIP-1 §5.7: omitting onUpdate means forget — replacing the key must drop
	// the old record and mark its commit for GC. Only an explicit "retain"
	// keeps the old generation.
	t.Run("onUpdate defaults to forget, explicit retain keeps history", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			onUpdate *string
			forgets  bool
		}{
			{"default", nil, true},
			{"retain", ptr("retain"), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				key := "cckv://con1owner/timeline/onupdate-" + tc.name
				oldID := "onupdate-" + tc.name + "-1-old"
				newID := "onupdate-" + tc.name + "-2-new"
				for i, id := range []string{oldID, newID} {
					createdAt := time.Date(2026, 1, 2, 3, 4, 5+i, 0, time.UTC)
					sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
						Kind:      "record",
						Key:       key,
						Value:     map[string]string{"body": id},
						Author:    "con1author",
						Schema:    "https://schema.example/post.json",
						CreatedAt: createdAt,
						OnUpdate:  tc.onUpdate,
					})
					withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
						applied, err := repo.CreateRecord(ctx, tx, id, key, "con1owner", "con1owner", "https://schema.example/post.json", tc.onUpdate, nil, []string{}, nil, createdAt)
						require.True(t, applied)
						return err
					})
				}

				var recordKey models.RecordKey
				require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
				require.NotNil(t, recordKey.RecordID)
				require.Equal(t, newID, *recordKey.RecordID)

				var commit models.CommitLog
				require.NoError(t, db.Where("id = ?", oldID).Take(&commit).Error)
				require.Equal(t, tc.forgets, commit.GcCandidate)

				var oldRecordCount int64
				require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", oldID).Count(&oldRecordCount).Error)
				if tc.forgets {
					require.Zero(t, oldRecordCount)
				} else {
					require.EqualValues(t, 1, oldRecordCount)
				}
			})
		}
	})

	// CIP-0 §8.4: entities are exempt from onUpdate — replacing an entity
	// document must keep the old commit intact (no GC flag), even when the
	// document itself declares onUpdate: forget.
	t.Run("entity replacement retains the old document", func(t *testing.T) {
		for i, id := range []string{"entity-1-old", "entity-2-new"} {
			createdAt := time.Date(2026, 1, 2, 3, 4, 5+i, 0, time.UTC)
			sd := repositorySignedDocument(t, concrnt.Document[schemas.Entity]{
				Kind:      "entity",
				Value:     schemas.Entity{Domain: "example.com"},
				Author:    "con1entity",
				Schema:    schemas.EntityURL,
				CreatedAt: createdAt,
				OnUpdate:  ptr("forget"),
			})
			withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1entity"}, func(tx usecase.RepositoryTx) error {
				applied, err := repo.CreateEntity(ctx, tx, "con1entity", nil, "example.com", id)
				require.True(t, applied)
				return err
			})
		}

		var entity models.Entity
		require.NoError(t, db.Where("id = ?", "con1entity").Take(&entity).Error)
		require.Equal(t, "entity-2-new", entity.DocumentID)

		var oldCommit models.CommitLog
		require.NoError(t, db.Where("id = ?", "entity-1-old").Take(&oldCommit).Error)
		require.False(t, oldCommit.GcCandidate)
	})

	t.Run("create reference record", func(t *testing.T) {
		targetURI := "cckv://con1target/timeline/post-2"
		targetCreatedAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
		targetSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       targetURI,
			Value:     map[string]string{"body": "target"},
			Author:    "con1target",
			Schema:    "https://schema.example/target.json",
			CreatedAt: targetCreatedAt,
		})

		refSD := repositorySignedDocument(t, concrnt.Document[schemas.Reference]{
			Kind: "record",
			Key:  "cckv://con1owner/timeline/ref-1",
			Value: schemas.Reference{
				Href: targetURI,
			},
			Author:    "con1owner",
			Schema:    schemas.ReferenceURL,
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		})
		refSD.References = map[string]concrnt.SignedDocument{
			targetURI: targetSD,
		}

		withRepositoryTx(t, ctx, repo, "reference-record", "127.0.0.1", refSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, "reference-record", "cckv://con1owner/timeline/ref-1", "con1owner", "con1owner", "https://schema.example/target.json", nil, nil, []string{}, &targetURI, targetCreatedAt)
			return err
		})

		var record models.Record
		require.NoError(t, db.Where("document_id = ?", "reference-record").Take(&record).Error)
		require.NotNil(t, record.Redirect)
		require.Equal(t, targetURI, *record.Redirect)
		require.Equal(t, "https://schema.example/target.json", record.Schema)
		require.True(t, record.CreatedAt.Equal(targetCreatedAt))
	})

	t.Run("timeline removal tuple matches chunkline body IDs", func(t *testing.T) {
		chunklineRepo := NewChunklineRepository(db)

		// plain record: item ID is the record key URI
		timeline, itemID, err := repo.GetTimelineRemoval(ctx, key)
		require.NoError(t, err)
		require.Equal(t, "cckv://con1owner/timeline", timeline)
		require.Equal(t, key, itemID)

		// reference record: item ID is the redirect target
		refKey := "cckv://con1owner/timeline/ref-1"
		refTarget := "cckv://con1target/timeline/post-2"
		timeline, itemID, err = repo.GetTimelineRemoval(ctx, refKey)
		require.NoError(t, err)
		require.Equal(t, "cckv://con1owner/timeline", timeline)
		require.Equal(t, refTarget, itemID)

		// the advertised ID must equal the BodyItem.ID() the body endpoint
		// serves for the same member, or readers can't match them up
		for keyURI, wantID := range map[string]string{key: key, refKey: refTarget} {
			var recordKey models.RecordKey
			require.NoError(t, db.Where("uri = ?", keyURI).Take(&recordKey).Error)
			require.NotNil(t, recordKey.RecordCreatedAt)
			chunkID := recordKey.RecordCreatedAt.Unix() / 600

			items, err := chunklineRepo.LoadLocalBody(ctx, "cckv://con1owner/timeline", chunkID)
			require.NoError(t, err)
			ids := make([]string, 0, len(items))
			for _, item := range items {
				ids = append(ids, item.ID())
			}
			require.Contains(t, ids, wantID)
		}

		// the timeline row itself is not a chunkline member
		timeline, itemID, err = repo.GetTimelineRemoval(ctx, "cckv://con1owner/timeline")
		require.NoError(t, err)
		require.Empty(t, timeline)
		require.Empty(t, itemID)

		// unknown keys report not-found
		_, _, err = repo.GetTimelineRemoval(ctx, "cckv://con1owner/timeline/no-such-key")
		require.Error(t, err)
	})

	t.Run("create association and toggle ack", func(t *testing.T) {
		variant := "reply"
		associationSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:               "association",
			Value:              map[string]string{"body": "comment"},
			Author:             "con1author",
			Schema:             "https://schema.example/comment.json",
			CreatedAt:          time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
			Associate:          &key,
			AssociationVariant: &variant,
		})
		associationUnique := fmt.Sprintf("%x", xxh3.HashString("con1owner"+"con1author"+key+variant))
		associationCreatedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
		var inserted bool
		withRepositoryTx(t, ctx, repo, "association-record", "127.0.0.1", associationSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			inserted, err = repo.CreateAssociation(ctx, tx, "association-record", key, "con1owner", "con1author", "https://schema.example/comment.json", &variant, associationUnique, associationCreatedAt)
			return err
		})
		require.True(t, inserted)

		var association models.Association
		require.NoError(t, db.Where("document_id = ?", "association-record").Take(&association).Error)
		require.Equal(t, "con1owner", association.Owner)
		require.Equal(t, "con1author", association.Author)
		require.Equal(t, variant, *association.Variant)
		require.Equal(t, associationUnique, association.Unique)
		requireCommitOwner(t, db, "association-record", "con1owner")

		// duplicate deliveries are silent no-ops: the same document re-sent
		// (primary-key conflict)...
		withRepositoryTx(t, ctx, repo, "association-record", "127.0.0.1", associationSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			inserted, err = repo.CreateAssociation(ctx, tx, "association-record", key, "con1owner", "con1author", "https://schema.example/comment.json", &variant, associationUnique, associationCreatedAt)
			return err
		})
		require.False(t, inserted)

		// ...and the same logical association re-signed under a new document id
		// (unique-key conflict)
		withRepositoryTx(t, ctx, repo, "association-record-retry", "127.0.0.1", associationSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			inserted, err = repo.CreateAssociation(ctx, tx, "association-record-retry", key, "con1owner", "con1author", "https://schema.example/comment.json", &variant, associationUnique, associationCreatedAt)
			return err
		})
		require.False(t, inserted)

		var associationCount int64
		require.NoError(t, db.Model(&models.Association{}).Where(`"unique" = ?`, associationUnique).Count(&associationCount).Error)
		require.EqualValues(t, 1, associationCount)

		ackSchema := "https://schema.example/like.json"
		ackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "ack",
			Value:     map[string]string{"context": "like"},
			Author:    "con1author",
			Schema:    ackSchema,
			CreatedAt: time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
			Associate: &key,
		})
		ackCreatedAt := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
		withRepositoryTx(t, ctx, repo, "ack-1-on", "127.0.0.1", ackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.Acknowledge(ctx, tx, "ack-1-on", "con1author", "con1owner", ackSchema, ackCreatedAt)
			require.True(t, applied)
			return err
		})

		var ack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&ack).Error)
		require.True(t, ack.Valid)
		require.Equal(t, "ack-1-on", ack.DocumentID)
		requireCommitOwner(t, db, "ack-1-on", "con1author")
		requireCommitOwner(t, db, "ack-1-on", "con1owner")

		unackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "unack",
			Value:     map[string]string{"context": "like"},
			Author:    "con1author",
			Schema:    ackSchema,
			CreatedAt: ackCreatedAt,
			Associate: &key,
		})
		withRepositoryTx(t, ctx, repo, "ack-2-off", "127.0.0.1", unackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.UnAcknowledge(ctx, tx, "ack-2-off", "con1author", "con1owner", ackSchema, ackCreatedAt)
			require.True(t, applied)
			return err
		})

		var unack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&unack).Error)
		require.False(t, unack.Valid)
		require.Equal(t, "ack-2-off", unack.DocumentID)
		requireCommitOwner(t, db, "ack-2-off", "con1author")
		requireCommitOwner(t, db, "ack-2-off", "con1owner")

		// CIP-10 §4 accept-if-newer: a replayed ack older than the stored
		// unack must not resurrect it...
		tx, err := repo.BeginTx(ctx)
		require.NoError(t, err)
		applied, err := repo.Acknowledge(ctx, tx, "ack-0-stale", "con1author", "con1owner", ackSchema, ackCreatedAt)
		require.NoError(t, err)
		require.False(t, applied)
		require.NoError(t, tx.Rollback(ctx))

		var afterStaleAck models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&afterStaleAck).Error)
		require.False(t, afterStaleAck.Valid)
		require.Equal(t, "ack-2-off", afterStaleAck.DocumentID)

		// ...and after a newer ack (t2), an unack replayed from before it (t1)
		// must not roll the state back either.
		reackCreatedAt := time.Date(2026, 4, 6, 6, 7, 8, 0, time.UTC)
		reackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "ack",
			Value:     map[string]string{"context": "like"},
			Author:    "con1author",
			Schema:    ackSchema,
			CreatedAt: reackCreatedAt,
			Associate: &key,
		})
		withRepositoryTx(t, ctx, repo, "ack-3-on", "127.0.0.1", reackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.Acknowledge(ctx, tx, "ack-3-on", "con1author", "con1owner", ackSchema, reackCreatedAt)
			require.True(t, applied)
			return err
		})

		tx, err = repo.BeginTx(ctx)
		require.NoError(t, err)
		applied, err = repo.UnAcknowledge(ctx, tx, "ack-2x-stale", "con1author", "con1owner", ackSchema, ackCreatedAt)
		require.NoError(t, err)
		require.False(t, applied)
		require.NoError(t, tx.Rollback(ctx))

		var afterStaleUnack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&afterStaleUnack).Error)
		require.True(t, afterStaleUnack.Valid)
		require.Equal(t, "ack-3-on", afterStaleUnack.DocumentID)
		require.True(t, afterStaleUnack.CreatedAt.Equal(reackCreatedAt))
	})

	t.Run("rollback removes commit and record", func(t *testing.T) {
		rollbackKey := "cckv://con1owner/timeline/rollback"
		rollbackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       rollbackKey,
			Value:     map[string]string{"body": "rollback"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.json",
			CreatedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
		})

		tx, err := repo.BeginTx(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.CreateCommitLog(ctx, tx, "rollback-record", "127.0.0.1", rollbackSD.Document, rollbackSD.Proof))
		require.NoError(t, repo.CreateCommitOwners(ctx, tx, "rollback-record", []string{"con1owner"}))
		_, err = repo.CreateRecord(ctx, tx, "rollback-record", rollbackKey, "con1owner", "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, tx.Rollback(ctx))

		var count int64
		require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("delete removes record payload and keeps commits", func(t *testing.T) {
		deleteKey := "cckv://con1owner/timeline/delete-target"
		deleteTargetSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       deleteKey,
			Value:     map[string]string{"body": "delete target"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.json",
			CreatedAt: time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC),
		})
		withRepositoryTx(t, ctx, repo, "delete-target", "127.0.0.1", deleteTargetSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, "delete-target", deleteKey, "con1owner", "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC))
			return err
		})

		deleteSD := repositorySignedDocument(t, concrnt.Document[schemas.Delete]{
			Kind:      "delete",
			Value:     schemas.Delete(deleteKey),
			Author:    "con1author",
			CreatedAt: time.Date(2026, 6, 8, 8, 9, 10, 0, time.UTC),
		})
		withRepositoryTx(t, ctx, repo, "delete-commit", "127.0.0.1", deleteSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.DeleteRecordByKey(ctx, tx, deleteKey)
		})

		var count int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "delete-target").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", "delete-target").Count(&count).Error)
		require.EqualValues(t, 1, count)

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "delete-commit").Take(&commit).Error)
		require.Equal(t, deleteSD.Document, commit.Document)
		requireCommitOwner(t, db, "delete-commit", "con1owner")
	})

	t.Run("commit log methods marshal proof and ignore conflicts", func(t *testing.T) {
		href := "cckv://con1owner/timeline/conflict"
		proof := concrnt.Proof{
			Type: concrnt.ProofTypeNone,
			Href: &href,
		}

		tx, err := repo.BeginTx(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.CreateCommitLog(ctx, tx, "commit-methods", "127.0.0.1", "first", proof))
		require.NoError(t, repo.CreateCommitLog(ctx, tx, "commit-methods", "192.0.2.1", "second", concrnt.Proof{Type: "ignored"}))
		require.NoError(t, repo.CreateCommitOwners(ctx, tx, "commit-methods", []string{"con1owner", "con1owner"}))
		require.NoError(t, tx.Commit(ctx))

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "commit-methods").Take(&commit).Error)
		require.Equal(t, "127.0.0.1", commit.IP)
		require.Equal(t, "first", commit.Document)
		require.JSONEq(t, `{"type":"none","href":"cckv://con1owner/timeline/conflict"}`, commit.Proof)

		var ownerCount int64
		require.NoError(t, db.Model(&models.CommitOwner{}).Where("commit_log_id = ? AND owner = ?", "commit-methods", "con1owner").Count(&ownerCount).Error)
		require.EqualValues(t, 1, ownerCount)
	})

	t.Run("write methods reject invalid tx", func(t *testing.T) {
		err := repo.CreateCommitLog(ctx, nil, "invalid", "127.0.0.1", "{}", concrnt.Proof{Type: concrnt.ProofTypeNone})
		require.Error(t, err)

		err = repo.CreateCommitLog(ctx, fakeRecordTx{}, "invalid", "127.0.0.1", "{}", concrnt.Proof{Type: concrnt.ProofTypeNone})
		require.Error(t, err)

		err = repo.CreateCommitOwners(ctx, nil, "invalid", []string{"con1owner"})
		require.Error(t, err)

		err = repo.CreateCommitOwners(ctx, fakeRecordTx{}, "invalid", []string{"con1owner"})
		require.Error(t, err)

		_, err = repo.CreateRecord(ctx, nil, "invalid", key, "con1owner", "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Now())
		require.Error(t, err)

		_, err = repo.CreateRecord(ctx, fakeRecordTx{}, "invalid", key, "con1owner", "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Now())
		require.Error(t, err)

		err = repo.DeleteRecordByKey(ctx, nil, key)
		require.Error(t, err)

		err = repo.DeleteRecordByKey(ctx, fakeRecordTx{}, key)
		require.Error(t, err)

		err = repo.DeleteRecordByDocumentID(ctx, nil, "invalid")
		require.Error(t, err)

		err = repo.DeleteRecordByDocumentID(ctx, fakeRecordTx{}, "invalid")
		require.Error(t, err)

		err = repo.DeleteAssociation(ctx, nil, "invalid")
		require.Error(t, err)

		err = repo.DeleteAssociation(ctx, fakeRecordTx{}, "invalid")
		require.Error(t, err)
	})
}

// QueryRecordSubtree must match by path hierarchy — never siblings sharing a
// string prefix, never keys that only match via unescaped LIKE metacharacters —
// and deleting the enumerated keys must remove exactly those records (with
// their associations) while leaving siblings and commitlogs in place.
func TestRecordSubtreeQueryAndDelete(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	base := "cckv://con1owner/lists/item"
	keys := []string{
		base,                             // the base record itself
		base + "/a",                      // child
		base + "/a/b",                    // grandchild
		base + "2",                       // sibling sharing the string prefix — never in the subtree
		"cckv://con1owner/lists/it_em/x", // LIKE-metacharacter decoy: '_' matches any char unescaped
	}
	createdAt := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	for i, key := range keys {
		id := fmt.Sprintf("subtree-%d", i)
		sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": key},
			Author:    "con1owner",
			Schema:    "https://schema.example/item.json",
			CreatedAt: createdAt,
		})
		withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, id, key, "con1owner", "con1owner", "https://schema.example/item.json", nil, nil, []string{}, nil, createdAt)
			return err
		})
	}

	// an association targeting a subtree member must cascade with the delete
	variant := "reply"
	associationSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
		Kind:      "association",
		Value:     map[string]string{"body": "on child"},
		Author:    "con1author",
		Schema:    "https://schema.example/comment.json",
		CreatedAt: createdAt,
		Associate: &keys[1],
	})
	withRepositoryTx(t, ctx, repo, "subtree-association", "127.0.0.1", associationSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
		_, err := repo.CreateAssociation(ctx, tx, "subtree-association", keys[1], "con1owner", "con1author", "https://schema.example/comment.json", &variant, "subtree-unique", createdAt)
		return err
	})

	urisOf := func(sds []concrnt.SignedDocument) []string {
		uris := make([]string, 0, len(sds))
		for _, sd := range sds {
			require.NotNil(t, sd.CCKV)
			require.NotNil(t, sd.CCFS)
			uris = append(uris, *sd.CCKV)
		}
		return uris
	}

	t.Run("subtree including self", func(t *testing.T) {
		sds, err := repo.QueryRecordSubtree(ctx, base, true)
		require.NoError(t, err)
		require.Equal(t, []string{base, base + "/a", base + "/a/b"}, urisOf(sds))
	})

	t.Run("children only", func(t *testing.T) {
		sds, err := repo.QueryRecordSubtree(ctx, base, false)
		require.NoError(t, err)
		require.Equal(t, []string{base + "/a", base + "/a/b"}, urisOf(sds))
	})

	t.Run("metacharacters in base are escaped", func(t *testing.T) {
		// unescaped, 'it_em' would also match 'it/em', 'item', ... — it must
		// only match its own literal subtree
		sds, err := repo.QueryRecordSubtree(ctx, "cckv://con1owner/lists/it_em", true)
		require.NoError(t, err)
		require.Equal(t, []string{"cckv://con1owner/lists/it_em/x"}, urisOf(sds))
	})

	t.Run("subtree delete removes exactly the enumerated keys", func(t *testing.T) {
		sds, err := repo.QueryRecordSubtree(ctx, base, true)
		require.NoError(t, err)

		deleteSD := repositorySignedDocument(t, concrnt.Document[schemas.Delete]{
			Kind:      "delete",
			Value:     schemas.Delete(base + "*"),
			Author:    "con1owner",
			CreatedAt: createdAt,
		})
		withRepositoryTx(t, ctx, repo, "subtree-delete", "127.0.0.1", deleteSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			for _, uri := range urisOf(sds) {
				if err := repo.DeleteRecordByKey(ctx, tx, uri); err != nil {
					return err
				}
			}
			return nil
		})

		// the subtree is gone: records and their key rows
		var count int64
		for i := range 3 {
			require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", fmt.Sprintf("subtree-%d", i)).Count(&count).Error)
			require.Zero(t, count, "record %d must be deleted", i)
		}
		require.NoError(t, db.Model(&models.RecordKey{}).Where("uri IN ?", []string{base, base + "/a", base + "/a/b"}).Where("record_id IS NOT NULL").Count(&count).Error)
		require.Zero(t, count)

		// the association on the deleted child cascaded away
		require.NoError(t, db.Model(&models.Association{}).Where("document_id = ?", "subtree-association").Count(&count).Error)
		require.Zero(t, count)

		// siblings and decoys survive
		for i := 3; i < 5; i++ {
			require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", fmt.Sprintf("subtree-%d", i)).Count(&count).Error)
			require.EqualValues(t, 1, count, "record %d must survive", i)
		}

		// commitlogs of the deleted records stay (delete never erases history)
		for i := range 3 {
			require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", fmt.Sprintf("subtree-%d", i)).Count(&count).Error)
			require.EqualValues(t, 1, count, "commitlog %d must survive", i)
		}
	})

}

// The query API's author filter narrows list queries to documents written by
// one author — e.g. an AP outbox enumerating only the user's own posts out of
// their timeline. All keys in a timeline live in the timeline owner's space,
// so the discriminating column is the document author (which distribute
// references preserve: the document-reference proof forces the reference
// document's author to match the target's).
func TestRecordQueryAuthorFilter(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	parent := "cckv://con1owner/tl"
	entries := []struct {
		key    string
		author string
	}{
		{parent + "/e1", "con1alice"},
		{parent + "/e2", "con1bob"},
		{parent + "/e3", "con1alice"},
	}
	createdAt := time.Date(2026, 8, 14, 9, 10, 11, 0, time.UTC)
	for i, e := range entries {
		id := fmt.Sprintf("authorfilter-%d", i)
		sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       e.key,
			Value:     map[string]string{"body": e.key},
			Author:    e.author,
			Schema:    "https://schema.example/item.json",
			CreatedAt: createdAt.Add(time.Duration(i) * time.Second),
		})
		withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, id, e.key, "con1owner", e.author, "https://schema.example/item.json", nil, nil, []string{}, nil, createdAt.Add(time.Duration(i)*time.Second))
			return err
		})
	}

	urisOfRows := func(rows []usecase.QueryRow) []string {
		uris := make([]string, 0, len(rows))
		for _, row := range rows {
			require.NotNil(t, row.Row.CCKV)
			uris = append(uris, *row.Row.CCKV)
		}
		return uris
	}

	t.Run("by parent with author", func(t *testing.T) {
		rows, err := repo.QueryByParent(ctx, parent, "", "con1alice", nil, nil, 0, "asc")
		require.NoError(t, err)
		require.Equal(t, []string{parent + "/e1", parent + "/e3"}, urisOfRows(rows))
	})

	t.Run("by parent without author returns all", func(t *testing.T) {
		rows, err := repo.QueryByParent(ctx, parent, "", "", nil, nil, 0, "asc")
		require.NoError(t, err)
		require.Equal(t, []string{parent + "/e1", parent + "/e2", parent + "/e3"}, urisOfRows(rows))
	})

	t.Run("by prefix with author", func(t *testing.T) {
		rows, err := repo.QueryByPrefix(ctx, parent+"/", "", "con1bob", nil, nil, 0, "asc")
		require.NoError(t, err)
		require.Equal(t, []string{parent + "/e2"}, urisOfRows(rows))
	})
}

// CIP-12 §5.3: the hierarchical policy stack is emitted root-first with the
// target resource itself last.
func TestHierarchicalRecordPoliciesRootFirst(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	policyJSON := `{"entries":[{"url":"https://example.com/p.json"}]}`
	keys := []string{
		"cckv://con1powner/l1",
		"cckv://con1powner/l1/l2",
		"cckv://con1powner/l1/l2/l3",
	}
	for i, key := range keys {
		id := fmt.Sprintf("policy-record-%d", i)
		createdAt := time.Date(2026, 6, 1, 0, 0, i, 0, time.UTC)
		sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": "x"},
			Author:    "con1powner",
			Schema:    "https://schema.example/post.json",
			CreatedAt: createdAt,
		})
		p := policyJSON
		withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1powner"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.CreateRecord(ctx, tx, id, key, "con1powner", "con1powner", "https://schema.example/post.json", nil, &p, []string{}, nil, createdAt)
			require.True(t, applied)
			return err
		})
	}

	stack, err := repo.GetHierarchicalRecordPolicies(ctx, keys[2])
	require.NoError(t, err)

	sources := make([]string, len(stack))
	for i, layer := range stack {
		sources[i] = layer.Source
	}
	require.Equal(t, keys, sources, "layers must be emitted root-first, self last")
}

// A level with no policy of its own but with distributions must still be
// emitted — as an empty layer carrying its virtual parents — otherwise the
// destination timelines' policies never reach the stack (CIP-12 §5.3).
func TestHierarchicalRecordPoliciesEmitsPolicylessDistributingLevel(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	policyJSON := `{"entries":[{"url":"https://example.com/p.json"}]}`
	parentKey := "cckv://con1downer/d1"
	leafKey := "cckv://con1downer/d1/post"
	timeline := "cckv://con1tl/timelines/home"

	for i, tc := range []struct {
		key           string
		policies      *string
		distributions []string
	}{
		{parentKey, &policyJSON, []string{}},
		{leafKey, nil, []string{timeline}},
	} {
		id := fmt.Sprintf("distributing-record-%d", i)
		createdAt := time.Date(2026, 6, 2, 0, 0, i, 0, time.UTC)
		sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       tc.key,
			Value:     map[string]string{"body": "x"},
			Author:    "con1downer",
			Schema:    "https://schema.example/post.json",
			CreatedAt: createdAt,
		})
		withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1downer"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.CreateRecord(ctx, tx, id, tc.key, "con1downer", "con1downer", "https://schema.example/post.json", nil, tc.policies, tc.distributions, nil, createdAt)
			require.True(t, applied)
			return err
		})
	}

	stack, err := repo.GetHierarchicalRecordPolicies(ctx, leafKey)
	require.NoError(t, err)

	require.Len(t, stack, 2)
	require.Equal(t, parentKey, stack[0].Source)
	require.Equal(t, leafKey, stack[1].Source)
	require.Empty(t, stack[1].Entries, "a policyless level is an empty layer")
	require.NotNil(t, stack[1].VirtualParents)
	require.Equal(t, []string{timeline}, *stack[1].VirtualParents)
}

// A parent placeholder row (record_id IS NULL) must not trip the conditional
// upsert: writing a record to a key that so far only exists as a placeholder
// parent must apply.
func TestCreateRecordAppliesOverParentPlaceholder(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	parentKey := "cckv://con1ph/parent"
	childKey := "cckv://con1ph/parent/child"

	commit := func(id, key string, createdAt time.Time) {
		sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": "x"},
			Author:    "con1ph",
			Schema:    "https://schema.example/post.json",
			CreatedAt: createdAt,
		})
		withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1ph"}, func(tx usecase.RepositoryTx) error {
			applied, err := repo.CreateRecord(ctx, tx, id, key, "con1ph", "con1ph", "https://schema.example/post.json", nil, nil, []string{}, nil, createdAt)
			require.True(t, applied)
			return err
		})
	}

	// committing the child materializes the parent as a placeholder
	commit("placeholder-child", childKey, time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC))

	var rk models.RecordKey
	require.NoError(t, db.Where("uri = ?", parentKey).Take(&rk).Error)
	require.Nil(t, rk.RecordID, "the parent must exist as a placeholder")

	commit("placeholder-parent", parentKey, time.Date(2026, 6, 3, 0, 0, 1, 0, time.UTC))

	rk = models.RecordKey{}
	require.NoError(t, db.Where("uri = ?", parentKey).Take(&rk).Error)
	require.NotNil(t, rk.RecordID)
	require.Equal(t, "placeholder-parent", *rk.RecordID)
}

// SELECT ... FOR UPDATE cannot lock a RecordKey row that does not exist yet,
// so two commits racing on a fresh key both pass the fast-path — the
// conditional ON CONFLICT clause is what keeps the newer document. The older
// commit must come back unapplied whether it lands while the newer one is
// still in flight (blocked on the unique index) or after it committed.
func TestCreateRecordFreshKeyConditionalUpsert(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	repo := NewRecordRepository(db)

	key := "cckv://con1race/fresh"
	newID, newAt := "race-2-new", time.Date(2026, 6, 4, 0, 0, 1, 0, time.UTC)
	oldID, oldAt := "race-1-old", time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	signedFor := func(createdAt time.Time) concrnt.SignedDocument {
		return repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": "x"},
			Author:    "con1race",
			Schema:    "https://schema.example/post.json",
			CreatedAt: createdAt,
		})
	}

	// the newer commit inserts the fresh RecordKey but holds its tx open, so
	// the older commit can neither see nor lock the row
	tx1, err := repo.BeginTx(ctx)
	require.NoError(t, err)
	sdNew := signedFor(newAt)
	require.NoError(t, repo.CreateCommitLog(ctx, tx1, newID, "127.0.0.1", sdNew.Document, sdNew.Proof))
	applied, err := repo.CreateRecord(ctx, tx1, newID, key, "con1race", "con1race", "https://schema.example/post.json", nil, nil, []string{}, nil, newAt)
	require.NoError(t, err)
	require.True(t, applied)

	type outcome struct {
		applied bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		tx2, err := repo.BeginTx(ctx)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		sdOld := signedFor(oldAt)
		if err := repo.CreateCommitLog(ctx, tx2, oldID, "127.0.0.1", sdOld.Document, sdOld.Proof); err != nil {
			_ = tx2.Rollback(ctx)
			done <- outcome{err: err}
			return
		}
		applied, err := repo.CreateRecord(ctx, tx2, oldID, key, "con1race", "con1race", "https://schema.example/post.json", nil, nil, []string{}, nil, oldAt)
		if err != nil {
			_ = tx2.Rollback(ctx)
			done <- outcome{err: err}
			return
		}
		if applied {
			done <- outcome{applied: applied, err: tx2.Commit(ctx)}
			return
		}
		_ = tx2.Rollback(ctx)
		done <- outcome{applied: applied}
	}()

	// let the older commit reach the unique-index wait, then land the newer one
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, tx1.Commit(ctx))

	res := <-done
	require.NoError(t, res.err)
	require.False(t, res.applied, "the older document must lose the fresh-key race")

	var rk models.RecordKey
	require.NoError(t, db.Where("uri = ?", key).Take(&rk).Error)
	require.NotNil(t, rk.RecordID)
	require.Equal(t, newID, *rk.RecordID)
}

func withRepositoryTx(t *testing.T, ctx context.Context, repo usecase.RecordRepository, id string, ip string, sd concrnt.SignedDocument, owners []string, fn func(tx usecase.RepositoryTx) error) {
	t.Helper()

	tx, err := repo.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateCommitLog(ctx, tx, id, ip, sd.Document, sd.Proof))
	require.NoError(t, repo.CreateCommitOwners(ctx, tx, id, owners))
	if err := fn(tx); err != nil {
		require.NoError(t, tx.Rollback(ctx))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))
}

func ptr[T any](v T) *T {
	return &v
}

type fakeRecordTx struct{}

func (fakeRecordTx) Commit(ctx context.Context) error {
	return nil
}

func (fakeRecordTx) Rollback(ctx context.Context) error {
	return nil
}

func requireCommitOwner(t *testing.T, db *gorm.DB, commitID string, owner string) {
	t.Helper()

	var commitOwner models.CommitOwner
	require.NoError(t, db.Where("commit_log_id = ? AND owner = ?", commitID, owner).Take(&commitOwner).Error)
}

func repositorySignedDocument(t *testing.T, doc any) concrnt.SignedDocument {
	t.Helper()

	docBytes, err := json.Marshal(doc)
	require.NoError(t, err)

	return concrnt.SignedDocument{
		Document: string(docBytes),
		Proof: concrnt.Proof{
			Type: concrnt.ProofTypeNone,
		},
	}
}
