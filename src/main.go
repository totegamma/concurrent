package main

import (
    "database/sql"
    "fmt"
    "net/http"
    _ "github.com/jackc/pgx/v4/stdlib"
)


func main() {
    uri := "postgres://postgres:password@localhost/concurrent"

    fmt.Println("hello")
    fmt.Println("connect to db")
    var err error
    DB, err := sql.Open("pgx", uri)
    if err != nil {
        panic(err)
    }
    defer DB.Close()
    fmt.Println(DB)

    backend := Backend {
        DB: DB,
    }

    fmt.Println("start web")
    http.HandleFunc("/messages", backend.messageHandler)
    http.HandleFunc("/characters", backend.characterHandler)
    http.HandleFunc("/associations", backend.associationHandler)
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "ok");
    })
    http.ListenAndServe(":8000", nil)
}

