package message

import (
    "fmt"
    "strings"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/stream"
)

type MessageService struct {
    repo MessageRepository
    stream stream.StreamService
}

func NewMessageService(repo MessageRepository, stream stream.StreamService) MessageService {
    return MessageService{repo: repo, stream: stream}
}

func (s *MessageService) GetMessage(id string) Message{
    var message Message
    message = s.repo.Get(id)
    return message
}

func (s *MessageService) GetMessages(followee []string) []Message{
    var messages []Message
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

func (s *MessageService) PostMessage(message Message) {
    if err := util.VerifySignature(message.Payload, message.Author, message.Signature); err != nil {
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

func (s *MessageService) DeleteMessage(id string) {
    deleted := s.repo.Delete(id)
    for _, stream := range strings.Split(deleted.Streams, ",") {
        s.stream.Delete(stream, id)
    }
}

