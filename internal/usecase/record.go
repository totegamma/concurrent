package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/zeebo/xxh3"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/utils"
	"github.com/concrnt/concrnt/policy"
	"github.com/concrnt/concrnt/schemas"
)

type RecordRepository interface {
	BeginTx(ctx context.Context) (RepositoryTx, error)

	CreateCommitLog(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof any) error
	CreateCommitOwners(ctx context.Context, tx RepositoryTx, id string, owners []string) error
	HasCommitLog(ctx context.Context, id string) (bool, error)
	// CreateEntity reports whether the upsert applied — false when the stored
	// entity already carries a newer-or-equal documentID (accept-if-newer).
	CreateEntity(ctx context.Context, tx RepositoryTx, ccid string, alias *string, domain string, documentID string) (bool, error)
	// CreateRecord reports whether the write applied — false when the key's
	// stored record already carries a newer-or-equal documentID
	// (accept-if-newer).
	CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, author string, schema string, onUpdate *string, policies *string, distributions []string, redirect *string, createdAt time.Time) (bool, error)
	CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) (bool, error)
	// Acknowledge / UnAcknowledge report whether the transition applied —
	// false when the stored (from, to, schema) state already carries a
	// newer-or-equal documentID (accept-if-newer).
	Acknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)
	UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)
	DeleteRecordByKey(ctx context.Context, tx RepositoryTx, targetURI string) error
	DeleteRecordByDocumentID(ctx context.Context, tx RepositoryTx, documentID string) error
	DeleteAssociation(ctx context.Context, tx RepositoryTx, documentID string) error

	// QueryRecordSubtree enumerates every live record at base itself
	// (includeSelf) and under base's path subtree, URI-ordered. Unlike
	// QueryByPrefix it never matches sibling keys ("item2" for base "item")
	// and escapes pattern metacharacters — it returns exactly the set a
	// range delete may remove.
	QueryRecordSubtree(ctx context.Context, base string, includeSelf bool) ([]concrnt.SignedDocument, error)

	// GetTimelineRemoval reports the chunkline (timeline URI, item ID) tuple a
	// record key currently occupies, or ("", "") when it is not a timeline
	// member. The item ID must match the BodyItem.ID() the chunkline body
	// endpoint serves for that member.
	GetTimelineRemoval(ctx context.Context, keyURI string) (timeline string, itemID string, err error)

	GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error)
	GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error)
	GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error)

	GetDistributions(ctx context.Context, uri string) ([]string, error)

	GetAcknowledgeRecords(ctx context.Context, from, to, schema string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error)
	GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error)
	GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error)

	QueryByPrefix(ctx context.Context, prefix, schema, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	QueryByParent(ctx context.Context, parent, schema, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
}

// QueryRow is a raw list-query result row paired with its effective sort key
// (the DB-side created_at the repository ordered by). Pagination cursors are
// derived from this key, so it must be carried alongside the document rather
// than re-parsed from it.
type QueryRow struct {
	Row       concrnt.SignedDocument
	CreatedAt time.Time
}

type RepositoryTx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type PostProcessAction func(ctx context.Context) error

type SignalService interface {
	Publish(ctx context.Context, channel string, event concrnt.Event) error
}

// DeliveryQueue hands off federation delivery jobs to be executed
// asynchronously (host resolution + local publish/commit or remote HTTP
// commit). See internal/domain.DeliveryJob for the job shape and
// internal/worker.DeliveryWorker for the consumer side.
type DeliveryQueue interface {
	Enqueue(ctx context.Context, job domain.DeliveryJob) error
}

type PolicyService interface {
	Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error
}

// KVS is a store of TTL'd string sets, used here to hold per-timeline
// removed-item advertisements. Implemented by internal/infra/kvs.Redis; nil in
// offline tooling/tests.
type KVS interface {
	SetAdd(ctx context.Context, key string, value string, ttl time.Duration) error
	SetMembers(ctx context.Context, key string) ([]string, error)
}

type commitApplyResult struct {
	result        *concrnt.SignedDocument
	owners        []string
	postProcesses []PostProcessAction
	// noop marks an apply that changed nothing (accept-if-newer loss): the
	// whole tx is rolled back — commit_log included — and success returned,
	// so rejected-as-older documents never enter the history.
	noop bool
}

type RecordUsecase struct {
	repo      RecordRepository
	residence ResidenceRepository
	server    *ServerUsecase
	config    *domain.Config
	client    *client.Client
	signal    SignalService
	policy    PolicyService
	delivery  DeliveryQueue
	kvs       KVS
	cache     *cache.Cache
}

func NewRecordUsecase(
	repo RecordRepository,
	residence ResidenceRepository,
	server *ServerUsecase,
	config *domain.Config,
	client *client.Client,
	signal SignalService,
	policy PolicyService,
	delivery DeliveryQueue,
	kvs KVS,
) *RecordUsecase {
	return &RecordUsecase{
		repo:      repo,
		residence: residence,
		server:    server,
		config:    config,
		client:    client,
		signal:    signal,
		policy:    policy,
		delivery:  delivery,
		kvs:       kvs,
		cache:     cache.New(10*time.Minute, 15*time.Minute),
	}
}

// documentIDFor derives a document's content+time CDID, the id it is stored
// under. It is time-prefixed and content-hashed, so string comparison orders
// documents by createdAt with a deterministic content tiebreaker.
func documentIDFor(document string, createdAt time.Time) string {
	hash := concrnt.GetHash([]byte(document))
	var hash10 [10]byte
	copy(hash10[:], hash[:10])
	return cdid.New(hash10, createdAt).String()
}

// resolver returns uc.client as a DocumentResolver, or a nil interface when
// the client itself is nil (offline tooling, tests) — assigning a typed nil
// pointer directly would bypass Verify's nil-resolver guard and panic on use.
func (uc *RecordUsecase) resolver() concrnt.DocumentResolver {
	if uc.client == nil {
		return nil
	}
	return uc.client
}

func GetReferrerFromReferences(sd concrnt.SignedDocument, requesterID string) *string {
	requesterCCKV := concrnt.CCURI{Scheme: "cckv", Owner: requesterID}.String()
	entityRef, ok := sd.References[requesterCCKV]
	if ok {
		var entity concrnt.Document[schemas.Entity]
		err := json.Unmarshal([]byte(entityRef.Document), &entity)
		if err != nil {
			return nil
		}
		return &entity.Value.Domain
	}
	return nil
}

func (uc *RecordUsecase) Commit(ctx context.Context, ip string, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Commit")
	defer span.End()

	// CIP-1 §4.1: the signed serialization must not exceed 32 KiB. Checked
	// before anything else — including the system-service-account bypass —
	// because sd.Document is exactly what CreateCommitLog persists.
	if len(sd.Document) > domain.MaxDocumentSize {
		err := domain.ValidationError{Field: "document", Message: fmt.Sprintf("document exceeds the maximum size of %d bytes", domain.MaxDocumentSize)}
		span.RecordError(err)
		return nil, err
	}

	var doc concrnt.Document[any]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	documentID := documentIDFor(sd.Document, doc.CreatedAt)

	// CIP-3 §3.1: authority-bearing fields must not use alias-form owners
	// (@<FQDN>) — a signed routing identifier must not depend on mutable DNS.
	// Entry validation like the size check above: applies to every committer.
	// The resolve path (GetEntity) still accepts aliases.
	if err := rejectAliasOwners(doc); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// A document already in commit_logs was fully applied once; re-delivery
	// (client retry, federation redelivery, dump re-import) is a no-op
	// success. This is also the replay guard: deleted and superseded
	// documents stay in commit_logs, so a captured copy can't be replayed
	// back in. Once a gc'd commit_log falls out, the backdate window below
	// rejects the document instead. Checked before signature verification —
	// documentID is derived from the full document bytes, so a hit reveals
	// nothing the requester doesn't already hold, and verification can be
	// expensive (remote subkey fetches).
	alreadyCommitted, err := uc.repo.HasCommitLog(ctx, documentID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if alreadyCommitted {
		return &sd, nil
	}

	serviceAccountType, _ := ctx.Value(interop.ServiceAccountTypeCtxKey).(string)
	isServiceAccount := serviceAccountType == "system"

	// A signed createdAt is otherwise attacker-controlled, and a far-future
	// stamp would let one document dominate every later one (e.g. entity
	// accept-if-newer freezes on the newest createdAt). Reject too-far-future
	// documents globally, with a generous clock-skew tolerance.
	if doc.CreatedAt.After(time.Now().Add(domain.MaxFutureSkew)) {
		err := domain.ValidationError{Field: "createdAt", Message: "createdAt is too far in the future"}
		span.RecordError(err)
		return nil, err
	}

	// System service accounts (migration/import via conctl) carry the server's
	// own key and legitimately replay unsigned (none-proof) historical
	// documents with backdated timestamps, so they skip signature verification
	// and the backdate window below. Every other committer is verified:
	//   - document-reference proofs verify against the recursively-verified
	//     inline copy in References (the author vouching for their own
	//     document), so commits stay valid even when the referenced document's
	//     origin server is unreachable (e.g. mid-migration imports);
	//   - subkey enact documents are always fetched from their authoritative
	//     server inside Verify, so revoked subkeys can't be replayed inline.
	if !isServiceAccount {
		// Entity documents must be master-key signed (CIP-0 §8.2): affiliation
		// is an account-level statement, and combined with the backdate
		// exemption below a subkey proof would let a leaked, since-revoked
		// subkey forge a backdated affiliation forever (CIP-13 §8 relies on
		// the backdate window to bound exactly that).
		var allowedProofs []string
		if doc.Kind == "entity" {
			allowedProofs = []string{concrnt.ProofTypeEcrecover}
		}
		if err := sd.VerifyWithProofTypes(ctx, uc.resolver(), allowedProofs); err != nil {
			span.RecordError(err)
			if errors.Is(err, concrnt.ErrNoneProofNotAllowed) {
				slog.Error("Unauthorized commit with none proof", "error", err.Error())
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "none proof type is only allowed for system service accounts"}, err)
			}
			if doc.Kind == "entity" && errors.Is(err, concrnt.ErrProofTypeNotAllowed) {
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "entity documents must be signed with " + concrnt.ProofTypeEcrecover}, err)
			}
			if errors.Is(err, concrnt.ErrUnsupportedProofType) {
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "unsupported proof type: " + sd.Proof.Type}, err)
			}
			return nil, errors.Join(domain.ValidationError{Field: "proof", Message: "signature verification failed"}, err)
		}

		// Entity documents are exempt from the backdate window: an affiliation
		// signature is long-lived and re-presented indefinitely (federated
		// resolution commits fetched copies, GetEntity), accept-if-newer
		// already no-ops old replays, and the master-key requirement above
		// keeps the leaked-subkey backdating bound of CIP-13 §8 intact.
		// Self-service migration: an authenticated user importing their own
		// repository dump (LocalOnlyExecute never re-federates) may replay
		// historical documents past the backdate window — their own, and
		// documents by others that target their content (e.g. inbound
		// associations carried over in the dump). Signature verification
		// still applies.
		backdateExempt := doc.Kind == "entity"
		if !backdateExempt && mode == domain.CommitModeLocalOnlyExecute {
			if authenticated, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity); ok {
				backdateExempt = doc.Author == authenticated.ID
				if !backdateExempt && doc.Key != "" {
					if parsed, err := concrnt.ParseCCURI(doc.Key); err == nil && parsed.Owner == authenticated.ID {
						backdateExempt = true
					}
				}
				if !backdateExempt && doc.Associate != nil {
					if parsed, err := concrnt.ParseCCURI(*doc.Associate); err == nil && parsed.Owner == authenticated.ID {
						backdateExempt = true
					}
				}
			}
		}

		// Reject documents older than the backdate window. Together with the
		// commit_logs check above this makes a deletion permanent against
		// replay — a captured document is either still in commit_logs (and
		// no-ops) or already too old to accept. This is also what keeps a
		// future gc of flagged commit_logs safe, provided the retention
		// period is at least MaxBackdate.
		if !backdateExempt && doc.CreatedAt.Before(time.Now().Add(-domain.MaxBackdate)) {
			err := domain.ValidationError{Field: "createdAt", Message: "createdAt is older than the allowed backdate window"}
			span.RecordError(err)
			return nil, err
		}
	}

	requesterID := doc.Author
	referrer := GetReferrerFromReferences(sd, requesterID)

	requester, requesterErr := uc.GetEntity(ctx, concrnt.CCURI{Scheme: "cckv", Owner: requesterID, Hint: referrer}.String())
	if requesterErr != nil {
		span.RecordError(requesterErr)
	}

	// Only "entity" commits (self-registration) may proceed without an
	// already-resolvable requester entity.
	if doc.Kind != "entity" && requester == nil {
		err := errors.Join(domain.ValidationError{Field: "document.author", Message: fmt.Sprintf("requester entity not found for %s operation", doc.Kind)}, requesterErr)
		span.RecordError(err)
		return nil, err
	}

	targetUserID := ""
	if doc.Key != "" {
		parsed, err := concrnt.ParseCCURI(doc.Key)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		targetUserID = parsed.Owner
	}
	if doc.Associate != nil {
		parsed, err := concrnt.ParseCCURI(*doc.Associate)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		targetUserID = parsed.Owner
	}
	if targetUserID != "" && targetUserID != requesterID {

		blockingUsers, err := uc.getBlockingUsers(ctx, requesterID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		if slices.Contains(blockingUsers, targetUserID) {
			err := errors.New("operation blocked due to user block relationship")
			span.RecordError(err)
			return nil, err
		}
	}

	var applyCommit func(tx RepositoryTx) (*commitApplyResult, error)

	switch doc.Kind {
	case "entity":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.saveEntity(ctx, tx, documentID, sd)
		}

	case "record":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.createRecord(ctx, tx, documentID, ip, *requester, doc, sd, mode)
		}

	case "association":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.createAssociation(ctx, tx, documentID, ip, *requester, doc, sd, mode)
		}

	case "ack":
		referrer := GetReferrerFromReferences(sd, requester.CCKV())
		targetUserID := *doc.Associate
		if referrer != nil {
			targetUserID = targetUserID + "@" + *referrer
		}
		targetUser, err := uc.GetEntity(ctx, targetUserID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.acknowledge(ctx, tx, documentID, ip, *requester, *targetUser, doc, sd, mode)
		}

	case "unack":
		referrer := GetReferrerFromReferences(sd, requester.CCKV())
		targetUserID := *doc.Associate
		if referrer != nil {
			targetUserID = targetUserID + "@" + *referrer
		}
		targetUser, err := uc.GetEntity(ctx, targetUserID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.unacknowledge(ctx, tx, documentID, ip, *requester, *targetUser, doc, sd, mode)
		}
	case "delete":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.deleteRecord(ctx, tx, *requester, sd, mode)
		}
	default:
		err := errors.New("unsupported document kind: " + doc.Kind)
		span.RecordError(err)
		return nil, err
	}

	tx, err := uc.repo.BeginTx(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof); err != nil {
		span.RecordError(err)
		return nil, err
	}

	applyResult, err := applyCommit(tx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Nothing was applied (accept-if-newer loss): let the deferred rollback
	// discard the tx — commit_log included — and just report success.
	if applyResult.noop {
		slog.Info("commit no-op (accept-if-newer loss)",
			"documentID", documentID, "kind", doc.Kind, "author", doc.Author)
		return applyResult.result, nil
	}

	if err := uc.repo.CreateCommitOwners(ctx, tx, documentID, applyResult.owners); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		span.RecordError(err)
		return nil, err
	}
	committed = true

	// A same-key overwrite or delete must be visible to this server's own
	// verification paths immediately, not after the resource cache's TTL:
	// auth and commit verification resolve documents (e.g. subkey enact,
	// CIP-13 revocation) through uc.client, which caches for 10 minutes.
	if uc.client != nil {
		if doc.Key != "" {
			uc.client.InvalidateResource(doc.Key)
		}
		if doc.Kind == "delete" {
			if target, ok := doc.Value.(string); ok && target != "" {
				uc.client.InvalidateResource(target)
			}
		}
	}

	for _, task := range applyResult.postProcesses {
		if err := task(ctx); err != nil {
			slog.Error(
				"failed to run post-commit action",
				slog.String("error", err.Error()),
			)
			span.RecordError(err)
		}
	}

	return applyResult.result, nil
}

