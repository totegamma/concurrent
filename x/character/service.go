package character

import (
	"context"
	"encoding/json"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"log"
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
		log.Printf("error occured while GetCharacters in characterRepository. error: %v\n", err)
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
		return core.Character{}, err
	}

	if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
		log.Println("verify signature err: ", err)
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
		return core.Character{}, err
	}

	return character, nil
}
