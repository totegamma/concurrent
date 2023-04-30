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
        case http.MethodGet:
            streams_str := r.URL.Query().Get("streams")
            streams := strings.Split(streams_str, ",")
            messages := h.service.GetRecent(streams)

            jsonstr, err := json.Marshal(messages)
            if err != nil {
                log.Fatalf("getMessages json.Marshal error:%v", err)
            }
            fmt.Printf("message: %v\n", messages)
            fmt.Fprint(w, string(jsonstr))
        case http.MethodPost:

            body := r.Body
            defer body.Close()
            buf := new(bytes.Buffer)
            io.Copy(buf, body)
            var query StreamPostQuery
            json.Unmarshal(buf.Bytes(), &query)

            id := h.service.Post(query.Stream, query.ID)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", id))
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}
