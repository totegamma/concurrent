package character

import (
    "fmt"
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
        fmt.Printf("error occured while GetCharacters in characterRepository. error: %v\n", err)
        return []Character{}
    }
    return characters
}

func (s* CharacterService) PutCharacter(character Character) {
    if err := util.VerifySignature(character.Payload, character.Author, character.R, character.S); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }
    s.repo.Upsert(character)
}

