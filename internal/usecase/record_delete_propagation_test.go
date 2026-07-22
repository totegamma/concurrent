package usecase

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/schemas"
)

// propagationRepo extends the range-delete stub with a fixed document store,
// so the remote branch of deleteRecord can look up stored References.
type propagationRepo struct {
	rangeDeleteRepo
	documents map[string]*concrnt.SignedDocument
}

func (r *propagationRepo) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	if sd, ok := r.documents[uri]; ok {
		return sd, nil
	}
	return nil, domain.ErrNotFound
}

const propagationDest = "cckv://example.com/timelines/home"

// propagationTarget builds a validly signed remote record fixture distributed
// to this server, together with the auto-generated distribution Reference this
// server would hold for it (CIP-7 §4.1).
func propagationTarget(t *testing.T, ccid, priv, key string, distributes []string) (targetSD concrnt.SignedDocument, targetCDID string) {
	t.Helper()
	createdAt := time.Now().Add(-time.Minute)
	targetSD = signTestDocument(t, concrnt.Document[any]{
		Kind:        "record",
		Key:         key,
		Value:       map[string]any{"body": "x"},
		Author:      ccid,
		Schema:      "https://example.com/post.json",
		CreatedAt:   createdAt,
		Distributes: &distributes,
	}, priv)
	return targetSD, documentIDFor(targetSD.Document, createdAt)
}

// distributionReference builds the stored auto-generated Reference row at
// <dest>/<targetCDID> pointing at href.
func distributionReference(t *testing.T, ccid, priv, dest, targetCDID, href string) (string, *concrnt.SignedDocument) {
	t.Helper()
	refKey, err := url.JoinPath(dest, targetCDID)
	if err != nil {
		t.Fatalf("compose reference key: %v", err)
	}
	createdAt := time.Now().Add(-time.Minute)
	refSD := signTestDocument(t, concrnt.Document[schemas.Reference]{
		Kind:      "record",
		Key:       refKey,
		Value:     schemas.Reference{Href: href},
		Author:    ccid,
		Schema:    schemas.ReferenceURL,
		CreatedAt: createdAt,
	}, priv)
	ccfs := concrnt.ComposeCCFSURI("example.com", concrnt.CCFSTypeConcrnt, documentIDFor(refSD.Document, createdAt))
	k := refKey
	refSD.CCKV = &k
	refSD.CCFS = &ccfs
	return refKey, &refSD
}

func newPropagationUsecase(ccid string, cfg *domain.Config, repo RecordRepository, pol PolicyService, delivery DeliveryQueue, kvs KVS) *RecordUsecase {
	return NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "remote.example.net"}},
		newTestServerUsecase(cfg),
		cfg,
		client.New(cfg.FQDN),
		nopSignalService{},
		pol,
		delivery,
		kvs,
	)
}

// A propagated delete whose inlined target verifies, matches its reference
// URI, is authored by the deleter, and distributes to a locally-managed
// destination removes exactly the auto-generated Reference row, tombstones it,
// and emits the deleted event (CIP-4 §6.1).
func TestDeletePropagationRemovesDistributionReference(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	targetKey := "cckv://remote.example.net/posts/1"
	targetSD, targetCDID := propagationTarget(t, ccid, priv, targetKey, []string{propagationDest})
	refKey, refSD := distributionReference(t, ccid, priv, propagationDest, targetCDID, targetKey)

	repo := &propagationRepo{documents: map[string]*concrnt.SignedDocument{refKey: refSD}}
	kvs := &stubKVS{}
	delivery := &recordingDeliveryQueue{}
	uc := newPropagationUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, delivery, kvs)

	sd := signedDelete(t, ccid, priv, targetKey)
	sd.References = map[string]concrnt.SignedDocument{targetKey: targetSD}

	result, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if !slices.Equal(repo.deletedURIs, []string{refKey}) {
		t.Fatalf("deleted URIs = %v, want [%s]", repo.deletedURIs, refKey)
	}
	if !slices.Contains(kvs.setKeys, tombstoneKey(*refSD.CCFS)) {
		t.Fatalf("reference row was not tombstoned, set keys: %v", kvs.setKeys)
	}
	deletedEvents := 0
	for _, job := range delivery.jobs {
		if job.Event != nil && job.Event.Type == "deleted" {
			deletedEvents++
			if job.Event.URI != targetKey {
				t.Fatalf("deleted event URI = %s, want %s", job.Event.URI, targetKey)
			}
		}
	}
	if deletedEvents == 0 {
		t.Fatal("no deleted event was enqueued")
	}
	if result == nil || result.Document != targetSD.Document {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// A propagated range delete matches References by subtree and removes each
// target's Reference row.
func TestDeletePropagationRange(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	base := "cckv://remote.example.net/lists/l1"
	selfSD, selfCDID := propagationTarget(t, ccid, priv, base, []string{propagationDest})
	childSD, childCDID := propagationTarget(t, ccid, priv, base+"/a", []string{propagationDest})
	selfRefKey, selfRefSD := distributionReference(t, ccid, priv, propagationDest, selfCDID, base)
	childRefKey, childRefSD := distributionReference(t, ccid, priv, propagationDest, childCDID, base+"/a")

	repo := &propagationRepo{documents: map[string]*concrnt.SignedDocument{
		selfRefKey:  selfRefSD,
		childRefKey: childRefSD,
	}}
	uc := newPropagationUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, &stubKVS{})

	sd := signedDelete(t, ccid, priv, base+"*")
	sd.References = map[string]concrnt.SignedDocument{
		base:        selfSD,
		base + "/a": childSD,
	}

	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	want := []string{selfRefKey, childRefKey}
	slices.Sort(want)
	got := slices.Clone(repo.deletedURIs)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("deleted URIs = %v, want %v", got, want)
	}
}