// rejectAliasOwners rejects alias-form owners (@<FQDN>, CIP-0 §7.2) in the
// authority-bearing fields of a commit — key, associate, the delete target,
// and each distributes entry (CIP-3 §3.1). Unparseable URIs pass through:
// they are rejected downstream where the field actually matters.
func rejectAliasOwners(doc concrnt.Document[any]) error {
	check := func(field, uri string) error {
		parsed, err := concrnt.ParseCCURI(uri)
		if err == nil && strings.HasPrefix(parsed.Owner, "@") {
			return domain.ValidationError{Field: field, Message: "alias-form owners are not allowed in commits"}
		}
		return nil
	}
	if doc.Key != "" {
		if err := check("key", doc.Key); err != nil {
			return err
		}
	}
	if doc.Associate != nil {
		if err := check("associate", *doc.Associate); err != nil {
			return err
		}
	}
	if doc.Kind == "delete" {
		if target, ok := doc.Value.(string); ok {
			base := strings.TrimSuffix(strings.TrimSuffix(target, "*"), "/")
			if err := check("value", base); err != nil {
				return err
			}
		}
	}
	for _, dest := range distributionsFromPtr(doc.Distributes) {
		if err := check("distributes", dest); err != nil {
			return err
		}
	}
	return nil
}

