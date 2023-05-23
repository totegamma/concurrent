package message

import (
    "encoding/json"
    "log"

    "github.com/totegamma/concurrent/x/socket"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/util"
)

// Service is a service of message
type Service struct {
    repo *Repository
    stream *stream.Service
    socket *socket.Service
}

// NewService is used for wire.go
func NewService(repo *Repository, stream *stream.Service, socket *socket.Service) *Service {
    return &Service{repo: repo, stream: stream, socket: socket}
}

// GetMessage returns a message by ID
func (s *Service) GetMessage(id string) Message{
    var message Message
    message = s.repo.Get(id)
    return message
}

// PostMessage creates new message
func (s *Service) PostMessage(objectStr string, signature string, streams []string) error {

    var object signedObject
    err := json.Unmarshal([]byte(objectStr), &object)
    if err != nil {
        return err
    }

    if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
        log.Println("verify signature err: ", err)
        return err
    }

    message := Message{
        Author: object.Signer,
        Schema: object.Schema,
        Payload: objectStr,
        Signature: signature,
        Streams: streams,
    }

    id := s.repo.Create(&message)
    for _, stream := range message.Streams {
        s.stream.Post(stream, id, message.Author)
    }

    jsonstr, _ := json.Marshal(streamEvent{
        Type: "message",
        Action: "create",
        Body: message,
    })
    s.socket.NotifyAllClients(jsonstr)
    return nil
}

// DeleteMessage deletes a message by ID
func (s *Service) DeleteMessage(id string) {
    deleted := s.repo.Delete(id)
    for _, stream := range deleted.Streams {
        s.stream.Delete(stream, id)
    }
    jsonstr, _ := json.Marshal(streamEvent{
        Type: "message",
        Action: "delete",
        Body: deleted,
    })
    s.socket.NotifyAllClients(jsonstr)
}

