package association

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/policy"
)

type service struct {
	repo         Repository
	client       client.Client
	entity       core.EntityService
	domain       core.DomainService
	profile      core.ProfileService
	timeline     core.TimelineService
	subscription core.SubscriptionService
	message      core.MessageService
	key          core.KeyService
	policy       core.PolicyService
	config       core.Config
}

// NewService creates a new association service
func NewService(
	repo Repository,
	client client.Client,
	entity core.EntityService,
	domain core.DomainService,
	profile core.ProfileService,
	timeline core.TimelineService,
	subscription core.SubscriptionService,
	message core.MessageService,
	key core.KeyService,
	policy core.PolicyService,
	config core.Config,
) core.AssociationService {
	return &service{
		repo,
		client,
		entity,
		domain,
		profile,
		timeline,
		subscription,
		message,
		key,
		policy,
		config,
	}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Count")
	defer span.End()

	return s.repo.Count(ctx)
}

func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Association.Service.Clean")
	defer span.End()

	err := s.repo.Clean(ctx, ccid)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

// PostAssociation creates a new association
// If targetType is messages, it also posts the association to the target message's timelines
// returns the created association
func (s *service) Create(ctx context.Context, mode core.CommitMode, document string, signature string) (core.Association, []string, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Create")
	defer span.End()

	var doc core.AssociationDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	hash := core.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := "a" + cdid.New(hash10, signedAt).String()

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	isLocalEntry := false

	if core.IsCCID(doc.Owner) {
		ownerEntity, err := s.entity.Get(ctx, doc.Owner)
		if err != nil {
			span.RecordError(err)
			return core.Association{}, []string{}, err
		}
		if ownerEntity.Domain == s.config.FQDN {
			isLocalEntry = true
		}
	} else if core.IsCSID(doc.Owner) {
		if doc.Owner == s.config.CSID {
			isLocalEntry = true
		}
	} else {
		return core.Association{}, []string{}, errors.New("invalid owner")
	}

	bodyStr, err := json.Marshal(doc.Body)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	uniqueKey := doc.Signer + doc.Schema + doc.Target + doc.Variant + string(bodyStr)
	uniqueHash := core.GetHash([]byte(uniqueKey))
	unique := hex.EncodeToString(uniqueHash[:16])

	association := core.Association{
		ID:        id,
		Author:    doc.Signer,
		Owner:     doc.Owner,
		Schema:    doc.Schema,
		Target:    doc.Target,
		Document:  document,
		Signature: signature,
		Timelines: doc.Timelines,
		Variant:   doc.Variant,
		Unique:    unique,
	}

	if isLocalEntry { // signerが自ドメイン管轄の場合、リソースを作成

		switch doc.Target[0] {
		case 'm': // message
			target, err := s.message.GetAsUser(ctx, association.Target, signer)
			if err != nil {
				span.RecordError(err)
				return core.Association{}, []string{}, err
			}

			timelinePolicyResults := make([]core.PolicyEvalResult, len(target.Timelines))
			for i, timelineID := range target.Timelines {
				timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					continue
				}

				var params map[string]any = make(map[string]any)
				if timeline.PolicyParams != nil {
					json.Unmarshal([]byte(*timeline.PolicyParams), &params)
				}

				result, err := s.policy.TestWithPolicyURL(
					ctx,
					timeline.Policy,
					core.RequestContext{
						Self:     timeline,
						Params:   params,
						Document: doc,
					},
					"timeline.message.association.attach",
				)
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					timelinePolicyResults[i] = core.PolicyEvalResultDefault
					continue
				}

				timelinePolicyResults[i] = result
			}

			timelinePolicyResult := policy.AccumulateOr(timelinePolicyResults)
			timelinePolicyIsDominant, timlinePolicyAllowed := policy.IsDominant(timelinePolicyResult)
			if timelinePolicyIsDominant && !timlinePolicyAllowed {
				return association, []string{}, core.ErrorPermissionDenied{}
			}

			var params map[string]any = make(map[string]any)
			if target.PolicyParams != nil {
				json.Unmarshal([]byte(*target.PolicyParams), &params)
			}

			messagePolicyResult, err := s.policy.TestWithPolicyURL(
				ctx,
				target.Policy,
				core.RequestContext{
					Requester: signer,
					Self:      target,
					Params:    params,
					Document:  doc,
				},
				"message.association.attach",
			)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())

			}

			result := s.policy.Summerize([]core.PolicyEvalResult{timelinePolicyResult, messagePolicyResult}, "message.association.attach", nil)
			if !result {
				return association, []string{}, core.ErrorPermissionDenied{}
			}

		case 'p': // profile
			target, err := s.profile.Get(ctx, association.Target)
			if err != nil {
				span.RecordError(err)
				return association, []string{}, err
			}

			var params map[string]any = make(map[string]any)
			if target.PolicyParams != nil {
				err := json.Unmarshal([]byte(*target.PolicyParams), &params)
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					span.RecordError(err)
				}
			}

			policyEvalResult, err := s.policy.TestWithPolicyURL(
				ctx,
				target.Policy,
				core.RequestContext{
					Requester: signer,
					Self:      target,
					Params:    params,
					Document:  doc,
				},
				"profile.association.attach",
			)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
			}

			result := s.policy.Summerize([]core.PolicyEvalResult{policyEvalResult}, "profile.association.attach", nil)
			if !result {
				return association, []string{}, core.ErrorPermissionDenied{}
			}

		case 't': // timeline
			target, err := s.timeline.GetTimeline(ctx, association.Target)
			if err != nil {
				span.RecordError(err)
				return association, []string{}, err
			}

			var params map[string]any = make(map[string]any)
			if target.PolicyParams != nil {
				err := json.Unmarshal([]byte(*target.PolicyParams), &params)
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					span.RecordError(err)
				}
			}

			policyEvalResult, err := s.policy.TestWithPolicyURL(
				ctx,
				target.Policy,
				core.RequestContext{
					Requester: signer,
					Self:      target,
					Params:    params,
					Document:  doc,
				},
				"timeline.association.attach",
			)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
			}

			result := s.policy.Summerize([]core.PolicyEvalResult{policyEvalResult}, "timeline.association.attach", nil)
			if !result {
				return association, []string{}, core.ErrorPermissionDenied{}
			}

		case 's': // subscription
			target, err := s.subscription.GetSubscription(ctx, association.Target)
			if err != nil {
				span.RecordError(err)
				return association, []string{}, err
			}

			var params map[string]any = make(map[string]any)
			if target.PolicyParams != nil {
				err := json.Unmarshal([]byte(*target.PolicyParams), &params)
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					span.RecordError(err)
				}
			}

			policyEvalResult, err := s.policy.TestWithPolicyURL(
				ctx,
				target.Policy,
				core.RequestContext{
					Requester: signer,
					Self:      target,
					Params:    params,
					Document:  doc,
				},
				"subscription.association.attach",
			)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
			}

			result := s.policy.Summerize([]core.PolicyEvalResult{policyEvalResult}, "subscription.association.attach", nil)
			if !result {
				return association, []string{}, core.ErrorPermissionDenied{}
			}
		}

		association, err = s.repo.Create(ctx, association)
		if err != nil {
			if errors.Is(err, core.ErrorAlreadyExists{}) {
				return association, []string{}, core.NewErrorAlreadyExists()
			}
			span.RecordError(err)
			return association, []string{}, err
		}
	}

	destinations := make(map[string][]string)
	for _, timeline := range doc.Timelines {
		normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
		if err != nil {
			span.RecordError(err)
			continue
		}
		split := strings.Split(normalized, "@")
		if len(split) <= 1 {
			span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
			continue
		}
		domain := split[len(split)-1]

		if _, ok := destinations[domain]; !ok {
			destinations[domain] = []string{}
		}
		destinations[domain] = append(destinations[domain], timeline)
	}

	for domain, timelines := range destinations {
		if domain == s.config.FQDN {
			// localなら、timelineのエントリを生成→Eventを発行
			for _, timeline := range timelines {

				posted, err := s.timeline.PostItem(ctx, timeline, core.TimelineItem{
					ResourceID: association.ID,
					Owner:      association.Owner,
					Author:     &association.Author,
					Schema:     association.Schema,
				}, document, signature)
				if err != nil {
					span.RecordError(err)
					continue
				}

				event := core.Event{
					Timeline:  timeline,
					Item:      &posted,
					Document:  document,
					Signature: signature,
					Resource:  association,
				}

				err = s.timeline.PublishEvent(ctx, event)
				if err != nil {
					slog.ErrorContext(ctx, "failed to publish event", slog.String("error", err.Error()), slog.String("module", "timeline"))
					span.RecordError(err)
					continue
				}
			}
		} else if isLocalEntry && mode != core.CommitModeLocalOnlyExec { // ここでリソースを作成したなら、リモートにもリレー
			// send to remote
			packet := core.Commit{
				Document:  document,
				Signature: signature,
			}

			packetStr, err := json.Marshal(packet)
			if err != nil {
				span.RecordError(err)
				continue
			}

			_, err = s.domain.GetByFQDN(ctx, domain)
			if err != nil {
				span.RecordError(err)
				continue
			}

			s.client.Commit(ctx, domain, string(packetStr), nil, nil)
		}
	}

	if doc.Target[0] == 'm' {
		// Associationだけの追加対応
		// メッセージの場合は、ターゲットのタイムラインにも追加する
		if isLocalEntry && mode != core.CommitModeLocalOnlyExec {
			targetMessage, err := s.message.GetAsUser(ctx, association.Target, signer)
			if err != nil {
				span.RecordError(err)
				return association, []string{}, err
			}

			for _, timeline := range targetMessage.Timelines {
				normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
				if err != nil {
					span.RecordError(err)
					continue
				}
				split := strings.Split(normalized, "@")
				if len(split) <= 1 {
					span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
					continue
				}
				domain := split[len(split)-1]
				if domain == s.config.FQDN {
					event := core.Event{
						Timeline:  timeline,
						Document:  document,
						Signature: signature,
						Resource:  association,
					}
					err := s.timeline.PublishEvent(ctx, event)
					if err != nil {
						slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
						span.RecordError(err)
						return association, []string{}, err
					}
				} else {
					documentObj := core.EventDocument{
						Timeline:  timeline,
						Document:  document,
						Signature: signature,
						Resource:  association,
						DocumentBase: core.DocumentBase[any]{
							Signer:   s.config.CCID,
							Type:     "event",
							SignedAt: time.Now(),
						},
					}

					document, err := json.Marshal(documentObj)
					if err != nil {
						span.RecordError(err)
						return association, []string{}, err
					}

					signatureBytes, err := core.SignBytes([]byte(document), s.config.PrivateKey)
					if err != nil {
						span.RecordError(err)
						return association, []string{}, err
					}

					signature := hex.EncodeToString(signatureBytes)

					packetObj := core.Commit{
						Document:  string(document),
						Signature: signature,
					}

					packet, err := json.Marshal(packetObj)
					if err != nil {
						span.RecordError(err)
						return association, []string{}, err
					}

					s.client.Commit(ctx, domain, string(packet), nil, nil)
				}
			}
		}
	}

	affected, err := s.timeline.GetOwners(ctx, doc.Timelines)
	if err != nil {
		span.RecordError(err)
	}

	return association, affected, nil
}