func (uc *RecordUsecase) saveEntity(ctx context.Context, tx RepositoryTx, documentID string, sd concrnt.SignedDocument) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Accept-if-newer fast path: an entity document only replaces the stored
	// one when its documentID is greater. documentID is a time-prefixed,
	// content-hashed, sortable CDID, so this orders by createdAt and breaks
	// exact-createdAt ties deterministically (both federated servers converge
	// on the same winner). Older-or-equal replays (e.g. re-running a
	// migration/import, where the same document reproduces the same documentID)
	// succeed as a no-op here, skipping the registration/alias checks below so
	// stale replays can't fail on them. CreateEntity re-checks the same
	// ordering under a row lock, so this check is only an optimization, not the
	// authoritative guard against concurrent writers.
	existing, err := uc.residence.GetEntityByCCID(ctx, entity.Author)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		span.RecordError(err)
		return nil, err
	}
	if existing != nil && existing.SignedDocument != nil {
		var existingDoc concrnt.Document[schemas.Entity]
		if err := json.Unmarshal([]byte(existing.SignedDocument.Document), &existingDoc); err != nil {
			// corrupt stored document: log and let the incoming one overwrite it
			slog.Error("failed to decode stored entity document, overwriting", "ccid", entity.Author, "error", err.Error())
		} else if documentID <= documentIDFor(existing.SignedDocument.Document, existingDoc.CreatedAt) {
			return &commitApplyResult{result: &sd, noop: true}, nil
		}
	}

	// CIP-0 §9: only interact with servers on the same layer. Fail-closed: an
	// entity is only stored when its home server resolves and is green (same
	// layer, not tagged _blocked). For the local domain Resolve returns
	// GetThisServer, which is trivially green.
	// None-proof documents are exempt: they only reach here via the system
	// service account (Verify rejects them for everyone else), and migration
	// imports replay historical entities whose home servers may be offline or
	// still on v1.
	if sd.Proof.Type != concrnt.ProofTypeNone {
		entityServer, err := uc.server.Resolve(ctx, entity.Value.Domain, nil)
		if err != nil {
			err = errors.Join(domain.ValidationError{Field: "value.domain", Message: "failed to resolve the entity's domain"}, err)
			span.RecordError(err)
			return nil, err
		}
		if entityServer.Layer() != uc.config.Layer {
			err := domain.ValidationError{Field: "value.domain", Message: "the entity's domain is on a different layer"}
			span.RecordError(err)
			return nil, err
		}
		if serverTag := entityServer.Tag(); serverTag.Has("_blocked") {
			err := domain.ValidationError{Field: "value.domain", Message: "the entity's domain is blocked"}
			span.RecordError(err)
			return nil, err
		}
	}

	if entity.Value.Domain == uc.config.FQDN {
		// if local, check if author is registered
		_, err := uc.residence.GetMeta(ctx, entity.Author)
		if err != nil {
			span.RecordError(err)
			return nil, errors.New("user is not registered for this domain")
		}
	}

	if entity.Value.Alias != nil {
		name := "_concrnt." + *entity.Value.Alias
		txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
		if err != nil {
			span.RecordError(err)
			return nil, errors.New("alias ownership verification failed: TXT record not found for " + name)
		}

		verified := false
		for _, record := range txtrecords {
			parsed, err := concrnt.ParseCCURI(record)
			if err != nil {
				continue
			}
			if parsed.Owner == entity.Author {
				verified = true
				break
			}
		}

		if !verified {
			err := errors.New("alias ownership verification failed: no valid TXT record found for " + name)
			span.RecordError(err)
			return nil, err
		}
	}

	applied, err := uc.repo.CreateEntity(ctx, tx, entity.Author, entity.Value.Alias, entity.Value.Domain, documentID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	// Lost the newer-wins recheck under the row lock to a concurrent writer:
	// same accept-if-newer no-op as the fast path above.
	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	return &commitApplyResult{result: &sd, owners: []string{entity.Author}}, nil
}

func distributionsFromPtr(distributions *[]string) []string {
	if distributions == nil {
		return nil
	}
	return *distributions
}

func (uc *RecordUsecase) createReferenceDistributionActions(ctx context.Context, ip string, author string, href string, requester domain.Entity, sd concrnt.SignedDocument, destinations []string, mode domain.CommitMode) ([]PostProcessAction, error) {
	if mode != domain.CommitModeExecute || len(destinations) == 0 {
		return nil, nil
	}

	requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
	if err != nil {
		return nil, err
	}

	// the key segment is the hash-based CDID of the href, so a record keeps
	// the same reference key across accept-if-newer overwrites and the new
	// reference replaces the old row instead of piling up next to it
	refSegment := cdid.MakeHash([]byte(href)).String()

	postProcesses := make([]PostProcessAction, 0, len(destinations))
	for _, destURI := range destinations {
		key, err := url.JoinPath(destURI, refSegment)
		if err != nil {
			slog.Error("failed to join path for distribution", slog.String("destination", destURI), slog.String("href", href), slog.String("error", err.Error()))
			continue
		}

		distDoc := concrnt.Document[schemas.Reference]{
			Kind: "record",
			Key:  key,
			Value: schemas.Reference{
				Href: href,
			},
			Author:    author,
			Schema:    schemas.ReferenceURL,
			CreatedAt: time.Now(),
		}
		docBytes, err := json.Marshal(distDoc)
		if err != nil {
			return nil, err
		}
		distSD := concrnt.SignedDocument{
			Document: string(docBytes),
			Proof: concrnt.Proof{
				Type: concrnt.ProofTypeDocumentReference,
				Href: &href,
			},
			References: map[string]concrnt.SignedDocument{
				requester.CCKV(): *requesterSD,
				href:             sd,
			},
		}

		destURI := destURI
		postProcesses = append(postProcesses,
			func(ctx context.Context) error {
				return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
					ResolveURI: destURI,
					Payload:    distSD,
					Local:      domain.DeliveryLocalCommit,
					Remote:     domain.DeliveryRemoteCommit,
					IP:         ip,
				})
			},
		)
	}

	return postProcesses, nil
}

