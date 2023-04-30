package handler

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"fmt"
	"io"
	"log"
	"net/http"
	"concurrent/domain/model"
	"concurrent/domain/service"
)

type MessageHandler struct {
    service service.MessageService
}

func NewMessageHandler(service service.MessageService) MessageHandler {
    return MessageHandler{service: service}
}

func (h MessageHandler) Handle(w http.ResponseWriter, r *http.Request) {

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            _, id := filepath.Split(r.URL.Path)

            if id != "" {
                fmt.Println(id)
            }

            message := h.service.GetMessage(id)
            response := model.MessageResponse {
                Message: message,
            }

            jsonstr, err := json.Marshal(response)
            if err != nil {
                log.Fatalf("getMessages json.Marshal error:%v", err)
            }

            fmt.Fprint(w, string(jsonstr))
        case http.MethodPost:
            body := r.Body
            defer body.Close()
            buf := new(bytes.Buffer)
            io.Copy(buf, body)
            var message model.Message
            json.Unmarshal(buf.Bytes(), &message)
            h.service.PostMessage(message)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, "{\"message\": \"accept\"}")
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

