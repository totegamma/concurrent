package character

import (
	"context"
	"encoding/json"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
)

// Service is service of characters
type Service struct {
	repo *Repository
}

// NewService is for wire.go
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetCharacters returns characters by owner and schema
func (s *Service) GetCharacters(ctx context.Context, owner string, schema string) ([]core.Character, error) {
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
func (s *Service) PutCharacter(ctx context.Context, objectStr string, signature string, id string) (core.Character, error) {
	ctx, span := tracer.Start(ctx, "ServicePutCharacter")
	defer span.End()

	var object signedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Character{}, err
	}

	if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
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
