package usecase

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
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
	CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, schema string, onUpdate *string, policies *string, distributions []string, redirect *string, createdAt time.Time) (string, error)
	CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) error
	Acknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time, resultURI string) (string, error)
	UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time) error
	Delete(ctx context.Context, tx RepositoryTx, targetURI string) (string, error)

	GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error)
	GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error)
	GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error)

	GetDistributions(ctx context.Context, uri string) ([]string, error)

	GetAcknowledgeRecords(ctx context.Context, from, to, context string) ([]concrnt.SignedDocument, error)
	GetAcknowledgeRecordCounts(ctx context.Context, from, to, context string) (map[string]int64, error)
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

type PolicyService interface {
	Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error
}

type commitApplyResult struct {
	result        *concrnt.SignedDocument
	owners        []string
	postProcesses []PostProcessAction
}

type RecordUsecase struct {
	repo      RecordRepository
	residence ResidenceRepository
	config    *domain.Config
	client    *client.Client
	signal    SignalService
	policy    PolicyService
	cache     *cache.Cache
}

func NewRecordUsecase(
	repo RecordRepository,
	residence ResidenceRepository,
	config *domain.Config,
	client *client.Client,
	signal SignalService,
	policy PolicyService,
) *RecordUsecase {
	return &RecordUsecase{
		repo:      repo,
		residence: residence,
		config:    config,
		client:    client,
		signal:    signal,
		policy:    policy,
		cache:     cache.New(10*time.Minute, 15*time.Minute),
	}
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
	} else {
		slog.Debug("no reference found for requester in commit references", slog.String("requester_cckv", requesterCCKV))
	}
	return nil
}

