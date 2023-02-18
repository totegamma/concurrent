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

type MessageService struct {
    repo MessageRepository
}

func NewMessageService(repo MessageRepository) MessageService {
    return MessageService{repo: repo}
}

func (s *MessageService) getMessages(followee []string) []Message{
    var messages []Message

    if (len(followee) > 0) {
        messages = s.repo.GetFollowee(followee)
    } else {
        messages = s.repo.GetAll()
    }

    return messages
}

func (s *MessageService) postMessage(message Message) {
    if err := verifySignature(message.Payload, message.Author, message.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }

    s.repo.Create(message)
}

func (backend Backend) messageHandler(w http.ResponseWriter, r *http.Request) {

    messageService := SetupMessageService(backend.DB)

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            var filter_str = r.URL.Query().Get("users")
            var filter = strings.Split(filter_str, ",")
            messages := messageService.getMessages(filter)
            response := MessagesResponse {
                Messages: messages,
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
            var message Message
            json.Unmarshal(buf.Bytes(), &message)
            messageService.postMessage(message)
            w.WriteHeader(http.StatusCreated)
            fmt.Fprintf(w, "{\"message\": \"accept\"}")
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

