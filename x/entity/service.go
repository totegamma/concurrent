package entity

import (
    "log"
    "net/http"
    "io/ioutil"
    "encoding/json"
    "github.com/totegamma/concurrent/x/host"
)

// Service is entity service
type Service struct {
    repository Repository
}

// NewService is for wire.go
func NewService(repository Repository) Service {
    return Service{ repository }
}


// Create updates stream information
func (s *Service) Create(ccaddr string, meta string) {
    s.repository.Create(&Entity{
        ID: ccaddr,
        Role: "default",
        Meta: meta,
    })
}

// Get returns stream information by ID
func (s *Service) Get(key string) Entity {
    return s.repository.Get(key)
}

// List returns streamList by schema
func (s *Service) List() []SafeEntity {
    entities := s.repository.GetList()
    return entities
}

// PullRemoteEntities copies remote entities
func (s *Service) PullRemoteEntities(host host.Host) error {
    req, err := http.NewRequest("GET", "https://" + host.ID + "/api/v1/entity/list", nil)
    if err != nil {
        return err
    }
    client := new(http.Client)
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    body, _ := ioutil.ReadAll(resp.Body)

    var remoteEntities []SafeEntity
    json.Unmarshal(body, &remoteEntities)

    log.Print(remoteEntities)

    for _, entity := range remoteEntities {
        s.repository.Upsert(&Entity{
            ID: entity.ID,
            Host: host.ID,
        })
    }

    return nil
}

