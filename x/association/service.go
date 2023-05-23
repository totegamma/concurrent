package association

import (
    "log"
    "encoding/json"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/socket"
)

// Service is association service
type Service struct {
    repo *Repository
    stream *stream.Service
    socket *socket.Service
}

// NewService is used for wire.go
func NewService(repo *Repository, stream *stream.Service, socket *socket.Service) *Service {
    return &Service{repo: repo, stream: stream, socket: socket}
}

// PostAssociation creates new association
func (s *Service) PostAssociation(objectStr string, signature string, streams []string, targetType string) error {

    var object signedObject
    err := json.Unmarshal([]byte(objectStr), &object)
    if err != nil {
        return err
    }

    if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
        log.Println("verify signature err: ", err)
        return err
    }

    association := Association {
        Author: object.Signer,
        Schema: object.Schema,
        TargetID: object.Target,
        TargetType: targetType,
        Payload: objectStr,
        Signature: signature,
        Streams: streams,
    }

    s.repo.Create(&association)
    for _, stream := range association.Streams {
        s.stream.Post(stream, association.ID, "")
    }

    jsonstr, _ := json.Marshal(StreamEvent{
        Type: "association",
        Action: "create",
        Body: association,
    })
    s.socket.NotifyAllClients(jsonstr)

    return nil
}

// Get returns an association by ID
func (s *Service) Get(id string) Association {
    return s.repo.Get(id)
}

// GetOwn returns associations by author
func (s *Service) GetOwn(author string) []Association {
    return s.repo.GetOwn(author)
}

// Delete deletes an association by ID
func (s *Service) Delete(id string) {
    deleted := s.repo.Delete(id)
    jsonstr, _ := json.Marshal(StreamEvent{
        Type: "association",
        Action: "delete",
        Body: deleted,
    })
    s.socket.NotifyAllClients(jsonstr)
}

