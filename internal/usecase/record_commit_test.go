package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/policy"
	"github.com/concrnt/concrnt/schemas"
)

type stubRecordRepo struct{ RecordRepository }

type stubResidenceRepo struct{ ResidenceRepository }

func (stubResidenceRepo) GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error) {
	return nil, domain.ErrNotFound
}

type fakeTx struct{}

func (fakeTx) Commit(ctx context.Context) error   { return nil }
func (fakeTx) Rollback(ctx context.Context) error { return nil }

// recordingRecordRepo satisfies the write path of RecordRepository in memory
// and records which mutations were attempted.
type recordingRecordRepo struct {
	RecordRepository
	createEntityCalled bool
	createRecordCalled bool
}

func (r *recordingRecordRepo) BeginTx(ctx context.Context) (RepositoryTx, error) {
	return fakeTx{}, nil
}
func (r *recordingRecordRepo) CreateCommitLog(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof any) error {
	return nil
}
func (r *recordingRecordRepo) CreateCommitOwners(ctx context.Context, tx RepositoryTx, id string, owners []string) error {
	return nil
}
func (r *recordingRecordRepo) CreateEntity(ctx context.Context, tx RepositoryTx, ccid string, alias *string, domain string, documentID string) error {
	r.createEntityCalled = true
	return nil
}
func (r *recordingRecordRepo) CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, schema string, onUpdate *string, policies *string, distributions []string, redirect *string, createdAt time.Time) error {
	r.createRecordCalled = true
	return nil
}
func (r *recordingRecordRepo) GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error) {
	return nil, nil
}
func (r *recordingRecordRepo) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	return nil, domain.ErrNotFound
}

// fixedResidenceRepo serves one pre-existing entity for every lookup.
type fixedResidenceRepo struct {
	ResidenceRepository
	entity *domain.Entity
}

func (s fixedResidenceRepo) GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error) {
	if s.entity == nil {
		return nil, domain.ErrNotFound
	}
	return s.entity, nil
}

type nopPolicyService struct{}

func (nopPolicyService) Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error {
	return nil
}

type nopSignalService struct{}

func (nopSignalService) Publish(ctx context.Context, channel string, event concrnt.Event) error {
	return nil
}

func signTestDocument[T any](t *testing.T, doc concrnt.Document[T], privKeyHex string) concrnt.SignedDocument {
	t.Helper()

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

// Entity commits are accept-if-newer: a document older than (or as old as)
// the stored one succeeds as a no-op instead of clobbering the newer
// affiliation; only a strictly newer document reaches the repository.
func TestCommitEntityAcceptIfNewer(t *testing.T) {
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	privKeyHex := hex.EncodeToString(priv)
	ccid, err := concrnt.PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		t.Fatalf("derive ccid: %v", err)
	}

	newEntityDoc := func(createdAt time.Time) concrnt.SignedDocument {
		return signTestDocument(t, concrnt.Document[schemas.Entity]{
			Kind:      "entity",
			Value:     schemas.Entity{Domain: "remote.example.net"},
			Author:    ccid,
			CreatedAt: createdAt,
		}, privKeyHex)
	}

	// Recent so the always-on backdate window doesn't reject these commits.
	storedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	stored := newEntityDoc(storedAt)

	cases := []struct {
		name             string
		createdAt        time.Time
		wantCreateEntity bool
	}{
		{"older", storedAt.Add(-time.Hour), false},
		{"same", storedAt, false},
		{"newer", storedAt.Add(time.Hour), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingRecordRepo{}
			uc := NewRecordUsecase(
				repo,
				fixedResidenceRepo{entity: &domain.Entity{
					ID:             ccid,
					Domain:         "remote.example.net",
					SignedDocument: &stored,
				}},
				&domain.Config{FQDN: "example.com"},
				nil,
				nil,
				nil,
				nil,
				nil,
			)

			sd := newEntityDoc(tc.createdAt)
			if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
				t.Fatalf("Commit returned error: %v", err)
			}
			if repo.createEntityCalled != tc.wantCreateEntity {
				t.Fatalf("CreateEntity called = %v, want %v", repo.createEntityCalled, tc.wantCreateEntity)
			}
		})
	}
}