// deleteRecord handles both a plain delete and the trailing-asterisk range
// notation ("...item*" = item plus its subtree, "...item/*" = subtree only) —
// a plain delete is simply a range whose enumeration is the single addressed
// document. When this server is authoritative for the target key, the target
// rows are removed and the delete is fanned out to every distribute
// destination; on the receiving (non-authoritative) side the targets are
// recovered from References. In both cases the distribute reference records
// this server holds (created by createReferenceDistributionActions) are
// deleted in the same transaction and advertised via /chunkline/removed, and
// a delete that concerns neither the target key nor any locally-held
// distributed copy is a no-op success. The delete policy is evaluated on
// every authoritative target before anything is removed, and any failure —
// including a single policy denial — makes the commit transaction roll back
// in full, commitlog included: deletion is all-or-nothing. The one exception
// is a policy denial on a reference-record sweep: that row is skipped (kept,
// the pre-sweep status quo), so a destination timeline's policy can never
// block deleting the record itself.
func (uc *RecordUsecase) deleteRecord(ctx context.Context, tx RepositoryTx, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Delete")
	defer span.End()

	var deletedoc concrnt.Document[schemas.Delete]
	err := json.Unmarshal([]byte(sd.Document), &deletedoc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	rawTarget := string(deletedoc.Value)

	rangeBase, includeSelf, isRange, err := parseRangeDeleteTarget(rawTarget)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	targetHost, err := uc.client.ResolveResourceHost(ctx, rawTarget)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	authoritative := targetHost == uc.config.FQDN

	// enumerate the targets: the stored subtree/document when this server is
	// authoritative for the target key, the References entries otherwise (for
	// a range the origin server enqueues one delivery job per deleted target,
	// each carrying that target in References, so matching References against
	// the range is how a receiving server learns the target list)
	var targets []concrnt.SignedDocument
	var targetURIs []string // per-target address (cckv/ccfs URI) deletion and policy key on
	if authoritative {
		if isRange {
			targets, err = uc.repo.QueryRecordSubtree(ctx, rangeBase, includeSelf)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			if len(targets) == 0 {
				err := domain.NotFoundError{Resource: rawTarget}
				span.RecordError(err)
				return nil, err
			}
			for _, target := range targets {
				targetURIs = append(targetURIs, *target.CCKV)
			}
		} else {
			targetSD, err := uc.repo.GetSignedDocument(ctx, rawTarget)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			targets = []concrnt.SignedDocument{*targetSD}
			targetURIs = []string{rawTarget}
		}
	} else {
		if isRange {
			for refURI := range sd.References {
				if (includeSelf && refURI == rangeBase) || strings.HasPrefix(refURI, rangeBase+"/") {
					targetURIs = append(targetURIs, refURI)
				}
			}
			slices.Sort(targetURIs)
		} else if _, ok := sd.References[rawTarget]; ok {
			targetURIs = []string{rawTarget}
		}
		// no matching reference: this delete concerns nothing this server can
		// act on — pass it through as a no-op success (commitlog included, so
		// a later delivery that does carry the targets is not deduplicated away)
		if len(targetURIs) == 0 {
			return &commitApplyResult{result: &sd, noop: true}, nil
		}
		for _, targetURI := range targetURIs {
			targets = append(targets, sd.References[targetURI])
		}
	}

	targetDocs := make([]concrnt.Document[any], len(targets))
	for i, target := range targets {
		err = json.Unmarshal([]byte(target.Document), &targetDocs[i])
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	// evaluate the delete policy on every authoritative target before
	// removing anything
	if authoritative {
		for i := range targets {
			policyRoot := targetURIs[i]
			if targetDocs[i].Associate != nil {
				policyRoot = *targetDocs[i].Associate
			}

			stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, policyRoot)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			err = uc.policy.Eval(
				ctx,
				policy.RequestContext{
					Requester: requester,
					Self:      targetDocs[i],
				},
				stack,
				policyDeleteAction(targetDocs[i]),
				targetURIs[i],
			)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		}
	}

	remoteKind := domain.DeliveryRemoteNone
	if authoritative {
		// only the authoritative server re-federates the delete; a receiving
		// server acts on its own copies and signals its own subscribers
		remoteKind = domain.DeliveryRemoteCommit
	}

	postProcesses := []PostProcessAction{}
	for i := range targets {
		targetSD := &targets[i]
		targetDoc := targetDocs[i]
		targetURI := targetURIs[i]

		var removedTimeline, removedItemID string

		if authoritative {
			switch targetDoc.Kind {
			case "record":

				// capture which chunkline item this record occupies before the
				// delete cascades its record_keys row away; advertised via
				// /chunkline/removed so readers can drop it from cached chunks
				if mode == domain.CommitModeExecute && uc.kvs != nil && targetSD.CCKV != nil {
					tl, id, err := uc.repo.GetTimelineRemoval(ctx, *targetSD.CCKV)
					if err != nil {
						span.RecordError(err) // non-fatal: the deleted item just lingers in caches
					} else {
						removedTimeline, removedItemID = tl, id
					}
				}

				parsedURI, err := concrnt.ParseCCURI(targetURI)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}

				switch parsedURI.Scheme {
				case "cckv":
					err = uc.repo.DeleteRecordByKey(ctx, tx, targetURI)
					if err != nil {
						span.RecordError(err)
						return nil, err
					}
				case "ccfs":
					if parsedURI.Type != concrnt.CCFSTypeConcrnt {
						err := errors.New("unsupported ccfs type for delete record: " + parsedURI.Type)
						span.RecordError(err)
						return nil, err
					}
					err = uc.repo.DeleteRecordByDocumentID(ctx, tx, parsedURI.CDID)
					if err != nil {
						span.RecordError(err)
						return nil, err
					}
				default:
					err := errors.New("unsupported document scheme for delete record: " + parsedURI.Scheme)
					span.RecordError(err)
					return nil, err
				}

			case "association":

				parsedURI, err := concrnt.ParseCCURI(targetURI)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}

				if parsedURI.Scheme != "ccfs" || parsedURI.Type != concrnt.CCFSTypeConcrnt {
					err := errors.New("unsupported document scheme for delete association: " + targetURI)
					span.RecordError(err)
					return nil, err
				}

				err = uc.repo.DeleteAssociation(ctx, tx, parsedURI.CDID)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
			default:
				err := errors.New("unsupported document kind for delete: " + targetDoc.Kind)
				span.RecordError(err)
				return nil, err
			}
		}

		// sweep the reference records createReferenceDistributionActions left
		// on this server: their key is destination + "/" + the hash-based CDID
		// of the target's href (its cckv key for records, its ccfs URI for
		// keyless documents), so a row existing under that key is exactly
		// "this destination was distributed to and lives here" — remote and
		// never-delivered destinations fall out as not-found. The href is
		// re-derived from the signed target document itself, mirroring the
		// creation side, so it holds regardless of how the delete addressed
		// the target (cckv or ccfs)
		href := targetDoc.Key
		if targetDoc.Kind != "record" {
			if targetDoc.Associate == nil {
				err := errors.New("unsupported document kind for distribution sweep: " + targetDoc.Kind)
				span.RecordError(err)
				return nil, err
			}
			parsedAssociate, err := concrnt.ParseCCURI(*targetDoc.Associate)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			href = concrnt.CCURI{
				Scheme: "ccfs",
				Owner:  parsedAssociate.Owner,
				Type:   concrnt.CCFSTypeConcrnt,
				CDID:   documentIDFor(targetSD.Document, targetDoc.CreatedAt),
			}.String()
		}
		refSegment := cdid.MakeHash([]byte(href)).String()
		for _, dest := range distributionsFromPtr(targetDoc.Distributes) {
			refKey, err := url.JoinPath(dest, refSegment)
			if err != nil {
				slog.Error("failed to join path for distribution sweep", slog.String("destination", dest), slog.String("href", href), slog.String("error", err.Error()))
				continue
			}

			refSD, err := uc.repo.GetSignedDocument(ctx, refKey)
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			var refDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(refSD.Document), &refDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, refKey)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			err = uc.policy.Eval(
				ctx,
				policy.RequestContext{
					Requester: requester,
					Self:      refDoc,
				},
				stack,
				policyDeleteAction(refDoc),
				refKey,
			)
			if errors.Is(err, domain.ErrPermissionDenied) {
				slog.Info("reference record kept: destination policy denied the delete", slog.String("ref_key", refKey), slog.String("requester", requester.ID))
				continue
			}
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			if mode == domain.CommitModeExecute && uc.kvs != nil {
				tl, id, err := uc.repo.GetTimelineRemoval(ctx, refKey)
				if err != nil {
					span.RecordError(err) // non-fatal: the deleted item just lingers in caches
				} else if tl != "" {
					postProcesses = append(postProcesses, func(ctx context.Context) error {
						return uc.kvs.SetAdd(ctx, removedItemsKey(tl), id, removedItemsTTL)
					})
				}
			}

			err = uc.repo.DeleteRecordByKey(ctx, tx, refKey)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		}

		if mode != domain.CommitModeExecute {
			continue
		}

		if uc.kvs != nil && removedTimeline != "" {
			tl, id := removedTimeline, removedItemID
			postProcesses = append(postProcesses, func(ctx context.Context) error {
				return uc.kvs.SetAdd(ctx, removedItemsKey(tl), id, removedItemsTTL)
			})
		}

		destinations := []string{targetURI}
		if targetDoc.Distributes != nil {
			destinations = append(destinations, *targetDoc.Distributes...)
		}
		for _, dest := range destinations {
			remoteSD := concrnt.SignedDocument{
				Document: sd.Document,
				Proof:    sd.Proof,
				References: map[string]concrnt.SignedDocument{
					targetURI: *targetSD,
				},
			}
			postProcesses = append(
				postProcesses,
				func(ctx context.Context) error {
					return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
						ResolveURI: dest,
						Payload:    remoteSD,
						Local:      domain.DeliveryLocalPublish,
						Remote:     remoteKind,
						Event: &concrnt.Event{
							Type:      "deleted",
							URI:       targetURI,
							Timestamp: time.Now(),
						},
					})
				},
			)
		}

		if targetDoc.Associate != nil {
			associatedURI := *targetDoc.Associate

			var associatedSD *concrnt.SignedDocument
			if authoritative {
				associatedSD, err = uc.repo.GetSignedDocument(ctx, associatedURI)
				if err != nil {
					slog.Error("failed to fetch associated document for signal", slog.String("associated_uri", associatedURI), slog.String("error", err.Error()))
					span.RecordError(err)
					return nil, err
				}
			} else {
				ref, ok := sd.References[associatedURI]
				if !ok {
					err := errors.New("associated document not found in references for remote delete")
					slog.Error("associated document not found in references for remote delete", slog.String("associated_uri", associatedURI))
					span.RecordError(err)
					return nil, err
				}
				associatedSD = &ref
			}

			var associatedDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(associatedSD.Document), &associatedDoc)
			if err != nil {
				slog.Error("failed to unmarshal associated document for signal", slog.String("associated_uri", associatedURI), slog.String("error", err.Error()))
				span.RecordError(err)
				return nil, err
			}

			destinations := []string{associatedURI}
			if associatedDoc.Distributes != nil {
				destinations = append(destinations, *associatedDoc.Distributes...)
			}

			for _, dest := range destinations {
				remoteSD := concrnt.SignedDocument{
					Document: sd.Document,
					Proof:    sd.Proof,
					References: map[string]concrnt.SignedDocument{
						targetURI:     *targetSD,
						associatedURI: *associatedSD,
					},
				}
				postProcesses = append(postProcesses,
					func(ctx context.Context) error {
						return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
							ResolveURI: dest,
							Payload:    remoteSD,
							Local:      domain.DeliveryLocalPublish,
							Remote:     remoteKind,
							Event: &concrnt.Event{
								Type:      "unassociated",
								URI:       associatedURI,
								Timestamp: time.Now(),
							},
						})
					},
				)
			}
		}
	}

	// targets are URI-ordered, so with includeSelf the base record itself
	// leads and becomes the reported result
	return &commitApplyResult{result: &targets[0], owners: uc.localEntityOwners(ctx, requester), postProcesses: postProcesses}, nil
}

