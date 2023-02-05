package main

import (
    "net/http"
    "strings"
    "database/sql"
    "github.com/lib/pq"
    "log"
    "encoding/json"
    "fmt"
    "bytes"
    "io"
)

func (backend Backend) getMessages(w http.ResponseWriter, r *http.Request) {

    var filter_str = r.URL.Query().Get("users")
    var filter = strings.Split(filter_str, ",")

    var rows *sql.Rows

    if (filter_str != "") {
        var err error
        rows, err = backend.DB.Query("SELECT * FROM messages WHERE author = ANY($1)", pq.Array(filter))
        if err != nil {
            log.Fatalf("getMessages db.Query error:%v", err)
        }

    } else {
        var err error
        rows, err = backend.DB.Query("SELECT * FROM messages")
        if err != nil {
            log.Fatalf("getMessages db.Query error:%v", err)
        }
    }

    defer rows.Close()

    var response Response

    for rows.Next() {
        u := &Message{}
        if err := rows.Scan(&u.ID, &u.CDate, &u.Author, &u.Payload, &u.Signature); err != nil {
            log.Fatalf("getMessages rows.Scan error:%v", err)
        }
        response.Messages = append(response.Messages, u)
    }

    err := rows.Err()
    if err != nil {
        log.Fatalf("getMessages rows.Err error:%v", err)
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

    res, err := backend.DB.Exec("INSERT INTO messages (author, schema, payload, signature) VALUES ($1, $2, $3, $4)",
                    message.Author,
                    message.Schema,
                    message.Payload,
                    message.Signature,
                )
    if err != nil {
        log.Fatalf("postMessage db.Exec error:%v", err)
    }

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept: %v\"}", res)
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
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

