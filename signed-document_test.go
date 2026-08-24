package concrnt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/schemas"
)

func signedTestAck(t *testing.T, kind string) (SignedDocument, string) {
	t.Helper()
	author, priv := newTestIdentity(t)
	target, _ := newTestIdentity(t)
	associate := CCURI{Scheme: "cckv", Owner: target}.String()
	sd := signDocument(t, Document[map[string]string]{
		Kind:      kind,
		Value:     map[string]string{"context": "follow"},
		Author:    author,
		Schema:    "https://example.com/follow.json",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		Associate: &associate,
	}, priv)
	return sd, target
}

func mirrorOf(t *testing.T, original SignedDocument) SignedDocument {
	t.Helper()
	mirror, err := original.DeriveAcked()
	if err != nil {
		t.Fatalf("DeriveAcked: %v", err)
	}
	return mirror
}

// The derivation flips only the kind and is deterministic — the mirror of the
// same ack must be byte-identical across calls (its CDID depends on it).
func TestDeriveAcked(t *testing.T) {
	ackSD, _ := signedTestAck(t, "ack")

	mirror1, err := ackSD.DeriveAcked()
	if err != nil {
		t.Fatalf("DeriveAcked: %v", err)
	}
	mirror2, err := ackSD.DeriveAcked()
	if err != nil {
		t.Fatalf("DeriveAcked: %v", err)
	}
	if mirror1.Document != mirror2.Document {
		t.Fatal("derivation is not deterministic")
	}
	if mirror1.Proof.Type != ProofTypeAckReference || mirror1.Proof.Document == nil || *mirror1.Proof.Document != ackSD.Document {
		t.Fatal("mirror proof does not embed the original")
	}

	var orig, derived Document[map[string]string]
	if err := json.Unmarshal([]byte(ackSD.Document), &orig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(mirror1.Document), &derived); err != nil {
		t.Fatal(err)
	}
	if derived.Kind != "acked" {
		t.Fatalf("kind = %s, want acked", derived.Kind)
	}
	if derived.Author != orig.Author || derived.Schema != orig.Schema ||
		*derived.Associate != *orig.Associate || !derived.CreatedAt.Equal(orig.CreatedAt) ||
		derived.Value["context"] != orig.Value["context"] {
		t.Fatalf("mirror fields diverged: %s", mirror1.Document)
	}

	unackSD, _ := signedTestAck(t, "unack")
	unackMirror, err := unackSD.DeriveAcked()
	if err != nil {
		t.Fatalf("DeriveAcked(unack): %v", err)
	}
	var unackDerived Document[map[string]string]
	if err := json.Unmarshal([]byte(unackMirror.Document), &unackDerived); err != nil {
		t.Fatal(err)
	}
	if unackDerived.Kind != "unacked" {
		t.Fatalf("kind = %s, want unacked", unackDerived.Kind)
	}

	recordSD := signedCommit(t, "record")
	if _, err := recordSD.DeriveAcked(); err == nil {
		t.Fatal("expected non-ack kinds to be rejected")
	}
}

// signedCommit builds a signed non-ack document for negative cases.
func signedCommit(t *testing.T, kind string) SignedDocument {
	t.Helper()
	author, priv := newTestIdentity(t)
	return signDocument(t, Document[map[string]string]{
		Kind:      kind,
		Key:       CCURI{Scheme: "cckv", Owner: author, Key: "posts/1"}.String(),
		Value:     map[string]string{"body": "hi"},
		Author:    author,
		Schema:    "https://example.com/post.json",
		CreatedAt: time.Now(),
	}, priv)
}

