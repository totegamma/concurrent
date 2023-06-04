package entity

import (
    "log"
    "net/http"
    "io/ioutil"
    "encoding/json"
    "github.com/totegamma/concurrent/x/core"
    "github.com/totegamma/concurrent/x/util"
)

// Service is entity service
type Service struct {
    repository *Repository
    config util.Config
}

// NewService is for wire.go
func NewService(repository *Repository, config util.Config) *Service {
    return &Service{ repository, config }
}


// Create updates stream information
func (s *Service) Create(ccaddr string, meta string) error {
    return s.repository.Create(&core.Entity{
        ID: ccaddr,
        Role: "default",
        Meta: meta,
    })
}

// Get returns stream information by ID
func (s *Service) Get(key string) (core.Entity, error) {
    return s.repository.Get(key)
}

// List returns streamList by schema
func (s *Service) List() ([]SafeEntity, error) {
    return s.repository.GetList()
}

// PullRemoteEntities copies remote entities
func (s *Service) PullRemoteEntities(host core.Host) error {
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
        err := s.repository.Upsert(&core.Entity{
            ID: entity.ID,
            Host: host.ID,
            Meta: "null",
        })
        if err != nil {
            log.Println(err)
        }
    }

    return nil
}

// ResolveHost resolves host from user address
func (s *Service) ResolveHost(user string) (string, error) {
    entity, err := s.repository.Get(user)
    if err != nil {
        return "", err
    }
    fqdn := entity.Host
    if fqdn == "" {
        fqdn = s.config.FQDN
    }
    return fqdn, nil
}


// IsUserExists returns true if user exists
func (s *Service) IsUserExists(user string) bool {
    entity, err := s.repository.Get(user)
    if err != nil {
        return false
    }
    return entity.ID != "" && entity.Host == ""
}

