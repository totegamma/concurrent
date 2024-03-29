package character

import (
	"context"
	"encoding/json"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
)

// Service is the interface for character service
type Service interface {
	Put(ctx context.Context, objectStr string, signature string, id string) (core.Character, error)
	Count(ctx context.Context) (int64, error)
	Get(ctx context.Context, id string) (core.Character, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Character, error)
	GetByAuthor(ctx context.Context, owner string) ([]core.Character, error)
	GetBySchema(ctx context.Context, schema string) ([]core.Character, error)
	Delete(ctx context.Context, id string) (core.Character, error)
}

type service struct {
	repo Repository
	key  key.Service
}

// NewService creates a new character service
func NewService(repo Repository, key key.Service) Service {
	return &service{repo, key}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// GetByAuthorAndSchema returns characters by owner and schema
func (s *service) GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByAuthorAndSchema")
	defer span.End()

	return s.repo.GetByAuthorAndSchema(ctx, owner, schema)
}

// GetByAuthor returns characters by owner
func (s *service) GetByAuthor(ctx context.Context, owner string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByAuthor")
	defer span.End()

	return s.repo.GetByAuthor(ctx, owner)
}

// GetBySchema returns characters by schema
func (s *service) GetBySchema(ctx context.Context, schema string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetBySchema")
	defer span.End()

	return s.repo.GetBySchema(ctx, schema)
}

// PutCharacter creates new character if the signature is valid
func (s *service) Put(ctx context.Context, objectStr string, signature string, id string) (core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServicePutCharacter")
	defer span.End()

	var object signedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Character{}, err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return core.Character{}, err
	}

	character := core.Character{
		ID:        id,
		Author:    object.Signer,
		Schema:    object.Schema,
		Payload:   objectStr,
		Signature: signature,
	}

	err = s.repo.Upsert(ctx, character)
	if err != nil {
		span.RecordError(err)
		return core.Character{}, err
	}

	return character, nil
}

// Delete deletes character
func (s *service) Delete(ctx context.Context, id string) (core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repo.Delete(ctx, id)
}

func (s *service) Get(ctx context.Context, id string) (core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repo.Get(ctx, id)
}
