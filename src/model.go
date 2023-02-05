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
    Cdate time.Time `json:"cdate"`
    Author string `json:"author"`
    Payload string `json:"payload"`
    Signature string `json:"signature"`
}

type Response struct {
    Messages []*Message `json:"messages"`
}

