package service

import (
    "fmt"
    "concurrent/domain/model"
    "concurrent/domain/repository"
)

type AssociationService struct {
    repo repository.AssociationRepository
}

func NewAssociationService(repo repository.AssociationRepository) AssociationService {
    return AssociationService{repo: repo}
}

func (s *AssociationService) PostAssociation(association model.Association) {
    if err := VerifySignature(association.Payload, association.Author, association.Signature.R, association.Signature.S); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }

    s.repo.Create(association)
}

