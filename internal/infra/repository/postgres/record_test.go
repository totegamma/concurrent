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
		oldWrite := usecase.RecordWrite{
			Commit: usecase.CommitWrite{
				ID:       "record-old",
				IP:       "127.0.0.1",
				Document: oldSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1owner"},
			},
			DocumentID:    "record-old",
			Key:           key,
			Owner:         "con1owner",
			Schema:        "https://schema.example/post.json",
			Distributions: distributions,
			CreatedAt:     createdAt,
		}

		resultURI, err := repo.CreateRecord(ctx, oldWrite)
		require.NoError(t, err)
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
		newWrite := usecase.RecordWrite{
			Commit: usecase.CommitWrite{
				ID:       "record-new",
				IP:       "127.0.0.1",
				Document: newSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1owner"},
			},
			DocumentID:    "record-new",
			Key:           key,
			Owner:         "con1owner",
			Schema:        "https://schema.example/post.v2.json",
			Distributions: []string{},
			CreatedAt:     newCreatedAt,
		}

		_, err = repo.CreateRecord(ctx, newWrite)
		require.NoError(t, err)

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

		write := usecase.RecordWrite{
			Commit: usecase.CommitWrite{
				ID:       "reference-record",
				IP:       "127.0.0.1",
				Document: refSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1owner"},
			},
			DocumentID:    "reference-record",
			Key:           "cckv://con1owner/timeline/ref-1",
			Owner:         "con1owner",
			Schema:        "https://schema.example/target.json",
			Distributions: []string{},
			Redirect:      &targetURI,
			CreatedAt:     targetCreatedAt,
		}
		_, err := repo.CreateRecord(ctx, write)
		require.NoError(t, err)

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
		associationWrite := usecase.AssociationWrite{
			Commit: usecase.CommitWrite{
				ID:       "association-record",
				IP:       "127.0.0.1",
				Document: associationSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1owner"},
			},
			DocumentID: "association-record",
			TargetURI:  key,
			Owner:      "con1owner",
			Author:     "con1author",
			Schema:     "https://schema.example/comment.json",
			Variant:    &variant,
			Unique:     associationUnique,
			CreatedAt:  time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		}
		require.NoError(t, repo.CreateAssociation(ctx, associationWrite))

		var association models.Association
		require.NoError(t, db.Where("document_id = ?", "association-record").Take(&association).Error)
		require.Equal(t, "con1owner", association.Owner)
		require.Equal(t, "con1author", association.Author)
		require.Equal(t, variant, *association.Variant)
		require.Equal(t, associationWrite.Unique, association.Unique)
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
		ackWrite := usecase.AckWrite{
			Commit: usecase.CommitWrite{
				ID:       "ack-on",
				IP:       "127.0.0.1",
				Document: ackSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1author", "con1owner"},
			},
			DocumentID: "ack-on",
			From:       "con1author",
			To:         "con1owner",
			Context:    "like",
			Valid:      true,
			CreatedAt:  time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
			ResultURI:  "ccfs://con1owner/ack-on",
		}

		resultURI, err := repo.Acknowledge(ctx, ackWrite)
		require.NoError(t, err)
		require.Equal(t, "ccfs://con1owner/ack-on", resultURI)

		var ack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND context = ?`, "con1author", "con1owner", "like").Take(&ack).Error)
		require.True(t, ack.Valid)
		require.Equal(t, "ack-on", ack.DocumentID)
		requireCommitOwner(t, db, "ack-on", "con1author")
		requireCommitOwner(t, db, "ack-on", "con1owner")

		unackWrite := usecase.AckWrite{
			Commit: usecase.CommitWrite{
				ID:       "ack-off",
				IP:       "127.0.0.1",
				Document: ackSD.Document,
				Proof:    `{"type":"none"}`,
				Owners:   []string{"con1author", "con1owner"},
			},
			DocumentID: "ack-off",
			From:       "con1author",
			To:         "con1owner",
			Context:    "like",
			Valid:      false,
			CreatedAt:  time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC),
			ResultURI:  "ccfs://con1owner/ack-off",
		}
		require.NoError(t, repo.UnAcknowledge(ctx, unackWrite))

		var unack models.Ack
		require.NoError(t, db.Where(`"from" = ? AND "to" = ? AND context = ?`, "con1author", "con1owner", "like").Take(&unack).Error)
		require.False(t, unack.Valid)
		require.Equal(t, "ack-off", unack.DocumentID)
		requireCommitOwner(t, db, "ack-off", "con1author")
		requireCommitOwner(t, db, "ack-off", "con1owner")
	})
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
