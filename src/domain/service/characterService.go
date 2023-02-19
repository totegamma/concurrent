package service

import (
    "fmt"
    "concurrent/domain/model"
    "concurrent/domain/repository"
)

type CharacterService struct {
    repo repository.CharacterRepository
}

func NewCharacterService(repo repository.CharacterRepository) CharacterService {
    return CharacterService{repo: repo}
}

func (s* CharacterService) GetCharacters(owner string, schema string) []model.Character {
    characters, err := s.repo.Get(owner, schema)
    if err != nil {
        fmt.Printf("error occured while GetCharacters in characterRepository. error: %v\n", err)
        return []model.Character{}
    }
    return characters
}

func (s* CharacterService) PutCharacter(character model.Character) {
    if err := VerifySignature(character.Payload, character.Author, character.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }
    s.repo.Upsert(character)
}

