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

    var filter_schema = r.URL.Query().Get("schema")

    rows, err := backend.DB.Query("SELECT * FROM characters WHERE schema = $1", filter_schema)
    if err != nil {
        log.Fatalf("getCharacters db.Query error:%v", err)
    }

    defer rows.Close()

    var response Response

    for rows.Next() {
        u := &Character{}
        if err := rows.Scan(&u.Author, &u.Schema, &u.Payload, &u.Signature, &u.CDate); err != nil {
            log.Fatalf("getCharacters rows.Scan error:%v", err)
        }
        response.Characters = append(response.Characters, u)
    }

    err = rows.Err()
    if err != nil {
        log.Fatalf("getCharacters rows.Err error:%v", err)
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

    res, err := backend.DB.Exec(
                    `
                    INSERT INTO characters (author, schema, payload, signature)
                    VALUES ($1, $2, $3, $4)
                    ON CONFLICT(author, schema)
                    DO UPDATE SET
                        payload = $3,
                        signature = $4
                    `,
                    character.Author,
                    character.Schema,
                    character.Payload,
                    character.Signature,
                )
    if err != nil {
        log.Fatalf("putCharacter db.Exec error:%v", err)
    }

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept: %v\"}", res)
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
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

