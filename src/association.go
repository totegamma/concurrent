package main

import (
    "net/http"
    "log"
    "encoding/json"
    "fmt"
    "bytes"
    "io"
)


func (backend Backend) postAssociation(w http.ResponseWriter, r *http.Request) {
    body := r.Body
    defer body.Close()

    buf := new(bytes.Buffer)
    io.Copy(buf, body)

    var assosiation Association
    json.Unmarshal(buf.Bytes(), &assosiation)

    if err := verifySignature(assosiation.Payload, assosiation.Author, assosiation.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        w.WriteHeader(http.StatusBadRequest)
        fmt.Fprint(w, "invalid signature")
        return
    } else {
        fmt.Println("承認")
    }

    res, err := backend.DB.Exec("INSERT INTO assosiation (author, schema, target, payload, signature) VALUES ($1, $2, $3, $4, $5)",
                    assosiation.Author,
                    assosiation.Schema,
                    assosiation.Target,
                    assosiation.Payload,
                    assosiation.Signature,
                )
    if err != nil {
        log.Fatalf("postAssosiation db.Exec error:%v", err)
    }

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept: %v\"}", res)
}

func (backend Backend) associationHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodPost:
            backend.postAssociation(w, r)
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

