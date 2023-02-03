package main

import (
    "io"
    "fmt"
    "log"
    "time"
    "bytes"
    "crypto"
    "net/http"
    "crypto/rsa"
    "crypto/x509"
    "database/sql"
    "encoding/json"
    "encoding/base64"
    _ "github.com/jackc/pgx/v4/stdlib"
)

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

type Backend struct {
    DB *sql.DB
}

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
    http.HandleFunc("/", backend.restHandler)
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "ok");
    })
    http.ListenAndServe(":8000", nil)
}

func (backend Backend) getMessages(w http.ResponseWriter, r *http.Request) {
    rows, err := backend.DB.Query("SELECT * FROM messages")
    if err != nil {
        log.Fatalf("getMessages db.Query error:%v", err)
    }
    defer rows.Close()

    var response Response

    for rows.Next() {
        u := &Message{}
        if err := rows.Scan(&u.ID, &u.Cdate, &u.Author, &u.Payload, &u.Signature); err != nil {
            log.Fatalf("getMessages rows.Scan error:%v", err)
        }
        response.Messages = append(response.Messages, u)
    }

    err = rows.Err()
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

    res, err := backend.DB.Exec("INSERT INTO messages (author, payload, signature) VALUES ($1, $2, $3)",
                    message.Author,
                    message.Payload,
                    message.Signature,
                )
    if err != nil {
        log.Fatalf("postMessage db.Exec error:%v", err)
    }

    w.WriteHeader(http.StatusCreated)
    fmt.Fprintf(w, "{\"message\": \"accept: %v\"}", res)
}

func (backend Backend) restHandler(w http.ResponseWriter, r *http.Request) {
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

func verifySignature(message string, keystr string, signature string) error {
    // PEMの中身はDERと同じASN.1のバイナリデータをBase64によってエンコーディングされたテキストなのでBase64でデコードする
    // ゆえにDERエンコード形式に変換
    keyBytes, err := base64.StdEncoding.DecodeString(keystr)
    if err != nil {
        return err
    }

    // DERでエンコードされた公開鍵を解析する
    // 成功すると、pubは* rsa.PublicKey、* dsa.PublicKey、または* ecdsa.PublicKey型になる
    pub, err := x509.ParsePKIXPublicKey(keyBytes)
    if err != nil {
        return err
    }

    // 署名文字列はBase64でエンコーディングされたテキストなのでBase64でデコードする
    signDataByte, err := base64.StdEncoding.DecodeString(signature)
    if err != nil {
        return err
    }

    // SHA-256のハッシュ関数を使って受信データのハッシュ値を算出する
    h := crypto.Hash.New(crypto.SHA256)
    h.Write([]byte(message))
    hashed := h.Sum(nil)

    // 署名の検証、有効な署名はnilを返すことによって示される
    // ここで何をしているかというと、、
    // ①送信者のデータ（署名データ）を公開鍵で復号しハッシュ値を算出
    // ②受信側で算出したハッシュ値と、①のハッシュ値を比較し、一致すれば、「送信者が正しい」「データが改ざんされていない」ということを確認できる
    err = rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA256, hashed, signDataByte)
    if err != nil {
        return err
    }

    return nil
}