// Forged propagated deletes are rejected without touching anything: an
// unverifiable inline target, an identity mismatch with the reference URI, an
// author mismatch, a target not distributed here, and a stored row that is not
// the distribution Reference for the target.
func TestDeletePropagationRejectsForgeries(t *testing.T) {
	ccid, priv := newIdentity(t)
	otherCCID, otherPriv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	targetKey := "cckv://remote.example.net/posts/1"
	targetSD, targetCDID := propagationTarget(t, ccid, priv, targetKey, []string{propagationDest})
	refKey, refSD := distributionReference(t, ccid, priv, propagationDest, targetCDID, targetKey)

	run := func(t *testing.T, mutate func(repo *propagationRepo, sd *concrnt.SignedDocument)) (*propagationRepo, error) {
		t.Helper()
		repo := &propagationRepo{documents: map[string]*concrnt.SignedDocument{refKey: refSD}}
		sd := signedDelete(t, ccid, priv, targetKey)
		sd.References = map[string]concrnt.SignedDocument{targetKey: targetSD}
		mutate(repo, &sd)
		uc := newPropagationUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, &stubKVS{})
		_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
		return repo, err
	}

	assertNothingDeleted := func(t *testing.T, repo *propagationRepo, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected the propagated delete to be rejected")
		}
		if len(repo.deletedURIs) != 0 {
			t.Fatalf("nothing may be deleted, got %v", repo.deletedURIs)
		}
	}

	t.Run("unverifiable inline target", func(t *testing.T) {
		unsigned := targetSD
		unsigned.Proof = concrnt.Proof{Type: concrnt.ProofTypeNone}
		repo, err := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {
			sd.References = map[string]concrnt.SignedDocument{targetKey: unsigned}
		})
		assertNothingDeleted(t, repo, err)
	})

	t.Run("identity mismatch with reference URI", func(t *testing.T) {
		decoy, _ := propagationTarget(t, ccid, priv, "cckv://remote.example.net/posts/2", []string{propagationDest})
		repo, err := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {
			sd.References = map[string]concrnt.SignedDocument{targetKey: decoy}
		})
		assertNothingDeleted(t, repo, err)
		if _, err2 := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {}); err2 != nil {
			t.Fatalf("control case failed: %v", err2)
		}
	})

	t.Run("author mismatch", func(t *testing.T) {
		foreign, _ := propagationTarget(t, otherCCID, otherPriv, targetKey, []string{propagationDest})
		repo, err := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {
			sd.References = map[string]concrnt.SignedDocument{targetKey: foreign}
		})
		assertNothingDeleted(t, repo, err)
		if !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
	})

	t.Run("no locally managed destination", func(t *testing.T) {
		elsewhere, _ := propagationTarget(t, ccid, priv, targetKey, []string{"cckv://other.example.net/timelines/x"})
		repo, err := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {
			sd.References = map[string]concrnt.SignedDocument{targetKey: elsewhere}
		})
		assertNothingDeleted(t, repo, err)
		if !errors.Is(err, domain.ErrMisdirected) {
			t.Fatalf("expected misdirected error, got %v", err)
		}
	})

	t.Run("stored row is not the distribution reference", func(t *testing.T) {
		_, wrongRef := distributionReference(t, ccid, priv, propagationDest, targetCDID, "cckv://remote.example.net/posts/other")
		repo, err := run(t, func(repo *propagationRepo, sd *concrnt.SignedDocument) {
			repo.documents[refKey] = wrongRef
		})
		assertNothingDeleted(t, repo, err)
	})

	t.Run("policy denial rolls back", func(t *testing.T) {
		repo := &propagationRepo{documents: map[string]*concrnt.SignedDocument{refKey: refSD}}
		sd := signedDelete(t, ccid, priv, targetKey)
		sd.References = map[string]concrnt.SignedDocument{targetKey: targetSD}
		uc := newPropagationUsecase(ccid, cfg, repo, &denyKeysPolicyService{deny: map[string]bool{refKey: true}}, &recordingDeliveryQueue{}, &stubKVS{})
		_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
		if !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if len(repo.txs) != 1 || repo.txs[0].committed || !repo.txs[0].rolledBack {
			t.Fatalf("transaction must be rolled back, got %+v", repo.txs)
		}
	})
}

// A propagated delete whose Reference row is already absent is an idempotent
// redelivery: it succeeds as a no-op instead of poisoning the origin's
// delivery queue with permanent errors.
func TestDeletePropagationMissingReferenceIsNoOp(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	targetKey := "cckv://remote.example.net/posts/1"
	targetSD, _ := propagationTarget(t, ccid, priv, targetKey, []string{propagationDest})

	repo := &propagationRepo{documents: map[string]*concrnt.SignedDocument{}}
	uc := newPropagationUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, &stubKVS{})

	sd := signedDelete(t, ccid, priv, targetKey)
	sd.References = map[string]concrnt.SignedDocument{targetKey: targetSD}

	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if len(repo.deletedURIs) != 0 {
		t.Fatalf("nothing may be deleted, got %v", repo.deletedURIs)
	}
}
