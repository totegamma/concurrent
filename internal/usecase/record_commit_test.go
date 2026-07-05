package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

type stubRecordRepo struct{ RecordRepository }

type stubResidenceRepo struct{ ResidenceRepository }

func (stubResidenceRepo) GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error) {
	return nil, domain.ErrNotFound
}

func signedCommitDocument(t *testing.T, kind string) concrnt.SignedDocument {
	t.Helper()

	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	privKeyHex := hex.EncodeToString(priv)
	ccid, err := concrnt.PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		t.Fatalf("derive ccid: %v", err)
	}

	doc := concrnt.Document[any]{
		Kind:      kind,
		Key:       concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "test/1"}.String(),
		Author:    ccid,
		Schema:    "https://example.com/schema.json",
		CreatedAt: time.Now(),
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	sigBytes, err := concrnt.SignBytes(docBytes, privKeyHex)
	if err != nil {
		t.Fatalf("sign document: %v", err)
	}
	signature := hex.EncodeToString(sigBytes)

	return concrnt.SignedDocument{
		Document: string(docBytes),
		Proof: concrnt.Proof{
			Type:      concrnt.ProofTypeEcrecover,
			Signature: &signature,
		},
	}
}

// A validly signed commit whose author entity cannot be resolved (e.g. its
// entity document has a none proof, so a remote server refuses to import it)
// must fail with an error, not a nil-pointer panic.
func TestCommitUnresolvableRequesterReturnsError(t *testing.T) {
	uc := NewRecordUsecase(
		stubRecordRepo{},
		stubResidenceRepo{},
		&domain.Config{FQDN: "example.com"},
		nil,
		nil,
		nil,
		nil,
	)

	for _, kind := range []string{"record", "association"} {
		t.Run(kind, func(t *testing.T) {
			sd := signedCommitDocument(t, kind)

			_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "requester entity not found") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
