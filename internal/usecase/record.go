package usecase

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/cdid"
	"github.com/totegamma/concrnt-playground/client"
	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/service"
	"github.com/totegamma/concrnt-playground/internal/utils"
	"github.com/totegamma/concrnt-playground/policy"
	"github.com/totegamma/concrnt-playground/schemas"
)

// RecordRepository defines storage operations for records/commits.
type RecordRepository interface {
	CreateRecord(ctx context.Context, ip string, documentID string, sd concrnt.SignedDocument) (string, error)
	CreateAssociation(ctx context.Context, ip string, documentID string, parsed concrnt.Document[any], sd concrnt.SignedDocument) error
	Acknowledge(ctx context.Context, ip string, documentID string, sd concrnt.SignedDocument) (string, error)
	UnAcknowledge(ctx context.Context, ip string, documentID string, sd concrnt.SignedDocument) error
	Delete(ctx context.Context, sd concrnt.SignedDocument) (string, error)

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

type RecordUsecase struct {
	repo   RecordRepository
	config *domain.Config
	client *client.Client
	entity *EntityUsecase
	signal *service.SignalService
	policy *service.PolicyService
	cache  *cache.Cache
}

func NewRecordUsecase(
	repo RecordRepository,
	config *domain.Config,
	client *client.Client,
	entity *EntityUsecase,
	signal *service.SignalService,
	policy *service.PolicyService,
) *RecordUsecase {
	return &RecordUsecase{
		repo:   repo,
		config: config,
		client: client,
		entity: entity,
		signal: signal,
		policy: policy,
		cache:  cache.New(10*time.Minute, 15*time.Minute),
	}
}

func GetReferrerFromReferences(sd concrnt.SignedDocument, requesterID string) *string {
	requesterCCKV := concrnt.ComposeCCURI("cckv", requesterID, "")
	entityRef, ok := sd.References[requesterCCKV]
	if ok {
		jsonBytes, err := json.Marshal(entityRef)
		if err != nil {
			return nil
		}
		var entity concrnt.Document[schemas.Entity]
		err = json.Unmarshal(jsonBytes, &entity)
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
		if !uc.config.Debug {
			err := errors.New("none proof type is only allowed in debug mode")
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

	requester, err := uc.entity.Get(ctx, requesterID, referrer)
	if err != nil {
		span.RecordError(err)
		// return nil, err
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

	// accept
	switch doc.Schema {
	// 特殊なスキーマの場合の処理
	case schemas.EntityURL:
		return uc.saveEntity(ctx, sd)
	case schemas.DeleteURL:
		if requester == nil {
			err := errors.New("requester entity not found for delete operation")
			span.RecordError(err)
			return nil, err
		}
		return uc.deleteRecord(ctx, *requester, sd, mode)
	case schemas.AcknowledgeURL:
		if requester == nil {
			err := errors.New("requester entity not found for ack operation")
			span.RecordError(err)
			return nil, err
		}
		return uc.acknowledge(ctx, ip, *requester, sd, mode)
	case schemas.UnAcknowledgeURL:
		if requester == nil {
			err := errors.New("requester entity not found for unack operation")
			span.RecordError(err)
			return nil, err
		}
		return uc.unacknowledge(ctx, ip, *requester, sd, mode)
	default:
		if requester == nil {
			err := errors.New("requester entity not found for record or associate operation")
			fmt.Printf("Error: %v\n", err)
			span.RecordError(err)
			return nil, err
		}
		// Associateフィールドがあれば通常Recordではない
		if doc.Associate != nil {
			return uc.createAssociation(ctx, ip, *requester, doc, sd, mode)
		} else { // 通常Record
			return uc.createRecord(ctx, ip, *requester, doc, sd, mode)
		}
	}
}

func (uc *RecordUsecase) saveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.SignedDocument, error) {
	return uc.entity.SaveEntity(ctx, sd)
}

func (uc *RecordUsecase) deleteRecord(ctx context.Context, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
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
				This:      targetDoc,
			},
			stack,
			"net.concrnt.core.commit.delete",
			targetURI,
		)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		_, err = uc.repo.Delete(ctx, sd)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		// signal
		if mode == domain.CommitModeExecute {
			destinations := []string{targetURI}
			if targetDoc.Distributes != nil {
				destinations = append(destinations, *targetDoc.Distributes...)
			}
			for _, dest := range destinations {
				host, err := uc.client.ResolveResourceHost(ctx, dest)
				if err != nil {
					fmt.Printf("Error resolving resource host for signal: %v\n", err)
					span.RecordError(err)
					continue
				}

				if host == uc.config.FQDN { // local
					err = uc.signal.Publish(ctx, dest, concrnt.Event{
						Type: "deleted",
						URI:  targetURI,
					})
					if err != nil {
						fmt.Printf("Error publishing signal for delete: %v\n", err)
						span.RecordError(err)
						return nil, err
					}
				} else { // remote
					sd := concrnt.SignedDocument{
						Document: sd.Document,
						Proof:    sd.Proof,
						References: map[string]concrnt.SignedDocument{
							targetURI: *targetSD,
						},
					}
					err = uc.client.Commit(ctx, host, sd)
					if err != nil {
						fmt.Printf("Error committing delete document to remote: %v\n", err)
						span.RecordError(err)
						return nil, err
					}
				}
			}

			if targetDoc.Associate != nil {
				associatedURI := *targetDoc.Associate
				associatedSD, err := uc.repo.GetSignedDocument(ctx, associatedURI)
				if err != nil {
					fmt.Printf("Error fetching associated document for signal: %v\n", err)
					span.RecordError(err)
					return nil, err
				}

				var associatedDoc concrnt.Document[any]
				err = json.Unmarshal([]byte(associatedSD.Document), &associatedDoc)
				if err != nil {
					fmt.Printf("Error unmarshaling associated document for signal: %v\n", err)
					span.RecordError(err)
					return nil, err
				}

				destinations := []string{*targetDoc.Associate}
				if associatedDoc.Distributes != nil {
					destinations = append(destinations, *associatedDoc.Distributes...)
				}

				for _, dest := range destinations {
					host, err := uc.client.ResolveResourceHost(ctx, dest)
					if err != nil {
						fmt.Printf("Error resolving resource host for unassociation signal: %v\n", err)
						span.RecordError(err)
						continue
					}

					if host == uc.config.FQDN { // local
						err = uc.signal.Publish(ctx, dest, concrnt.Event{
							Type: "unassociated",
							URI:  *targetDoc.Associate,
						})
						if err != nil {
							fmt.Printf("Error publishing signal for unassociation: %v\n", err)
							span.RecordError(err)
							return nil, err
						}
					} else { // remote
						sd := concrnt.SignedDocument{
							Document: sd.Document,
							Proof:    sd.Proof,
							References: map[string]concrnt.SignedDocument{
								targetURI:     *targetSD,
								associatedURI: *associatedSD,
							},
						}
						err = uc.client.Commit(ctx, host, sd)
						if err != nil {
							fmt.Printf("Error committing unassociation document to remote: %v\n", err)
							span.RecordError(err)
							return nil, err
						}
					}
				}
			}
		}
		return targetSD, nil

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

		destinations := []string{targetURI}
		if document.Distributes != nil {
			destinations = append(destinations, *document.Distributes...)
		}

		for _, dest := range destinations {
			host, err := uc.client.ResolveResourceHost(ctx, dest)
			if err != nil {
				fmt.Printf("Error resolving resource host for signal: %v\n", err)
				span.RecordError(err)
				continue
			}

			if host == uc.config.FQDN { // local
				err = uc.signal.Publish(ctx, dest, concrnt.Event{
					Type: "deleted",
					URI:  targetURI,
				})
				if err != nil {
					fmt.Printf("Error publishing signal for delete: %v\n", err)
					span.RecordError(err)
					return nil, err
				}
			}
		}

		if document.Associate != nil {
			associatedURI := *document.Associate
			associatedSD, ok := sd.References[associatedURI]
			if !ok {
				fmt.Printf("Associated document not found in references for remote delete: %v\n", associatedURI)
				span.RecordError(errors.New("associated document not found in references for remote delete"))
				return nil, err
			}

			var associatedDoc concrnt.Document[any]
			err = json.Unmarshal([]byte(associatedSD.Document), &associatedDoc)
			if err != nil {
				fmt.Printf("Error unmarshaling associated document for signal: %v\n", err)
				span.RecordError(err)
				return nil, err
			}

			destinations := []string{}
			if associatedDoc.Distributes != nil {
				destinations = append(destinations, *associatedDoc.Distributes...)
			}
			destinations = append(destinations, associatedURI)

			for _, dest := range destinations {
				host, err := uc.client.ResolveResourceHost(ctx, dest)
				if err != nil {
					fmt.Printf("Error resolving resource host for unassociation signal: %v\n", err)
					span.RecordError(err)
					continue
				}

				if host == uc.config.FQDN { // local
					err = uc.signal.Publish(ctx, dest, concrnt.Event{
						Type: "unassociated",
						URI:  associatedURI,
					})
					if err != nil {
						fmt.Printf("Error publishing signal for unassociation: %v\n", err)
						span.RecordError(err)
						return nil, err
					}
				}
			}
		}

		return &targetSD, nil
	}
}

