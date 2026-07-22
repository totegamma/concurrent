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
	"github.com/concrnt/concrnt/internal/service"
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

// testServerRepo serves a fixed server descriptor for every lookup.
type testServerRepo struct {
	server *domain.Server
}

func (s testServerRepo) Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error) {
	if s.server == nil {
		return nil, domain.ErrNotFound
	}
	return s.server, nil
}

func (s testServerRepo) List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error) {
	return nil, nil
}

// newTestServerUsecase wires a ServerUsecase that resolves every remote domain
// to a green (same-layer, unblocked) server.
func newTestServerUsecase(cfg *domain.Config) *ServerUsecase {
	server := &domain.Server{WellKnown: concrnt.WellKnownConcrnt{Layer: cfg.Layer}}
	return NewServerUsecase(testServerRepo{server: server}, cfg, concrnt.SoftwareInfo{}, service.NewModuleManager(map[string]string{}, nil), nil)
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
	cfg := &domain.Config{FQDN: "example.com"}
	uc := NewRecordUsecase(
		stubRecordRepo{},
		stubResidenceRepo{},
		newTestServerUsecase(cfg),
		cfg,
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
			cfg := &domain.Config{FQDN: "example.com"}
			uc := NewRecordUsecase(
				repo,
				fixedResidenceRepo{entity: &domain.Entity{
					ID:             ccid,
					Domain:         "remote.example.net",
					SignedDocument: &stored,
				}},
				newTestServerUsecase(cfg),
				cfg,
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
	cfg := &domain.Config{FQDN: "example.com"}
	uc := NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "remote.example.net"}},
		newTestServerUsecase(cfg),
		cfg,
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
	setTTLs   map[string]time.Duration
	addedSets []string
}

func (s *stubKVS) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	s.setKeys = append(s.setKeys, key)
	if s.keys == nil {
		s.keys = map[string]bool{}
	}
	s.keys[key] = true
	if s.setTTLs == nil {
		s.setTTLs = map[string]time.Duration{}
	}
	s.setTTLs[key] = ttl
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
		newTestServerUsecase(cfg),
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

