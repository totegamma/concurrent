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

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
)

type service struct {
	repo     Repository
	client   client.Client
	entity   core.EntityService
	domain   core.DomainService
	timeline core.TimelineService
	message  core.MessageService
	key      core.KeyService
	config   core.Config
}

// NewService creates a new association service
func NewService(
	repo Repository,
	client client.Client,
	entity core.EntityService,
	domain core.DomainService,
	timeline core.TimelineService,
	message core.MessageService,
	key core.KeyService,
	config core.Config,
) core.AssociationService {
	return &service{
		repo,
		client,
		entity,
		domain,
		timeline,
		message,
		key,
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
func (s *service) Create(ctx context.Context, mode core.CommitMode, document string, signature string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Create")
	defer span.End()

	var doc core.AssociationDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	hash := core.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := "a" + cdid.New(hash10, signedAt).String()

	owner, err := s.entity.Get(ctx, doc.Owner)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	bodyStr, err := json.Marshal(doc.Body)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
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

	if owner.Domain == s.config.FQDN { // signerが自ドメイン管轄の場合、リソースを作成
		association, err = s.repo.Create(ctx, association)
		if err != nil {
			if errors.Is(err, core.ErrorAlreadyExists{}) {
				return association, core.NewErrorAlreadyExists()
			}
			span.RecordError(err)
			return association, err
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
					}, document, signature)
					if err != nil {
						span.RecordError(err)
						continue
					}

					event := core.Event{
						Timeline:  timeline,
						Item:      posted,
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
			} else if owner.Domain == s.config.FQDN && mode != core.CommitModeLocalOnlyExec { // ここでリソースを作成したなら、リモートにもリレー
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

		// Associationだけの追加対応
		// メッセージの場合は、ターゲットのタイムラインにも追加する
		if owner.Domain == s.config.FQDN && mode != core.CommitModeLocalOnlyExec {
			targetMessage, err := s.message.Get(ctx, association.Target, doc.Signer) //NOTE: これはownerのドメインしか実行できない
			if err != nil {
				span.RecordError(err)
				return association, err
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
						return association, err
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
						return association, err
					}

					signatureBytes, err := core.SignBytes([]byte(document), s.config.PrivateKey)
					if err != nil {
						span.RecordError(err)
						return association, err
					}

					signature := hex.EncodeToString(signatureBytes)

					packetObj := core.Commit{
						Document:  string(document),
						Signature: signature,
					}

					packet, err := json.Marshal(packetObj)
					if err != nil {
						span.RecordError(err)
						return association, err
					}

					s.client.Commit(ctx, domain, string(packet), nil, nil)
				}
			}
		}
	}

	return association, nil
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
func (s *service) Delete(ctx context.Context, mode core.CommitMode, document, signature string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Service.Delete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	targetAssociation, err := s.repo.Get(ctx, doc.Target)
	if err != nil {
		if errors.Is(err, core.ErrorNotFound{}) {
			return core.Association{}, core.NewErrorAlreadyDeleted()
		}

		span.RecordError(err)
		return core.Association{}, err
	}

	requester := doc.Signer

	targetMessage, err := s.message.Get(ctx, targetAssociation.Target, requester)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if (targetAssociation.Author != requester) && (targetMessage.Author != requester) {
		return core.Association{}, fmt.Errorf("you are not authorized to perform this action")
	}

	err = s.repo.Delete(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
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
			return targetAssociation, err
		}
	}

	if targetAssociation.Target[0] == 'm' && mode != core.CommitModeLocalOnlyExec { // distribute is needed only when targetType is messages
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
					return targetAssociation, err
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
					return targetAssociation, err
				}

				signatureBytes, err := core.SignBytes([]byte(document), s.config.PrivateKey)
				if err != nil {
					span.RecordError(err)
					return targetAssociation, err
				}

				signature := hex.EncodeToString(signatureBytes)

				packetObj := core.Commit{
					Document:  string(document),
					Signature: signature,
				}

				packet, err := json.Marshal(packetObj)
				if err != nil {
					span.RecordError(err)
					return targetAssociation, err
				}

				s.client.Commit(ctx, domain, string(packet), nil, nil)
			}
		}
	}

	return targetAssociation, nil
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
