package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/schemas"
)

func TestRecordRepositoryDatastoreIntegration(t *testing.T) {
	if os.Getenv("DATASTORE_EMULATOR_HOST") == "" || os.Getenv("DATASTORE_PROJECT_ID") == "" {
		t.Skip("DATASTORE_EMULATOR_HOST and DATASTORE_PROJECT_ID are required")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, os.Getenv("DATASTORE_PROJECT_ID"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	namespace := fmt.Sprintf("record-test-%d", time.Now().UnixNano())
	repo := NewRecordRepository(client, namespace)

	key := "cckv://con1owner/timeline/post-1"
	parent := "cckv://con1owner/timeline"

	oldCreatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	oldSD := datastoreSignedDocument(t, concrnt.Document[map[string]string]{
		Key:       key,
		Value:     map[string]string{"body": "old"},
		Author:    "con1author",
		Schema:    "https://schema.example/post.json",
		CreatedAt: oldCreatedAt,
	})
	_, err = repo.CreateRecord(ctx, usecase.RecordWrite{
		Commit: usecase.CommitWrite{
			ID:       "record-old",
			IP:       "127.0.0.1",
			Document: oldSD.Document,
			Proof:    `{"type":"none"}`,
			Owners:   []string{"con1owner"},
		},
		DocumentID: "record-old",
		Key:        key,
		Owner:      "con1owner",
		Schema:     "https://schema.example/post.json",
		CreatedAt:  oldCreatedAt,
	})
	require.NoError(t, err)

	got, err := repo.GetSignedDocument(ctx, key)
	require.NoError(t, err)
	require.Equal(t, oldSD.Document, got.Document)
	require.Equal(t, oldSD.Proof, got.Proof)
	require.NotNil(t, got.CCKV)
	require.NotNil(t, got.CCFS)

	newCreatedAt := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	newSD := datastoreSignedDocument(t, concrnt.Document[map[string]string]{
		Key:       key,
		Value:     map[string]string{"body": "new"},
		Author:    "con1author",
		Schema:    "https://schema.example/post.v2.json",
		CreatedAt: newCreatedAt,
	})
	_, err = repo.CreateRecord(ctx, usecase.RecordWrite{
		Commit: usecase.CommitWrite{
			ID:       "record-new",
			IP:       "127.0.0.1",
			Document: newSD.Document,
			Proof:    `{"type":"none"}`,
			Owners:   []string{"con1owner"},
		},
		DocumentID: "record-new",
		Key:        key,
		Owner:      "con1owner",
		Schema:     "https://schema.example/post.v2.json",
		CreatedAt:  newCreatedAt,
	})
	require.NoError(t, err)

	got, err = repo.GetSignedDocument(ctx, key)
	require.NoError(t, err)
	require.Equal(t, newSD.Document, got.Document)

	childKey := "cckv://con1owner/timeline/post-2"
	childCreatedAt := time.Date(2026, 1, 4, 3, 4, 5, 0, time.UTC)
	childSD := datastoreSignedDocument(t, concrnt.Document[map[string]string]{
		Key:       childKey,
		Value:     map[string]string{"body": "child"},
		Author:    "con1author",
		Schema:    "https://schema.example/post.v2.json",
		CreatedAt: childCreatedAt,
	})
	_, err = repo.CreateRecord(ctx, usecase.RecordWrite{
		Commit: usecase.CommitWrite{
			ID:       "record-child",
			IP:       "127.0.0.1",
			Document: childSD.Document,
			Proof:    `{"type":"none"}`,
			Owners:   []string{"con1owner"},
		},
		DocumentID: "record-child",
		Key:        childKey,
		Owner:      "con1owner",
		Schema:     "https://schema.example/post.v2.json",
		CreatedAt:  childCreatedAt,
	})
	require.NoError(t, err)

	byParent, err := repo.QueryByParent(ctx, parent, "https://schema.example/post.v2.json", nil, nil, 1, "desc")
	require.NoError(t, err)
	require.Len(t, byParent, 1)
	require.Equal(t, childSD.Document, byParent[0].Document)

	byPrefix, err := repo.QueryByPrefix(ctx, parent+"/", "https://schema.example/post.v2.json", nil, nil, 10, "asc")
	require.NoError(t, err)
	require.Len(t, byPrefix, 2)
	require.Equal(t, newSD.Document, byPrefix[0].Document)
	require.Equal(t, childSD.Document, byPrefix[1].Document)

	variant := "reply"
	associationSD := datastoreSignedDocument(t, concrnt.Document[map[string]string]{
		Value:              map[string]string{"body": "comment"},
		Author:             "con1author",
		Schema:             "https://schema.example/comment.json",
		CreatedAt:          time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		Associate:          &key,
		AssociationVariant: &variant,
	})
	associationUnique := fmt.Sprintf("%x", xxh3.HashString("con1owner"+"con1author"+key+variant))
	err = repo.CreateAssociation(ctx, usecase.AssociationWrite{
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
	})
	require.NoError(t, err)

	associations, err := repo.GetAssociatedRecords(ctx, key, "https://schema.example/comment.json", variant, "con1author")
	require.NoError(t, err)
	require.Len(t, associations, 1)
	require.Equal(t, associationSD.Document, associations[0].Document)

	ackSD := datastoreSignedDocument(t, concrnt.Document[schemas.Acknowledge]{
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

	counts, err := repo.GetAcknowledgeRecordCounts(ctx, "con1author", "con1owner", "like")
	require.NoError(t, err)
	require.Equal(t, int64(1), counts["like"])

	ackWrite.Commit.ID = "ack-off"
	ackWrite.DocumentID = "ack-off"
	ackWrite.Valid = false
	err = repo.UnAcknowledge(ctx, ackWrite)
	require.NoError(t, err)

	counts, err = repo.GetAcknowledgeRecordCounts(ctx, "con1author", "con1owner", "like")
	require.NoError(t, err)
	require.Zero(t, counts["like"])
}

func datastoreSignedDocument(t *testing.T, doc any) concrnt.SignedDocument {
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