func (uc *RecordUsecase) createRecord(ctx context.Context, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CreateRecord")
	defer span.End()

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, parsed.CreatedAt).String()

	resultURI, err := uc.repo.CreateRecord(ctx, ip, documentID, sd)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	// signal
	err = uc.signal.Publish(ctx, resultURI, concrnt.Event{
		Type:       "created",
		URI:        resultURI,
		References: map[string]concrnt.SignedDocument{resultURI: sd},
	})
	if err != nil {
		fmt.Printf("Error publishing signal: %v\n", err)
		span.RecordError(err)
		return nil, err
	}

	if parsed.Distributes != nil {
		requesterSD, err := uc.entity.GetSD(ctx, requester.ID, &requester.Domain)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		for _, destURI := range *parsed.Distributes {

			host, err := uc.client.ResolveResourceHost(ctx, destURI)
			if err != nil {
				fmt.Printf("Error resolving resource host for distribution: %v\n", err)
				span.RecordError(err)
				continue
			}

			dest, err := concrnt.ParseCCURI(destURI)
			if err != nil {
				fmt.Printf("Error parsing memberOf URI: %v\n", err)
				span.RecordError(err)
				continue
			}

			key, err := url.JoinPath(destURI, documentID)
			if err != nil {
				fmt.Printf("Error joining path for distribution: %v\n", err)
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

			if host == uc.config.FQDN { // local
				_, err = uc.Commit(ctx, ip, distSD, mode)
				if err != nil {
					fmt.Printf("Error committing local memberOf item: %v\n", err)
					span.RecordError(err)
					continue
				}
			} else { // remote
				if mode != domain.CommitModeExecute {
					continue
				}
				err = uc.client.Commit(ctx, dest.Owner, distSD)
				if err != nil {
					fmt.Printf("Error committing remote memberOf item: %v\n", err)
					span.RecordError(err)
					continue
				}
			}
		}
	}

	return &sd, nil
}

