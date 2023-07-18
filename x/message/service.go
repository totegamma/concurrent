package message

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/stream"
	"github.com/totegamma/concurrent/x/util"
)

// Service is a service of message
type Service struct {
	rdb    *redis.Client
	repo   *Repository
	stream *stream.Service
}

// NewService is used for wire.go
func NewService(rdb *redis.Client, repo *Repository, stream *stream.Service) *Service {
	return &Service{rdb, repo, stream}
}

// Get returns a message by ID
func (s *Service) Get(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repo.Get(ctx, id)
}

// PostMessage creates new message
func (s *Service) PostMessage(ctx context.Context, objectStr string, signature string, streams []string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServicePostMessage")
	defer span.End()

	var object SignedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
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

	id, err := s.repo.Create(ctx, &message)
	if err != nil {
		span.RecordError(err)
		return message, err
	}

	for _, stream := range message.Streams {
		s.stream.Post(ctx, stream, id, "message", message.Author, "", "")
	}

	return message, nil
}

// Delete deletes a message by ID
func (s *Service) Delete(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	for _, deststream := range deleted.Streams {
		jsonstr, _ := json.Marshal(stream.Event{
			Stream: deststream,
			Type:   "message",
			Action: "delete",
			Body: stream.Element{
				ID: deleted.ID,
			},
		})
		err := s.rdb.Publish(context.Background(), deststream, jsonstr).Err()
		if err != nil {
			span.RecordError(err)
			return deleted, err
		}
	}

	return deleted, err
}
