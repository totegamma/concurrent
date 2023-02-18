package main

import (
    "net/http"
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

    var association Association
    json.Unmarshal(buf.Bytes(), &association)

    if err := verifySignature(association.Payload, association.Author, association.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        w.WriteHeader(http.StatusBadRequest)
        fmt.Fprint(w, "invalid signature")
        return
    } else {
        fmt.Println("承認")
    }

    backend.DB.Create(&association)

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept\"}")
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