// A document-reference commit whose target is inlined in References must
// succeed with no client at all (resolver == nil) — this is the migration /
// import scenario, where the referenced record's origin server may not be
// reachable (e.g. it hasn't migrated yet).
func TestCommitDocumentReferenceInlineTargetOffline(t *testing.T) {
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	privKeyHex := hex.EncodeToString(priv)
	ccid, err := concrnt.PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		t.Fatalf("derive ccid: %v", err)
	}

	// Recent so the always-on backdate window doesn't reject these commits.
	createdAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	targetURI := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()
	targetSD := signTestDocument(t, concrnt.Document[any]{
		Kind:      "record",
		Key:       targetURI,
		Author:    ccid,
		Schema:    "https://example.com/post.json",
		CreatedAt: createdAt,
	}, privKeyHex)

	refKey := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "timelines/home/entry1"}.String()
	refDoc := concrnt.Document[schemas.Reference]{
		Kind:      "record",
		Key:       refKey,
		Value:     schemas.Reference{Href: targetURI},
		Author:    ccid,
		Schema:    schemas.ReferenceURL,
		CreatedAt: createdAt,
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := concrnt.SignedDocument{
		Document:   string(refDocBytes),
		Proof:      concrnt.Proof{Type: concrnt.ProofTypeDocumentReference, Href: &targetURI},
		References: map[string]concrnt.SignedDocument{targetURI: targetSD},
	}

	repo := &recordingRecordRepo{}
	uc := NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "remote.example.net"}},
		&domain.Config{FQDN: "example.com"},
		nil, // no client: verification must not need the network
		nopSignalService{},
		nopPolicyService{},
		nil,
		nil,
	)

	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if !repo.createRecordCalled {
		t.Fatal("CreateRecord was not called")
	}
}

// stubKVS records Set calls and answers Exists from the keys it holds.
type stubKVS struct {
	keys      map[string]bool
	setKeys   []string
	addedSets []string
}

func (s *stubKVS) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	s.setKeys = append(s.setKeys, key)
	if s.keys == nil {
		s.keys = map[string]bool{}
	}
	s.keys[key] = true
	return nil
}
func (s *stubKVS) Exists(ctx context.Context, key string) (bool, error) {
	return s.keys[key], nil
}
func (s *stubKVS) SetAdd(ctx context.Context, key string, value string, ttl time.Duration) error {
	s.addedSets = append(s.addedSets, key)
	return nil
}
func (s *stubKVS) SetMembers(ctx context.Context, key string) ([]string, error) {
	return nil, nil
}

// signedRecord builds a signed record commit by the given identity at createdAt.
func signedRecord(t *testing.T, ccid, privKeyHex string, createdAt time.Time) concrnt.SignedDocument {
	t.Helper()
	return signTestDocument(t, concrnt.Document[any]{
		Kind:      "record",
		Key:       concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String(),
		Author:    ccid,
		Schema:    "https://example.com/post.json",
		CreatedAt: createdAt,
	}, privKeyHex)
}

// newRecordCommitUsecase wires a usecase whose author entity is local and
// resolvable, so a plain record commit reaches the repository.
func newRecordCommitUsecase(ccid string, cfg *domain.Config, repo RecordRepository, store KVS) *RecordUsecase {
	return NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: cfg.FQDN}},
		cfg,
		nil,
		nopSignalService{},
		nopPolicyService{},
		nil,
		store,
	)
}

func newIdentity(t *testing.T) (ccid, privKeyHex string) {
	t.Helper()
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	privKeyHex = hex.EncodeToString(priv)
	ccid, err := concrnt.PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		t.Fatalf("derive ccid: %v", err)
	}
	return ccid, privKeyHex
}

// A document stamped too far in the future is rejected for everyone.
func TestCommitRejectsFarFutureCreatedAt(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	repo := &recordingRecordRepo{}
	uc := newRecordCommitUsecase(ccid, cfg, repo, nil)

	sd := signedRecord(t, ccid, priv, time.Now().Add(24*time.Hour))
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("expected future-createdAt rejection, got %v", err)
	}
	if repo.createRecordCalled {
		t.Fatal("CreateRecord should not be called for a far-future document")
	}
}

// Documents older than the backdate window are rejected, while documents
// within it commit normally.
func TestCommitBackdateWindow(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	t.Run("too old", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		sd := signedRecord(t, ccid, priv, time.Now().Add(-domain.MaxBackdate-time.Hour))
		_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
		if err == nil || !strings.Contains(err.Error(), "backdate") {
			t.Fatalf("expected backdate rejection, got %v", err)
		}
		if repo.createRecordCalled {
			t.Fatal("CreateRecord should not be called for a too-old document")
		}
	})

	t.Run("within window", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		sd := signedRecord(t, ccid, priv, time.Now().Add(-domain.MaxBackdate+time.Hour))
		if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if !repo.createRecordCalled {
			t.Fatal("CreateRecord was not called for an in-window document")
		}
	})
}

