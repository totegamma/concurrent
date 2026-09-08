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
	// Utilities
	BeginTx(ctx context.Context) (RepositoryTx, error)

	// Create / Update
	CreateCommitLog(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof any, owner string) error

	CreateEntity(ctx context.Context, tx RepositoryTx, ccid string, alias *string, domain string, documentID string, createdAt time.Time) (bool, error)
	CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, author string, schema string, onUpdate *string, policies *string, distributions []string, redirect *string, createdAt time.Time) (bool, error)
	CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) (bool, error)
	Acknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)
	UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)
	Acknowledged(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)
	UnAcknowledged(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, schema string, createdAt time.Time) (bool, error)

	// Read
	HasCommitLog(ctx context.Context, id string) (bool, error)
	GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error)

	GetAcknowledgeRecords(ctx context.Context, from, to, schema string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error)
	GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error)
	GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error)

	QueryByPrefix(ctx context.Context, prefix, schema, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)
	QueryByParent(ctx context.Context, parent, schema, author string, since, until *time.Time, limit int, order string) ([]QueryRow, error)

	QueryRecordSubtree(ctx context.Context, base string, includeSelf bool) ([]concrnt.SignedDocument, error)
	GetTimelineRemoval(ctx context.Context, keyURI string) (timeline string, itemID string, err error)
	GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error)
	GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error)

	GetDistributions(ctx context.Context, uri string) ([]string, error)

	// Delete
	// MarkCommitLogGcCandidate flags a commit log for `conctl op gc-commitlog`
	// once the backdate window has passed (CIP-3 §3.4 replay guard).
	MarkCommitLogGcCandidate(ctx context.Context, tx RepositoryTx, documentID string) error
	DeleteRecordByKey(ctx context.Context, tx RepositoryTx, targetURI string) error
	DeleteRecordByDocumentID(ctx context.Context, tx RepositoryTx, documentID string) error
	DeleteAssociation(ctx context.Context, tx RepositoryTx, documentID string) error
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
	postProcesses []PostProcessAction
	noop          bool
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

	if len(sd.Document) > domain.MaxDocumentSize {
		err := domain.ValidationError{Field: "document", Message: fmt.Sprintf("document exceeds the maximum size of %d bytes", domain.MaxDocumentSize)}
		span.RecordError(err)
		return nil, err
	}

	doc, err := sd.ParsedDocument()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := rejectAliasOwners(doc); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// acked/unacked are server-derived holdings of the associate owner
	// (CIP-10 §5.2) and follow their own verification and exemption rules.
	isAckedKind := doc.Kind == "acked" || doc.Kind == "unacked"

	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

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

	if doc.CreatedAt.After(time.Now().Add(domain.MaxFutureSkew)) {
		err := domain.ValidationError{Field: "createdAt", Message: "createdAt is too far in the future"}
		span.RecordError(err)
		return nil, err
	}

	if !isServiceAccount {

		// Entity documents must be master-key signed (CIP-0 §8.2); acked and
		// unacked documents are server derivations of an embedded ack and are
		// only legitimate under a document-direct proof (CIP-10 §5.2) — an
		// author-signed "acked" would let anyone write the target-side state.
		var allowedProofs []string
		if doc.Kind == "entity" {
			allowedProofs = []string{concrnt.ProofTypeEcrecover}
		}
		if isAckedKind {
			allowedProofs = []string{concrnt.ProofTypeDocumentDirect}
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
			if isAckedKind && errors.Is(err, concrnt.ErrProofTypeNotAllowed) {
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "acked/unacked documents must carry a " + concrnt.ProofTypeDocumentDirect + " proof"}, err)
			}
			if errors.Is(err, concrnt.ErrUnsupportedProofType) {
				return nil, errors.Join(domain.ValidationError{Field: "proof.type", Message: "unsupported proof type: " + sd.Proof.Type}, err)
			}
			return nil, errors.Join(domain.ValidationError{Field: "proof", Message: "signature verification failed"}, err)
		}

		// Entity documents are exempt from the backdate window (CIP-3 §3.4);
		// so are acked/unacked documents, whose createdAt is inherited from
		// the embedded ack that already passed the window when it was
		// accepted — delivery retries and repository replays may arrive
		// later than the window (CIP-10 §5.2).
		backdateExempt := doc.Kind == "entity" || isAckedKind
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
	// already-resolvable requester entity. So may acked/unacked documents:
	// they are the associate owner's own holding, replayable from a dump
	// after the acker's server is gone, and the embedded signature already
	// vouches for the author (CIP-10 §5.2) — an unresolvable author is simply
	// not local.
	if requester == nil {
		if doc.Kind != "entity" && !isAckedKind {
			err := errors.Join(domain.ValidationError{Field: "document.author", Message: fmt.Sprintf("requester entity not found for %s operation", doc.Kind)}, requesterErr)
			span.RecordError(err)
			return nil, err
		}
		if isAckedKind {
			requester = &domain.Entity{ID: requesterID}
		}
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
	// acked/unacked skip the block check: the embedded ack passed it when it
	// was accepted, and a later block must not make the associate owner's
	// own holding un-replayable (CIP-10 §5.2).
	if targetUserID != "" && targetUserID != requesterID && !isAckedKind {

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
			return uc.saveEntity(ctx, tx, ip, sd)
		}

	case "record":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.createRecord(ctx, tx, ip, *requester, doc, sd, mode)
		}

	case "association":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.createAssociation(ctx, tx, ip, *requester, doc, sd, mode)
		}

	case "ack", "acked", "unack", "unacked":
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
			return uc.processAck(ctx, tx, ip, *requester, *targetUser, doc, sd, mode)
		}

	case "delete":
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.deleteRecord(ctx, tx, ip, *requester, sd, mode)
		}
	default:
		err := errors.New("unsupported document kind: " + doc.Kind)
		span.RecordError(err)
		return nil, err
	}

	//  ** start transatction **

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

	applyResult, err := applyCommit(tx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if applyResult.noop {
		slog.Info("commit no-op (accept-if-newer loss)",
			"documentID", documentID, "kind", doc.Kind, "author", doc.Author)
		return applyResult.result, nil
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

func (uc *RecordUsecase) saveEntity(ctx context.Context, tx RepositoryTx, ip string, sd concrnt.SignedDocument) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
	}

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

	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, entity.Author); err != nil {
		span.RecordError(err)
		return nil, err
	}

	applied, err := uc.repo.CreateEntity(ctx, tx, entity.Author, entity.Value.Alias, entity.Value.Domain, documentID, entity.CreatedAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	return &commitApplyResult{result: &sd}, nil
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

func (uc *RecordUsecase) deleteRecord(ctx context.Context, tx RepositoryTx, ip string, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
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

	// A delete is committed the same way whichever way it arrived: from the
	// user, or forwarded by the author's server to a distribution destination
	// on the user's behalf (CIP-4 §6.1). The commit belongs to the owner of
	// the deleted namespace (CIP-3 §3.1) — every target of a range shares the
	// base's owner, so one commit log covers them all.
	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	parsedBase, err := concrnt.ParseCCURI(rangeBase)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, parsedBase.Owner); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// History is only kept for what asked for it: a deleted document with
	// onUpdate=retain keeps its commit and, with it, this delete's (the
	// replay needs both); everything else is flagged for gc once the
	// backdate window has passed (the replay guard then holds on its own).
	deleteRetained := false
	deletedAny := false
	flagDeleted := func(deleted *concrnt.SignedDocument, onUpdate *string) error {
		deletedAny = true
		if onUpdate != nil && *onUpdate == "retain" {
			deleteRetained = true
			return nil
		}
		deletedID, err := deleted.CDID()
		if err != nil {
			return err
		}
		return uc.repo.MarkCommitLogGcCandidate(ctx, tx, deletedID)
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

				if err := flagDeleted(targetSD, targetDoc.OnUpdate); err != nil {
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

				if err := flagDeleted(targetSD, nil); err != nil {
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
			documentID, err := targetSD.CDID()
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			href = concrnt.CCURI{
				Scheme: "ccfs",
				Owner:  parsedAssociate.Owner,
				Type:   concrnt.CCFSTypeConcrnt,
				CDID:   documentID,
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

			if err := flagDeleted(refSD, refDoc.OnUpdate); err != nil {
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

	// CIP-4 §6.1: a delete that removed nothing here (the forwarded targets
	// had no local reference rows) is not recorded — the tx rolls back — so
	// a later delivery that does find rows is not deduplicated away
	if !deletedAny {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	if !deleteRetained {
		if err := uc.repo.MarkCommitLogGcCandidate(ctx, tx, documentID); err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	// targets are URI-ordered, so with includeSelf the base record itself
	// leads and becomes the reported result
	return &commitApplyResult{result: &targets[0], postProcesses: postProcesses}, nil
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

func (uc *RecordUsecase) createRecord(ctx context.Context, tx RepositoryTx, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
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

	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, parsedKey.Owner); err != nil {
		span.RecordError(err)
		return nil, err
	}

	resultURI := parsed.Key
	applied, err := uc.repo.CreateRecord(ctx, tx, documentID, parsed.Key, parsedKey.Owner, parsed.Author, schema, parsed.OnUpdate, policies, distributions, redirect, createdAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

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

	sd.CCKV = &parsed.Key
	ccfs := concrnt.CCURI{
		Scheme: "ccfs",
		Owner:  parsedKey.Owner,
		Type:   concrnt.CCFSTypeConcrnt,
		CDID:   documentID,
	}.String()

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) createAssociation(ctx context.Context, tx RepositoryTx, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
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

	documentID, err := sd.CDID()
	if err != nil {
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

		if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, targetURI.Owner); err != nil {
			span.RecordError(err)
			return nil, err
		}

		applied, err := uc.repo.CreateAssociation(ctx, tx, documentID, *parsed.Associate, targetURI.Owner, parsed.Author, parsed.Schema, parsed.AssociationVariant, fmt.Sprintf("%x", uniqueHash), parsed.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		if !applied {
			return &commitApplyResult{result: &sd, noop: true}, nil
		}

		if mode == domain.CommitModeExecute {
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

		// 各タイムラインを購読しているクライアントに、そのタイムラインにassociationが追加されたことを通知するために、
		// association本体をリモートサーバーに転送し、eventの発生を促す。
		remoteKind := domain.DeliveryRemoteNone
		if isLocal {
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

	sd.CCFS = &ccfs

	return &commitApplyResult{result: &sd, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) processAck(ctx context.Context, tx RepositoryTx, ip string, from domain.Entity, to domain.Entity, doc concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Acknowledge")
	defer span.End()

	postProcesses := []PostProcessAction{}
	created := false

	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	switch doc.Kind {
	case "ack", "unack":
		if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, from.ID); err != nil {
			span.RecordError(err)
			return nil, err
		}
	case "acked", "unacked":
		if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, to.ID); err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	if uc.IsLocalEntity(ctx, &from) && (doc.Kind == "ack" || doc.Kind == "unack") { // create ack

		if doc.Kind == "ack" {
			created, err = uc.repo.Acknowledge(ctx, tx, documentID, from.ID, to.ID, doc.Schema, doc.CreatedAt)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		} else {
			created, err = uc.repo.UnAcknowledge(ctx, tx, documentID, from.ID, to.ID, doc.Schema, doc.CreatedAt)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		}

		// フォロー通知などを生成
		// CIP-3 §3.4: the ack's ccfs identity is held by the author's server
		ccfs := concrnt.CCURI{
			Scheme: "ccfs",
			Owner:  from.ID,
			Type:   concrnt.CCFSTypeConcrnt,
			CDID:   documentID,
		}.String()
		sd.CCFS = &ccfs

		actions, err := uc.createReferenceDistributionActions(ctx, ip, doc.Author, ccfs, from, sd, distributionsFromPtr(doc.Distributes), mode)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		postProcesses = append(postProcesses, actions...)

		// acked documentを生成・配送
		// DeriveAcked is the same derivation the receiving side re-runs to
		// verify the document-direct proof (verify.go), so the two stay in
		// lockstep by construction.
		ackedSD, err := sd.DeriveAcked()
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		// like reference distribution, only an executing commit ships the
		// acked document; a dump replay (LocalOnlyExecute) leaves the
		// associate owner's holding to that side's own dump
		if mode == domain.CommitModeExecute && uc.delivery != nil {
			postProcesses = append(postProcesses,
				func(ctx context.Context) error {
					return uc.delivery.Enqueue(ctx, domain.DeliveryJob{
						ResolveURI: to.CCKVWithHint(),
						Payload:    ackedSD,
						Local:      domain.DeliveryLocalCommit,
						Remote:     domain.DeliveryRemoteCommit,
						IP:         ip,
					})
				},
			)
		}

	} else {
		// ignore. because...
		// if remote: from should be the local entity.
		// if acked: acked document does not create ack
	}

	if uc.IsLocalEntity(ctx, &to) && (doc.Kind == "acked" || doc.Kind == "unacked") { // create acked

		switch doc.Kind {
		case "acked":
			created, err = uc.repo.Acknowledged(ctx, tx, documentID, from.ID, to.ID, doc.Schema, doc.CreatedAt)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		case "unacked":
			created, err = uc.repo.UnAcknowledged(ctx, tx, documentID, from.ID, to.ID, doc.Schema, doc.CreatedAt)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
		default:
			err := errors.New("unsupported document kind for acked: " + doc.Kind)
			span.RecordError(err)
			return nil, err
		}

		// CIP-3 §3.4: the acked's ccfs identity is held by the associate
		// owner's server
		ccfs := concrnt.CCURI{
			Scheme: "ccfs",
			Owner:  to.ID,
			Type:   concrnt.CCFSTypeConcrnt,
			CDID:   documentID,
		}.String()
		sd.CCFS = &ccfs
	}

	return &commitApplyResult{result: &sd, noop: !created, postProcesses: postProcesses}, nil

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
