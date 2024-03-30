package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/cdid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for message service
// Provides methods for message CRUD
type Service interface {
	Get(ctx context.Context, id string, requester string) (core.Message, error)
	GetWithOwnAssociations(ctx context.Context, id string, requester string) (core.Message, error)
	Create(ctx context.Context, objectStr string, signature string) (core.Message, error)
	Delete(ctx context.Context, id string) (core.Message, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	rdb      *redis.Client // TODO: remove this
	repo     Repository
	timeline timeline.Service
	key      key.Service
}

// NewService creates a new message service
func NewService(rdb *redis.Client, repo Repository, timeline timeline.Service, key key.Service) Service {
	return &service{rdb, repo, timeline, key}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// Get returns a message by ID
func (s *service) Get(ctx context.Context, id string, requester string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	message, err := s.repo.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	canRead := true

	for _, timelineID := range message.Timelines {
		ok := s.timeline.HasReadAccess(ctx, timelineID, requester)
		if !ok {
			canRead = false
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

	canRead := true

	for _, timelineID := range message.Timelines {
		ok := s.timeline.HasReadAccess(ctx, timelineID, requester)
		if !ok {
			canRead = false
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
func (s *service) Create(ctx context.Context, objectStr string, signature string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServicePostMessage")
	defer span.End()

	// TODO: check requester is authorized to post message

	var object core.CreateMessage[any]
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	hash := util.GetHash([]byte(objectStr))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := object.SignedAt
	id := cdid.New(hash10, signedAt).String()

	message := core.Message{
		ID:        id,
		Author:    object.Signer,
		Schema:    object.Schema,
		Document:  objectStr,
		Signature: signature,
		Timelines: object.Timelines,
	}

	if !object.SignedAt.IsZero() {
		message.CDate = object.SignedAt
	}

	created, err := s.repo.Create(ctx, message)
	if err != nil {
		span.RecordError(err)
		return message, err
	}

	ispublic := true
	for _, timeline := range object.Timelines {
		ok := s.timeline.HasReadAccess(ctx, timeline, "")
		if !ok {
			ispublic = false
			break
		}
	}

	var body interface{}
	if ispublic {
		body = created
	}

	for _, timeline := range message.Timelines {
		s.timeline.PostItem(ctx, timeline, core.TimelineItem{
			ObjectID:   created.ID,
			Owner:      object.Signer,
			TimelineID: timeline,
		}, body)
	}

	return created, nil
}

// Delete deletes a message by ID
// It also emits a delete event to the sockets
func (s *service) Delete(ctx context.Context, documentStr string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	var document core.DeleteDocument
	err := json.Unmarshal([]byte(documentStr), &document)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	deleteTarget, err := s.repo.Get(ctx, document.Target)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if deleteTarget.Author != document.Signer {
		return core.Message{}, fmt.Errorf("you are not authorized to perform this action")
	}

	deleted, err := s.repo.Delete(ctx, document.Target)
	slog.DebugContext(ctx, fmt.Sprintf("deleted: %v", deleted), slog.String("module", "message"))

	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	for _, desttimeline := range deleted.Timelines {
		jsonstr, _ := json.Marshal(core.Event{
			TimelineID: desttimeline,
			Type:       "message",
			Action:     "delete",
			Body:       deleted,
		})
		err := s.rdb.Publish(context.Background(), desttimeline, jsonstr).Err()
		if err != nil {
			span.RecordError(err)
			return deleted, err
		}
	}

	return deleted, err
}