func (uc *RecordUsecase) createAssociation(ctx context.Context, ip string, requester domain.Entity, parsed concrnt.Document[any], sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.CreateAssociation")
	defer span.End()

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, parsed.CreatedAt).String()

	target := *parsed.Associate
	targetURI, err := concrnt.ParseCCURI(*parsed.Associate)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	ccfs := concrnt.ComposeCCURI("ccfs", targetURI.Owner, documentID)

	isLocal, err := uc.entity.IsLocalByCCID(ctx, targetURI.Owner)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	created := false

	requesterSD, err := uc.entity.GetSD(ctx, requester.ID, &requester.Domain)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if isLocal {
		err := uc.repo.CreateAssociation(ctx, ip, documentID, parsed, sd)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		created = true

		for _, destURI := range *parsed.Distributes {

			host, err := uc.client.ResolveResourceHost(ctx, destURI)
			if err != nil {
				fmt.Printf("Error resolving resource host for distribution: %v\n", err)
				span.RecordError(err)
				continue
			}

			dest, err := concrnt.ParseCCURI(destURI)
			if err != nil {
				fmt.Printf("Error parsing memberOf URI: %v\n", err)
				span.RecordError(err)
				continue
			}

			key, err := url.JoinPath(destURI, documentID)
			if err != nil {
				fmt.Printf("Error joining path for distribution: %v\n", err)
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

			if host == uc.config.FQDN { // local
				_, err = uc.Commit(ctx, ip, distSD, mode)
				if err != nil {
					fmt.Printf("Error committing local memberOf item: %v\n", err)
					span.RecordError(err)
					continue
				}
			} else {
				if mode != domain.CommitModeExecute {
					continue
				}
				err = uc.client.Commit(ctx, dest.Owner, distSD)
				if err != nil {
					fmt.Printf("Error committing memberOf item: %v\n", err)
					span.RecordError(err)
					continue
				}
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

			distributions = append(distributions, *targetDoc.Distributes...)
		} else { // あるとき
			dists, err := uc.repo.GetDistributions(ctx, target)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			distributions = append(distributions, dists...)
		}

		for _, channel := range distributions {

			host, err := uc.client.ResolveResourceHost(ctx, channel)
			if err != nil {
				span.RecordError(err)
				continue
			}

			if host == uc.config.FQDN { // local
				err = uc.signal.Publish(ctx, channel, concrnt.Event{
					Type: "associated",
					URI:  target,
					References: map[string]concrnt.SignedDocument{
						ccfs: sd,
					},
				})
				if err != nil {
					span.RecordError(err)
					return nil, err
				}
			} else { // remote
				if !created {
					continue
				}

				sd := concrnt.SignedDocument{
					Document: sd.Document,
					Proof:    sd.Proof,
					References: map[string]concrnt.SignedDocument{
						requester.CCKV(): *requesterSD,
						target:           *targetSD,
					},
				}

				uc.client.Commit(ctx, host, sd)
			}
		}
	}

	return &sd, nil
}

func (uc *RecordUsecase) acknowledge(ctx context.Context, ip string, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.Acknowledge")
	defer span.End()

	var doc concrnt.Document[schemas.Acknowledge]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, doc.CreatedAt).String()

	referrer := GetReferrerFromReferences(sd, requester.CCKV())
	targetUser, err := uc.entity.Get(ctx, *doc.Associate, referrer)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if uc.entity.IsLocal(ctx, requester) || uc.entity.IsLocal(ctx, *targetUser) {
		_, err := uc.repo.Acknowledge(ctx, ip, documentID, sd)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	if !uc.entity.IsLocal(ctx, *targetUser) && mode == domain.CommitModeExecute {

		requesterSD, err := uc.entity.GetSD(ctx, requester.ID, &requester.Domain)
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
		err = uc.client.Commit(ctx, targetUser.Domain, distSD)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	return &sd, nil
}

func (uc *RecordUsecase) unacknowledge(ctx context.Context, ip string, requester domain.Entity, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.UnAcknowledge")
	defer span.End()

	var doc concrnt.Document[schemas.Acknowledge]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, doc.CreatedAt).String()

	targetUser, err := uc.entity.Get(ctx, *doc.Associate, nil)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if uc.entity.IsLocal(ctx, requester) || uc.entity.IsLocal(ctx, *targetUser) {
		err := uc.repo.UnAcknowledge(ctx, ip, documentID, sd)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	if !uc.entity.IsLocal(ctx, *targetUser) && mode == domain.CommitModeExecute {
		requesterSD, err := uc.entity.GetSD(ctx, requester.ID, &requester.Domain)
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
		err = uc.client.Commit(ctx, targetUser.Domain, distSD)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	return &sd, nil
}

func (uc *RecordUsecase) GetSigned(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetSigned")
	defer span.End()

	sd, err := uc.repo.GetSignedDocument(ctx, uri)
	if err != nil {
		return nil, err
	}

	var doc concrnt.Document[any]
	err = json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		return nil, err
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
			This:      doc,
		},
		stack,
		"net.concrnt.core.resolve",
		uri,
	)

	if err != nil {
		return nil, err
	}

	return sd, nil
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

	if prefix != "" && parent != "" {
		return nil, errors.New("prefix and parent cannot be specified at the same time")
	}

	if prefix != "" {
		return uc.repo.QueryByPrefix(ctx, prefix, schema, since, until, limit, order)
	}

	if parent != "" {
		return uc.repo.QueryByParent(ctx, parent, schema, since, until, limit, order)
	}

	return nil, errors.New("either prefix or parent must be specified")
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
