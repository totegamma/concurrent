package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/policy"
	"github.com/concrnt/concrnt/schemas"
)

func TestParseRangeDeleteTarget(t *testing.T) {
	cases := []struct {
		name      string
		target    string
		wantBase  string
		wantSelf  bool
		wantRange bool
		wantErr   bool
	}{
		{"plain key", "cckv://owner/path/item", "cckv://owner/path/item", false, false, false},
		{"subtree with self", "cckv://owner/path/item*", "cckv://owner/path/item", true, true, false},
		{"children only", "cckv://owner/path/item/*", "cckv://owner/path/item", false, true, false},
		{"inner asterisk", "cckv://owner/pa*th/item*", "", false, true, true},
		{"owner-wide children", "cckv://owner/*", "", false, true, true},
		{"owner-wide subtree", "cckv://owner*", "", false, true, true},
		{"ccfs scheme", "ccfs://owner/concrnt/abcdef*", "", false, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, includeSelf, isRange, err := parseRangeDeleteTarget(tc.target)
			if isRange != tc.wantRange {
				t.Fatalf("isRange = %v, want %v", isRange, tc.wantRange)
			}
			if tc.wantErr {
				if err == nil || !errors.Is(err, domain.ErrValidation) {
					t.Fatalf("expected validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if base != tc.wantBase {
				t.Fatalf("base = %q, want %q", base, tc.wantBase)
			}
			if includeSelf != tc.wantSelf {
				t.Fatalf("includeSelf = %v, want %v", includeSelf, tc.wantSelf)
			}
		})
	}
}

// recordingTx records whether the commit transaction was committed or rolled
// back, so tests can assert the commitlog's fate.
type recordingTx struct {
	committed  bool
	rolledBack bool
}

func (t *recordingTx) Commit(ctx context.Context) error   { t.committed = true; return nil }
func (t *recordingTx) Rollback(ctx context.Context) error { t.rolledBack = true; return nil }

type subtreeQueryCall struct {
	base        string
	includeSelf bool
}

// rangeDeleteRepo serves a fixed subtree enumeration and records the
// enumeration arguments and every delete attempt.
type rangeDeleteRepo struct {
	RecordRepository
	subtree     []concrnt.SignedDocument
	queryCalls  []subtreeQueryCall
	deletedURIs []string
	txs         []*recordingTx
}

func (r *rangeDeleteRepo) BeginTx(ctx context.Context) (RepositoryTx, error) {
	tx := &recordingTx{}
	r.txs = append(r.txs, tx)
	return tx, nil
}
func (r *rangeDeleteRepo) CreateCommitLog(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof any) error {
	return nil
}
func (r *rangeDeleteRepo) CreateCommitOwners(ctx context.Context, tx RepositoryTx, id string, owners []string) error {
	return nil
}
func (r *rangeDeleteRepo) GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error) {
	return nil, nil
}
func (r *rangeDeleteRepo) QueryRecordSubtree(ctx context.Context, base string, includeSelf bool) ([]concrnt.SignedDocument, error) {
	r.queryCalls = append(r.queryCalls, subtreeQueryCall{base: base, includeSelf: includeSelf})
	return r.subtree, nil
}
func (r *rangeDeleteRepo) DeleteRecordByKey(ctx context.Context, tx RepositoryTx, targetURI string) error {
	r.deletedURIs = append(r.deletedURIs, targetURI)
	return nil
}
func (r *rangeDeleteRepo) GetTimelineRemoval(ctx context.Context, keyURI string) (string, string, error) {
	return "", "", nil
}

// denyKeysPolicyService denies exactly the configured keys and records every
// evaluated key in order.
type denyKeysPolicyService struct {
	deny   map[string]bool
	evaled []string
}

func (p *denyKeysPolicyService) Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error {
	p.evaled = append(p.evaled, key)
	if p.deny[key] {
		return domain.PermissionError{Reason: "denied by test"}
	}
	return nil
}

