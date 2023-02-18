package main

import (
    "net/http"
    "encoding/json"
    "fmt"
    "bytes"
    "io"
)

type AssociationService struct {
    repo AssociationRepository
}

func NewAssociationService(repo AssociationRepository) AssociationService {
    return AssociationService{repo: repo}
}

func (s *AssociationService) postAssociation(association Association) {
    if err := verifySignature(association.Payload, association.Author, association.Signature); err != nil {
        fmt.Println("err: ", err)
        fmt.Println("拒否")
        return
    } else {
        fmt.Println("承認")
    }

    s.repo.Create(association)
}


func (backend Backend) associationHandler(w http.ResponseWriter, r *http.Request) {

    associationService := SetupAssociationService(backend.DB)

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodPost:
            body := r.Body
            defer body.Close()

            buf := new(bytes.Buffer)
            io.Copy(buf, body)

            var association Association
            json.Unmarshal(buf.Bytes(), &association)

            associationService.postAssociation(association)
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

