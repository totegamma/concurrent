package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/policy"
)

type service struct {
	repo     Repository
	client   client.Client
	entity   core.EntityService
	domain   core.DomainService
	timeline core.TimelineService
	key      core.KeyService
	policy   core.PolicyService
	config   core.Config
}

// NewService creates a new message service
func NewService(
	repo Repository,
	client client.Client,
	entity core.EntityService,
	domain core.DomainService,
	timeline core.TimelineService,
	key core.KeyService,
	policy core.PolicyService,
	config core.Config,
) core.MessageService {
	return &service{
		repo,
		client,
		entity,
		domain,
		timeline,
		key,
		policy,
		config,
	}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Count")
	defer span.End()

	return s.repo.Count(ctx)
}

func (s *service) isMessagePublic(ctx context.Context, message core.Message) (bool, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.isMessagePublic")
	defer span.End()

	var defaults map[string]bool
	if message.PolicyDefaults != nil {
		json.Unmarshal([]byte(*message.PolicyDefaults), &defaults)
	}

	// timeline policy check
	timelinePolicyResults := make([]core.PolicyEvalResult, len(message.Timelines))
	for i, timelineID := range message.Timelines {
		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}

		var params map[string]any = make(map[string]any)
		if timeline.PolicyParams != nil {
			err := json.Unmarshal([]byte(*timeline.PolicyParams), &params)
			if err != nil {
				span.RecordError(err)
				timelinePolicyResults[i] = core.PolicyEvalResultError
				continue
			}
		}

		result, err := s.policy.TestWithPolicyURL(
			ctx,
			timeline.Policy,
			core.RequestContext{
				Self:   timeline,
				Params: params,
			},
			"timeline.message.read",
		)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}

		timelinePolicyResults[i] = result
	}

	timelinePolicyResult := s.policy.AccumulateOr(timelinePolicyResults, "timeline.message.read", &defaults)
	timelinePolicyIsDominant, timelinePolicyAllowed := policy.IsDominant(timelinePolicyResult)
	if timelinePolicyIsDominant && !timelinePolicyAllowed {
		return false, nil
	}

	// message policy check
	messagePolicyResult := core.PolicyEvalResultDefault

	var err error
	var params map[string]any = make(map[string]any)
	if message.PolicyParams != nil {
		json.Unmarshal([]byte(*message.PolicyParams), &params)
	}

	messagePolicyResult, err = s.policy.TestWithPolicyURL(
		ctx,
		message.Policy,
		core.RequestContext{
			Self:   message,
			Params: params,
		},
		"message.read",
	)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	// accumulate polies
	result := s.policy.Summerize([]core.PolicyEvalResult{timelinePolicyResult, messagePolicyResult}, "message.read", &defaults)
	if !result {
		return false, nil
	}

	return true, nil
}

// Get returns a message by ID
func (s *service) GetAsGuest(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.GetAsGuest")
	defer span.End()

	message, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	isPublic, err := s.isMessagePublic(ctx, message)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if !isPublic {
		return core.Message{}, fmt.Errorf("no read access")
	}

	return message, nil
}

func (s *service) GetAsUser(ctx context.Context, id string, requester core.Entity) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.GetAsUser")
	defer span.End()

	message, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	var defaults map[string]bool
	if message.PolicyDefaults != nil {
		json.Unmarshal([]byte(*message.PolicyDefaults), &defaults)
	}

	timelinePolicyResults := make([]core.PolicyEvalResult, len(message.Timelines))
	for i, timelineID := range message.Timelines {
		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}

		var params map[string]any = make(map[string]any)
		if timeline.PolicyParams != nil {
			err := json.Unmarshal([]byte(*timeline.PolicyParams), &params)
			if err != nil {
				span.RecordError(err)
				timelinePolicyResults[i] = core.PolicyEvalResultError
				continue
			}
		}

		result, err := s.policy.TestWithPolicyURL(
			ctx,
			timeline.Policy,
			core.RequestContext{
				Self:      timeline,
				Params:    params,
				Requester: requester,
			},
			"timeline.message.read",
		)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}
		timelinePolicyResults[i] = result
	}

	timelinePolicyResult := s.policy.AccumulateOr(timelinePolicyResults, "timeline.message.read", &defaults)
	timelinePolicyIsDominant, timelinePolicyAllowed := policy.IsDominant(timelinePolicyResult)
	if timelinePolicyIsDominant && !timelinePolicyAllowed {
		return core.Message{}, fmt.Errorf("no read access")
	}

	messagePolicyResult := core.PolicyEvalResultDefault

	var params map[string]any = make(map[string]any)
	if message.PolicyParams != nil {
		json.Unmarshal([]byte(*message.PolicyParams), &params)
	}

	requestContext := core.RequestContext{
		Self:      message,
		Params:    params,
		Requester: requester,
	}

	messagePolicyResult, err = s.policy.TestWithPolicyURL(ctx, message.Policy, requestContext, "message.read")
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	result := s.policy.Summerize([]core.PolicyEvalResult{timelinePolicyResult, messagePolicyResult}, "message.read", &defaults)
	if !result {
		return core.Message{}, fmt.Errorf("no read access")
	}

	return message, nil
}

