package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/stream"
)

// Service is the interface for message service
// Provides methods for message CRUD
type Service interface {
	Get(ctx context.Context, id string) (core.Message, error)
	GetWithOwnAssociations(ctx context.Context, id string, requester string) (core.Message, error)
	PostMessage(ctx context.Context, objectStr string, signature string, streams []string) (core.Message, error)
	Delete(ctx context.Context, id string) (core.Message, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	rdb    *redis.Client
	repo   Repository
	stream stream.Service
	key    key.Service
}

// NewService creates a new message service
func NewService(rdb *redis.Client, repo Repository, stream stream.Service, key key.Service) Service {
	return &service{rdb, repo, stream, key}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// Get returns a message by ID
func (s *service) Get(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	message, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	canRead := false

	for _, streamID := range message.Streams {
		ok := s.stream.HasReadAccess(ctx, streamID, "")
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
	ctx, span := tracer.Start(ctx, "ServiceGetWithOwnAssociations")
	defer span.End()

	message, err := s.repo.GetWithOwnAssociations(ctx, id, requester)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	canRead := false

	for _, streamID := range message.Streams {
		ok := s.stream.HasReadAccess(ctx, streamID, requester)
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

// PostMessage creates a new message
// It also posts the message to the streams
func (s *service) PostMessage(ctx context.Context, objectStr string, signature string, streams []string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServicePostMessage")
	defer span.End()

	var object SignedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	message := core.Message{
		Author:    object.Signer,
		Schema:    object.Schema,
		Payload:   objectStr,
		Signature: signature,
		Streams:   streams,
	}

	if !object.SignedAt.IsZero() {
		message.CDate = object.SignedAt
	}

	created, err := s.repo.Create(ctx, message)
	if err != nil {
		span.RecordError(err)
		return message, err
	}

	for _, stream := range message.Streams {
		s.stream.PostItem(ctx, stream, core.StreamItem{
			Type:     "message",
			ObjectID: created.ID,
			Owner:    object.Signer,
			StreamID: stream,
		}, created)
	}

	return created, nil
}

// Delete deletes a message by ID
// It also emits a delete event to the sockets
func (s *service) Delete(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	deleted, err := s.repo.Delete(ctx, id)
	slog.DebugContext(ctx, fmt.Sprintf("deleted: %v", deleted), slog.String("module", "message"))

	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	for _, deststream := range deleted.Streams {
		jsonstr, _ := json.Marshal(core.Event{
			Stream: deststream,
			Type:   "message",
			Action: "delete",
			Body:   deleted,
		})
		err := s.rdb.Publish(context.Background(), deststream, jsonstr).Err()
		if err != nil {
			span.RecordError(err)
			return deleted, err
		}
	}

	return deleted, err
}