type recordingDeliveryQueue struct {
	jobs []domain.DeliveryJob
}

func (d *recordingDeliveryQueue) Enqueue(ctx context.Context, job domain.DeliveryJob) error {
	d.jobs = append(d.jobs, job)
	return nil
}

// subtreeRecord builds a stored record fixture the way QueryRecordSubtree
// serves them: document JSON plus CCKV and CCFS URIs.
func subtreeRecord(t *testing.T, uri string) concrnt.SignedDocument {
	t.Helper()
	return subtreeRecordAt(t, uri, time.Now())
}

func subtreeRecordAt(t *testing.T, uri string, createdAt time.Time) concrnt.SignedDocument {
	t.Helper()
	doc := concrnt.Document[map[string]string]{
		Kind:      "record",
		Key:       uri,
		Value:     map[string]string{"body": "x"},
		Author:    "example.com",
		Schema:    "https://example.com/item.json",
		CreatedAt: createdAt,
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture document: %v", err)
	}
	u := uri
	ccfs := concrnt.ComposeCCFSURI("example.com", concrnt.CCFSTypeConcrnt, fmt.Sprintf("doc-%x", len(uri)))
	return concrnt.SignedDocument{
		CCKV:     &u,
		CCFS:     &ccfs,
		Document: string(docBytes),
		Proof:    concrnt.Proof{Type: concrnt.ProofTypeNone},
	}
}

// signedDelete builds a signed delete commit for the given target URI.
func signedDelete(t *testing.T, ccid, privKeyHex, target string) concrnt.SignedDocument {
	t.Helper()
	return signTestDocument(t, concrnt.Document[schemas.Delete]{
		Kind:      "delete",
		Value:     schemas.Delete(target),
		Author:    ccid,
		Schema:    "https://schema.concrnt.net/delete.json",
		CreatedAt: time.Now(),
	}, privKeyHex)
}

// newRangeDeleteUsecase wires a usecase whose delete author is a local,
// resolvable entity. The subtree base's owner is the server FQDN itself, so
// ResolveResourceHost resolves it locally without any network.
func newRangeDeleteUsecase(ccid string, cfg *domain.Config, repo RecordRepository, pol PolicyService, delivery DeliveryQueue, kvs KVS) *RecordUsecase {
	return NewRecordUsecase(
		repo,
		fixedResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: cfg.FQDN}},
		newTestServerUsecase(cfg),
		cfg,
		client.New(cfg.FQDN),
		nopSignalService{},
		pol,
		delivery,
		kvs,
	)
}

const rangeBaseURI = "cckv://example.com/profiles/main/lists/l1"

