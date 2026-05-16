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
			Key:         key,
			Value:       map[string]string{"body": "old"},
			Author:      "con1author",
			Schema:      "https://schema.example/post.json",
			CreatedAt:   createdAt,
			Distributes: &distributions,
		})
		var resultURI string
		withRepositoryTx(t, ctx, repo, "record-old", "127.0.0.1", oldSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			resultURI, err = repo.CreateRecord(ctx, tx, "record-old", key, "con1owner", "https://schema.example/post.json", nil, distributions, nil, createdAt)
			return err
		})
		require.Equal(t, key, resultURI)

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "record-old").Take(&commit).Error)
		require.Equal(t, oldSD.Document, commit.Document)
		require.JSONEq(t, `{"type":"none"}`, commit.Proof)
		require.False(t, commit.GcCandidate)

		requireCommitOwner(t, db, "record-old", "con1owner")

		var record models.Record
		require.NoError(t, db.Where("document_id = ?", "record-old").Take(&record).Error)
		require.Equal(t, "con1owner", record.Owner)
		require.Equal(t, "https://schema.example/post.json", record.Schema)
		require.Equal(t, distributions, []string(record.Distributions))
		require.True(t, record.CreatedAt.Equal(createdAt))

		var recordKey models.RecordKey
		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-old", *recordKey.RecordID)

		gotSD, err := repo.GetSignedDocument(ctx, key)
		require.NoError(t, err)
		require.Equal(t, oldSD.Document, gotSD.Document)
		require.Equal(t, oldSD.Proof, gotSD.Proof)

		newCreatedAt := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
		newSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Key:       key,
			Value:     map[string]string{"body": "new"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.v2.json",
			CreatedAt: newCreatedAt,
		})
		withRepositoryTx(t, ctx, repo, "record-new", "127.0.0.1", newSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, "record-new", key, "con1owner", "https://schema.example/post.v2.json", nil, []string{}, nil, newCreatedAt)
			return err
		})

		require.NoError(t, db.Where("uri = ?", key).Take(&recordKey).Error)
		require.NotNil(t, recordKey.RecordID)
		require.Equal(t, "record-new", *recordKey.RecordID)

		require.NoError(t, db.Where("id = ?", "record-old").Take(&commit).Error)
		require.True(t, commit.GcCandidate)

		var oldRecordCount int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "record-old").Count(&oldRecordCount).Error)
		require.Zero(t, oldRecordCount)

		var newRecord models.Record
		require.NoError(t, db.Where("document_id = ?", "record-new").Take(&newRecord).Error)
		require.Equal(t, "https://schema.example/post.v2.json", newRecord.Schema)
		require.True(t, newRecord.CreatedAt.Equal(newCreatedAt))
	})

	t.Run("create reference record", func(t *testing.T) {
		targetURI := "cckv://con1target/timeline/post-2"
		targetCreatedAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
		targetSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Key:       targetURI,
			Value:     map[string]string{"body": "target"},
			Author:    "con1target",
			Schema:    "https://schema.example/target.json",
			CreatedAt: targetCreatedAt,
		})

		refSD := repositorySignedDocument(t, concrnt.Document[schemas.Reference]{
			Key: "cckv://con1owner/timeline/ref-1",
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
			_, err := repo.CreateRecord(ctx, tx, "reference-record", "cckv://con1owner/timeline/ref-1", "con1owner", "https://schema.example/target.json", nil, []string{}, &targetURI, targetCreatedAt)
			return err
		})

		var record models.Record
		require.NoError(t, db.Where("document_id = ?", "reference-record").Take(&record).Error)
		require.NotNil(t, record.Redirect)
		require.Equal(t, targetURI, *record.Redirect)
		require.Equal(t, "https://schema.example/target.json", record.Schema)
		require.True(t, record.CreatedAt.Equal(targetCreatedAt))
	})

	t.Run("create association and toggle ack", func(t *testing.T) {
		variant := "reply"
		associationSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Value:              map[string]string{"body": "comment"},
			Author:             "con1author",
			Schema:             "https://schema.example/comment.json",
			CreatedAt:          time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
			Associate:          &key,
			AssociationVariant: &variant,
		})
		associationUnique := fmt.Sprintf("%x", xxh3.HashString("con1owner"+"con1author"+key+variant))
		associationCreatedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
		withRepositoryTx(t, ctx, repo, "association-record", "127.0.0.1", associationSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.CreateAssociation(ctx, tx, "association-record", key, "con1owner", "con1author", "https://schema.example/comment.json", &variant, associationUnique, associationCreatedAt)
		})

		var association models.Association
		require.NoError(t, db.Where("document_id = ?", "association-record").Take(&association).Error)
		require.Equal(t, "con1owner", association.Owner)
		require.Equal(t, "con1author", association.Author)
		require.Equal(t, variant, *association.Variant)
		require.Equal(t, associationUnique, association.Unique)
		requireCommitOwner(t, db, "association-record", "con1owner")

		ackSD := repositorySignedDocument(t, concrnt.Document[schemas.Acknowledge]{
			Value: schemas.Acknowledge{
				Context: "like",
			},
			Author:    "con1author",
			Schema:    schemas.AcknowledgeURL,
			CreatedAt: time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
			Associate: &key,
		})
		ackCreatedAt := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
		var resultURI string
		withRepositoryTx(t, ctx, repo, "ack-on", "127.0.0.1", ackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			var err error
			resultURI, err = repo.Acknowledge(ctx, tx, "ack-on", "con1author", "con1owner", "like", true, ackCreatedAt, "ccfs://con1owner/ack-on")
			return err
		})
		require.Equal(t, "ccfs://con1owner/ack-on", resultURI)

		var ack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND context = ?`, "con1author", "con1owner", "like").Take(&ack).Error)
		require.True(t, ack.Valid)
		require.Equal(t, "ack-on", ack.DocumentID)
		requireCommitOwner(t, db, "ack-on", "con1author")
		requireCommitOwner(t, db, "ack-on", "con1owner")

		withRepositoryTx(t, ctx, repo, "ack-off", "127.0.0.1", ackSD, []string{"con1author", "con1owner"}, func(tx usecase.RepositoryTx) error {
			return repo.UnAcknowledge(ctx, tx, "ack-off", "con1author", "con1owner", "like", false, ackCreatedAt)
		})

		var unack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND context = ?`, "con1author", "con1owner", "like").Take(&unack).Error)
		require.False(t, unack.Valid)
		require.Equal(t, "ack-off", unack.DocumentID)
		requireCommitOwner(t, db, "ack-off", "con1author")
		requireCommitOwner(t, db, "ack-off", "con1owner")
	})

	t.Run("rollback removes commit and record", func(t *testing.T) {
		rollbackKey := "cckv://con1owner/timeline/rollback"
		rollbackSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Key:       rollbackKey,
			Value:     map[string]string{"body": "rollback"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.json",
			CreatedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
		})

		tx, err := repo.BeginTx(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.CreateCommitLog(ctx, tx, "rollback-record", "127.0.0.1", rollbackSD.Document, `{"type":"none"}`, []string{"con1owner"}))
		_, err = repo.CreateRecord(ctx, tx, "rollback-record", rollbackKey, "con1owner", "https://schema.example/post.json", nil, []string{}, nil, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, tx.Rollback(ctx))

		var count int64
		require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "rollback-record").Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("delete keeps delete commit", func(t *testing.T) {
		deleteKey := "cckv://con1owner/timeline/delete-target"
		deleteTargetSD := repositorySignedDocument(t, concrnt.Document[map[string]string]{
			Key:       deleteKey,
			Value:     map[string]string{"body": "delete target"},
			Author:    "con1author",
			Schema:    "https://schema.example/post.json",
			CreatedAt: time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC),
		})
		withRepositoryTx(t, ctx, repo, "delete-target", "127.0.0.1", deleteTargetSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.CreateRecord(ctx, tx, "delete-target", deleteKey, "con1owner", "https://schema.example/post.json", nil, []string{}, nil, time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC))
			return err
		})

		deleteSD := repositorySignedDocument(t, concrnt.Document[schemas.Delete]{
			Value:     schemas.Delete(deleteKey),
			Author:    "con1author",
			Schema:    schemas.DeleteURL,
			CreatedAt: time.Date(2026, 6, 8, 8, 9, 10, 0, time.UTC),
		})
		withRepositoryTx(t, ctx, repo, "delete-commit", "127.0.0.1", deleteSD, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
			_, err := repo.Delete(ctx, tx, deleteKey)
			return err
		})

		var count int64
		require.NoError(t, db.Model(&models.Record{}).Where("document_id = ?", "delete-target").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&models.CommitLog{}).Where("id = ?", "delete-target").Count(&count).Error)
		require.Zero(t, count)

		var commit models.CommitLog
		require.NoError(t, db.Where("id = ?", "delete-commit").Take(&commit).Error)
		require.Equal(t, deleteSD.Document, commit.Document)
		requireCommitOwner(t, db, "delete-commit", "con1owner")
	})

	t.Run("write methods reject invalid tx", func(t *testing.T) {
		_, err := repo.CreateRecord(ctx, nil, "invalid", key, "con1owner", "https://schema.example/post.json", nil, []string{}, nil, time.Now())
		require.Error(t, err)

		_, err = repo.CreateRecord(ctx, fakeRecordTx{}, "invalid", key, "con1owner", "https://schema.example/post.json", nil, []string{}, nil, time.Now())
		require.Error(t, err)
	})
}

func withRepositoryTx(t *testing.T, ctx context.Context, repo usecase.RecordRepository, id string, ip string, sd concrnt.SignedDocument, owners []string, fn func(tx usecase.RepositoryTx) error) {
	t.Helper()

	tx, err := repo.BeginTx(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateCommitLog(ctx, tx, id, ip, sd.Document, `{"type":"none"}`, owners))
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
