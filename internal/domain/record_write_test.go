package domain

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/schemas"
)

func TestNewRecordWrite(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	distributions := []string{"cckv://con1channel/timeline"}
	policyURL := "https://policy.example/record"
	policyDoc := concrnt.Policy{
		Entries: []concrnt.PolicyEntry{
			{URL: &policyURL},
		},
	}

	doc := concrnt.Document[map[string]string]{
		Key:         "cckv://con1owner/timeline/post-1",
		Value:       map[string]string{"body": "hello"},
		Author:      "con1author",
		Schema:      "https://schema.example/post.json",
		CreatedAt:   createdAt,
		Distributes: &distributions,
		Policy:      &policyDoc,
	}
	sd := mustSignedDocument(t, doc)

	write, err := NewRecordWrite("127.0.0.1", "doc1", sd)
	require.NoError(t, err)

	require.Equal(t, "doc1", write.DocumentID)
	require.Equal(t, doc.Key, write.Key)
	require.Equal(t, "con1owner", write.Owner)
	require.Equal(t, doc.Schema, write.Schema)
	require.Equal(t, createdAt, write.CreatedAt)
	require.Equal(t, distributions, write.Distributions)
	require.NotNil(t, write.Policies)

	var gotPolicy concrnt.Policy
	require.NoError(t, json.Unmarshal([]byte(*write.Policies), &gotPolicy))
	require.Equal(t, policyDoc, gotPolicy)

	require.Equal(t, "doc1", write.Commit.ID)
	require.Equal(t, "127.0.0.1", write.Commit.IP)
	require.Equal(t, sd.Document, write.Commit.Document)
	require.JSONEq(t, `{"type":"none"}`, write.Commit.Proof)
	require.Equal(t, []string{"con1owner"}, write.Commit.Owners)
}

func TestNewRecordWriteReferenceUsesReferencedDocumentMetadata(t *testing.T) {
	targetCreatedAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	targetURI := "cckv://con1target/timeline/post-1"
	targetDoc := concrnt.Document[map[string]string]{
		Key:       targetURI,
		Value:     map[string]string{"body": "target"},
		Author:    "con1target",
		Schema:    "https://schema.example/target.json",
		CreatedAt: targetCreatedAt,
	}
	targetSD := mustSignedDocument(t, targetDoc)

	refCreatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	refDoc := concrnt.Document[schemas.Reference]{
		Key: "cckv://con1owner/timeline/ref-1",
		Value: schemas.Reference{
			Href: targetURI,
		},
		Author:    "con1owner",
		Schema:    schemas.ReferenceURL,
		CreatedAt: refCreatedAt,
	}
	refSD := mustSignedDocument(t, refDoc)
	refSD.References = map[string]concrnt.SignedDocument{
		targetURI: targetSD,
	}

	write, err := NewRecordWrite("127.0.0.1", "refdoc1", refSD)
	require.NoError(t, err)

	require.Equal(t, "cckv://con1owner/timeline/ref-1", write.Key)
	require.Equal(t, targetURI, *write.Redirect)
	require.Equal(t, targetDoc.Schema, write.Schema)
	require.Equal(t, targetCreatedAt, write.CreatedAt)
}

func TestNewAssociationWrite(t *testing.T) {
	createdAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	associate := "cckv://con1target/timeline/post-1"
	variant := "reply"
	doc := concrnt.Document[map[string]string]{
		Value:              map[string]string{"body": "comment"},
		Author:             "con1author",
		Schema:             "https://schema.example/comment.json",
		CreatedAt:          createdAt,
		Associate:          &associate,
		AssociationVariant: &variant,
	}
	sd := mustSignedDocument(t, doc)
	expectedUnique := fmt.Sprintf("%x", xxh3.HashString("con1target"+"con1author"+associate+variant))

	write, err := NewAssociationWrite("127.0.0.1", "assocdoc1", mustAnyDocument(t, sd), sd)
	require.NoError(t, err)

	require.Equal(t, "assocdoc1", write.DocumentID)
	require.Equal(t, associate, write.TargetURI)
	require.Equal(t, "con1target", write.Owner)
	require.Equal(t, "con1author", write.Author)
	require.Equal(t, doc.Schema, write.Schema)
	require.Equal(t, variant, *write.Variant)
	require.Equal(t, expectedUnique, write.Unique)
	require.Equal(t, createdAt, write.CreatedAt)
	require.Equal(t, []string{"con1target"}, write.Commit.Owners)
}

func TestNewAckWrite(t *testing.T) {
	createdAt := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	associate := "cckv://con1target/timeline/post-1"
	doc := concrnt.Document[schemas.Acknowledge]{
		Value: schemas.Acknowledge{
			Context: "like",
		},
		Author:    "con1author",
		Schema:    schemas.AcknowledgeURL,
		CreatedAt: createdAt,
		Associate: &associate,
	}
	sd := mustSignedDocument(t, doc)

	write, err := NewAckWrite("127.0.0.1", "ackdoc1", sd, true)
	require.NoError(t, err)

	require.Equal(t, "ackdoc1", write.DocumentID)
	require.Equal(t, "con1author", write.From)
	require.Equal(t, "con1target", write.To)
	require.Equal(t, "like", write.Context)
	require.True(t, write.Valid)
	require.Equal(t, createdAt, write.CreatedAt)
	require.Equal(t, "ccfs://con1target/ackdoc1", write.ResultURI)
	require.Equal(t, []string{"con1author", "con1target"}, write.Commit.Owners)
}

func TestWriteBuildersRejectInvalidSchemes(t *testing.T) {
	recordDoc := concrnt.Document[map[string]string]{
		Key:       "https://example.com/post",
		Value:     map[string]string{"body": "hello"},
		Author:    "con1author",
		Schema:    "https://schema.example/post.json",
		CreatedAt: time.Now(),
	}
	_, err := NewRecordWrite("127.0.0.1", "doc1", mustSignedDocument(t, recordDoc))
	require.ErrorContains(t, err, "document key scheme must be cckv")

	associate := "https://example.com/post"
	associationDoc := concrnt.Document[map[string]string]{
		Value:     map[string]string{"body": "comment"},
		Author:    "con1author",
		Schema:    "https://schema.example/comment.json",
		CreatedAt: time.Now(),
		Associate: &associate,
	}
	associationSD := mustSignedDocument(t, associationDoc)
	_, err = NewAssociationWrite("127.0.0.1", "doc2", mustAnyDocument(t, associationSD), associationSD)
	require.ErrorContains(t, err, "document associate scheme must be cckv")
}

func mustAnyDocument(t *testing.T, sd concrnt.SignedDocument) concrnt.Document[any] {
	t.Helper()

	var doc concrnt.Document[any]
	require.NoError(t, json.Unmarshal([]byte(sd.Document), &doc))
	return doc
}

func mustSignedDocument(t *testing.T, doc any) concrnt.SignedDocument {
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