// A subtree range delete ("...l1*") enumerates with includeSelf, evaluates the
// policy on every target, deletes them all in one batch, commits, and then
// tombstones and announces each deleted target.
func TestCommitRangeDeleteSubtree(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	targets := []concrnt.SignedDocument{
		subtreeRecord(t, rangeBaseURI),
		subtreeRecord(t, rangeBaseURI+"/a"),
		subtreeRecord(t, rangeBaseURI+"/b"),
	}
	repo := &rangeDeleteRepo{subtree: targets}
	pol := &denyKeysPolicyService{}
	delivery := &recordingDeliveryQueue{}
	kvs := &stubKVS{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, pol, delivery, kvs)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"*")
	result, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}

	if len(repo.queryCalls) != 1 || repo.queryCalls[0] != (subtreeQueryCall{base: rangeBaseURI, includeSelf: true}) {
		t.Fatalf("unexpected enumeration calls: %+v", repo.queryCalls)
	}

	wantURIs := []string{rangeBaseURI, rangeBaseURI + "/a", rangeBaseURI + "/b"}
	if !slices.Equal(repo.deletedURIs, wantURIs) {
		t.Fatalf("deleted URIs = %v, want %v", repo.deletedURIs, wantURIs)
	}
	if !slices.Equal(pol.evaled, wantURIs) {
		t.Fatalf("policy evaluated on %v, want %v", pol.evaled, wantURIs)
	}
	if len(repo.txs) != 1 || !repo.txs[0].committed || repo.txs[0].rolledBack {
		t.Fatalf("unexpected tx state: %+v", repo.txs)
	}

	// every deleted target is tombstoned by its ccfs URI
	for _, target := range targets {
		if !slices.Contains(kvs.setKeys, tombstoneKey(*target.CCFS)) {
			t.Fatalf("missing tombstone for %s, set keys: %v", *target.CCFS, kvs.setKeys)
		}
	}

	// one "deleted" event per target, addressed to the concrete URI
	deletedEventURIs := []string{}
	for _, job := range delivery.jobs {
		if job.Event != nil && job.Event.Type == "deleted" {
			deletedEventURIs = append(deletedEventURIs, job.Event.URI)
		}
	}
	if !slices.Equal(deletedEventURIs, wantURIs) {
		t.Fatalf("deleted events for %v, want %v", deletedEventURIs, wantURIs)
	}

	// the reported result is the base record itself
	if result == nil || result.CCKV == nil || *result.CCKV != rangeBaseURI {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// A children-only range delete ("...l1/*") enumerates without includeSelf.
func TestCommitRangeDeleteChildrenOnly(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	repo := &rangeDeleteRepo{subtree: []concrnt.SignedDocument{subtreeRecord(t, rangeBaseURI+"/a")}}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, nil)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"/*")
	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if len(repo.queryCalls) != 1 || repo.queryCalls[0] != (subtreeQueryCall{base: rangeBaseURI, includeSelf: false}) {
		t.Fatalf("unexpected enumeration calls: %+v", repo.queryCalls)
	}
}

// A single denied target fails the whole range delete: nothing is deleted, the
// transaction (commitlog included) is rolled back, and no tombstone or signal
// is emitted.
func TestCommitRangeDeleteDenyRollsBackEverything(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	repo := &rangeDeleteRepo{subtree: []concrnt.SignedDocument{
		subtreeRecord(t, rangeBaseURI),
		subtreeRecord(t, rangeBaseURI+"/a"),
		subtreeRecord(t, rangeBaseURI+"/b"),
	}}
	pol := &denyKeysPolicyService{deny: map[string]bool{rangeBaseURI + "/b": true}}
	delivery := &recordingDeliveryQueue{}
	kvs := &stubKVS{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, pol, delivery, kvs)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"*")
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("expected permission error, got %v", err)
	}

	if len(repo.deletedURIs) != 0 {
		t.Fatalf("no target may be deleted on denial, got %v", repo.deletedURIs)
	}
	if len(repo.txs) != 1 || repo.txs[0].committed || !repo.txs[0].rolledBack {
		t.Fatalf("transaction must be rolled back, got %+v", repo.txs)
	}
	if len(kvs.setKeys) != 0 {
		t.Fatalf("no tombstone may be set on denial, got %v", kvs.setKeys)
	}
	if len(delivery.jobs) != 0 {
		t.Fatalf("no delivery may be enqueued on denial, got %d jobs", len(delivery.jobs))
	}
	// fail-fast: evaluation stops at the denied key
	if len(pol.evaled) != 3 || pol.evaled[2] != rangeBaseURI+"/b" {
		t.Fatalf("unexpected evaluation order: %v", pol.evaled)
	}
}

// A range that matches nothing is an error (consistent with deleting a missing
// single key) and rolls back the commitlog.
func TestCommitRangeDeleteEmptyIsNotFound(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	repo := &rangeDeleteRepo{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, nil)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"/*")
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
	if len(repo.txs) != 1 || repo.txs[0].committed || !repo.txs[0].rolledBack {
		t.Fatalf("transaction must be rolled back, got %+v", repo.txs)
	}
}

