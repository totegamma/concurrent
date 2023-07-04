package message

import (
	"context"
	"encoding/json"
	"log"

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
		return core.Message{}, err
	}

	if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
		log.Println("verify signature err: ", err)
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

	return s.repo.Delete(ctx, id)
}
