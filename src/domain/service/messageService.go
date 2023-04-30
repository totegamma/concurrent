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

    if (len(followee) > 0) {
        messages = s.repo.GetFollowee(followee)
    } else {
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