// A '*' anywhere but the trailing sentinel position is rejected before any
// enumeration happens.
func TestCommitRangeDeleteRejectsInnerAsterisk(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	repo := &rangeDeleteRepo{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, nil)

	sd := signedDelete(t, ccid, priv, "cckv://example.com/pa*th/item*")
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if len(repo.queryCalls) != 0 {
		t.Fatalf("no enumeration may happen for an invalid target, got %+v", repo.queryCalls)
	}
}

// Record keys may not contain '*': it is reserved as the range-delete
// sentinel, so a literal-asterisk key could never be deleted unambiguously.
func TestCommitRejectsAsteriskInRecordKey(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	repo := &recordingRecordRepo{}
	uc := newRecordCommitUsecase(ccid, cfg, repo, nil)

	sd := signTestDocument(t, concrnt.Document[any]{
		Kind:      "record",
		Key:       concrnt.CCURI{Scheme: "cckv", Owner: ccid, Key: "posts/it*em"}.String(),
		Author:    ccid,
		Schema:    "https://example.com/post.json",
		CreatedAt: time.Now(),
	}, priv)
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if repo.createRecordCalled {
		t.Fatal("CreateRecord must not be called for a key containing '*'")
	}
}

// A committed delete tombstones the delete command itself (CIP-3 §3.4): the
// target tombstones don't identify the command, so without it a captured
// delete of a reusable cckv key could be replayed to remove a newer document
// created there later.
func TestCommitDeleteReplayIsRejected(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}
	repo := &rangeDeleteRepo{subtree: []concrnt.SignedDocument{subtreeRecord(t, rangeBaseURI)}}
	kvs := &stubKVS{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, kvs)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"*")
	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}

	var deleteDoc concrnt.Document[any]
	if err := json.Unmarshal([]byte(sd.Document), &deleteDoc); err != nil {
		t.Fatalf("unmarshal delete document: %v", err)
	}
	selfCCFS := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  "example.com",
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentIDFor(sd.Document, deleteDoc.CreatedAt),
	}.String()
	if !slices.Contains(kvs.setKeys, tombstoneKey(selfCCFS)) {
		t.Fatalf("delete command was not tombstoned, set keys: %v", kvs.setKeys)
	}

	// a fresh document has since been created at the key; replaying the
	// captured delete must be rejected before it reaches deleteRecord
	deletedBefore := len(repo.deletedURIs)
	_, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("expected replay rejection, got %v", err)
	}
	if len(repo.deletedURIs) != deletedBefore {
		t.Fatalf("replayed delete removed documents: %v", repo.deletedURIs)
	}
}

// Tombstone TTLs are anchored on the later of the processing time and the
// affected documents' createdAt (CIP-3 §3.4): a target stamped near the
// future-skew limit must stay tombstoned past its own backdate window, and a
// range delete applies the latest involved timestamp to every tombstone.
func TestCommitDeleteTombstoneTTLOrigin(t *testing.T) {
	ccid, priv := newIdentity(t)
	cfg := &domain.Config{FQDN: "example.com"}

	skew := 11 * time.Hour
	repo := &rangeDeleteRepo{subtree: []concrnt.SignedDocument{
		subtreeRecord(t, rangeBaseURI),
		subtreeRecordAt(t, rangeBaseURI+"/a", time.Now().Add(skew)),
	}}
	kvs := &stubKVS{}
	uc := newRangeDeleteUsecase(ccid, cfg, repo, &denyKeysPolicyService{}, &recordingDeliveryQueue{}, kvs)

	sd := signedDelete(t, ccid, priv, rangeBaseURI+"*")
	if _, err := uc.Commit(context.Background(), "127.0.0.1", sd, domain.CommitModeExecute); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}

	// two targets + the delete command itself
	if len(kvs.setTTLs) != 3 {
		t.Fatalf("expected 3 tombstones, got %v", kvs.setTTLs)
	}
	want := domain.MaxBackdate + skew
	for key, ttl := range kvs.setTTLs {
		if ttl < want-time.Minute || ttl > want+time.Minute {
			t.Fatalf("tombstone %s has ttl %v, want ~%v", key, ttl, want)
		}
	}
}

