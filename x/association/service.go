package association

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/key"
)

// Service is the interface for association service
type Service interface {
	PostAssociation(ctx context.Context, objectStr string, signature string, streams []string, targetType string) (core.Association, error)
	Get(ctx context.Context, id string) (core.Association, error)
	GetOwn(ctx context.Context, author string) ([]core.Association, error)
	Delete(ctx context.Context, id, requester string) (core.Association, error)

	GetByTarget(ctx context.Context, targetID string) ([]core.Association, error)
	GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error)
	GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error)
	GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error)
	GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error)
	GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	repo    Repository
	stream  stream.Service
	message message.Service
    key key.Service
}

// NewService creates a new association service
func NewService(repo Repository, stream stream.Service, message message.Service, key key.Service) Service {
	return &service{repo, stream, message, key}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// PostAssociation creates a new association
// If targetType is messages, it also posts the association to the target message's streams
// returns the created association
func (s *service) PostAssociation(ctx context.Context, objectStr string, signature string, streams []string, targetType string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServicePostAssociation")
	defer span.End()

	var object SignedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	var content SignedObject
	err = json.Unmarshal([]byte(objectStr), &content)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	contentString, err := json.Marshal(content.Body)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	hash := sha256.Sum256(contentString)
	contentHash := hex.EncodeToString(hash[:])

	association := core.Association{
		Author:      object.Signer,
		Schema:      object.Schema,
		TargetID:    object.Target,
		TargetType:  targetType,
		Payload:     objectStr,
		Signature:   signature,
		Streams:     streams,
		ContentHash: contentHash,
		Variant:     object.Variant,
	}

	created, err := s.repo.Create(ctx, association)
	if err != nil {
		span.RecordError(err)
		return created, err // TODO: if err is duplicate key error, server should return 409
	}

	if targetType != "messages" { // distribute is needed only when targetType is messages
		return created, nil
	}

	targetMessage, err := s.message.Get(ctx, created.TargetID)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	item := core.StreamItem{
		Type:     "association",
		ObjectID: created.ID,
		Owner:    targetMessage.Author,
		Author:   created.Author,
	}

	for _, stream := range association.Streams {
		err = s.stream.PostItem(ctx, stream, item, created)
		if err != nil {
			slog.ErrorContext(ctx, "failed to post stream", slog.String("error", err.Error()), slog.String("module", "association"))
			span.RecordError(err)
		}
	}

	for _, postto := range targetMessage.Streams {
		event := core.Event{
			Stream: postto,
			Action: "create",
			Type:   "association",
			Item:   item,
			Body:   created,
		}
		err = s.stream.DistributeEvent(ctx, postto, event)

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
func (s *service) Delete(ctx context.Context, id, requester string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	targetAssociation, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	targetMessage, err := s.message.Get(ctx, targetAssociation.TargetID)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if (targetAssociation.Author != requester) && (targetMessage.Author != requester) {
		return core.Association{}, fmt.Errorf("you are not authorized to perform this action")
	}

	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if deleted.TargetType != "messages" { // distribute is needed only when targetType is messages
		return deleted, nil
	}

	for _, posted := range targetMessage.Streams {
		event := core.Event{
			Stream: posted,
			Type:   "association",
			Action: "delete",
			Body:   deleted,
		}
		err := s.stream.DistributeEvent(ctx, posted, event)
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
