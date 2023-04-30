package service

import (
    "fmt"
    "concurrent/domain/model"
    "concurrent/domain/repository"
)

type MessageService struct {
    repo repository.MessageRepository
}

func NewMessageService(repo repository.MessageRepository) MessageService {
    return MessageService{repo: repo}
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

    s.repo.Create(message)
}

