package character

import (
    "log"
    "github.com/totegamma/concurrent/x/util"
)

// Service is service of characters
type Service struct {
    repo Repository
}

// NewService is for wire.go
func NewService(repo Repository) Service {
    return Service{repo: repo}
}

// GetCharacters returns characters by owner and schema
func (s* Service) GetCharacters(owner string, schema string) []Character {
    characters, err := s.repo.Get(owner, schema)
    if err != nil {
        log.Printf("error occured while GetCharacters in characterRepository. error: %v\n", err)
        return []Character{}
    }
    return characters
}

// PutCharacter creates new character if the signature is valid
func (s* Service) PutCharacter(character Character) {
    if err := util.VerifySignature(character.Payload, character.Author, character.Signature); err != nil {
        log.Println("verify signature err: ", err)
        return
    }
    s.repo.Upsert(character)
}