// parseRangeDeleteTarget detects the trailing-asterisk range notation on a
// delete target: "cckv://.../item*" selects item itself plus its subtree,
// "cckv://.../item/*" selects the subtree only. Matching is by path hierarchy,
// not string prefix — "item2" is never part of "item*". A target without a
// trailing '*' is a plain single-key delete (isRange=false, no validation).
func parseRangeDeleteTarget(targetURI string) (base string, includeSelf bool, isRange bool, err error) {
	switch {
	case strings.HasSuffix(targetURI, "/*"):
		base = strings.TrimSuffix(targetURI, "/*")
	case strings.HasSuffix(targetURI, "*"):
		base = strings.TrimSuffix(targetURI, "*")
		includeSelf = true
	default:
		return targetURI, false, false, nil
	}

	if strings.Contains(base, "*") {
		return "", false, true, domain.ValidationError{Field: "value", Message: "'*' is only allowed as a trailing range sentinel in a delete target"}
	}
	parsed, perr := concrnt.ParseCCURI(base)
	if perr != nil {
		return "", false, true, domain.ValidationError{Field: "value", Message: "invalid range delete target: " + perr.Error()}
	}
	if parsed.Scheme != "cckv" {
		return "", false, true, domain.ValidationError{Field: "value", Message: "range delete targets must use the cckv scheme"}
	}
	if parsed.Key == "" {
		return "", false, true, domain.ValidationError{Field: "value", Message: "range delete requires a non-empty key"}
	}
	return base, includeSelf, true, nil
}

