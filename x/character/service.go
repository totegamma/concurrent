package character

import (
	"context"
	"encoding/json"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
)

// Service is the interface for character service
type Service interface {
	GetCharacters(ctx context.Context, owner string, schema string) ([]core.Character, error)
	PutCharacter(ctx context.Context, objectStr string, signature string, id string) (core.Character, error)
	Count(ctx context.Context) (int64, error)
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

// GetCharacters returns characters by owner and schema
func (s *service) GetCharacters(ctx context.Context, owner string, schema string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetCharacters")
	defer span.End()

	characters, err := s.repo.Get(ctx, owner, schema)
	if err != nil {
		span.RecordError(err)
		return []core.Character{}, err
	}
	return characters, nil
}

// PutCharacter creates new character if the signature is valid
func (s *service) PutCharacter(ctx context.Context, objectStr string, signature string, id string) (core.Character, error) {
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