// GetWithOwnAssociations returns a message by ID with associations
func (s *service) GetWithOwnAssociations(ctx context.Context, id string, requester string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.GetWithOwnAssociations")
	defer span.End()

	message, err := s.repo.GetWithOwnAssociations(ctx, id, requester)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	var defaults map[string]bool
	if message.PolicyDefaults != nil {
		json.Unmarshal([]byte(*message.PolicyDefaults), &defaults)
	}

	requesterEntity, err := s.entity.Get(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	timelinePolicyResults := make([]core.PolicyEvalResult, len(message.Timelines))
	for i, timelineID := range message.Timelines {
		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}

		var params map[string]any = make(map[string]any)
		if timeline.PolicyParams != nil {
			err := json.Unmarshal([]byte(*timeline.PolicyParams), &params)
			if err != nil {
				span.RecordError(err)
				timelinePolicyResults[i] = core.PolicyEvalResultError
				continue
			}
		}

		result, err := s.policy.TestWithPolicyURL(
			ctx,
			timeline.Policy,
			core.RequestContext{
				Self:      timeline,
				Params:    params,
				Requester: requesterEntity,
			},
			"timeline.message.read",
		)
		if err != nil {
			span.RecordError(err)
			timelinePolicyResults[i] = core.PolicyEvalResultError
			continue
		}
		timelinePolicyResults[i] = result
	}

	timelinePolicyResult := s.policy.AccumulateOr(timelinePolicyResults, "timeline.message.read", &defaults)
	timelinePolicyIsDominant, timelinePolicyAllowed := policy.IsDominant(timelinePolicyResult)
	if timelinePolicyIsDominant && !timelinePolicyAllowed {
		return core.Message{}, fmt.Errorf("no read access")
	}

	messagePolicyResult := core.PolicyEvalResultDefault

	var params map[string]any = make(map[string]any)
	if message.PolicyParams != nil {
		json.Unmarshal([]byte(*message.PolicyParams), &params)
	}

	requestContext := core.RequestContext{
		Self:      message,
		Params:    params,
		Requester: requesterEntity,
	}

	messagePolicyResult, err = s.policy.TestWithPolicyURL(ctx, message.Policy, requestContext, "message.read")
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	result := s.policy.Summerize([]core.PolicyEvalResult{timelinePolicyResult, messagePolicyResult}, "message.read", &defaults)
	if !result {
		return core.Message{}, fmt.Errorf("no read access")
	}

	return message, nil
}