func (uc *RecordUsecase) createRecord(ctx context.Context, tx RepositoryTx, documentID string, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CreateRecord")
	defer span.End()

	stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, parsed.Key)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	action := "record:create"
	policySelf := parsed

	existingSD, err := uc.repo.GetSignedDocument(ctx, parsed.Key)
	if err == nil {
		var existingDoc concrnt.Document[any]
		err = json.Unmarshal([]byte(existingSD.Document), &existingDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		// Accept-if-newer fast path (CIP-3 §3.4), same rule as saveEntity: a
		// document only replaces the stored one when its documentID (time-
		// prefixed, content-hashed CDID) is greater. Older-or-equal replays
		// succeed as a no-op before policy eval and key validation, so a stale
		// replay can't fail on checks that changed since — and, critically,
		// can't roll the key back or tombstone the newer stored version.
		// CreateRecord re-checks the same ordering under the RecordKey row
		// lock, so this check is only an optimization, not the authoritative
		// guard against concurrent writers.
		if documentID <= documentIDFor(existingSD.Document, existingDoc.CreatedAt) {
			return &commitApplyResult{result: &sd, noop: true}, nil
		}
		action = "record:update"
		policySelf = existingDoc
	} else if !errors.Is(err, domain.ErrNotFound) {
		span.RecordError(err)
		return nil, err
	}

	err = uc.policy.Eval(
		ctx,
		policy.RequestContext{
			Requester: requester,
			Self:      policySelf,
		},
		stack,
		action,
		parsed.Key,
	)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	parsedKey, err := concrnt.ParseCCURI(parsed.Key)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if parsedKey.Scheme != "cckv" {
		err := fmt.Errorf("invalid key: document key scheme must be cckv")
		span.RecordError(err)
		return nil, err
	}
	// '*' is reserved as the range-delete sentinel (parseRangeDeleteTarget):
	// a key containing it could never be addressed by an exact-match delete
	// unambiguously.
	if strings.Contains(parsedKey.Key, "*") {
		err := domain.ValidationError{Field: "key", Message: "record key must not contain '*'"}
		span.RecordError(err)
		return nil, err
	}
	// CIP-0 §7: a cckv key component is 1..1024 bytes. An empty key
	// (cckv://<owner>) addresses the entity itself, not a record slot.
	if parsedKey.Key == "" {
		err := domain.ValidationError{Field: "key", Message: "record key must not be empty"}
		span.RecordError(err)
		return nil, err
	}
	if len(parsedKey.Key) > domain.MaxRecordKeySize {
		err := domain.ValidationError{Field: "key", Message: fmt.Sprintf("record key exceeds the maximum size of %d bytes", domain.MaxRecordKeySize)}
		span.RecordError(err)
		return nil, err
	}

	var policies *string
	if parsed.Policy != nil {
		policyBytes, err := json.Marshal(parsed.Policy)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		policyStr := string(policyBytes)
		policies = &policyStr
	}

	distributions := []string{}
	if parsed.Distributes != nil {
		distributions = *parsed.Distributes
	}

	schema := parsed.Schema
	createdAt := parsed.CreatedAt
	var redirect *string

	if parsed.Schema == schemas.ReferenceURL {
		var refDoc concrnt.Document[schemas.Reference]
		err := json.Unmarshal([]byte(sd.Document), &refDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		redirect = &refDoc.Value.Href

		refSD, ok := sd.References[refDoc.Value.Href]
		if ok {
			var targetDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(refSD.Document), &targetDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			schema = targetDoc.Schema
			createdAt = targetDoc.CreatedAt
		} else {
			if refDoc.Value.Schema != nil {
				schema = *refDoc.Value.Schema
			}
			if refDoc.Value.CreatedAt != nil {
				createdAt = *refDoc.Value.CreatedAt
			}
		}
	}

	resultURI := parsed.Key
	applied, err := uc.repo.CreateRecord(ctx, tx, documentID, parsed.Key, parsedKey.Owner, parsed.Author, schema, parsed.OnUpdate, policies, distributions, redirect, createdAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	// A concurrent commit won the key between the fast-path check above and
	// the row lock: the stored record is newer-or-equal, so this one is the
	// same accept-if-newer no-op as the fast path.
	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	postProcesses := []PostProcessAction{}

	if mode == domain.CommitModeExecute {
		postProcesses = append(postProcesses, func(ctx context.Context) error {
			// runs post-commit, so the anonymous read evaluation sees the
			// just-stored record's own policy
			return uc.signal.Publish(ctx, resultURI, uc.markEventForAnonymous(ctx, concrnt.Event{
				Type:       "created",
				URI:        resultURI,
				References: map[string]concrnt.SignedDocument{resultURI: sd},
				Timestamp:  createdAt,
			}))
		})
	}

	if mode == domain.CommitModeExecute && parsed.Distributes != nil {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, parsed.Author, resultURI, requester, sd, *parsed.Distributes, mode)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		postProcesses = append(postProcesses, actions...)
	}

	owners, err := uc.localCommitOwners(ctx, parsedKey.Owner)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	sd.CCKV = &parsed.Key
	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  parsedKey.Owner,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) createAssociation(ctx context.Context, tx RepositoryTx, documentID string, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CreateAssociation")
	defer span.End()

	// CIP-9: an association is addressable only by its ccfs URI, never a key.
	if parsed.Key != "" {
		err := domain.ValidationError{Field: "key", Message: "association documents must not have a key"}
		span.RecordError(err)
		return nil, err
	}
	if parsed.AssociationVariant != nil && len(*parsed.AssociationVariant) > domain.MaxAssociationVariantSize {
		err := domain.ValidationError{Field: "associationVariant", Message: fmt.Sprintf("associationVariant exceeds the maximum size of %d bytes", domain.MaxAssociationVariantSize)}
		span.RecordError(err)
		return nil, err
	}

	stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, *parsed.Associate)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	err = uc.policy.Eval(
		ctx,
		policy.RequestContext{
			Requester: requester,
			Self:      parsed,
		},
		stack,
		policyCreateAction(parsed),
		*parsed.Associate,
	)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	target := *parsed.Associate
	targetURI, err := concrnt.ParseCCURI(*parsed.Associate)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if targetURI.Scheme != "cckv" {
		err := fmt.Errorf("invalid associate: document associate scheme must be cckv")
		span.RecordError(err)
		return nil, err
	}

	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  targetURI.Owner,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	isLocal, err := uc.IsLocalEntityByCCID(ctx, targetURI.Owner)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	created := false
	postProcesses := []PostProcessAction{}

	requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if isLocal {
		uniqueKey := *parsed.Associate + parsed.Author + parsed.Schema
		if parsed.AssociationVariant != nil {
			uniqueKey += *parsed.AssociationVariant
		}
		// bodyも一意性判定に含める(v1と同じ意味論)。bodyが異なるassociation
		// (同一ユーザーからの複数リプライ等)は共存できる
		bodyBytes, err := json.Marshal(parsed.Value)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		uniqueKey += string(bodyBytes)
		uniqueHash := xxh3.HashString(uniqueKey)

		inserted, err := uc.repo.CreateAssociation(ctx, tx, documentID, *parsed.Associate, targetURI.Owner, parsed.Author, parsed.Schema, parsed.AssociationVariant, fmt.Sprintf("%x", uniqueHash), parsed.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		// 重複配送(挿入なし)のときは参照配布もスキップする。さもないと
		// 別documentIDの論理重複がタイムラインに二重に載る
		created = inserted
		if created && mode == domain.CommitModeExecute {
			actions, err := uc.createReferenceDistributionActions(ctx, ip, parsed.Author, ccfs, requester, sd, distributionsFromPtr(parsed.Distributes), mode)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			postProcesses = append(postProcesses, actions...)
		}
	}

	// signal
	if mode == domain.CommitModeExecute {
		distributions := []string{target}

		targetSD, err := uc.repo.GetSignedDocument(ctx, target)
		if err != nil { // ないとき
			sd, ok := sd.References[target]
			if !ok {
				span.RecordError(err)
				return nil, errors.New("target document not found in references")
			}
			targetSD = &sd

			// Note: none-proof targets are deliberately rejected here — they
			// carry no verifiable authorship, so a committer-supplied inline
			// copy cannot be trusted.
			if err := targetSD.Verify(ctx, uc.resolver()); err != nil {
				span.RecordError(err)
				return nil, errors.Join(errors.New("target document failed signature verification"), err)
			}

			var targetDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(targetSD.Document), &targetDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			distributions = append(distributions, distributionsFromPtr(targetDoc.Distributes)...)
		} else { // あるとき
			dists, err := uc.repo.GetDistributions(ctx, target)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			distributions = append(distributions, dists...)
		}

		remoteKind := domain.DeliveryRemoteNone
		if created {
			remoteKind = domain.DeliveryRemoteCommit
		}

		for _, channel := range distributions {
			remoteSD := concrnt.SignedDocument{
				Document: sd.Document,
				Proof:    sd.Proof,
				References: map[string]concrnt.SignedDocument{
					requester.CCKV(): *requesterSD,
					target:           *targetSD,
				},
			}
			postProcesses = append(postProcesses,
				func(ctx context.Context) error {
					// only the realtime event carries visibility flags — the
					// delivery payload is the signed document remote servers
					// must verify in full, so it stays untouched
					event := uc.markEventForAnonymous(ctx, concrnt.Event{
						Type:        "associated",
						URI:         target,
						Association: &ccfs,
						Timestamp:   time.Now(),
						References: map[string]concrnt.SignedDocument{
							ccfs: sd,
						},
					})
					return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
						ResolveURI: channel,
						Payload:    remoteSD,
						Local:      domain.DeliveryLocalPublish,
						Remote:     remoteKind,
						Event:      &event,
					})
				},
			)
		}
	}

	owners := []string{}
	if isLocal {
		owners = append(owners, targetURI.Owner)
	}

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) localCommitOwners(ctx context.Context, candidates ...string) ([]string, error) {
	owners := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		if concrnt.IsCCID(candidate) {
			isLocal, err := uc.IsLocalEntityByCCID(ctx, candidate)
			if err != nil {
				return nil, err
			}
			if isLocal {
				owners = append(owners, candidate)
			}
		}
		if concrnt.IsCSID(candidate) {
			if candidate == uc.config.FQDN {
				owners = append(owners, candidate)
			}
		}
	}
	return owners, nil
}

func (uc *RecordUsecase) localEntityOwners(ctx context.Context, candidates ...domain.Entity) []string {
	owners := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.ID]; ok {
			continue
		}
		seen[candidate.ID] = struct{}{}
		if uc.IsLocalEntity(ctx, &candidate) {
			owners = append(owners, candidate.ID)
		}
	}
	return owners
}

