package message

import (
    "log"
    "context"
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
func (s *Service) GetMessage(id string) core.Message{
    var message core.Message
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

    message := core.Message{
        Author: object.Signer,
        Schema: object.Schema,
        Payload: objectStr,
        Signature: signature,
        Streams: streams,
    }

    id := s.repo.Create(&message)


    for _, stream := range message.Streams {
        s.stream.Post(stream, id, message.Author)
        jsonstr, _ := json.Marshal(streamEvent{
            Stream: stream,
            Type: "message",
            Action: "create",
            Body: message,
        })
        err := s.rdb.Publish(context.Background(), stream, jsonstr).Err()
        if err != nil {
            log.Printf("fail to publish message to Redis: %v", err)
        }
    }

    return nil
}

// DeleteMessage deletes a message by ID
func (s *Service) DeleteMessage(id string) {
    deleted := s.repo.Delete(id)
    for _, stream := range deleted.Streams {
        s.stream.Delete(stream, id)
        jsonstr, _ := json.Marshal(streamEvent{
            Stream: stream,
            Type: "message",
            Action: "delete",
            Body: deleted,
        })
        err := s.rdb.Publish(context.Background(), stream, jsonstr).Err()
        if err != nil {
            log.Printf("fail to publish message to Redis: %v", err)
        }
    }
}