// Create creates a new message
// It also posts the message to the timelines
func (s *service) Create(ctx context.Context, mode core.CommitMode, document string, signature string) (core.Message, []string, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Create")
	defer span.End()

	created := core.Message{}

	var doc core.MessageDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return created, []string{}, err
	}

	hash := core.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := "m" + cdid.New(hash10, signedAt).String()

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	var policyparams *string = nil
	if doc.PolicyParams != "" {
		policyparams = &doc.PolicyParams
	}

	var policydefaults *string = nil
	if doc.PolicyDefaults != "" {
		policydefaults = &doc.PolicyDefaults
	}

	if signer.Domain == s.config.FQDN { // signerが自ドメイン管轄の場合、リソースを作成

		message := core.Message{
			ID:             id,
			Author:         doc.Signer,
			Schema:         doc.Schema,
			Policy:         doc.Policy,
			PolicyParams:   policyparams,
			PolicyDefaults: policydefaults,
			Document:       document,
			Signature:      signature,
			Timelines:      doc.Timelines,
		}

		created, err = s.repo.Create(ctx, message)
		if err != nil {
			span.RecordError(err)
			return message, []string{}, err
		}

	}

	destinations := make(map[string][]string)
	for _, timelineID := range doc.Timelines {
		normalized, err := s.timeline.NormalizeTimelineID(ctx, timelineID)
		if err != nil {
			span.RecordError(errors.Wrap(err, "failed to normalize timeline id"))
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
		destinations[domain] = append(destinations[domain], timelineID)
	}

	ispublic, err := s.isMessagePublic(ctx, created)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	sendDocument := ""
	sendSignature := ""
	var sendResource *core.Message
	if ispublic {
		sendDocument = document
		sendSignature = signature
		sendResource = &created
	}

	for domain, timelines := range destinations {
		if domain == s.config.FQDN {
			// localなら、timelineのエントリを生成→Eventを発行
			for _, timeline := range timelines {

				timelineItem := core.TimelineItem{
					ResourceID: id,
					Owner:      doc.Signer,
					TimelineID: timeline,
					Schema:     doc.Schema,
				}

				if !doc.SignedAt.IsZero() {
					timelineItem.CDate = doc.SignedAt
				}

				posted, err := s.timeline.PostItem(ctx, timeline, timelineItem, sendDocument, sendSignature)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to post item"))
					continue
				}

				if mode == core.CommitModeExecute {
					// eventを放流
					event := core.Event{
						Timeline:  timeline,
						Item:      &posted,
						Document:  sendDocument,
						Signature: sendSignature,
						Resource:  sendResource,
					}

					err = s.timeline.PublishEvent(ctx, event)
					if err != nil {
						slog.ErrorContext(ctx, "failed to publish event", slog.String("error", err.Error()), slog.String("module", "timeline"))
						span.RecordError(errors.Wrap(err, "failed to publish event"))
						continue
					}
				}
			}
		} else if signer.Domain == s.config.FQDN && mode != core.CommitModeLocalOnlyExec { // ここでリソースを作成したなら、リモートにもリレー
			// remoteならdocumentをリレー
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

	// TODO: 実際に送信できたstreamの一覧を返すべき

	affected, err := s.timeline.GetOwners(ctx, doc.Timelines)
	if err != nil {
		span.RecordError(err)
	}

	if !slices.Contains(affected, doc.Signer) {
		affected = append(affected, doc.Signer)
	}

	return created, affected, nil
}

// Delete deletes a message by ID
// It also emits a delete event to the sockets
func (s *service) Delete(ctx context.Context, mode core.CommitMode, document, signature string) (core.Message, []string, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Delete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	deleteTarget, err := s.repo.Get(ctx, doc.Target)
	if err != nil {
		if errors.Is(err, core.ErrorNotFound{}) {
			return core.Message{}, []string{}, core.NewErrorAlreadyDeleted()
		}
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	var params map[string]any = make(map[string]any)
	if deleteTarget.PolicyParams != nil {
		err := json.Unmarshal([]byte(*deleteTarget.PolicyParams), &params)
		if err != nil {
			span.RecordError(err)
			return core.Message{}, []string{}, err
		}
	}

	result, err := s.policy.TestWithPolicyURL(
		ctx,
		deleteTarget.Policy,
		core.RequestContext{
			Self:     deleteTarget,
			Params:   params,
			Document: doc,
		},
		"message.delete",
	)

	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	finally := s.policy.Summerize([]core.PolicyEvalResult{result}, "message.delete", nil)
	if !finally {
		return core.Message{}, []string{}, core.ErrorPermissionDenied{}
	}

	err = s.repo.Delete(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	err = s.timeline.RemoveItemsByResourceID(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
	}

	ispublic, err := s.isMessagePublic(ctx, deleteTarget)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, []string{}, err
	}

	var publicResource *core.Message = nil
	if ispublic {
		publicResource = &deleteTarget
	}

	if mode != core.CommitModeLocalOnlyExec {
		for _, desttimeline := range deleteTarget.Timelines {
			event := core.Event{
				Timeline:  desttimeline,
				Document:  document,
				Signature: signature,
				Resource:  publicResource,
			}
			err := s.timeline.PublishEvent(ctx, event)
			if err != nil {
				span.RecordError(err)
				return deleteTarget, []string{}, err
			}
		}
	}

	affected, err := s.timeline.GetOwners(ctx, deleteTarget.Timelines)
	if err != nil {
		span.RecordError(err)
	}

	return deleteTarget, affected, err
}

func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Message.Service.Clean")
	defer span.End()

	return s.repo.Clean(ctx, ccid)
}