func (uc *RecordUsecase) acknowledge(ctx context.Context, tx RepositoryTx, documentID string, ip string, requester domain.Entity, targetUser domain.Entity, doc concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Acknowledge")
	defer span.End()

	applied := true
	if uc.IsLocalEntity(ctx, &requester) || uc.IsLocalEntity(ctx, &targetUser) {
		parsedAssociate, err := concrnt.ParseCCURI(*doc.Associate)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if parsedAssociate.Scheme != "cckv" {
			err := fmt.Errorf("invalid associate: document associate scheme must be cckv")
			span.RecordError(err)
			return nil, err
		}

		applied, err = uc.repo.Acknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Schema, doc.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	// CIP-10 §4 accept-if-newer loss: the stored (from, to, schema) state
	// already carries a newer-or-equal document, so this one changes nothing —
	// no proxy delivery, no distribution, and the commit tx rolls back.
	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  targetUser.ID,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	postProcesses := []PostProcessAction{}
	if !uc.IsLocalEntity(ctx, &targetUser) && mode == domain.CommitModeExecute {

		requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		distSD := concrnt.SignedDocument{
			Document: sd.Document,
			Proof:    sd.Proof,
			References: map[string]concrnt.SignedDocument{
				requester.CCKV(): *requesterSD,
			},
		}
		postProcesses = append(postProcesses,
			func(ctx context.Context) error {
				return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
					Host:    targetUser.Domain,
					Payload: distSD,
					Local:   domain.DeliveryLocalNone,
					Remote:  domain.DeliveryRemoteCommit,
				})
			},
		)
	}

	if uc.IsLocalEntity(ctx, &requester) {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, doc.Author, ccfs, requester, sd, distributionsFromPtr(doc.Distributes), mode)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		postProcesses = append(postProcesses, actions...)
	}

	owners := []string{}
	if uc.IsLocalEntity(ctx, &requester) {
		owners = append(owners, requester.ID)
	}
	if uc.IsLocalEntity(ctx, &targetUser) && !slices.Contains(owners, targetUser.ID) {
		owners = append(owners, targetUser.ID)
	}

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) unacknowledge(ctx context.Context, tx RepositoryTx, documentID string, ip string, requester domain.Entity, targetUser domain.Entity, doc concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.UnAcknowledge")
	defer span.End()

	applied := true
	if uc.IsLocalEntity(ctx, &requester) || uc.IsLocalEntity(ctx, &targetUser) {
		parsedAssociate, err := concrnt.ParseCCURI(*doc.Associate)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if parsedAssociate.Scheme != "cckv" {
			err := fmt.Errorf("invalid associate: document associate scheme must be cckv")
			span.RecordError(err)
			return nil, err
		}

		applied, err = uc.repo.UnAcknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Schema, doc.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	// Same accept-if-newer loss handling as acknowledge: an older unack must
	// not roll a newer stored transition back, nor trigger any side effects.
	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  targetUser.ID,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	postProcesses := []PostProcessAction{}
	if !uc.IsLocalEntity(ctx, &targetUser) && mode == domain.CommitModeExecute {
		requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		distSD := concrnt.SignedDocument{
			Document: sd.Document,
			Proof:    sd.Proof,
			References: map[string]concrnt.SignedDocument{
				requester.CCKV(): *requesterSD,
			},
		}
		postProcesses = append(postProcesses,
			func(ctx context.Context) error {
				return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
					Host:    targetUser.Domain,
					Payload: distSD,
					Local:   domain.DeliveryLocalNone,
					Remote:  domain.DeliveryRemoteCommit,
				})
			},
		)
	}

	if uc.IsLocalEntity(ctx, &requester) {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, doc.Author, ccfs, requester, sd, distributionsFromPtr(doc.Distributes), mode)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		postProcesses = append(postProcesses, actions...)
	}

	owners := []string{}
	if uc.IsLocalEntity(ctx, &requester) {
		owners = append(owners, requester.ID)
	}
	if uc.IsLocalEntity(ctx, &targetUser) && !slices.Contains(owners, targetUser.ID) {
		owners = append(owners, targetUser.ID)
	}

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) GetEntity(ctx context.Context, uri string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetEntity")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		return nil, err
	}

	if len(parsed.Owner) == 0 {
		err := fmt.Errorf("invalid URI: owner is required: %s", uri)
		span.RecordError(err)
		return nil, err
	}

	if parsed.Owner[0] == '@' { // alias
		alias := parsed.Owner[1:]
		sd, err := uc.residence.GetEntityByAlias(ctx, alias)
		if err == nil {
			return sd, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			span.RecordError(err)
			return nil, err
		}

		name := "_concrnt." + alias
		txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		redirect := ""
		for _, record := range txtrecords {
			parsed, err := concrnt.ParseCCURI(record)
			if err == nil && concrnt.IsCCID(parsed.Owner) && parsed.Hint != nil {
				redirect = record
				break
			}
		}
		if redirect == "" {
			return nil, errors.New("no valid CCURI found in TXT records")
		}

		return uc.GetEntity(ctx, redirect)
	} else {
		ccid := parsed.Owner
		entity, err := uc.residence.GetEntityByCCID(ctx, ccid)
		if err == nil {
			return entity, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			span.RecordError(err)
			return nil, err
		}

		if parsed.Hint == nil || *parsed.Hint == uc.config.FQDN {
			return nil, domain.NotFoundError{Resource: uri}
		}

		hint := *parsed.Hint

		var sd concrnt.SignedDocument
		err = uc.client.GetResource(ctx, uri, "application/json", &client.Options{
			Resolver: hint,
		}, &sd)
		if err != nil {
			return nil, err
		}

		_, err = uc.Commit(ctx, hint, sd, domain.CommitModeExecute)
		if err != nil {
			return nil, err
		}

		// commit済みなので今度は成功するはず
		entity, err = uc.residence.GetEntityByCCID(ctx, ccid)
		if err != nil {
			return nil, err
		}
		return entity, nil
	}
}

func (uc *RecordUsecase) GetSigned(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetSigned")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if parsed.Scheme == "cckv" && parsed.Key == "" { // entity document
		entity, err := uc.GetEntity(ctx, uri)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		return entity.SignedDocument, nil
	} else {
		sd, err := uc.repo.GetSignedDocument(ctx, uri)
		if err != nil {
			return nil, err
		}

		var doc concrnt.Document[schemas.Reference]
		err = json.Unmarshal([]byte(sd.Document), &doc)
		if err == nil && doc.Schema == schemas.ReferenceURL {

			newLocation, err := uc.client.ResolveResourceURI(ctx, doc.Value.Href, nil)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			return nil, domain.RedirectError{
				Location: newLocation,
				Body:     sd,
			}
		}

		err = uc.checkReadAccess(ctx, uri, *sd)
		if err != nil {
			return nil, err
		}

		return sd, nil
	}
}

func (uc *RecordUsecase) checkReadAccess(ctx context.Context, uri string, sd concrnt.SignedDocument) error {
	requester, _ := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	return uc.checkReadAccessAs(ctx, uri, sd, requester)
}

func (uc *RecordUsecase) checkReadAccessAs(ctx context.Context, uri string, sd concrnt.SignedDocument, requester domain.Entity) error {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CheckReadAccess")
	defer span.End()

	if uri == "" {
		err := errors.New("uri is required for read access evaluation")
		span.RecordError(err)
		return err
	}

	var doc concrnt.Document[any]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Associations have no record_keys row of their own; their policy stack is
	// rooted at the associated document (CIP-12 §5.3), same as deleteRecord.
	policyRoot := uri
	if doc.Associate != nil {
		policyRoot = *doc.Associate
	}

	stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, policyRoot)
	if err != nil {
		span.RecordError(err)
		stack = []concrnt.Policy{}
	}

	err = uc.policy.Eval(
		ctx,
		policy.RequestContext{
			Requester: requester,
			Self:      doc,
		},
		stack,
		policyReadAction(doc),
		uri,
	)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

