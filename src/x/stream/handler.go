package stream

import (
    "io"
    "fmt"
    "log"
    "bytes"
    "strings"
    "net/http"
    "encoding/json"
)

type StreamHandler struct {
    service StreamService
}

func NewStreamHandler(service StreamService) StreamHandler {
    return StreamHandler{service: service}
}

type StreamPostQuery struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
}

func (h StreamHandler) Handle(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet: // クエリがあればstreamの中身を、なければstreamのリストを返す
            streamStr := r.URL.Query().Get("stream")
            stream := h.service.Get(streamStr)

            jsonstr, err := json.Marshal(stream)
            if err != nil {
                log.Fatalf("getMessages json.Marshal error:%v", err)
            }
            fmt.Fprint(w, string(jsonstr))
        case http.MethodPost: // streamへの投稿
            body := r.Body
            defer body.Close()
            buf := new(bytes.Buffer)
            io.Copy(buf, body)
            var query StreamPostQuery
            json.Unmarshal(buf.Bytes(), &query)

            id := h.service.Post(query.Stream, query.ID)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", id))

        case http.MethodPut: //streamの作成・更新
            body := r.Body
            defer body.Close()
            buf := new(bytes.Buffer)
            io.Copy(buf, body)
            var stream Stream
            json.Unmarshal(buf.Bytes(), &stream)
            log.Println(stream)

            h.service.Upsert(&stream)
            fmt.Fprintf(w, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", stream.ID))
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

func (h StreamHandler) HandleRecent(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet: // クエリがあればstreamの中身を、なければstreamのリストを返す
            streamsStr := r.URL.Query().Get("streams")
            streams := strings.Split(streamsStr, ",")
            messages := h.service.GetRecent(streams)

            jsonstr, err := json.Marshal(messages)
            if err != nil {
                log.Fatalf("getMessages json.Marshal error:%v", err)
            }
            fmt.Fprint(w, string(jsonstr))
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

func (h StreamHandler) HandleList(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            schema := r.URL.Query().Get("schema")
            list := h.service.StreamListBySchema(schema)
            jsonstr, err := json.Marshal(list)
            if err != nil {
                log.Fatalf("getMessages json.Marshal error:%v", err)
            }
            fmt.Fprint(w, string(jsonstr))
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}