func (uc *RecordUsecase) Commit(ctx context.Context, ip string, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Commit")
	defer span.End()

	var doc concrnt.Document[any]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// validate
	switch sd.Proof.Type {
	case concrnt.ProofTypeEcrecover:
		if sd.Proof.Signature == nil {
			err := errors.New("[sub] signature is required for ecrecover proof")
			span.RecordError(err)
			return nil, err
		}
		signatureBytes, err := hex.DecodeString(*sd.Proof.Signature)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		err = concrnt.VerifySignature([]byte(sd.Document), signatureBytes, doc.Author)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	case concrnt.ProofTypeDocumentReference:
		if sd.Proof.Href == nil {
			err := errors.New("href is required for document-reference proof")
			span.RecordError(err)
			return nil, err
		}
		// TODO: 参照先のドキュメントの検証
	case concrnt.ProofTypeSubkey:
		if sd.Proof.Signature == nil {
			err := errors.New("[sub] signature is required for subkey proof")
			span.RecordError(err)
			return nil, err
		}

		if sd.Proof.Key == nil {
			err := errors.New("[sub] key is required for subkey proof")
			span.RecordError(err)
			return nil, err
		}

		var subKeyDoc concrnt.Document[schemas.Subkey]
		err := uc.client.GetRecord(ctx, *sd.Proof.Key, nil, &subKeyDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		signatureBytes, err := hex.DecodeString(*sd.Proof.Signature)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		err = concrnt.VerifySignature([]byte(sd.Document), signatureBytes, subKeyDoc.Value.CKID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	case concrnt.ProofTypeNone:
		serviceAccountType, ok := ctx.Value(interop.ServiceAccountTypeCtxKey).(string)
		if !ok || serviceAccountType != "system" {
			err := errors.New("none proof type is only allowed for system service accounts")
			slog.Error("Unauthorized commit with none proof", "error", err.Error())
			span.RecordError(err)
			return nil, err
		}

	default:
		err := errors.New("unsupported proof type: " + sd.Proof.Type)
		span.RecordError(err)
		return nil, err
	}

	requesterID := doc.Author
	referrer := GetReferrerFromReferences(sd, requesterID)

	requester, err := uc.GetEntity(ctx, concrnt.CCURI{Scheme: "cckv", Owner: requesterID, Hint: referrer}.String())
	if err != nil {
		span.RecordError(err)
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

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, doc.CreatedAt).String()

	var applyCommit func(tx RepositoryTx) (*commitApplyResult, error)

	// accept
	switch doc.Schema {
	// 特殊なスキーマの場合の処理
	case schemas.EntityURL:
		//return uc.saveEntity(ctx, tx, documentID, sd)
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.saveEntity(ctx, tx, documentID, sd)
		}
	case schemas.DeleteURL:
		if requester == nil {
			err := errors.New("requester entity not found for delete operation")
			span.RecordError(err)
			return nil, err
		}
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.deleteRecord(ctx, tx, *requester, sd, mode)
		}
	case schemas.AcknowledgeURL:
		if requester == nil {
			err := errors.New("requester entity not found for ack operation")
			span.RecordError(err)
			return nil, err
		}
		var ackDoc concrnt.Document[schemas.Acknowledge]
		err := json.Unmarshal([]byte(sd.Document), &ackDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		referrer := GetReferrerFromReferences(sd, requester.CCKV())
		targetUser, err := uc.GetEntity(ctx, concrnt.CCURI{Scheme: "cckv", Owner: *ackDoc.Associate, Hint: referrer}.String())
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.acknowledge(ctx, tx, documentID, *requester, *targetUser, ackDoc, sd, mode)
		}
	case schemas.UnAcknowledgeURL:
		if requester == nil {
			err := errors.New("requester entity not found for unack operation")
			span.RecordError(err)
			return nil, err
		}
		var ackDoc concrnt.Document[schemas.Acknowledge]
		err := json.Unmarshal([]byte(sd.Document), &ackDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		targetUser, err := uc.GetEntity(ctx, concrnt.CCURI{Scheme: "cckv", Owner: *ackDoc.Associate}.String())
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
			return uc.unacknowledge(ctx, tx, documentID, *requester, *targetUser, ackDoc, sd, mode)
		}
	default:
		if requester == nil {
			err := errors.New("requester entity not found for record or associate operation")
			slog.Error("requester entity not found for record or associate operation", slog.String("error", err.Error()))
			span.RecordError(err)
			return nil, err
		}
		// Associateフィールドがあれば通常Recordではない
		if doc.Associate != nil {
			applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
				return uc.createAssociation(ctx, tx, documentID, ip, *requester, doc, sd, mode)
			}
		} else { // 通常Record
			applyCommit = func(tx RepositoryTx) (*commitApplyResult, error) {
				return uc.createRecord(ctx, tx, documentID, ip, *requester, doc, sd, mode)
			}
		}
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

func (uc *RecordUsecase) saveEntity(ctx context.Context, tx RepositoryTx, documentID string, sd concrnt.SignedDocument) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
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

	err := uc.repo.CreateEntity(ctx, tx, entity.Author, entity.Value.Alias, entity.Value.Domain, documentID)
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

func (uc *RecordUsecase) deleteRecord(ctx context.Context, tx RepositoryTx, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Delete")
	defer span.End()

	var deletedoc concrnt.Document[schemas.Delete]
	err := json.Unmarshal([]byte(sd.Document), &deletedoc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	targetURI := string(deletedoc.Value)
	targetHost, err := uc.client.ResolveResourceHost(ctx, targetURI)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if targetHost == uc.config.FQDN {
		targetSD, err := uc.repo.GetSignedDocument(ctx, targetURI)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		targetDoc := concrnt.Document[any]{}
		err = json.Unmarshal([]byte(targetSD.Document), &targetDoc)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		policyRoot := string(deletedoc.Value)
		if targetDoc.Associate != nil {
			policyRoot = *targetDoc.Associate
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
				Self:      targetDoc,
			},
			stack,
			policyDeleteAction(targetDoc),
			targetURI,
		)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		_, err = uc.repo.Delete(ctx, tx, targetURI)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		postProcesses := []PostProcessAction{}
		if mode == domain.CommitModeExecute {
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
						host, err := uc.client.ResolveResourceHost(ctx, dest)
						if err != nil {
							return err
						}
						if host == uc.config.FQDN {
							return uc.signal.Publish(ctx, dest, concrnt.Event{
								Type: "deleted",
								URI:  targetURI,
							})
						}
						return uc.client.Commit(ctx, host, remoteSD)
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

				destinations := []string{*targetDoc.Associate}
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
							host, err := uc.client.ResolveResourceHost(ctx, dest)
							if err != nil {
								return err
							}
							if host == uc.config.FQDN {
								return uc.signal.Publish(ctx, dest, concrnt.Event{
									Type: "unassociated",
									URI:  associatedURI,
								})
							}
							return uc.client.Commit(ctx, host, remoteSD)
						},
					)
				}
			}
		}
		return &commitApplyResult{result: targetSD, owners: uc.localEntityOwners(ctx, requester), postProcesses: postProcesses}, nil

	} else { // remote entity. only emit signals.
		targetSD, ok := sd.References[targetURI]
		if !ok {
			err := errors.New("target document not found in references for remote delete")
			span.RecordError(err)
			return nil, err
		}

		document := concrnt.Document[any]{}
		err = json.Unmarshal([]byte(targetSD.Document), &document)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		postProcesses := []PostProcessAction{}
		destinations := []string{targetURI}
		if document.Distributes != nil {
			destinations = append(destinations, *document.Distributes...)
		}

		for _, dest := range destinations {
			postProcesses = append(postProcesses,
				func(ctx context.Context) error {
					host, err := uc.client.ResolveResourceHost(ctx, dest)
					if err != nil {
						return err
					}
					if host != uc.config.FQDN {
						return nil
					}
					return uc.signal.Publish(ctx, dest, concrnt.Event{
						Type: "deleted",
						URI:  targetURI,
					})
				},
			)
		}

		if document.Associate != nil {
			associatedURI := *document.Associate
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
						host, err := uc.client.ResolveResourceHost(ctx, dest)
						if err != nil {
							return err
						}
						if host != uc.config.FQDN {
							return nil
						}
						return uc.signal.Publish(ctx, dest, concrnt.Event{
							Type: "unassociated",
							URI:  associatedURI,
						})
					},
				)
			}
		}

		return &commitApplyResult{result: &targetSD, owners: uc.localEntityOwners(ctx, requester), postProcesses: postProcesses}, nil
	}
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

	var resultURI string
	resultURI, err = uc.repo.CreateRecord(ctx, tx, documentID, parsed.Key, parsedKey.Owner, schema, parsed.OnUpdate, policies, distributions, redirect, createdAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	postProcesses := []PostProcessAction{
		func(ctx context.Context) error {
			return uc.signal.Publish(ctx, resultURI, concrnt.Event{
				Type:       "created",
				URI:        resultURI,
				References: map[string]concrnt.SignedDocument{resultURI: sd},
			})
		},
	}

	if mode == domain.CommitModeExecute && parsed.Distributes != nil {
		requesterSD, err := uc.GetSigned(ctx, requester.CCKVWithHint())
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		for _, destURI := range *parsed.Distributes {
			key, err := url.JoinPath(destURI, documentID)
			if err != nil {
				slog.Error("failed to join path for distribution", slog.String("destination", destURI), slog.String("document_id", documentID), slog.String("error", err.Error()))
				span.RecordError(err)
				continue
			}

			distDoc := concrnt.Document[schemas.Reference]{
				Key: key,
				Value: schemas.Reference{
					Href: resultURI,
				},
				Author:    parsed.Author,
				Schema:    schemas.ReferenceURL,
				CreatedAt: time.Now(),
			}
			docBytes, err := json.Marshal(distDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			distSD := concrnt.SignedDocument{
				Document: string(docBytes),
				Proof: concrnt.Proof{
					Type: "document-reference",
					Href: &resultURI,
				},
				References: map[string]concrnt.SignedDocument{
					requester.CCKV(): *requesterSD,
					resultURI:        sd,
				},
			}

			destURI := destURI
			postProcesses = append(postProcesses,
				func(ctx context.Context) error {
					host, err := uc.client.ResolveResourceHost(ctx, destURI)
					if err != nil {
						return err
					}
					if host == uc.config.FQDN {
						_, err = uc.Commit(ctx, ip, distSD, mode)
						return err
					}
					dest, err := concrnt.ParseCCURI(destURI)
					if err != nil {
						return err
					}
					return uc.client.Commit(ctx, dest.Owner, distSD)
				},
			)
		}
	}

	owners, err := uc.localCommitOwners(ctx, parsedKey.Owner)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) createAssociation(ctx context.Context, tx RepositoryTx, documentID string, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CreateAssociation")
	defer span.End()

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

	ccfs := concrnt.ComposeCCURI("ccfs", targetURI.Owner, documentID)

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
		uniqueHash := xxh3.HashString(uniqueKey)

		err = uc.repo.CreateAssociation(ctx, tx, documentID, *parsed.Associate, targetURI.Owner, parsed.Author, parsed.Schema, parsed.AssociationVariant, fmt.Sprintf("%x", uniqueHash), parsed.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		created = true
		if mode == domain.CommitModeExecute {
			for _, destURI := range distributionsFromPtr(parsed.Distributes) {
				key, err := url.JoinPath(destURI, documentID)
				if err != nil {
					slog.Error("failed to join path for distribution", slog.String("destination", destURI), slog.String("document_id", documentID), slog.String("error", err.Error()))
					span.RecordError(err)
					continue
				}

				distDoc := concrnt.Document[schemas.Reference]{
					Key: key,
					Value: schemas.Reference{
						Href: ccfs,
					},
					Author:    parsed.Author,
					Schema:    schemas.ReferenceURL,
					CreatedAt: time.Now(),
				}
				docBytes, err := json.Marshal(distDoc)
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
				distSD := concrnt.SignedDocument{
					Document: string(docBytes),
					Proof: concrnt.Proof{
						Type: "document-reference",
						Href: &ccfs,
					},
					References: map[string]concrnt.SignedDocument{
						requester.CCKV(): *requesterSD,
						ccfs:             sd,
					},
				}

				destURI := destURI
				postProcesses = append(postProcesses,
					func(ctx context.Context) error {
						host, err := uc.client.ResolveResourceHost(ctx, destURI)
						if err != nil {
							return err
						}
						if host == uc.config.FQDN {
							_, err = uc.Commit(ctx, ip, distSD, mode)
							return err
						}
						dest, err := concrnt.ParseCCURI(destURI)
						if err != nil {
							return err
						}
						return uc.client.Commit(ctx, dest.Owner, distSD)
					},
				)
			}
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

			// TODO: 署名検証

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
					host, err := uc.client.ResolveResourceHost(ctx, channel)
					if err != nil {
						return err
					}
					if host == uc.config.FQDN {
						return uc.signal.Publish(ctx, channel, concrnt.Event{
							Type:        "associated",
							URI:         target,
							Association: &ccfs,
							References: map[string]concrnt.SignedDocument{
								ccfs: sd,
							},
						})
					}
					if !created {
						return nil
					}
					return uc.client.Commit(ctx, host, remoteSD)
				},
			)
		}
	}

	owners := []string{}
	if isLocal {
		owners = append(owners, targetURI.Owner)
	}

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