// markEventForAnonymous annotates every realtime-event document with the
// internal IsPublic flag: whether an anonymous requester may read it
// (CIP-11 §3.2 baseline). The full documents stay on the event so trusted
// internal consumers (NotificationReactor, modules on the redis pubsub) see
// everything; unauthenticated websocket subscribers get Event.PublicView,
// which drops the documents flagged false. Evaluated once per event here
// rather than per subscriber. Unevaluable documents are flagged not public
// (fail closed). References nested deeper than one level are removed: the
// public view would drop them anyway and no internal consumer reads them.
// The passed event's documents are copied, never mutated in place — the
// commit response and the delivery payload share the same underlying maps.
func (uc *RecordUsecase) markEventForAnonymous(ctx context.Context, event concrnt.Event) concrnt.Event {
	if len(event.References) == 0 {
		return event
	}
	marked := make(map[string]concrnt.SignedDocument, len(event.References))
	for uri, refSD := range event.References {
		public := uc.checkReadAccessAs(ctx, uri, refSD, domain.Entity{}) == nil
		refSD.IsPublic = &public
		// Nested references embed further documents (e.g. the distributed
		// original inside a timeline reference) that the loop above never
		// sees — apply the same anonymous evaluation to them.
		var nested map[string]concrnt.SignedDocument
		if len(refSD.References) > 0 {
			nested = make(map[string]concrnt.SignedDocument, len(refSD.References))
			for nestedURI, nestedSD := range refSD.References {
				nestedPublic := uc.checkReadAccessAs(ctx, nestedURI, nestedSD, domain.Entity{}) == nil
				nestedSD.IsPublic = &nestedPublic
				nestedSD.References = nil
				nested[nestedURI] = nestedSD
			}
		}
		refSD.References = nested
		marked[uri] = refSD
	}
	event.References = marked
	return event
}

func policyCreateAction(doc concrnt.Document[any]) string {
	if doc.Kind == "association" {
		return "association:create"
	}
	return "record:create"
}

func policyReadAction(doc concrnt.Document[any]) string {
	if doc.Kind == "association" {
		return "association:read"
	}
	return "record:read"
}

func policyDeleteAction(doc concrnt.Document[any]) string {
	if doc.Kind == "association" {
		return "association:delete"
	}
	return "record:delete"
}

// paginateWindow derives pagination cursors from rows fetched with limit+1:
// the peeked row past the window becomes next, the window head becomes prev.
// Cursors are computed before any read-access filtering so that clients can
// page past rows that get filtered out.
func paginateWindow(rows []QueryRow, limit int) ([]concrnt.SignedDocument, *time.Time, *time.Time) {
	var prev, next *time.Time
	if len(rows) > limit {
		next = &rows[limit].CreatedAt
		rows = rows[:limit]
	}
	if len(rows) > 0 {
		prev = &rows[0].CreatedAt
	}
	items := make([]concrnt.SignedDocument, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Row)
	}
	return items, prev, next
}

func (uc *RecordUsecase) GetAcknowledgeRecords(ctx context.Context, from, to, schema string, since, until *time.Time, limit int, order string) (concrnt.QueryResult, error) {
	rows, err := uc.repo.GetAcknowledgeRecords(ctx, from, to, schema, since, until, limit+1, order)
	if err != nil {
		return concrnt.QueryResult{}, err
	}
	items, prev, next := paginateWindow(rows, limit)
	return concrnt.QueryResult{Items: items, Prev: prev, Next: next}, nil
}

func (uc *RecordUsecase) GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error) {
	return uc.repo.GetAcknowledgeRecordCounts(ctx, from, to, schema)
}

func (uc *RecordUsecase) GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string, since, until *time.Time, limit int, order string) (concrnt.QueryResult, error) {
	rows, err := uc.repo.GetAssociatedRecords(ctx, targetURI, schema, variant, author, since, until, limit+1, order)
	if err != nil {
		return concrnt.QueryResult{}, err
	}
	items, prev, next := paginateWindow(rows, limit)
	return concrnt.QueryResult{Items: items, Prev: prev, Next: next}, nil
}

func (uc *RecordUsecase) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	return uc.repo.GetAssociatedRecordCountsBySchema(ctx, targetURI)
}

func (uc *RecordUsecase) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	return uc.repo.GetAssociatedRecordCountsByVariant(ctx, targetURI, schema)
}

func (uc *RecordUsecase) Query(
	ctx context.Context,
	prefix, parent, schema, author string,
	since, until *time.Time,
	limit int,
	order string,
) (concrnt.QueryResult, error) {
	var (
		rows []QueryRow
		err  error
	)

	if prefix != "" && parent != "" {
		return concrnt.QueryResult{}, errors.New("prefix and parent cannot be specified at the same time")
	}

	if prefix != "" {
		rows, err = uc.repo.QueryByPrefix(ctx, prefix, schema, author, since, until, limit+1, order)
	} else if parent != "" {
		rows, err = uc.repo.QueryByParent(ctx, parent, schema, author, since, until, limit+1, order)
	} else {
		return concrnt.QueryResult{}, errors.New("either prefix or parent must be specified")
	}

	if err != nil {
		return concrnt.QueryResult{}, err
	}

	items, prev, next := paginateWindow(rows, limit)

	filtered := make([]concrnt.SignedDocument, 0, len(items))
	for _, sd := range items {
		if sd.CCKV == nil {
			return concrnt.QueryResult{}, errors.New("queried record has no cckv")
		}

		err := uc.checkReadAccess(ctx, *sd.CCKV, sd)
		if err != nil {
			if errors.Is(err, domain.ErrPermissionDenied) {
				continue
			}
			return concrnt.QueryResult{}, err
		}

		filtered = append(filtered, sd)
	}

	return concrnt.QueryResult{Items: filtered, Prev: prev, Next: next}, nil
}

func (uc *RecordUsecase) DumpCommitLogs(ctx context.Context) (string, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetCommitlog")
	defer span.End()

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		err := errors.New("requester not found in context")
		span.RecordError(err)
		return "", err
	}

	commitLogs, err := uc.repo.GetAllCommitLogs(ctx, requester.ID)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	var result string
	for _, log := range commitLogs {
		line, err := json.Marshal(log)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		result += string(line) + "\n"
	}

	return result, nil
}

func (uc *RecordUsecase) IsLocalEntity(ctx context.Context, entity *domain.Entity) bool {
	return uc.config.FQDN == entity.Domain
}

func (uc *RecordUsecase) IsLocalEntityByCCID(ctx context.Context, entityID string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.IsLocalEntity")
	defer span.End()

	if concrnt.IsCCID(entityID) {
		entityID = concrnt.CCURI{
			Scheme: "cckv",
			Owner:  entityID,
		}.String()
	}

	entity, err := uc.GetEntity(ctx, entityID)
	if err != nil {
		span.RecordError(err)
		return false, err
	}

	return uc.IsLocalEntity(ctx, entity), nil
}

type ImportResult struct {
	Document string `json:"document,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (uc *RecordUsecase) ImportCommitLogs(ctx context.Context, ip string, jsonl string) []ImportResult {
	ctx, span := tracer.Start(ctx, "Usecase.Record.ImportCommitLogs")
	defer span.End()

	results := []ImportResult{}

	lines := strings.SplitSeq(jsonl, "\n")
	for line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var sd concrnt.SignedDocument
		err := json.Unmarshal([]byte(line), &sd)
		if err != nil {
			span.RecordError(err)
			result := ImportResult{
				Document: line,
				Error:    fmt.Sprintf("failed to parse line as SignedDocument: %v", err),
			}
			results = append(results, result)
			continue
		}

		_, err = uc.Commit(ctx, ip, sd, domain.CommitModeLocalOnlyExecute)
		if err != nil {
			span.RecordError(err)
			result := ImportResult{
				Document: line,
				Error:    fmt.Sprintf("failed to commit document: %v", err),
			}
			results = append(results, result)
			continue
		}
	}

	return results
}

func (uc *RecordUsecase) getBlockingUsers(ctx context.Context, userID string) ([]string, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetBlockingUsers")
	defer span.End()

	if cached, found := uc.cache.Get("blockingUsers:" + userID); found {
		if ids, ok := cached.([]string); ok {
			return ids, nil
		}
	}

	blockingKey := concrnt.ComposeCCURI("cckv", userID, ".concrnt/blocking")

	blockingUsers, err := uc.repo.QueryByParent(ctx, blockingKey, "", "", nil, nil, 0, "")
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	prefix := blockingKey + "/"

	ids := make([]string, len(blockingUsers))
	for i, row := range blockingUsers {
		id := strings.TrimPrefix(*row.Row.CCKV, prefix)
		ids[i] = id
	}

	uc.cache.Set("blockingUsers:"+userID, ids, cache.DefaultExpiration)

	return ids, nil
}
