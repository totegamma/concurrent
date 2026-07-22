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
		withRepositoryTx(t, ctx, repo, "record-1", "127.0.0.1", oldSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.CreateRecord(ctx, tx, "record-1", key, "con1owner", "https://schema.example/post.json", &onUpdate, nil, distributions, nil, createdAt)
		})

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "record-1").Take(&commit).Error)
		require.Equal(t, oldSD.Document, commit.Document)
		require.JSONEq(t, `{"type":"none"}`, commit.Proof)
		require.False(t, commit.GcCandidate)

		requireCommitOwner(t, db, "record-1", "con1owner")

		var record models.Record
		require.NoError(t, db.Where("document_id = ?", "record-1").Take(&record).Error)
		require.Equal(t, "con1owner", record.Owner)
		require.Equal(t, "https://schema.example/post.json", record.Schema)
		require.Equal(t, distributions, []string(record.Distributions))
		require.True(t, record.CreatedAt.Equal(createdAt))

		var recordKey models.RecordKey
		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-1", *recordKey.RecordID)
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
		withRepositoryTx(t, ctx, repo, "record-2", "127.0.0.1", newSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.CreateRecord(ctx, tx, "record-2", key, "con1owner", "https://schema.example/post.v2.json", &onUpdate, nil, []string{}, nil, newCreatedAt)
		})

		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-2", *recordKey.RecordID)
		require.NotNil(t, recordKey.RecordCreatedAt)
		require.True(t, recordKey.RecordCreatedAt.Equal(newCreatedAt))

		require.NoError(t, db.Where("id = ?", "record-1").Take(&commit).Error)
		require.True(t, commit.GcCandidate)

		var oldRecordCount int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "record-1").Count(&oldRecordCount).Error)
		require.Zero(t, oldRecordCount)

		var newRecord models.Record
		require.NoError(t, db.Where("document_id = ?", "record-2").Take(&newRecord).Error)
		require.Equal(t, "https://schema.example/post.v2.json", newRecord.Schema)
		require.True(t, newRecord.CreatedAt.Equal(newCreatedAt))
	})

	t.Run("older document is a no-op", func(t *testing.T) {
		// accept-if-newer (CIP-3 §3.4): "record-0" sorts before the stored
		// "record-2", so this write must not take effect nor GC the live record
		staleCreatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		staleSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "record",
			Key:       key,
			Value:     map[string]string{"body": "stale"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.json",
			CreatedAt: staleCreatedAt,
		})
		onUpdate := "forget"
		withRepositoryTx(t, ctx, repo, "record-0", "127.0.0.1", staleSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.CreateRecord(ctx, tx, "record-0", key, "con1owner", "https://schema.example/post.json", &onUpdate, nil, []string{}, nil, staleCreatedAt)
		})

		var recordKey models.RecordKey
		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-2", *recordKey.RecordID)

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "record-2").Take(&commit).Error)
		require.False(t, commit.GcCandidate)
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
			return repo.CreateRecord(ctx, tx, "reference-record", "cckv://con1owner/timeline/ref-1", "con1owner", "https://schema.example/target.json", nil, nil, []string{}, &targetURI, targetCreatedAt)
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
		// document IDs are sortable CDIDs; "ack-1on" < "ack-2off" mimics the
		// chronological order of the two transitions
		ackCreatedAt := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
		var ackUpdated bool
		withRepositoryTx(t, ctx, repo, "ack-1on", "127.0.0.1", ackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			ackUpdated, err = repo.Acknowledge(ctx, tx, "ack-1on", "con1author", "con1owner", ackSchema, ackCreatedAt)
			return err
		})
		require.True(t, ackUpdated)

		var ack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&ack).Error)
		require.True(t, ack.Valid)
		require.Equal(t, "ack-1on", ack.DocumentID)
		requireCommitOwner(t, db, "ack-1on", "con1author")
		requireCommitOwner(t, db, "ack-1on", "con1owner")

		unackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Kind:      "unack",
			Value:     map[string]string{"context": "like"},
			Author:    "con1author",
			Schema:    ackSchema,
			CreatedAt: ackCreatedAt,
			Associate: &key,
		})
		var unackUpdated bool
		withRepositoryTx(t, ctx, repo, "ack-2off", "127.0.0.1", unackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			unackUpdated, err = repo.UnAcknowledge(ctx, tx, "ack-2off", "con1author", "con1owner", ackSchema, ackCreatedAt)
			return err
		})
		require.True(t, unackUpdated)

		var unack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&unack).Error)
		require.False(t, unack.Valid)
		require.Equal(t, "ack-2off", unack.DocumentID)
		requireCommitOwner(t, db, "ack-2off", "con1author")
		requireCommitOwner(t, db, "ack-2off", "con1owner")

		// CIP-10 §4: a replayed older ack ("ack-1on" < stored "ack-2off") must
		// not roll the state back — the upsert reports no change
		var staleUpdated bool
		withRepositoryTx(t, ctx, repo, "ack-1on", "127.0.0.1", ackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			staleUpdated, err = repo.Acknowledge(ctx, tx, "ack-1on", "con1author", "con1owner", ackSchema, ackCreatedAt)
			return err
		})
		require.False(t, staleUpdated)

		var afterReplay models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND schema = ?`, "con1author", "con1owner", ackSchema).Take(&afterReplay).Error)
		require.False(t, afterReplay.Valid)
		require.Equal(t, "ack-2off", afterReplay.DocumentID)

		// an identical redelivery of the stored transition is likewise a no-op
		// success, not a primary-key conflict
		var redelivered bool
		withRepositoryTx(t, ctx, repo, "ack-2off", "127.0.0.1", unackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			redelivered, err = repo.UnAcknowledge(ctx, tx, "ack-2off", "con1author", "con1owner", ackSchema, ackCreatedAt)
			return err
		})
		require.False(t, redelivered)
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
		err = repo.CreateRecord(ctx, tx, "rollback-record", rollbackKey, "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, tx.Rollback(ctx))

		var count int64
		require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)

		// the replay guard sees committed rows only: a rolled-back commit
		// (e.g. an accept-if-newer no-op) leaves no trace
		has, err := repo.HasCommitLog(ctx, "rollback-record")
		require.NoError(t, err)
		require.False(t, has)
		has, err = repo.HasCommitLog(ctx, "record-2")
		require.NoError(t, err)
		require.True(t, has)
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
			return repo.CreateRecord(ctx, tx, "delete-target", deleteKey, "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC))
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

		err = repo.CreateRecord(ctx, nil, "invalid", key, "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Now())
		require.Error(t, err)

		err = repo.CreateRecord(ctx, fakeRecordTx{}, "invalid", key, "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, time.Now())
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
			return repo.CreateRecord(ctx, tx, id, key, "con1owner", "https://schema.example/item.json", nil, nil, []string{}, nil, createdAt)
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
			return repo.CreateRecord(ctx, tx, id, key, "con1powner", "https://schema.example/post.json", nil, &p, []string{}, nil, createdAt)
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
