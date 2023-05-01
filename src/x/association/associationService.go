package association

import (
    "fmt"
    "github.com/totegamma/concurrent/x/util"
)

type AssociationService struct {
    repo AssociationRepository
}

func NewAssociationService(repo AssociationRepository) AssociationService {
    return AssociationService{repo: repo}
}

func (s *AssociationService) PostAssociation(association Association) {
    if err := util.VerifySignature(association.Payload, association.Author, association.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }

    s.repo.Create(association)
}