func (uc *RecordUsecase) acknowledge(ctx context.Context, tx RepositoryTx, documentID string, requester domain.Entity, targetUser domain.Entity, doc concrnt.Document[schemas.Acknowledge], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Acknowledge")
	defer span.End()

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

		resultURI := concrnt.ComposeCCURI("ccfs", parsedAssociate.Owner, documentID)
		_, err = uc.repo.Acknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Value.Context, true, doc.CreatedAt, resultURI)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

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
				return uc.client.Commit(ctx, targetUser.Domain, distSD)
			},
		)
	}

	owners := []string{}
	if uc.IsLocalEntity(ctx, &requester) {
		owners = append(owners, requester.ID)
	}
	if uc.IsLocalEntity(ctx, &targetUser) && !slices.Contains(owners, targetUser.ID) {
		owners = append(owners, targetUser.ID)
	}
	return &commitApplyResult{result: &sd, owners: owners, postProcesses: postProcesses}, nil
}

func (uc *RecordUsecase) unacknowledge(ctx context.Context, tx RepositoryTx, documentID string, requester domain.Entity, targetUser domain.Entity, doc concrnt.Document[schemas.Acknowledge], sd concrnt.SignedDocument, mode domain.CommitMode) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.UnAcknowledge")
	defer span.End()

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

		err = uc.repo.UnAcknowledge(ctx, tx, documentID, doc.Author, parsedAssociate.Owner, doc.Value.Context, false, doc.CreatedAt)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

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
				return uc.client.Commit(ctx, targetUser.Domain, distSD)
			},
		)
	}

	owners := []string{}
	if uc.IsLocalEntity(ctx, &requester) {
		owners = append(owners, requester.ID)
	}
	if uc.IsLocalEntity(ctx, &targetUser) && !slices.Contains(owners, targetUser.ID) {
		owners = append(owners, targetUser.ID)
	}
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

		if parsed.Hint == nil {
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

		err = uc.checkReadAccess(ctx, uri, *sd)
		if err != nil {
			return nil, err
		}

		return sd, nil
	}
}

