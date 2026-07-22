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
	CreateEntity(ctx context.Context, tx RepositoryTx, ccid string, alias *string, domain string, documentID string) error
	CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, schema string, onUpdate *string, policies *string, distributions []string, redirect *string, createdAt time.Time) error
	CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) (bool, error)
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

	GetAcknowledgeRecords(ctx context.Context, from, to, schema string) ([]concrnt.SignedDocument, error)
	GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error)
	GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string) ([]concrnt.SignedDocument, error)
	GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error)
	GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error)

	QueryByPrefix(ctx context.Context, prefix, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error)
	QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error)
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

// KVS is a general-purpose key-value store (set-with-TTL, existence check,
// TTL'd string sets), used here to hold deletion tombstones and per-timeline
// removed-item advertisements. Implemented by internal/infra/kvs.Redis; nil in
// offline tooling/tests.
type KVS interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)
	SetAdd(ctx context.Context, key string, value string, ttl time.Duration) error
	SetMembers(ctx context.Context, key string) ([]string, error)
}

type commitApplyResult struct {
	result        *concrnt.SignedDocument
	owners        []string
	postProcesses []PostProcessAction
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

// tombstoneKey namespaces a deleted-document marker in the KVS.
func tombstoneKey(uri string) string { return "tombstone:" + uri }

// markKeyDeleted tombstones a deleted (or overwritten) document's ccfs URI,
// so a captured copy of that exact document can't be replayed back in. Keying
// by content id rather than cckv key leaves the key itself reusable for fresh
// documents. The tombstone must outlive the backdate window measured from the
// later of the processing time and the affected document's createdAt (CIP-3
// §3.4): a document stamped near the future-skew limit stays inside the
// backdate window after a now-based tombstone would have expired.
func (uc *RecordUsecase) markKeyDeleted(ctx context.Context, uri string, createdAt time.Time) error {
	if uc.kvs == nil {
		return nil
	}
	ttl := domain.MaxBackdate
	if d := time.Until(createdAt); d > 0 {
		ttl += d
	}
	return uc.kvs.Set(ctx, tombstoneKey(uri), "1", ttl)
}

// deleteTargetOwner extracts the owner of a delete command's target URI, with
// the range sentinel ('*' / '/*') stripped. The delete command's own ccfs
// tombstone is keyed under this owner; the replay guard in Commit and the
// tombstone write in deleteRecord must derive it identically. Returns "" for
// unparseable targets (rejected downstream anyway).
func deleteTargetOwner(rawTarget string) string {
	base := strings.TrimSuffix(rawTarget, "*")
	base = strings.TrimSuffix(base, "/")
	parsed, err := concrnt.ParseCCURI(base)
	if err != nil {
		return ""
	}
	return parsed.Owner
}

// isKeyDeleted reports whether a document URI is currently tombstoned.
func (uc *RecordUsecase) isKeyDeleted(ctx context.Context, uri string) (bool, error) {
	if uc.kvs == nil {
		return false, nil
	}
	return uc.kvs.Exists(ctx, tombstoneKey(uri))
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
	// and the replay guards below. Every other committer is verified:
	//   - document-reference proofs verify against the recursively-verified
	//     inline copy in References (the author vouching for their own
	//     document), so commits stay valid even when the referenced document's
	//     origin server is unreachable (e.g. mid-migration imports);
	//   - subkey enact documents are always fetched from their authoritative
	//     server inside Verify, so revoked subkeys can't be replayed inline.
	if !isServiceAccount {
		if err := sd.Verify(ctx, uc.resolver()); err != nil {
			span.RecordError(err)
			if errors.Is(err, concrnt.ErrNoneProofNotAllowed) {
				slog.Error("Unauthorized commit with none proof", "error", err.Error())
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "none proof type is only allowed for system service accounts"}, err)
			}
			if errors.Is(err, concrnt.ErrUnsupportedProofType) {
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "unsupported proof type: " + sd.Proof.Type}, err)
			}
			return nil, errors.Join(domain.ValidationError{Field: "proof", Message: "signature verification failed"}, err)
		}

		// Self-service migration: an authenticated user importing their own
		// repository dump (LocalOnlyExecute never re-federates) may replay
		// historical documents past the backdate window — their own, and
		// documents by others that target their content (e.g. inbound
		// associations carried over in the dump). Signature verification and
		// the deleted-document tombstone below still apply.
		backdateExempt := false
		if mode == domain.CommitModeLocalOnlyExecute {
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

		// Replay guards: reject documents older than the backdate window, and
		// reject re-committing a document that was explicitly deleted (or
		// superseded by an overwrite) within it. Together they make a deletion
		// permanent against replay — a captured document is either still
		// tombstoned or already too old to accept. The tombstone is keyed by
		// the document's ccfs URI (content id), not its cckv key, so a deleted
		// key stays reusable: only the exact deleted document is rejected,
		// while a fresh document at the same key has a different CDID and
		// commits normally.
		if !backdateExempt && doc.CreatedAt.Before(time.Now().Add(-domain.MaxBackdate)) {
			err := domain.ValidationError{Field: "createdAt", Message: "createdAt is older than the allowed backdate window"}
			span.RecordError(err)
			return nil, err
		}
		// The ccfs owner must be derived exactly as at creation time: records
		// use the key's owner (createRecord), associations the associate's
		// owner (createAssociation), delete commands the target's owner
		// (deleteRecord tombstones the delete document itself under it — the
		// target tombstone alone doesn't identify the command, so a captured
		// delete of a reusable key could otherwise be replayed against a newer
		// document at that key). Unparseable keys skip the check — they are
		// rejected downstream anyway.
		ccfsOwner := ""
		if doc.Key != "" {
			if parsed, err := concrnt.ParseCCURI(doc.Key); err == nil {
				ccfsOwner = parsed.Owner
			}
		} else if doc.Associate != nil {
			if parsed, err := concrnt.ParseCCURI(*doc.Associate); err == nil {
				ccfsOwner = parsed.Owner
			}
		} else if doc.Kind == "delete" {
			if target, ok := doc.Value.(string); ok {
				ccfsOwner = deleteTargetOwner(target)
			}
		}
		if ccfsOwner != "" {
			ccfs := concrnt.CCURI{
				Scheme: "ccfs",
				Owner:  ccfsOwner,
				Type:   concrnt.CCFSTypeConcrnt,
				CDID:   documentID,
			}.String()
			deleted, err := uc.isKeyDeleted(ctx, ccfs)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			if deleted {
				err := domain.ValidationError{Field: "document", Message: "cannot re-commit an explicitly deleted document"}
				span.RecordError(err)
				return nil, err
			}
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

	// CIP-3 §3.1: commits whose target this server does not manage are
	// rejected with 421. System service accounts (migration/import) replay
	// foreign-authored history and skip the check like the other guards;
	// CommitModeCacheRemoteEntity is GetEntity's internal re-entry for caching
	// remote entity documents, which are foreign by definition.
	if !isServiceAccount && mode != domain.CommitModeCacheRemoteEntity {
		if err := uc.checkCommitAuthority(ctx, doc, sd); err != nil {
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
		// CIP-3 §3.1: an ack is only this server's to process when it manages
		// either party. A both-remote ack was previously relayed without being
		// stored; the spec now requires a 421 instead.
		if !isServiceAccount && !uc.IsLocalEntity(ctx, requester) && !uc.IsLocalEntity(ctx, targetUser) {
			err := domain.MisdirectedError{Target: targetUserID}
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
		if !isServiceAccount && !uc.IsLocalEntity(ctx, requester) && !uc.IsLocalEntity(ctx, targetUser) {
			err := domain.MisdirectedError{Target: targetUserID}
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

// isAuthoritativeOwner reports whether the owner part of uri belongs to this
// server (CIP-0 name resolution): CCIDs resolve via the entity's domain,
// CSIDs compare against the server's own CSID, and literal-host owners
// against the FQDN.
func (uc *RecordUsecase) isAuthoritativeOwner(ctx context.Context, uri string) (bool, error) {
	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		return false, err
	}
	switch {
	case concrnt.IsCCID(parsed.Owner):
		entity, err := uc.GetEntity(ctx, concrnt.CCURI{Scheme: "cckv", Owner: parsed.Owner, Hint: parsed.Hint}.String())
		if err != nil {
			return false, err
		}
		return uc.IsLocalEntity(ctx, entity), nil
	case concrnt.IsCSID(parsed.Owner):
		return parsed.Owner == uc.config.CSID, nil
	default:
		return parsed.Owner == uc.config.FQDN, nil
	}
}

// checkCommitAuthority rejects commits whose target this server does not
// manage with a MisdirectedError (CIP-3 §3.1) — including targets whose owner
// cannot be resolved at all, since this server demonstrably isn't their home.
// Delete commits are gated inside deleteRecord instead: their authority
// depends on the verified distributes of the inlined target (CIP-4 §6.1).
// Ack/unack are gated in their dispatch arms, where both parties' entities
// are already resolved. Distribution Reference proxy-commits (CIP-7) need no
// exception: their key owner is the local destination owner.
func (uc *RecordUsecase) checkCommitAuthority(ctx context.Context, doc concrnt.Document[any], sd concrnt.SignedDocument) error {
	switch doc.Kind {
	case "record":
		if doc.Key == "" {
			return nil // rejected downstream
		}
		local, err := uc.isAuthoritativeOwner(ctx, doc.Key)
		if err != nil {
			return errors.Join(domain.MisdirectedError{Target: doc.Key}, err)
		}
		if !local {
			return domain.MisdirectedError{Target: doc.Key}
		}
	case "association":
		if doc.Associate == nil {
			return nil // rejected downstream
		}
		local, err := uc.isAuthoritativeOwner(ctx, *doc.Associate)
		if err == nil && local {
			return nil
		}
		// Implementation exception beyond CIP-3 §3.1's list: association
		// fan-out (createAssociation) delivers the association to the target's
		// distribution channels as a commit whose associate owner is foreign.
		// Accept it when the inlined target's distributes name a channel
		// managed here — mirroring the delete-propagation rule (CIP-4 §6.1);
		// the inlined target is verified in createAssociation before use.
		if targetSD, ok := sd.References[*doc.Associate]; ok {
			var targetDoc concrnt.Document[any]
			if json.Unmarshal([]byte(targetSD.Document), &targetDoc) == nil {
				for _, dest := range distributionsFromPtr(targetDoc.Distributes) {
					if destLocal, err := uc.isAuthoritativeOwner(ctx, dest); err == nil && destLocal {
						return nil
					}
				}
			}
		}
		return domain.MisdirectedError{Target: *doc.Associate}
	case "entity":
		var entity concrnt.Document[schemas.Entity]
		if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
			return err
		}
		if entity.Value.Domain != uc.config.FQDN {
			return domain.MisdirectedError{Target: entity.Value.Domain}
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
			return &commitApplyResult{result: &sd, owners: []string{entity.Author}}, nil
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

	err = uc.repo.CreateEntity(ctx, tx, entity.Author, entity.Value.Alias, entity.Value.Domain, documentID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &commitApplyResult{result: &sd, owners: []string{entity.Author}}, nil
}

func distributionsFromPtr(distributions *[]string) []string {
	if distributions == nil {
		return nil
	}
	return *distributions
}

func (uc *RecordUsecase) createReferenceDistributionActions(ctx context.Context, ip string, documentID string, author string, href string, requester domain.Entity, sd concrnt.SignedDocument, destinations []string, mode domain.CommitMode) ([]PostProcessAction, error) {
	if mode != domain.CommitModeExecute || len(destinations) == 0 {
		return nil, nil
	}

	requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
	if err != nil {
		return nil, err
	}

	postProcesses := make([]PostProcessAction, 0, len(destinations))
	for _, destURI := range destinations {
		key, err := url.JoinPath(destURI, documentID)
		if err != nil {
			slog.Error("failed to join path for distribution", slog.String("destination", destURI), slog.String("document_id", documentID), slog.String("error", err.Error()))
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
// document. The delete policy is evaluated on every target before anything is
// removed, and any failure — including a single policy denial — makes the
// commit transaction roll back in full, commitlog included: deletion is
// all-or-nothing.
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

	if targetHost == uc.config.FQDN {

		// enumerate the targets: the subtree for a range, the single
		// addressed document otherwise
		var targets []concrnt.SignedDocument
		var targetURIs []string // per-target address (cckv/ccfs URI) deletion and policy key on
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

		// evaluate the delete policy on every target before removing anything
		targetDocs := make([]concrnt.Document[any], len(targets))
		for i, target := range targets {
			err = json.Unmarshal([]byte(target.Document), &targetDocs[i])
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

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

		// Tombstone expiry origin (CIP-3 §3.4): the later of the processing
		// time and the createdAt of any involved document — for a range delete,
		// the latest across every target and the delete command itself.
		latestCreatedAt := deletedoc.CreatedAt
		for i := range targetDocs {
			if targetDocs[i].CreatedAt.After(latestCreatedAt) {
				latestCreatedAt = targetDocs[i].CreatedAt
			}
		}

		postProcesses := []PostProcessAction{}
		for i := range targets {
			targetSD := &targets[i]
			targetDoc := targetDocs[i]
			targetURI := targetURIs[i]

			var removedTimeline, removedItemID string

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

			if mode != domain.CommitModeExecute {
				continue
			}

			// Tombstone the deleted document's ccfs URI (content id) so a
			// captured copy of it can't be replayed back in during the
			// backdate window (see the replay guard in Commit), while its
			// cckv key stays reusable for fresh documents. GetSignedDocument
			// and QueryRecordSubtree compose CCFS for cckv lookups too, so
			// this covers cckv- and ccfs-addressed record deletes and
			// association deletes alike. Appended as a post-process so it
			// only runs once the delete has actually committed.
			if uc.kvs != nil {
				tombstoneURI := targetURI
				if targetSD.CCFS != nil {
					tombstoneURI = *targetSD.CCFS
				}
				postProcesses = append(postProcesses, func(ctx context.Context) error {
					return uc.markKeyDeleted(ctx, tombstoneURI, latestCreatedAt)
				})
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
							Remote:     domain.DeliveryRemoteCommit,
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
				associatedSD, err := uc.repo.GetSignedDocument(ctx, associatedURI)
				if err != nil {
					slog.Error("failed to fetch associated document for signal", slog.String("associated_uri", associatedURI), slog.String("error", err.Error()))
					span.RecordError(err)
					return nil, err
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
								Remote:     domain.DeliveryRemoteCommit,
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

		// Tombstone the delete command itself, keyed exactly as the replay
		// guard in Commit derives it: the target tombstones don't identify the
		// command, so a captured delete whose value is a reusable cckv key (or
		// range) could otherwise be replayed to remove a newer document created
		// there later. As a post-process this is only recorded for deletes that
		// actually committed — failed or denied deletes roll back before it.
		if mode == domain.CommitModeExecute && uc.kvs != nil {
			if owner := deleteTargetOwner(rawTarget); owner != "" {
				selfCCFS := concrnt.CCURI{
					Scheme: "ccfs",
					Owner:  owner,
					Type:   concrnt.CCFSTypeConcrnt,
					CDID:   documentIDFor(sd.Document, deletedoc.CreatedAt),
				}.String()
				postProcesses = append(postProcesses, func(ctx context.Context) error {
					return uc.markKeyDeleted(ctx, selfCCFS, latestCreatedAt)
				})
			}
		}

		// targets are URI-ordered, so with includeSelf the base record itself
		// leads and becomes the reported result
		return &commitApplyResult{result: &targets[0], owners: uc.localEntityOwners(ctx, requester), postProcesses: postProcesses}, nil

	} else { // delete propagated from the target's authoritative server (CIP-4 §6.1)

		// recover the concrete targets from References: for a range the
		// origin server enqueues one delivery job per deleted target, each
		// carrying that target in References, so matching References against
		// the range is how this server learns the target list
		var refURIs []string
		if isRange {
			for refURI := range sd.References {
				if (includeSelf && refURI == rangeBase) || strings.HasPrefix(refURI, rangeBase+"/") {
					refURIs = append(refURIs, refURI)
				}
			}
			if len(refURIs) == 0 {
				err := errors.New("no reference matched range delete target")
				span.RecordError(err)
				return nil, err
			}
			slices.Sort(refURIs)
		} else {
			if _, ok := sd.References[rawTarget]; !ok {
				err := errors.New("target document not found in references for remote delete")
				span.RecordError(err)
				return nil, err
			}
			refURIs = []string{rawTarget}
		}

		// A propagated delete may only remove the auto-generated distribution
		// References (CIP-7 §4.1) this server holds for the deleted target —
		// never arbitrary local documents — and only after the inlined target
		// verifies. Any confirmation failure rejects the whole commit.
		hasLocalDestination := false
		ownerCandidates := []string{}
		postProcesses := []PostProcessAction{}
		for _, targetURI := range refURIs {
			targetSD := sd.References[targetURI]

			// CIP-4 §6.1 (1): the inlined copy must be structurally and
			// cryptographically valid on its own
			if err := targetSD.Verify(ctx, uc.resolver()); err != nil {
				span.RecordError(err)
				return nil, errors.Join(domain.ValidationError{Field: "references", Message: "inlined delete target failed verification"}, err)
			}

			var targetDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(targetSD.Document), &targetDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			// derive the target's identities the same way commit does: cckv
			// from its key, ccfs from the namespace owner + content CDID
			targetCDID := documentIDFor(targetSD.Document, targetDoc.CreatedAt)
			ccfsOwner := targetDoc.Author
			if targetDoc.Key != "" {
				parsedKey, err := concrnt.ParseCCURI(targetDoc.Key)
				if err != nil {
					span.RecordError(err)
					return nil, domain.ValidationError{Field: "references", Message: "inlined delete target has an unparseable key"}
				}
				ccfsOwner = parsedKey.Owner
			} else if targetDoc.Associate != nil {
				parsedAssoc, err := concrnt.ParseCCURI(*targetDoc.Associate)
				if err != nil {
					span.RecordError(err)
					return nil, domain.ValidationError{Field: "references", Message: "inlined delete target has an unparseable associate"}
				}
				ccfsOwner = parsedAssoc.Owner
			}
			targetCCFS := concrnt.CCURI{
				Scheme: "ccfs",
				Owner:  ccfsOwner,
				Type:   concrnt.CCFSTypeConcrnt,
				CDID:   targetCDID,
			}.String()

			// CIP-4 §6.1 (1): the derived identity must match the References
			// key URI — an unrelated document can't be smuggled in under it
			if targetURI != targetDoc.Key && targetURI != targetCCFS {
				err := domain.ValidationError{Field: "references", Message: "inlined delete target identity does not match its reference URI"}
				span.RecordError(err)
				return nil, err
			}

			// CIP-4 §6.1 (2): only the target's author may delete it this way
			if targetDoc.Author != deletedoc.Author {
				err := domain.PermissionError{Reason: "delete author does not match the target document author"}
				span.RecordError(err)
				return nil, err
			}

			// CIP-4 §6.1 (3): only destinations named in the target's signed
			// distributes — and managed by this server — may be touched
			for _, dest := range distributionsFromPtr(targetDoc.Distributes) {
				destHost, err := uc.client.ResolveResourceHost(ctx, dest)
				if err != nil {
					span.RecordError(err)
					continue
				}
				if destHost != uc.config.FQDN {
					continue
				}
				hasLocalDestination = true
				if parsedDest, err := concrnt.ParseCCURI(dest); err == nil {
					ownerCandidates = append(ownerCandidates, parsedDest.Owner)
				}

				// CIP-4 §6.1 (4): the deletable row is exactly the
				// auto-generated Reference <dest>/<target CDID> whose href
				// points at the target. An already-absent row is an idempotent
				// redelivery, not a failure.
				refKey, err := url.JoinPath(dest, targetCDID)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
				refSD, err := uc.repo.GetSignedDocument(ctx, refKey)
				if errors.Is(err, domain.ErrNotFound) {
					continue
				}
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
				var refDoc concrnt.Document[schemas.Reference]
				err = json.Unmarshal([]byte(refSD.Document), &refDoc)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
				if refDoc.Schema != schemas.ReferenceURL ||
					(refDoc.Value.Href != targetURI && refDoc.Value.Href != targetDoc.Key && refDoc.Value.Href != targetCCFS) {
					err := domain.ValidationError{Field: "value", Message: "stored document is not a distribution reference for the delete target"}
					span.RecordError(err)
					return nil, err
				}

				// CIP-4 §6.1 (5): deleting the Reference must pass policy
				var refDocAny concrnt.Document[any]
				err = json.Unmarshal([]byte(refSD.Document), &refDocAny)
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
						Self:      refDocAny,
					},
					stack,
					policyDeleteAction(refDocAny),
					refKey,
				)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}

				// capture the chunkline membership before the delete cascades
				// the record_keys row away, then actually remove the row
				var removedTimeline, removedItemID string
				if mode == domain.CommitModeExecute && uc.kvs != nil {
					tl, id, err := uc.repo.GetTimelineRemoval(ctx, refKey)
					if err != nil {
						span.RecordError(err) // non-fatal: the deleted item just lingers in caches
					} else {
						removedTimeline, removedItemID = tl, id
					}
				}
				if err := uc.repo.DeleteRecordByKey(ctx, tx, refKey); err != nil {
					span.RecordError(err)
					return nil, err
				}

				if mode != domain.CommitModeExecute {
					continue
				}

				// tombstone the deleted Reference (this server is its
				// authoritative holder), TTL anchored per CIP-3 §3.4
				latestCreatedAt := deletedoc.CreatedAt
				for _, ts := range []time.Time{targetDoc.CreatedAt, refDoc.CreatedAt} {
					if ts.After(latestCreatedAt) {
						latestCreatedAt = ts
					}
				}
				if uc.kvs != nil {
					tombstoneURI := refKey
					if refSD.CCFS != nil {
						tombstoneURI = *refSD.CCFS
					}
					anchor := latestCreatedAt
					postProcesses = append(postProcesses, func(ctx context.Context) error {
						return uc.markKeyDeleted(ctx, tombstoneURI, anchor)
					})
				}
				if uc.kvs != nil && removedTimeline != "" {
					tl, id := removedTimeline, removedItemID
					postProcesses = append(postProcesses, func(ctx context.Context) error {
						return uc.kvs.SetAdd(ctx, removedItemsKey(tl), id, removedItemsTTL)
					})
				}
			}

			if mode != domain.CommitModeExecute {
				continue
			}

			destinations := []string{targetURI}
			if targetDoc.Distributes != nil {
				destinations = append(destinations, *targetDoc.Distributes...)
			}

			for _, dest := range destinations {
				postProcesses = append(postProcesses,
					func(ctx context.Context) error {
						return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
							ResolveURI: dest,
							Local:      domain.DeliveryLocalPublish,
							Remote:     domain.DeliveryRemoteNone,
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
				associatedSD, ok := sd.References[associatedURI]
				if !ok {
					slog.Error("associated document not found in references for remote delete", slog.String("associated_uri", associatedURI))
					span.RecordError(errors.New("associated document not found in references for remote delete"))
					return nil, errors.New("associated document not found in references for remote delete")
				}

				var associatedDoc concrnt.Document[any]
				err = json.Unmarshal([]byte(associatedSD.Document), &associatedDoc)
				if err != nil {
					slog.Error("failed to unmarshal associated document for signal", slog.String("associated_uri", associatedURI), slog.String("error", err.Error()))
					span.RecordError(err)
					return nil, err
				}

				destinations := []string{}
				if associatedDoc.Distributes != nil {
					destinations = append(destinations, *associatedDoc.Distributes...)
				}
				destinations = append(destinations, associatedURI)

				for _, dest := range destinations {
					postProcesses = append(postProcesses,
						func(ctx context.Context) error {
							return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
								ResolveURI: dest,
								Local:      domain.DeliveryLocalPublish,
								Remote:     domain.DeliveryRemoteNone,
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

		// CIP-3 §3.1: a propagated delete is only this server's to process
		// when the verified distributes name a destination managed here
		if !hasLocalDestination {
			err := domain.MisdirectedError{Target: rawTarget}
			span.RecordError(err)
			return nil, err
		}

		owners, err := uc.localCommitOwners(ctx, ownerCandidates...)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		for _, owner := range uc.localEntityOwners(ctx, requester) {
			if !slices.Contains(owners, owner) {
				owners = append(owners, owner)
			}
		}

		result := sd.References[refURIs[0]]
		return &commitApplyResult{result: &result, owners: owners, postProcesses: postProcesses}, nil
	}
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

	var existingDoc concrnt.Document[any]
	existingSD, err := uc.repo.GetSignedDocument(ctx, parsed.Key)
	if err == nil {
		err = json.Unmarshal([]byte(existingSD.Document), &existingDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		action = "record:update"
		policySelf = existingDoc
	} else if !errors.Is(err, domain.ErrNotFound) {
		span.RecordError(err)
		return nil, err
	}

	// Accept-if-newer (CIP-3 §3.4), same as saveEntity: a key overwrite only
	// happens when the incoming documentID (time-prefixed sortable CDID) is
	// greater than the stored one. Older-or-equal replays succeed as a no-op —
	// no storage, no tombstone, no delivery — so a captured old version can't
	// roll the key back and tombstone the live document. CreateRecord re-checks
	// the same ordering under a row lock as the authoritative guard.
	if existingSD != nil && documentID <= documentIDFor(existingSD.Document, existingDoc.CreatedAt) {
		parsedKey, err := concrnt.ParseCCURI(parsed.Key)
		if err != nil {
			span.RecordError(err)
			return nil, err
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
		return &commitApplyResult{result: &sd, owners: owners}, nil
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
	err = uc.repo.CreateRecord(ctx, tx, documentID, parsed.Key, parsedKey.Owner, schema, parsed.OnUpdate, policies, distributions, redirect, createdAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	postProcesses := []PostProcessAction{}

	// Overwrite: tombstone the superseded version's ccfs URI so a captured
	// copy of it can't be replayed back in (rollback) during the backdate
	// window — this is what keeps a later delete of this key permanent even
	// against older versions. Skipped when the incoming document is the
	// stored one itself: idempotent redelivery must not tombstone the live
	// version. Appended as a post-process so it only runs once the overwrite
	// has actually committed.
	if mode == domain.CommitModeExecute && uc.kvs != nil && existingSD != nil && existingSD.CCFS != nil {
		oldCCFS := *existingSD.CCFS
		oldCreatedAt := existingDoc.CreatedAt
		if parsedOld, err := concrnt.ParseCCURI(oldCCFS); err == nil && parsedOld.CDID != documentID {
			postProcesses = append(postProcesses, func(ctx context.Context) error {
				return uc.markKeyDeleted(ctx, oldCCFS, oldCreatedAt)
			})
		}
	}

	if mode == domain.CommitModeExecute {
		postProcesses = append(postProcesses, func(ctx context.Context) error {
			// runs post-commit, so the anonymous read evaluation sees the
			// just-stored record's own policy
			return uc.signal.Publish(ctx, resultURI, uc.redactEventForAnonymous(ctx, concrnt.Event{
				Type:       "created",
				URI:        resultURI,
				References: map[string]concrnt.SignedDocument{resultURI: sd},
				Timestamp:  createdAt,
			}))
		})
	}

	if mode == domain.CommitModeExecute && parsed.Distributes != nil {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, documentID, parsed.Author, resultURI, requester, sd, *parsed.Distributes, mode)
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
			actions, err := uc.createReferenceDistributionActions(ctx, ip, documentID, parsed.Author, ccfs, requester, sd, distributionsFromPtr(parsed.Distributes), mode)
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
					// only the realtime event is redacted for anonymous
					// readers — the delivery payload is the signed document
					// remote servers must verify in full
					event := uc.redactEventForAnonymous(ctx, concrnt.Event{
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

	// CIP-10 §4: only a newer transition (by CDID order) may change the stored
	// state. When the upsert reports no change — a replayed older or identical
	// ack — the commit is a no-op success and must not proxy-deliver or
	// distribute either.
	updated := true
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

		updated, err = uc.repo.Acknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Schema, doc.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  targetUser.ID,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	postProcesses := []PostProcessAction{}
	if updated && !uc.IsLocalEntity(ctx, &targetUser) && mode == domain.CommitModeExecute {

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

	if updated && uc.IsLocalEntity(ctx, &requester) {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, documentID, doc.Author, ccfs, requester, sd, distributionsFromPtr(doc.Distributes), mode)
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

	// see acknowledge: older-or-equal transitions are side-effect-free no-ops
	updated := true
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

		updated, err = uc.repo.UnAcknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Schema, doc.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  targetUser.ID,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	postProcesses := []PostProcessAction{}
	if updated && !uc.IsLocalEntity(ctx, &targetUser) && mode == domain.CommitModeExecute {
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

	if updated && uc.IsLocalEntity(ctx, &requester) {
		actions, err := uc.createReferenceDistributionActions(ctx, ip, documentID, doc.Author, ccfs, requester, sd, distributionsFromPtr(doc.Distributes), mode)
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

		// CommitModeCacheRemoteEntity: this caches a *remote* entity document,
		// which the CIP-3 §3.1 authority check would otherwise 421
		_, err = uc.Commit(ctx, hint, sd, domain.CommitModeCacheRemoteEntity)
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

// redactEventForAnonymous strips realtime-event documents an anonymous
// requester may not read (CIP-11 §3.2): subscriptions carry no authentication,
// so this guest-baseline policy evaluation at publish time is the enforcement
// mechanism. Evaluated once per event here rather than per subscriber; the
// event itself (type/uri/timestamp) is always delivered. Unevaluable
// documents are redacted (fail closed).
func (uc *RecordUsecase) redactEventForAnonymous(ctx context.Context, event concrnt.Event) concrnt.Event {
	if len(event.References) == 0 {
		return event
	}
	readable := make(map[string]concrnt.SignedDocument, len(event.References))
	for uri, refSD := range event.References {
		if err := uc.checkReadAccessAs(ctx, uri, refSD, domain.Entity{}); err != nil {
			continue
		}
		readable[uri] = refSD
	}
	event.References = readable
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

func (uc *RecordUsecase) GetAcknowledgeRecords(ctx context.Context, from, to, schema string) ([]concrnt.SignedDocument, error) {
	return uc.repo.GetAcknowledgeRecords(ctx, from, to, schema)
}

func (uc *RecordUsecase) GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error) {
	return uc.repo.GetAcknowledgeRecordCounts(ctx, from, to, schema)
}

func (uc *RecordUsecase) GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string) ([]concrnt.SignedDocument, error) {
	return uc.repo.GetAssociatedRecords(ctx, targetURI, schema, variant, author)
}

func (uc *RecordUsecase) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	return uc.repo.GetAssociatedRecordCountsBySchema(ctx, targetURI)
}

func (uc *RecordUsecase) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	return uc.repo.GetAssociatedRecordCountsByVariant(ctx, targetURI, schema)
}

func (uc *RecordUsecase) Query(
	ctx context.Context,
	prefix, parent, schema string,
	since, until *time.Time,
	limit int,
	order string,
) ([]concrnt.SignedDocument, error) {
	var (
		results []concrnt.SignedDocument
		err     error
	)

	if prefix != "" && parent != "" {
		return nil, errors.New("prefix and parent cannot be specified at the same time")
	}

	if prefix != "" {
		results, err = uc.repo.QueryByPrefix(ctx, prefix, schema, since, until, limit, order)
	} else if parent != "" {
		results, err = uc.repo.QueryByParent(ctx, parent, schema, since, until, limit, order)
	} else {
		return nil, errors.New("either prefix or parent must be specified")
	}

	if err != nil {
		return nil, err
	}

	filtered := make([]concrnt.SignedDocument, 0, len(results))
	for _, sd := range results {
		if sd.CCKV == nil {
			return nil, errors.New("queried record has no cckv")
		}

		err := uc.checkReadAccess(ctx, *sd.CCKV, sd)
		if err != nil {
			if errors.Is(err, domain.ErrPermissionDenied) {
				continue
			}
			return nil, err
		}

		filtered = append(filtered, sd)
	}

	return filtered, nil
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

	blockingUsers, err := uc.repo.QueryByParent(ctx, blockingKey, "", nil, nil, 0, "")
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	prefix := blockingKey + "/"

	ids := make([]string, len(blockingUsers))
	for i, sd := range blockingUsers {
		id := strings.TrimPrefix(*sd.CCKV, prefix)
		ids[i] = id
	}

	uc.cache.Set("blockingUsers:"+userID, ids, cache.DefaultExpiration)

	return ids, nil
}
