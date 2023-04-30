package service

import (
    "fmt"
    "strings"
    "concurrent/domain/model"
    "concurrent/domain/repository"
    "concurrent/x/stream"
)

type MessageService struct {
    repo repository.MessageRepository
    stream stream.StreamService
}

func NewMessageService(repo repository.MessageRepository, stream stream.StreamService) MessageService {
    return MessageService{repo: repo, stream: stream}
}

func (s *MessageService) GetMessage(id string) model.Message{
    var message model.Message
    message = s.repo.Get(id)
    return message
}

func (s *MessageService) GetMessages(followee []string) []model.Message{
    var messages []model.Message
    fmt.Printf("%v\n", followee);
    fmt.Printf("switch: %v\n", len(followee))
    if (len(followee) > 0) {
        fmt.Println("get followee")
        messages = s.repo.GetFollowee(followee)
    } else {
        fmt.Println("get all")
        messages = s.repo.GetAll()
    }

    return messages
}

func (s *MessageService) PostMessage(message model.Message) {
    if err := VerifySignature(message.Payload, message.Author, message.R, message.S); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }
    id := s.repo.Create(message)
    for _, stream := range strings.Split(message.Streams, ",") {
        s.stream.Post(stream, id)
    }
}

