package character

import (
    "log"
    "github.com/totegamma/concurrent/x/util"
)

type CharacterService struct {
    repo CharacterRepository
}

func NewCharacterService(repo CharacterRepository) CharacterService {
    return CharacterService{repo: repo}
}

func (s* CharacterService) GetCharacters(owner string, schema string) []Character {
    characters, err := s.repo.Get(owner, schema)
    if err != nil {
        log.Printf("error occured while GetCharacters in characterRepository. error: %v\n", err)
        return []Character{}
    }
    return characters
}

func (s* CharacterService) PutCharacter(character Character) {
    if err := util.VerifySignature(character.Payload, character.Author, character.Signature); err != nil {
        log.Println("verify signature err: ", err)
        return
    }
    s.repo.Upsert(character)
}