func (uc *RecordUsecase) checkReadAccess(ctx context.Context, uri string, sd concrnt.SignedDocument) error {
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

	stack, err := uc.repo.GetHierarchicalRecordPolicies(ctx, uri)
	if err != nil {
		span.RecordError(err)
		stack = []concrnt.Policy{}
	}

	requester, _ := ctx.Value(interop.RequesterCtxKey).(domain.Entity)

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

func policyCreateAction(doc concrnt.Document[any]) string {
	if doc.Associate != nil {
		return "association:create"
	}
	return "record:create"
}

func policyReadAction(doc concrnt.Document[any]) string {
	if doc.Associate != nil {
		return "association:read"
	}
	return "record:read"
}

func policyDeleteAction(doc concrnt.Document[any]) string {
	if doc.Associate != nil {
		return "association:delete"
	}
	return "record:delete"
}

func (uc *RecordUsecase) GetAcknowledgeRecords(ctx context.Context, from, to, context string) ([]concrnt.SignedDocument, error) {
	return uc.repo.GetAcknowledgeRecords(ctx, from, to, context)
}

func (uc *RecordUsecase) GetAcknowledgeRecordCounts(ctx context.Context, from, to, context string) (map[string]int64, error) {
	return uc.repo.GetAcknowledgeRecordCounts(ctx, from, to, context)
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
