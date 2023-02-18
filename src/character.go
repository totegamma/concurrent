package main

import (
    "io"
    "log"
    "fmt"
    "bytes"
    "net/http"
    "encoding/json"
)

func (backend Backend) getCharacters(w http.ResponseWriter, r *http.Request) {

    var filter_author = r.URL.Query().Get("author")
    var filter_schema = r.URL.Query().Get("schema")

    fmt.Print(filter_author)
    fmt.Print(filter_schema)

    var characters []Character

    backend.DB.Where("author = $1 AND schema = $2", filter_author, filter_schema).Find(&characters);

    response := CharactersResponse {
        Characters: characters,
    }

    jsonstr, err := json.Marshal(response)
    if err != nil {
        log.Fatalf("getCharacters json.Marshal error:%v", err)
    }

    fmt.Fprint(w, string(jsonstr))
}

func (backend Backend) putCharacter(w http.ResponseWriter, r *http.Request) {
    body := r.Body
    defer body.Close()

    buf := new(bytes.Buffer)
    io.Copy(buf, body)

    var character Character
    json.Unmarshal(buf.Bytes(), &character)

    if err := verifySignature(character.Payload, character.Author, character.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        w.WriteHeader(http.StatusBadRequest)
        fmt.Fprint(w, "invalid signature")
        return
    } else {
        fmt.Println("承認")
    }

    backend.DB.Create(&character)

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept\"}")
}

func (backend Backend) characterHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            backend.getCharacters(w, r)
        case http.MethodPut:
            backend.putCharacter(w, r)
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

