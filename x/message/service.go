package message

import (
    "log"
    "encoding/json"
    "github.com/redis/go-redis/v9"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/core"
)

// Service is a service of message
type Service struct {
    rdb *redis.Client
    repo *Repository
    stream *stream.Service
}

// NewService is used for wire.go
func NewService(rdb *redis.Client, repo *Repository, stream *stream.Service) *Service {
    return &Service{rdb, repo, stream}
}

// GetMessage returns a message by ID
func (s *Service) GetMessage(id string) (core.Message, error) {
    return s.repo.Get(id)
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

    message := core.Message{
        Author: object.Signer,
        Schema: object.Schema,
        Payload: objectStr,
        Signature: signature,
        Streams: streams,
    }

    id, err := s.repo.Create(&message)
    if err != nil {
        return err
    }

    for _, stream := range message.Streams {
        s.stream.Post(stream, id, message.Author, "")
    }

    return nil
}

// DeleteMessage deletes a message by ID
func (s *Service) DeleteMessage(id string) (core.Message, error) {
    return s.repo.Delete(id)
}

