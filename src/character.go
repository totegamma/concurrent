package main

import (
    "io"
    "log"
    "fmt"
    "bytes"
    "net/http"
    "encoding/json"
)

type CharacterService struct {
    repo CharacterRepository
}

func NewCharacterService(repo CharacterRepository) *CharacterService {
    return &CharacterService{repo: repo}
}

func (s* CharacterService) getCharacters(owner string, schema string) []Character {
    return s.repo.Get(owner, schema)
}

func (s* CharacterService) putCharacter(character Character) {
    if err := verifySignature(character.Payload, character.Author, character.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }
    s.repo.Upsert(character)
}

func (backend Backend) characterHandler(w http.ResponseWriter, r *http.Request) {

    characterRepository := NewCharacterRepository(backend.DB)
    characterService := NewCharacterService(*characterRepository)

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            var filter_author = r.URL.Query().Get("author")
            var filter_schema = r.URL.Query().Get("schema")
            characters := characterService.getCharacters(filter_author, filter_schema)
            response := CharactersResponse {
                Characters: characters,
            }
            jsonstr, err := json.Marshal(response)
            if err != nil {
                log.Fatalf("getCharacters json.Marshal error:%v", err)
            }
            fmt.Fprint(w, string(jsonstr))
        case http.MethodPut:
            body := r.Body
            defer body.Close()

            buf := new(bytes.Buffer)
            io.Copy(buf, body)

            var character Character
            json.Unmarshal(buf.Bytes(), &character)
            characterService.putCharacter(character)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, "{\"message\": \"accept\"}")
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