// A canonical mirror verifies self-contained (nil resolver): the embedded
// original supplies the signature, the byte-equal derivation supplies the
// binding.
func TestVerifyAckReferenceValid(t *testing.T) {
	for _, kind := range []string{"ack", "unack"} {
		t.Run(kind, func(t *testing.T) {
			original, _ := signedTestAck(t, kind)
			mirror := mirrorOf(t, original)
			if err := mirror.Verify(context.Background(), nil); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}

func TestVerifyAckReferenceRejectsForgeries(t *testing.T) {
	original, target := signedTestAck(t, "ack")

	t.Run("tampered mirror document", func(t *testing.T) {
		mirror := mirrorOf(t, original)
		mirror.Document = strings.Replace(mirror.Document, `"follow"`, `"forged"`, 1)
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected tampered mirror to fail verification")
		}
	})

	t.Run("kind correspondence", func(t *testing.T) {
		// An acked mirror must derive from an ack: swap the derived kind to
		// unacked while embedding the ack — byte comparison must catch it.
		mirror := mirrorOf(t, original)
		mirror.Document = strings.Replace(mirror.Document, `"kind":"acked"`, `"kind":"unacked"`, 1)
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected kind mismatch to fail verification")
		}
	})

	t.Run("tampered embedded document", func(t *testing.T) {
		mirror := mirrorOf(t, original)
		forged := strings.Replace(*mirror.Proof.Document, `"follow"`, `"forged"`, 1)
		mirror.Proof.Document = &forged
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected embedded-signature mismatch to fail verification")
		}
	})

	t.Run("embedded document must be author-signed", func(t *testing.T) {
		mirror := mirrorOf(t, original)
		mirror.Proof.Proof = &Proof{Type: ProofTypeNone}
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected unsigned embedded document to fail verification")
		}
	})

	t.Run("missing embedding", func(t *testing.T) {
		mirror := mirrorOf(t, original)
		mirror.Proof.Document = nil
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected missing embedded document to fail verification")
		}
	})

	t.Run("non-ack embedded document", func(t *testing.T) {
		record := signedCommit(t, "record")
		doc := record.Document
		proof := record.Proof
		mirror := SignedDocument{
			Document: doc,
			Proof:    Proof{Type: ProofTypeAckReference, Document: &doc, Proof: &proof},
		}
		if err := mirror.Verify(context.Background(), nil); err == nil {
			t.Fatal("expected non-ack embedded document to fail verification")
		}
	})

	_ = target
}

// The distribution reference derivation must reproduce the CIP-7 §4.1 shape:
// kind=record under <destination>/<hash-CDID(href)>, reference.json schema,
// original author and a document-reference proof with the original inlined —
// and it must verify self-contained.
func TestDeriveDistributionReference(t *testing.T) {
	author, priv := newTestIdentity(t)
	key := CCURI{Scheme: "cckv", Owner: author, Key: "posts/1"}.String()
	createdAt := time.Now().UTC().Truncate(time.Millisecond)
	original := signDocument(t, Document[map[string]string]{
		Kind:      "record",
		Key:       key,
		Value:     map[string]string{"body": "hi"},
		Author:    author,
		Schema:    "https://example.com/post.json",
		CreatedAt: createdAt,
	}, priv)

	dest := CCURI{Scheme: "cckv", Owner: author, Key: "timelines/home"}.String()
	now := time.Now()
	ref, err := original.DeriveDistributionReference(dest, schemas.Reference{Href: key}, now)
	if err != nil {
		t.Fatalf("DeriveDistributionReference: %v", err)
	}

	var doc Document[schemas.Reference]
	if err := json.Unmarshal([]byte(ref.Document), &doc); err != nil {
		t.Fatal(err)
	}
	wantKey := dest + "/" + cdid.MakeHash([]byte(key)).String()
	if doc.Kind != "record" || doc.Key != wantKey || doc.Schema != schemas.ReferenceURL ||
		doc.Author != author || doc.Value.Href != key {
		t.Fatalf("unexpected derivation: %s", ref.Document)
	}
	if ref.Proof.Type != ProofTypeDocumentReference || ref.Proof.Href == nil || *ref.Proof.Href != key {
		t.Fatalf("unexpected proof: %+v", ref.Proof)
	}
	if _, ok := ref.References[key]; !ok {
		t.Fatal("original document is not inlined under References[href]")
	}
	// deterministic given the same stamp
	again, err := original.DeriveDistributionReference(dest, schemas.Reference{Href: key}, now)
	if err != nil || again.Document != ref.Document {
		t.Fatalf("derivation is not deterministic: %v", err)
	}
	// self-contained verification via the inlined original
	if err := ref.Verify(context.Background(), nil); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, err := original.DeriveDistributionReference(dest, schemas.Reference{}, now); err == nil {
		t.Fatal("expected empty href to be rejected")
	}
}
