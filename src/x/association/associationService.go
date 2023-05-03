package association

import (
    "log"
    "encoding/json"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/socket"
)

type AssociationService struct {
    repo AssociationRepository
    socket *socket.SocketService
}

func NewAssociationService(repo AssociationRepository, socketService *socket.SocketService) AssociationService {
    return AssociationService{repo: repo, socket: socketService}
}

func (s *AssociationService) PostAssociation(association Association) {
    if err := util.VerifySignature(association.Payload, association.Author, association.Signature); err != nil {
        log.Println("verify signature err: ", err)
        return
    }

    s.repo.Create(association)

    jsonstr, _ := json.Marshal(AssociationStreamEvent{
        Type: "association",
        Action: "create",
        Body: association,
    })
    s.socket.NotifyAllClients(jsonstr)
}

func (s *AssociationService) GetOwn(author string) []Association {
    return s.repo.GetOwn(author)
}

func (s *AssociationService) Delete(id string) {
    deleted := s.repo.Delete(id)
    jsonstr, _ := json.Marshal(AssociationStreamEvent{
        Type: "association",
        Action: "delete",
        Body: deleted,
    })
    s.socket.NotifyAllClients(jsonstr)
}