// ccfsURIOf derives the ccfs URI a signed document commits under, the same way
// Commit does: CDID from the document body + createdAt, owner from the key.
func ccfsURIOf(t *testing.T, sd concrnt.SignedDocument, owner string) string {
	t.Helper()
	var doc concrnt.Document[any]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	return concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  owner,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentIDFor(sd.Document, doc.CreatedAt),
	}.String()
}

// The tombstone is keyed by the document's ccfs URI (content id): replaying
// the exact deleted document is rejected, while a fresh document at the same
// cckv key commits normally — deleting a key must not make it unusable.
func TestCommitRejectsTombstonedDocument(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	deleted := signedRecord(t, ccid, priv, time.Now().Add(-time.Hour))
	kvs := &stubKVS{keys: map[string]bool{tombstoneKey(ccfsURIOf(t, deleted, ccid)): true}}

	t.Run("replay of the deleted document", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)
		_, err := uc.Commit(context.Background(), "127.0.0.1", deleted, domain.CommitModeExecute)
		if err == nil || !strings.Contains(err.Error(), "deleted") {
			t.Fatalf("expected tombstone rejection, got %v", err)
		}
		if repo.createRecordCalled {
			t.Fatal("CreateRecord should not be called for a tombstoned document")
		}
	})

	t.Run("fresh document at the same key", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)
		fresh := signedRecord(t, ccid, priv, time.Now())
		if _, err := uc.Commit(context.Background(), "127.0.0.1", fresh, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if !repo.createRecordCalled {
			t.Fatal("CreateRecord was not called for a fresh document at a deleted key")
		}
	})
}

// System service accounts bypass the replay guards entirely: an old,
// tombstoned commit still applies (this is the migration/import path).
func TestCommitSystemAccountBypassesReplayGuards(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	repo := &recordingRecordRepo{}

	sd := signedRecord(t, ccid, priv, time.Now().Add(-100*24*time.Hour))
	kvs := &stubKVS{keys: map[string]bool{tombstoneKey(ccfsURIOf(t, sd, ccid)): true}}
	uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)

	ctx := context.WithValue(context.Background(), interop.ServiceAccountTypeCtxKey, "system")
	if _, err := uc.Commit(ctx, "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("system commit returned error: %v", err)
	}
	if !repo.createRecordCalled {
		t.Fatal("CreateRecord was not called for a system-account commit")
	}
}

// overwritableRecordRepo serves a stored signed document for one cckv key,
// simulating an existing record about to be overwritten.
type overwritableRecordRepo struct {
	recordingRecordRepo
	key      string
	existing *concrnt.SignedDocument
}

func (r *overwritableRecordRepo) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	if uri == r.key && r.existing != nil {
		return r.existing, nil
	}
	return nil, domain.ErrNotFound
}

