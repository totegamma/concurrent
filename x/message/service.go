package message

import (
    "log"
    "strings"
    "encoding/json"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/socket"
)

// Service is a service of message
type Service struct {
    repo Repository
    stream stream.Service
    socket *socket.Service
}

// NewService is used for wire.go
func NewService(repo Repository, stream stream.Service, socket *socket.Service) Service {
    return Service{repo: repo, stream: stream, socket: socket}
}

// GetMessage returns a message by ID
func (s *Service) GetMessage(id string) Message{
    var message Message
    message = s.repo.Get(id)
    return message
}

// PostMessage creates new message
func (s *Service) PostMessage(message Message) {
    if err := util.VerifySignature(message.Payload, message.Author, message.Signature); err != nil {
        log.Println("verify signature err: ", err)
        return
    }
    id := s.repo.Create(&message)
    for _, stream := range strings.Split(message.Streams, ",") {
        s.stream.Post(stream, id)
    }

    jsonstr, _ := json.Marshal(streamEvent{
        Type: "message",
        Action: "create",
        Body: message,
    })
    s.socket.NotifyAllClients(jsonstr)
}

// DeleteMessage deletes a message by ID
func (s *Service) DeleteMessage(id string) {
    deleted := s.repo.Delete(id)
    for _, stream := range strings.Split(deleted.Streams, ",") {
        s.stream.Delete(stream, id)
    }
    jsonstr, _ := json.Marshal(streamEvent{
        Type: "message",
        Action: "delete",
        Body: deleted,
    })
    s.socket.NotifyAllClients(jsonstr)
}

