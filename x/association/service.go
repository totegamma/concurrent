package association

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/x/cdid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for association service
type Service interface {
	Create(ctx context.Context, document, signature string) (core.Association, error)
	Delete(ctx context.Context, document, signature string) (core.Association, error)

	Get(ctx context.Context, id string) (core.Association, error)
	GetOwn(ctx context.Context, author string) ([]core.Association, error)
	GetByTarget(ctx context.Context, targetID string) ([]core.Association, error)
	GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error)
	GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error)
	GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error)
	GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error)
	GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	repo     Repository
	client   client.Client
	entity   entity.Service
	timeline timeline.Service
	message  message.Service
	key      key.Service
	config   util.Config
}

// NewService creates a new association service
func NewService(
	repo Repository,
	client client.Client,
	entity entity.Service,
	timeline timeline.Service,
	message message.Service,
	key key.Service,
	config util.Config,
) Service {
	return &service{
		repo,
		client,
		entity,
		timeline,
		message,
		key,
		config,
	}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// PostAssociation creates a new association
// If targetType is messages, it also posts the association to the target message's timelines
// returns the created association
func (s *service) Create(ctx context.Context, document string, signature string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServicePostAssociation")
	defer span.End()

	created := core.Association{}

	var doc core.CreateAssociation[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	hash := util.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := "a" + cdid.New(hash10, signedAt).String()

	owner, err := s.entity.Get(ctx, doc.Owner)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	if owner.Domain == s.config.Concurrent.FQDN { // signerが自ドメイン管轄の場合、リソースを作成
		association := core.Association{
			ID:        id,
			Author:    doc.Signer,
			Owner:     doc.Owner,
			Schema:    doc.Schema,
			TargetID:  doc.Target,
			Document:  document,
			Signature: signature,
			Timelines: doc.Timelines,
			Variant:   doc.Variant,
		}

		created, err = s.repo.Create(ctx, association)
		if err != nil {
			span.RecordError(err)
			return created, err // TODO: if err is duplicate key error, server should return 409
		}
	}

	if doc.Target[0] == 'm' {
		destinations := make(map[string][]string)
		for _, timeline := range doc.Timelines {
			normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
			if err != nil {
				span.RecordError(err)
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
			destinations[domain] = append(destinations[domain], timeline)
		}

		for domain, timelines := range destinations {
			if domain == s.config.Concurrent.FQDN {
				// localなら、timelineのエントリを生成→Eventを発行
				for _, timeline := range timelines {
					posted, err := s.timeline.PostItem(ctx, timeline, core.TimelineItem{
						ObjectID: created.ID,
						Owner:    created.Owner,
						Author:   &created.Author,
					}, document, signature)
					if err != nil {
						span.RecordError(err)
						continue
					}

					event := core.Event{
						TimelineID: timeline,
						Item:       posted,
						Document:   document,
						Signature:  signature,
					}

					err = s.timeline.PublishEvent(ctx, event)
					if err != nil {
						slog.ErrorContext(ctx, "failed to publish event", slog.String("error", err.Error()), slog.String("module", "timeline"))
						span.RecordError(err)
						continue
					}
				}
			} else if owner.Domain == s.config.Concurrent.FQDN { // ここでリソースを作成したなら、リモートにもリレー
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
				s.client.Commit(ctx, domain, string(packetStr))
			}
		}

		// Associationだけの追加対応
		// メッセージの場合は、ターゲットのタイムラインにも追加する
		targetMessage, err := s.message.Get(ctx, created.TargetID, doc.Signer) //NOTE: これはownerのドメインしか実行できない
		if err != nil {
			span.RecordError(err)
			return created, err
		}

		for _, timeline := range targetMessage.Timelines {
			normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
			if err != nil {
				span.RecordError(err)
				continue
			}
			split := strings.Split(normalized, "@")
			if len(split) != 2 {
				span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
				continue
			}
			domain := split[1]
			if domain == s.config.Concurrent.FQDN {
				event := core.Event{
					TimelineID: timeline,
					Document:   document,
					Signature:  signature,
					Resource:   created,
				}
				err := s.timeline.PublishEvent(ctx, event)
				if err != nil {
					slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
					span.RecordError(err)
					return created, err
				}
			} else {
				documentObj := core.EventDocument{
					TimelineID: timeline,
					Document:   document,
					Signature:  signature,
					Resource:   created,
				}

				document, err := json.Marshal(documentObj)
				if err != nil {
					span.RecordError(err)
					return created, err
				}

				signatureBytes, err := util.SignBytes([]byte(document), s.config.Concurrent.PrivateKey)
				if err != nil {
					span.RecordError(err)
					return created, err
				}

				signature := hex.EncodeToString(signatureBytes)

				packetObj := core.Commit{
					Document:  string(document),
					Signature: signature,
				}

				packet, err := json.Marshal(packetObj)
				if err != nil {
					span.RecordError(err)
					return created, err
				}

				s.client.Commit(ctx, domain, string(packet))
			}
		}
	}

	return created, nil
}

// Get returns an association by ID
func (s *service) Get(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repo.Get(ctx, id)
}

// GetOwn returns associations by author
func (s *service) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetOwn")
	defer span.End()

	return s.repo.GetOwn(ctx, author)
}

// Delete deletes an association by ID
func (s *service) Delete(ctx context.Context, document, signature string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	targetAssociation, err := s.repo.Get(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	requester := doc.Signer

	targetMessage, err := s.message.Get(ctx, targetAssociation.TargetID, requester)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if (targetAssociation.Author != requester) && (targetMessage.Author != requester) {
		return core.Association{}, fmt.Errorf("you are not authorized to perform this action")
	}

	deleted, err := s.repo.Delete(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	for _, posted := range targetAssociation.Timelines {
		event := core.Event{
			TimelineID: posted,
			Document:   document,
			Signature:  signature,
		}
		err := s.timeline.PublishEvent(ctx, event)
		if err != nil {
			slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
			return deleted, err
		}
	}

	if deleted.TargetID[0] == 'm' { // distribute is needed only when targetType is messages
		for _, timeline := range targetMessage.Timelines {

			normalized, err := s.timeline.NormalizeTimelineID(ctx, timeline)
			if err != nil {
				span.RecordError(err)
				continue
			}
			split := strings.Split(normalized, "@")
			if len(split) != 2 {
				span.RecordError(fmt.Errorf("invalid timeline id: %s", normalized))
				continue
			}
			domain := split[1]

			if domain == s.config.Concurrent.FQDN {
				event := core.Event{
					TimelineID: timeline,
					Document:   document,
					Signature:  signature,
					Resource:   deleted,
				}
				err := s.timeline.PublishEvent(ctx, event)
				if err != nil {
					slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
					span.RecordError(err)
					return deleted, err
				}
			} else {
				documentObj := core.EventDocument{
					TimelineID: timeline,
					Document:   document,
					Signature:  signature,
					Resource:   deleted,
				}

				document, err := json.Marshal(documentObj)
				if err != nil {
					span.RecordError(err)
					return deleted, err
				}

				signatureBytes, err := util.SignBytes([]byte(document), s.config.Concurrent.PrivateKey)
				if err != nil {
					span.RecordError(err)
					return deleted, err
				}

				signature := hex.EncodeToString(signatureBytes)

				packetObj := core.Commit{
					Document:  string(document),
					Signature: signature,
				}

				packet, err := json.Marshal(packetObj)
				if err != nil {
					span.RecordError(err)
					return deleted, err
				}

				s.client.Commit(ctx, domain, string(packet))
			}
		}
	}

	return deleted, nil
}

// GetByTarget returns associations by target
func (s *service) GetByTarget(ctx context.Context, targetID string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByTarget")
	defer span.End()

	return s.repo.GetByTarget(ctx, targetID)
}

// GetCountsBySchema returns the number of associations by schema
func (s *service) GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetCountsBySchema")
	defer span.End()

	return s.repo.GetCountsBySchema(ctx, messageID)
}

// GetBySchema returns associations by schema and variant
func (s *service) GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetBySchema")
	defer span.End()

	return s.repo.GetBySchema(ctx, messageID, schema)
}

// GetCountsBySchemaAndVariant returns the number of associations by schema and variant
func (s *service) GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetCountsBySchemaAndVariant")
	defer span.End()

	return s.repo.GetCountsBySchemaAndVariant(ctx, messageID, schema)
}

// GetBySchemaAndVariant returns associations by schema and variant
func (s *service) GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetBySchemaAndVariant")
	defer span.End()

	return s.repo.GetBySchemaAndVariant(ctx, messageID, schema, variant)
}

// GetOwnByTarget returns associations by target and author
func (s *service) GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetOwnByTarget")
	defer span.End()

	return s.repo.GetOwnByTarget(ctx, targetID, author)
}