// Overwriting a record tombstones the superseded version's ccfs URI (so the
// old version can't be replayed back in), while an idempotent redelivery of
// the stored document itself must not tombstone the live version.
func TestCommitOverwriteTombstonesOldVersion(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	key := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()

	old := signedRecord(t, ccid, priv, time.Now().Add(-time.Hour))
	oldCCFS := ccfsURIOf(t, old, ccid)
	old.CCKV = &key
	old.CCFS = &oldCCFS

	t.Run("overwrite tombstones the old version", func(t *testing.T) {
		repo := &overwritableRecordRepo{key: key, existing: &old}
		kvs := &stubKVS{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)

		fresh := signedRecord(t, ccid, priv, time.Now())
		if _, err := uc.Commit(context.Background(), "127.0.0.1", fresh, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if !repo.createRecordCalled {
			t.Fatal("CreateRecord was not called for an overwrite")
		}
		if !slices.Contains(kvs.setKeys, tombstoneKey(oldCCFS)) {
			t.Fatalf("old version's ccfs was not tombstoned, set keys: %v", kvs.setKeys)
		}
	})

	t.Run("idempotent redelivery does not tombstone itself", func(t *testing.T) {
		repo := &overwritableRecordRepo{key: key, existing: &old}
		kvs := &stubKVS{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)

		if _, err := uc.Commit(context.Background(), "127.0.0.1", old, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if len(kvs.setKeys) != 0 {
			t.Fatalf("redelivery of the stored document tombstoned keys: %v", kvs.setKeys)
		}
	})
}

// associationRecordingRepo captures the unique key passed to CreateAssociation.
type associationRecordingRepo struct {
	recordingRecordRepo
	uniques []string
}

func (r *associationRecordingRepo) CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) (bool, error) {
	r.uniques = append(r.uniques, unique)
	return true, nil
}

// importRecordingRepo additionally answers the blocking-list query issued when
// a commit's author and target owner differ.
type importRecordingRepo struct {
	associationRecordingRepo
}

func (r *importRecordingRepo) QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

// Self-service migration: an authenticated requester importing a repository
// dump (LocalOnlyExecute) is exempt from the backdate window for their own
// documents and for others' documents targeting their content. The exemption
// requires both the import mode and authentication.
func TestCommitBackdateExemptForAuthenticatedSelfImport(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	old := time.Now().Add(-domain.MaxBackdate - time.Hour)
	authedCtx := context.WithValue(context.Background(), interop.RequesterCtxKey, domain.Entity{ID: ccid, Domain: cfg.FQDN})

	t.Run("own old document imports", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		sd := signedRecord(t, ccid, priv, old)
		if _, err := uc.Commit(authedCtx, "127.0.0.1", sd, domain.CommitModeLocalOnlyExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if !repo.createRecordCalled {
			t.Fatal("CreateRecord was not called for an exempt import")
		}
	})

	t.Run("unauthenticated import rejected", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		sd := signedRecord(t, ccid, priv, old)
		_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeLocalOnlyExecute)
		if err == nil || !strings.Contains(err.Error(), "backdate") {
			t.Fatalf("expected backdate rejection, got %v", err)
		}
	})

	t.Run("non-import mode rejected", func(t *testing.T) {
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		sd := signedRecord(t, ccid, priv, old)
		_, err := uc.Commit(authedCtx, "127.0.0.1", sd, domain.CommitModeExecute)
		if err == nil || !strings.Contains(err.Error(), "backdate") {
			t.Fatalf("expected backdate rejection, got %v", err)
		}
	})

	t.Run("other author targeting requester imports", func(t *testing.T) {
		otherCCID, otherPriv := newIdentity(t)
		associate := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()
		sd := signTestDocument(t, concrnt.Document[any]{
			Kind:      "association",
			Associate: &associate,
			Author:    otherCCID,
			Schema:    "https://example.com/a/like.json",
			CreatedAt: old,
		}, otherPriv)

		repo := &importRecordingRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		if _, err := uc.Commit(authedCtx, "127.0.0.1", sd, domain.CommitModeLocalOnlyExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if len(repo.uniques) != 1 {
			t.Fatal("CreateAssociation was not called for an inbound association import")
		}
	})

	t.Run("other author unrelated target rejected", func(t *testing.T) {
		otherCCID, otherPriv := newIdentity(t)
		sd := signedRecord(t, otherCCID, otherPriv, old)

		repo := &importRecordingRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		_, err := uc.Commit(authedCtx, "127.0.0.1", sd, domain.CommitModeLocalOnlyExecute)
		if err == nil || !strings.Contains(err.Error(), "backdate") {
			t.Fatalf("expected backdate rejection, got %v", err)
		}
	})
}

// The association unique key includes the body: associations that differ only
// in body must coexist (v1 semantics — e.g. two replies by the same author to
// the same message), while a byte-identical body still dedupes.
func TestCommitAssociationUniqueIncludesBody(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	repo := &associationRecordingRepo{}
	uc := newRecordCommitUsecase(ccid, cfg, repo, nil)

	associate := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()
	commitWithBody := func(body any) {
		t.Helper()
		sd := signTestDocument(t, concrnt.Document[any]{
			Kind:      "association",
			Associate: &associate,
			Value:     body,
			Author:    ccid,
			Schema:    "https://example.com/a/reply.json",
			CreatedAt: time.Now(),
		}, priv)
		if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeDryRun); err != nil {
			t.Fatalf("commit returned error: %v", err)
		}
	}

	commitWithBody(map[string]any{"messageId": "m1"})
	commitWithBody(map[string]any{"messageId": "m2"})
	commitWithBody(map[string]any{"messageId": "m1"})

	if len(repo.uniques) != 3 {
		t.Fatalf("expected 3 CreateAssociation calls, got %d", len(repo.uniques))
	}
	if repo.uniques[0] == repo.uniques[1] {
		t.Fatal("associations with different bodies must not share a unique key")
	}
	if repo.uniques[0] != repo.uniques[2] {
		t.Fatal("associations with identical bodies must share a unique key")
	}
}
