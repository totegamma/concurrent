package concrnt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	mirrorDoc, err := DeriveAckMirror(original.Document)
	if err != nil {
		t.Fatalf("DeriveAckMirror: %v", err)
	}
	doc := original.Document
	proof := original.Proof
	return SignedDocument{
		Document: mirrorDoc,
		Proof: Proof{
			Type:     ProofTypeAckReference,
			Document: &doc,
			Proof:    &proof,
		},
	}
}

// The derivation flips only the kind and is deterministic — the mirror of the
// same ack must be byte-identical across calls (its CDID depends on it).
func TestDeriveAckMirror(t *testing.T) {
	ackSD, _ := signedTestAck(t, "ack")

	mirror1, err := DeriveAckMirror(ackSD.Document)
	if err != nil {
		t.Fatalf("DeriveAckMirror: %v", err)
	}
	mirror2, err := DeriveAckMirror(ackSD.Document)
	if err != nil {
		t.Fatalf("DeriveAckMirror: %v", err)
	}
	if mirror1 != mirror2 {
		t.Fatal("derivation is not deterministic")
	}

	var orig, derived Document[map[string]string]
	if err := json.Unmarshal([]byte(ackSD.Document), &orig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(mirror1), &derived); err != nil {
		t.Fatal(err)
	}
	if derived.Kind != "acked" {
		t.Fatalf("kind = %s, want acked", derived.Kind)
	}
	if derived.Author != orig.Author || derived.Schema != orig.Schema ||
		*derived.Associate != *orig.Associate || !derived.CreatedAt.Equal(orig.CreatedAt) ||
		derived.Value["context"] != orig.Value["context"] {
		t.Fatalf("mirror fields diverged: %s", mirror1)
	}

	unackSD, _ := signedTestAck(t, "unack")
	unackMirror, err := DeriveAckMirror(unackSD.Document)
	if err != nil {
		t.Fatalf("DeriveAckMirror(unack): %v", err)
	}
	var unackDerived Document[map[string]string]
	if err := json.Unmarshal([]byte(unackMirror), &unackDerived); err != nil {
		t.Fatal(err)
	}
	if unackDerived.Kind != "unacked" {
		t.Fatalf("kind = %s, want unacked", unackDerived.Kind)
	}

	recordSD := signedCommit(t, "record")
	if _, err := DeriveAckMirror(recordSD.Document); err == nil {
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
