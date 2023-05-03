package message

import (
    "log"
    "strings"
    "encoding/json"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/socket"
)

type MessageService struct {
    repo MessageRepository
    stream stream.StreamService
    socket *socket.SocketService
}

func NewMessageService(repo MessageRepository, stream stream.StreamService, socketService *socket.SocketService) MessageService {
    return MessageService{repo: repo, stream: stream, socket: socketService}
}

func (s *MessageService) GetMessage(id string) Message{
    var message Message
    message = s.repo.Get(id)
    return message
}

func (s *MessageService) PostMessage(message Message) {
    if err := util.VerifySignature(message.Payload, message.Author, message.Signature); err != nil {
        log.Println("verify signature err: ", err)
        return
    }
    id := s.repo.Create(message)
    for _, stream := range strings.Split(message.Streams, ",") {
        s.stream.Post(stream, id)
    }

    jsonstr, _ := json.Marshal(MessageStreamEvent{
        Type: "message",
        Action: "create",
        Body: message,
    })
    s.socket.NotifyAllClients(jsonstr)
}

func (s *MessageService) DeleteMessage(id string) {
    deleted := s.repo.Delete(id)
    for _, stream := range strings.Split(deleted.Streams, ",") {
        s.stream.Delete(stream, id)
    }
    jsonstr, _ := json.Marshal(MessageStreamEvent{
        Type: "message",
        Action: "delete",
        Body: deleted,
    })
    s.socket.NotifyAllClients(jsonstr)
}