// Accept-if-newer (CIP-3 §3.4): an older document arriving at a key holding a
// newer one succeeds as a no-op — the stored document stays and nothing is
// tombstoned — so a captured old version can't roll the key back and tombstone
// the live document. An identical redelivery is likewise a no-op.
func TestCommitRecordAcceptIfNewer(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	key := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()

	stored := signedRecord(t, ccid, priv, time.Now())
	storedCCFS := ccfsURIOf(t, stored, ccid)
	stored.CCKV = &key
	stored.CCFS = &storedCCFS

	t.Run("older document is a no-op", func(t *testing.T) {
		repo := &overwritableRecordRepo{key: key, existing: &stored}
		kvs := &stubKVS{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)

		old := signedRecord(t, ccid, priv, time.Now().Add(-time.Hour))
		result, err := uc.Commit(context.Background(), "127.0.0.1", old, domain.CommitModeExecute)
		if err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if repo.createRecordCalled {
			t.Fatal("CreateRecord must not be called for an older document")
		}
		if len(kvs.setKeys) != 0 {
			t.Fatalf("no tombstone may be set for an older document, got %v", kvs.setKeys)
		}
		if result == nil || result.CCKV == nil || *result.CCKV != key {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("identical redelivery is a no-op", func(t *testing.T) {
		repo := &overwritableRecordRepo{key: key, existing: &stored}
		uc := newRecordCommitUsecase(ccid, cfg, repo, &stubKVS{})
		if _, err := uc.Commit(context.Background(), "127.0.0.1", stored, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if repo.createRecordCalled {
			t.Fatal("CreateRecord must not be called for an identical redelivery")
		}
	})
}

// An overwrite tombstone's TTL is anchored on the superseded version's
// createdAt when that lies in the future (CIP-3 §3.4): a version stamped near
// the future-skew limit must stay tombstoned past its own backdate window.
func TestCommitOverwriteTombstoneTTLOrigin(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	key := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()

	skew := 11 * time.Hour
	old := signedRecord(t, ccid, priv, time.Now().Add(skew))
	oldCCFS := ccfsURIOf(t, old, ccid)
	old.CCKV = &key
	old.CCFS = &oldCCFS

	repo := &overwritableRecordRepo{key: key, existing: &old}
	kvs := &stubKVS{}
	uc := newRecordCommitUsecase(ccid, cfg, repo, kvs)

	fresh := signedRecord(t, ccid, priv, time.Now().Add(skew+30*time.Minute))
	if _, err := uc.Commit(context.Background(), "127.0.0.1", fresh, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}

	want := domain.MaxBackdate + skew
	ttl := kvs.setTTLs[tombstoneKey(oldCCFS)]
	if ttl < want-time.Minute || ttl > want+time.Minute {
		t.Fatalf("overwrite tombstone ttl = %v, want ~%v", ttl, want)
	}
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

// ackRecordingRepo satisfies the ack/unack write path and reports a
// configurable updated/no-op result.
type ackRecordingRepo struct {
	recordingRecordRepo
	updated           bool
	acknowledgeCalled bool
}

func (r *ackRecordingRepo) Acknowledge(ctx context.Context, tx RepositoryTx, documentID, from, to, schema string, createdAt time.Time) (bool, error) {
	r.acknowledgeCalled = true
	return r.updated, nil
}
func (r *ackRecordingRepo) UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID, from, to, schema string, createdAt time.Time) (bool, error) {
	r.acknowledgeCalled = true
	return r.updated, nil
}
func (r *ackRecordingRepo) QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

// mapResidenceRepo serves entities from a fixed ccid->entity map.
type mapResidenceRepo struct {
	ResidenceRepository
	entities map[string]*domain.Entity
}

func (m mapResidenceRepo) GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error) {
	if e, ok := m.entities[ccid]; ok {
		return e, nil
	}
	return nil, domain.ErrNotFound
}

// CIP-10 §4: an ack/unack transition the repository reports as older-or-equal
// (no state change) is a no-op success without side effects — in particular no
// proxy delivery to the remote target's server. A newer transition still
// proxy-delivers.
func TestCommitAckNoOpSkipsSideEffects(t *testing.T) {
	ccid, priv := newIdentity(t)
	targetCCID, _ := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	requester := &domain.Entity{ID: ccid, Domain: cfg.FQDN, SignedDocument: &concrnt.SignedDocument{Document: "{}"}}
	target := &domain.Entity{ID: targetCCID, Domain: "remote.example.net"}
	residence := mapResidenceRepo{entities: map[string]*domain.Entity{ccid: requester, targetCCID: target}}

	assoc := concrnt.CCURI{Scheme: "cckv", Owner: targetCCID}.String()
	sd := signTestDocument(t, concrnt.Document[any]{
		Kind:      "ack",
		Associate: &assoc,
		Author:    ccid,
		Schema:    "https://schema.concrnt.net/ack.json",
		CreatedAt: time.Now(),
	}, priv)

	commitAck := func(t *testing.T, updated bool) (*ackRecordingRepo, *recordingDeliveryQueue) {
		t.Helper()
		repo := &ackRecordingRepo{updated: updated}
		delivery := &recordingDeliveryQueue{}
		uc := NewRecordUsecase(
			repo,
			residence,
			newTestServerUsecase(cfg),
			cfg,
			nil,
			nopSignalService{},
			nopPolicyService{},
			delivery,
			nil,
		)
		if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
			t.Fatalf("Commit returned error: %v", err)
		}
		if !repo.acknowledgeCalled {
			t.Fatal("Acknowledge was not called")
		}
		return repo, delivery
	}

	t.Run("stale transition enqueues nothing", func(t *testing.T) {
		_, delivery := commitAck(t, false)
		if len(delivery.jobs) != 0 {
			t.Fatalf("no delivery may be enqueued for a stale ack, got %d jobs", len(delivery.jobs))
		}
	})

	t.Run("newer transition proxy-delivers", func(t *testing.T) {
		_, delivery := commitAck(t, true)
		if len(delivery.jobs) != 1 || delivery.jobs[0].Host != target.Domain {
			t.Fatalf("expected one proxy delivery to %s, got %+v", target.Domain, delivery.jobs)
		}
	})
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

// CIP-1 §4.1: an oversized document is rejected for everyone, including
// system service accounts — nothing larger than 32 KiB may reach the DB.
func TestCommitRejectsOversizedDocument(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	sd := signTestDocument(t, concrnt.Document[any]{
		Kind:      "record",
		Key:       concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String(),
		Value:     strings.Repeat("x", domain.MaxDocumentSize),
		Author:    ccid,
		Schema:    "https://example.com/post.json",
		CreatedAt: time.Now(),
	}, priv)

	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"regular commit", context.Background()},
		{"system service account", context.WithValue(context.Background(), interop.ServiceAccountTypeCtxKey, "system")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingRecordRepo{}
			uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
			_, err := uc.Commit(tc.ctx, "127.0.0.1", sd, domain.CommitModeExecute)
			if err == nil || !strings.Contains(err.Error(), "maximum size") {
				t.Fatalf("expected size rejection, got %v", err)
			}
			if repo.createRecordCalled {
				t.Fatal("CreateRecord should not be called for an oversized document")
			}
		})
	}
}

// CIP-9: an association document must not carry a key, and its
// associationVariant is capped at 512 bytes.
func TestCommitAssociationConstraints(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	associate := concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/1"}.String()

	commit := func(t *testing.T, mutate func(*concrnt.Document[any])) error {
		t.Helper()
		doc := concrnt.Document[any]{
			Kind:      "association",
			Associate: &associate,
			Value:     map[string]any{"messageId": "m1"},
			Author:    ccid,
			Schema:    "https://example.com/a/reply.json",
			CreatedAt: time.Now(),
		}
		mutate(&doc)
		uc := newRecordCommitUsecase(ccid, cfg, &associationRecordingRepo{}, nil)
		_, err := uc.Commit(context.Background(), "127.0.0.1", signTestDocument(t, doc, priv), domain.CommitModeDryRun)
		return err
	}

	if err := commit(t, func(doc *concrnt.Document[any]) {
		doc.Key = concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "assoc/1"}.String()
	}); err == nil || !strings.Contains(err.Error(), "must not have a key") {
		t.Fatalf("expected keyed association rejection, got %v", err)
	}

	long := strings.Repeat("v", domain.MaxAssociationVariantSize+1)
	if err := commit(t, func(doc *concrnt.Document[any]) { doc.AssociationVariant = &long }); err == nil || !strings.Contains(err.Error(), "associationVariant") {
		t.Fatalf("expected oversized variant rejection, got %v", err)
	}

	max := strings.Repeat("v", domain.MaxAssociationVariantSize)
	if err := commit(t, func(doc *concrnt.Document[any]) { doc.AssociationVariant = &max }); err != nil {
		t.Fatalf("512-byte variant should commit, got %v", err)
	}
}

