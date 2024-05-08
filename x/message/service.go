package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

type service struct {
	repo     Repository
	client   client.Client
	entity   core.EntityService
	timeline core.TimelineService
	key      core.KeyService
	policy   core.PolicyService
	config   core.Config
}

// NewService creates a new message service
func NewService(repo Repository, client client.Client, entity core.EntityService, timeline core.TimelineService, key core.KeyService, policy core.PolicyService, config core.Config) core.MessageService {
	return &service{repo, client, entity, timeline, key, policy, config}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Count")
	defer span.End()

	return s.repo.Count(ctx)
}

// Get returns a message by ID
func (s *service) Get(ctx context.Context, id string, requester string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Get")
	defer span.End()

	message, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	canRead := false
	for _, timelineID := range message.Timelines {
		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			continue
		}

		if timeline.Policy == "" {
			canRead = true
			break
		}

		action, ok := ctx.Value(core.RequestPathCtxKey).(string)
		if !ok {
			span.RecordError(fmt.Errorf("action not found"))
			return core.Message{}, fmt.Errorf("invalid action")
		}

		ok, err = s.policy.HasNoRulesWithPolicyURL(ctx, timeline.Policy, action)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			continue
		}

		if ok {
			canRead = true
			break
		}

	}
	if !canRead {
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

	requesterEntity, err := s.entity.Get(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	canRead := false
	for _, timelineID := range message.Timelines {
		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			continue
		}

		if timeline.Policy == "" {
			canRead = true
			break
		}

		var params map[string]any
		if timeline.PolicyParams != nil {
			err := json.Unmarshal([]byte(*timeline.PolicyParams), &params)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				span.RecordError(err)
				continue
			}
		}

		requestContext := core.RequestContext{
			Self:      timeline,
			Params:    params,
			Requester: requesterEntity,
		}

		action, ok := ctx.Value(core.RequestPathCtxKey).(string)
		if !ok {
			span.RecordError(fmt.Errorf("action not found"))
			return core.Message{}, fmt.Errorf("invalid action")
		}

		ok, err = s.policy.TestWithPolicyURL(ctx, timeline.Policy, requestContext, action)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			continue
		}

		if ok {
			canRead = true
			break
		}

	}
	if !canRead {
		return core.Message{}, fmt.Errorf("no read access")
	}

	return message, nil
}

// Create creates a new message
// It also posts the message to the timelines
func (s *service) Create(ctx context.Context, mode core.CommitMode, document string, signature string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Create")
	defer span.End()

	created := core.Message{}

	var doc core.CreateMessage[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	hash := util.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := "m" + cdid.New(hash10, signedAt).String()

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	var policyparams *string = nil
	if doc.PolicyParams != "" {
		policyparams = &doc.PolicyParams
	}

	if signer.Domain == s.config.FQDN { // signerが自ドメイン管轄の場合、リソースを作成

		message := core.Message{
			ID:           id,
			Author:       doc.Signer,
			Schema:       doc.Schema,
			Policy:       doc.Policy,
			PolicyParams: policyparams,
			Document:     document,
			Signature:    signature,
			Timelines:    doc.Timelines,
		}

		created, err = s.repo.Create(ctx, message)
		if err != nil {
			span.RecordError(err)
			return message, err
		}

	}

	ispublic := false
	destinations := make(map[string][]string)
	for _, timelineID := range doc.Timelines {

		timeline, err := s.timeline.GetTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			continue
		}

		if timeline.Policy == "" {
			ispublic = true
		} else if ispublic == false {
			ok, err := s.policy.HasNoRulesWithPolicyURL(ctx, timeline.Policy, "GET:/message/")
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				continue
			}

			if ok {
				ispublic = true
			}
		}

		normalized, err := s.timeline.NormalizeTimelineID(ctx, timelineID)
		if err != nil {
			span.RecordError(errors.Wrap(err, "failed to normalize timeline id"))
			continue
		}
		split := strings.Split(normalized, "@")
		if len(split) != 2 {
			span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
			continue
		}
		domain := split[1]

		if _, ok := destinations[domain]; !ok {
			destinations[domain] = []string{}
		}
		destinations[domain] = append(destinations[domain], timelineID)
	}

	sendDocument := ""
	sendSignature := ""
	if ispublic {
		sendDocument = document
		sendSignature = signature
	}

	for domain, timelines := range destinations {
		if domain == s.config.FQDN {
			// localなら、timelineのエントリを生成→Eventを発行
			for _, timeline := range timelines {

				timelineItem := core.TimelineItem{
					ResourceID: id,
					Owner:      doc.Signer,
					TimelineID: timeline,
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
						Item:      posted,
						Document:  sendDocument,
						Signature: sendSignature,
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
			s.client.Commit(ctx, domain, string(packetStr))
		}
	}

	// TODO: 実際に送信できたstreamの一覧を返すべき

	return created, nil
}

// Delete deletes a message by ID
// It also emits a delete event to the sockets
func (s *service) Delete(ctx context.Context, mode core.CommitMode, document, signature string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Service.Delete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	deleteTarget, err := s.repo.Get(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if deleteTarget.Author != doc.Signer {
		return core.Message{}, fmt.Errorf("you are not authorized to perform this action")
	}

	deleted, err := s.repo.Delete(ctx, doc.Target)
	slog.DebugContext(ctx, fmt.Sprintf("deleted: %v", deleted), slog.String("module", "message"))

	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if mode != core.CommitModeLocalOnlyExec {
		for _, desttimeline := range deleted.Timelines {
			event := core.Event{
				Timeline:  desttimeline,
				Document:  document,
				Signature: signature,
			}
			err := s.timeline.PublishEvent(ctx, event)
			if err != nil {
				span.RecordError(err)
				return deleted, err
			}
		}
	}

	return deleted, err
}