// Get returns an association by ID
func (s *service) Get(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Get")
	defer span.End()

	return s.repo.Get(ctx, id)
}

// GetOwn returns associations by author
func (s *service) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetOwn")
	defer span.End()

	return s.repo.GetOwn(ctx, author)
}

// Delete deletes an association by ID
func (s *service) Delete(ctx context.Context, mode core.CommitMode, document, signature string) (core.Association, []string, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Delete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	targetAssociation, err := s.repo.Get(ctx, doc.Target)
	if err != nil {
		if errors.Is(err, core.ErrorNotFound{}) {
			return core.Association{}, []string{}, core.NewErrorAlreadyDeleted()
		}

		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	result, err := s.policy.TestWithPolicyURL(
		ctx,
		"",
		core.RequestContext{
			Requester: signer,
			Self:      targetAssociation,
			Document:  doc,
		},
		"association.delete",
	)

	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	finally := s.policy.Summerize([]core.PolicyEvalResult{result}, "association.delete", nil)
	if !finally {
		return core.Association{}, []string{}, core.ErrorPermissionDenied{}
	}

	err = s.repo.Delete(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, []string{}, err
	}

	err = s.timeline.RemoveItemsByResourceID(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
	}

	for _, posted := range targetAssociation.Timelines {
		event := core.Event{
			Timeline:  posted,
			Document:  document,
			Signature: signature,
			Resource:  targetAssociation,
		}
		err := s.timeline.PublishEvent(ctx, event)
		if err != nil {
			slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
			return targetAssociation, []string{}, err
		}
	}

	if targetAssociation.Target[0] == 'm' && mode != core.CommitModeLocalOnlyExec { // distribute is needed only when targetType is messages

		targetMessage, err := s.message.GetAsUser(ctx, targetAssociation.Target, signer)
		if err != nil {
			span.RecordError(err)
			return core.Association{}, []string{}, err
		}

		for _, timeline := range targetMessage.Timelines {

			normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
			if err != nil {
				span.RecordError(err)
				continue
			}
			split := strings.Split(normalized, "@")
			if len(split) <= 1 {
				span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
				continue
			}
			domain := split[len(split)-1]

			if domain == s.config.FQDN {
				event := core.Event{
					Timeline:  timeline,
					Document:  document,
					Signature: signature,
					Resource:  targetAssociation,
				}
				err := s.timeline.PublishEvent(ctx, event)
				if err != nil {
					slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
					span.RecordError(err)
					return targetAssociation, []string{}, err
				}
			} else {
				documentObj := core.EventDocument{
					Timeline:  timeline,
					Document:  document,
					Signature: signature,
					Resource:  targetAssociation,
				}

				document, err := json.Marshal(documentObj)
				if err != nil {
					span.RecordError(err)
					return targetAssociation, []string{}, err
				}

				signatureBytes, err := core.SignBytes([]byte(document), s.config.PrivateKey)
				if err != nil {
					span.RecordError(err)
					return targetAssociation, []string{}, err
				}

				signature := hex.EncodeToString(signatureBytes)

				packetObj := core.Commit{
					Document:  string(document),
					Signature: signature,
				}

				packet, err := json.Marshal(packetObj)
				if err != nil {
					span.RecordError(err)
					return targetAssociation, []string{}, err
				}

				s.client.Commit(ctx, domain, string(packet), nil, nil)
			}
		}
	}

	affected, err := s.timeline.GetOwners(ctx, targetAssociation.Timelines)
	if err != nil {
		span.RecordError(err)
	}

	return targetAssociation, affected, nil
}

// GetByTarget returns associations by target
func (s *service) GetByTarget(ctx context.Context, targetID string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetByTarget")
	defer span.End()

	return s.repo.GetByTarget(ctx, targetID)
}

// GetCountsBySchema returns the number of associations by schema
func (s *service) GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetCountsBySchema")
	defer span.End()

	return s.repo.GetCountsBySchema(ctx, messageID)
}

// GetBySchema returns associations by schema and variant
func (s *service) GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetBySchema")
	defer span.End()

	return s.repo.GetBySchema(ctx, messageID, schema)
}

// GetCountsBySchemaAndVariant returns the number of associations by schema and variant
func (s *service) GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetCountsBySchemaAndVariant")
	defer span.End()

	return s.repo.GetCountsBySchemaAndVariant(ctx, messageID, schema)
}

// GetBySchemaAndVariant returns associations by schema and variant
func (s *service) GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetBySchemaAndVariant")
	defer span.End()

	return s.repo.GetBySchemaAndVariant(ctx, messageID, schema, variant)
}

// GetOwnByTarget returns associations by target and author
func (s *service) GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.GetOwnByTarget")
	defer span.End()

	return s.repo.GetOwnByTarget(ctx, targetID, author)
}
