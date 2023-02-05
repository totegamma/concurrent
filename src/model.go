package main

import (
    "time"
    "database/sql"
)

type Backend struct {
    DB *sql.DB
}

type Message struct {
    ID string `json:"id"`
    Author string `json:"author"`
    Schema string `json:"schema"`
    Payload string `json:"payload"`
    Signature string `json:"signature"`
    CDate time.Time `json:"cdate"`
    Associations []string `json: "associations"`
}

type Character struct {
    Author string `json:"author"`
    Schema string `json:"schema"`
    Payload string `json:"payload"`
    Signature string `json:"signature"`
    CDate time.Time `json:"cdate"`
}

type Association struct {
    ID string `json:"id"`
    Author string `json:"author"`
    Schema string `json:"schema"`
    Target string `json:"target"`
    Payload string `json:"payload"`
    Signature string `json:"signature"`
    CDate time.Time `json:"cdate"`
}

type Response struct {
    Messages []*Message `json:"messages"`
    Characters []*Character `json:"characters"`
}