// CIP-0 §7: a record's cckv key component is 1..1024 bytes.
func TestCommitRecordKeyConstraints(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	commit := func(t *testing.T, key string) (*recordingRecordRepo, error) {
		t.Helper()
		sd := signTestDocument(t, concrnt.Document[any]{
			Kind:      "record",
			Key:       key,
			Author:    ccid,
			Schema:    "https://example.com/post.json",
			CreatedAt: time.Now(),
		}, priv)
		repo := &recordingRecordRepo{}
		uc := newRecordCommitUsecase(ccid, cfg, repo, nil)
		_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
		return repo, err
	}

	if _, err := commit(t, concrnt.CCURI{Scheme: "cckv", Owner: ccid}.String()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-key rejection, got %v", err)
	}

	if _, err := commit(t, concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: strings.Repeat("k", domain.MaxRecordKeySize+1)}.String()); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("expected oversized-key rejection, got %v", err)
	}

	repo, err := commit(t, concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: strings.Repeat("k", domain.MaxRecordKeySize)}.String())
	if err != nil {
		t.Fatalf("1024-byte key should commit, got %v", err)
	}
	if !repo.createRecordCalled {
		t.Fatal("CreateRecord was not called for a max-length key")
	}
}

// saveEntity is fail-closed on the entity's home server: the entity is only
// stored when its server resolves, sits on the same layer, and is not tagged
// _blocked. (The green path is covered by TestCommitEntityAcceptIfNewer.)
func TestCommitEntityRequiresGreenServer(t *testing.T) {
	ccid, priv := newIdentity(t)

	cases := []struct {
		name    string
		server  *domain.Server
		wantErr string
	}{
		{"unresolvable server", nil, "failed to resolve"},
		{"layer mismatch", &domain.Server{WellKnown: concrnt.WellKnownConcrnt{Layer: "othernet"}}, "different layer"},
		{"blocked server", &domain.Server{TagString: "_blocked", WellKnown: concrnt.WellKnownConcrnt{Layer: "mainnet"}}, "blocked"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingRecordRepo{}
			cfg := &domain.Config{FQDN: "example.com", Layer: "mainnet"}
			serverUC := NewServerUsecase(testServerRepo{server: tc.server}, cfg, concrnt.SoftwareInfo{}, service.NewModuleManager(map[string]string{}, nil), nil)
			uc := NewRecordUsecase(
				repo,
				fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "remote.example.net"}},
				serverUC,
				cfg,
				nil,
				nil,
				nil,
				nil,
				nil,
			)

			sd := signTestDocument(t, concrnt.Document[schemas.Entity]{
				Kind:      "entity",
				Value:     schemas.Entity{Domain: "remote.example.net"},
				Author:    ccid,
				CreatedAt: time.Now(),
			}, priv)

			_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
			if repo.createEntityCalled {
				t.Fatal("CreateEntity should not be called for a non-green server")
			}
		})
	}
}

// None-proof entity documents are exempt from the green-server check: they
// only reach saveEntity via the system service account (migration/import),
// where the entity's home server may be offline, on another layer, or still
// on v1.
func TestCommitEntityNoneProofSkipsGreenServerCheck(t *testing.T) {
	ccid, _ := newIdentity(t)

	repo := &recordingRecordRepo{}
	cfg := &domain.Config{FQDN: "example.com", Layer: "mainnet"}
	unresolvable := NewServerUsecase(testServerRepo{server: nil}, cfg, concrnt.SoftwareInfo{}, service.NewModuleManager(map[string]string{}, nil), nil)
	uc := NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "remote.example.net"}},
		unresolvable,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	docBytes, err := json.Marshal(concrnt.Document[schemas.Entity]{
		Kind:      "entity",
		Value:     schemas.Entity{Domain: "remote.example.net"},
		Author:    ccid,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	sd := concrnt.SignedDocument{
		Document: string(docBytes),
		Proof:    concrnt.Proof{Type: concrnt.ProofTypeNone},
	}

	ctx := context.WithValue(context.Background(), interop.ServiceAccountTypeCtxKey, "system")
	if _, err := uc.Commit(ctx, "127.0.0.1", sd, domain.CommitModeLocalOnlyExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if !repo.createEntityCalled {
		t.Fatal("CreateEntity was not called for a none-proof entity import")
	}
}
