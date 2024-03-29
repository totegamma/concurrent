package association

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/timeline"
)

// Service is the interface for association service
type Service interface {
	Create(ctx context.Context, documentStr string, signature string) (core.Association, error)
	Delete(ctx context.Context, documentStr string) (core.Association, error)

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
	timeline timeline.Service
	message  message.Service
	key      key.Service
}

// NewService creates a new association service
func NewService(repo Repository, timeline timeline.Service, message message.Service, key key.Service) Service {
	return &service{repo, timeline, message, key}
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
func (s *service) Create(ctx context.Context, documentStr string, signature string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServicePostAssociation")
	defer span.End()

	var document core.CreateAssociation[any]
	err := json.Unmarshal([]byte(documentStr), &document)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	association := core.Association{
		Author:    document.Signer,
		Schema:    document.Schema,
		TargetTID: document.Target,
		Payload:   documentStr,
		Signature: signature,
		Timelines: document.Timelines,
		Variant:   document.Variant,
	}

	created, err := s.repo.Create(ctx, association)
	if err != nil {
		span.RecordError(err)
		return created, err // TODO: if err is duplicate key error, server should return 409
	}

	if document.Target[0] != 'm' {
		return created, nil
	}

	targetMessage, err := s.message.Get(ctx, created.TargetTID, document.Signer)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	item := core.TimelineItem{
		Type:     "association",
		ObjectID: created.ID,
		Owner:    targetMessage.Author,
		Author:   &created.Author,
	}

	for _, timeline := range association.Timelines {
		err = s.timeline.PostItem(ctx, timeline, item, created)
		if err != nil {
			slog.ErrorContext(ctx, "failed to post timeline", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
		}
	}

	for _, postto := range targetMessage.Timelines {
		event := core.Event{
			TimelineID: postto,
			Action:     "create",
			Type:       "association",
			Item:       item,
			Body:       created,
		}
		err = s.timeline.DistributeEvent(ctx, postto, event)

		if err != nil {
			slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
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
func (s *service) Delete(ctx context.Context, documentStr string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	var document core.DeleteDocument
	err := json.Unmarshal([]byte(documentStr), &document)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	targetAssociation, err := s.repo.Get(ctx, document.Body.TargetID)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	requester := document.Signer

	targetMessage, err := s.message.Get(ctx, targetAssociation.TargetTID, requester)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if (targetAssociation.Author != requester) && (targetMessage.Author != requester) {
		return core.Association{}, fmt.Errorf("you are not authorized to perform this action")
	}

	deleted, err := s.repo.Delete(ctx, document.Body.TargetID)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if deleted.TargetTID[0] != 'm' { // distribute is needed only when targetType is messages
		return deleted, nil
	}

	for _, posted := range targetMessage.Timelines {
		event := core.Event{
			TimelineID: posted,
			Type:       "association",
			Action:     "delete",
			Body:       deleted,
		}
		err := s.timeline.DistributeEvent(ctx, posted, event)
		if err != nil {
			slog.ErrorContext(ctx, "failed to publish message to Redis", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
			return deleted, err
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
