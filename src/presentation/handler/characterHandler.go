package handler

import (
    "io"
    "log"
    "fmt"
    "bytes"
    "net/http"
    "encoding/json"
    "concurrent/domain/model"
    "concurrent/domain/service"
)

type CharacterHandler struct {
    service service.CharacterService
}

func NewCharacterHandler(service service.CharacterService) CharacterHandler {
    return CharacterHandler{service: service}
}

func (h CharacterHandler) Handle(w http.ResponseWriter, r *http.Request) {


    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            var filter_author = r.URL.Query().Get("author")
            var filter_schema = r.URL.Query().Get("schema")
            characters := h.service.GetCharacters(filter_author, filter_schema)
            response := model.CharactersResponse {
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

            var character model.Character
            json.Unmarshal(buf.Bytes(), &character)
            h.service.PutCharacter(character)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, "{\"message\": \"accept\"}")
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

