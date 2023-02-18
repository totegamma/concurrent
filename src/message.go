package main

import (
    "net/http"
    "strings"
    "log"
    "encoding/json"
    "fmt"
    "bytes"
    "io"
)

func (backend Backend) getMessages(w http.ResponseWriter, r *http.Request) {

    var filter_str = r.URL.Query().Get("users")
    var filter = strings.Split(filter_str, ",")
    var messages []Message

    if (filter_str != "") {
        backend.DB.Where("author = ANY($1)", filter).Find(&messages)
    } else {
        backend.DB.Find(&messages)
    }

    response := MessagesResponse {
        Messages: messages,
    }

    jsonstr, err := json.Marshal(response)
    if err != nil {
        log.Fatalf("getMessages json.Marshal error:%v", err)
    }

    fmt.Fprint(w, string(jsonstr))
}

func (backend Backend) postMessage(w http.ResponseWriter, r *http.Request) {
    body := r.Body
    defer body.Close()

    buf := new(bytes.Buffer)
    io.Copy(buf, body)

    var message Message
    json.Unmarshal(buf.Bytes(), &message)

    if err := verifySignature(message.Payload, message.Author, message.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        w.WriteHeader(http.StatusBadRequest)
        fmt.Fprint(w, "invalid signature")
        return
    } else {
        fmt.Println("承認")
    }

    backend.DB.Create(&message)

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept\"}")
}

func (backend Backend) messageHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            backend.getMessages(w, r)
        case http.MethodPost:
            backend.postMessage(w, r)
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

