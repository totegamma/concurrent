package entity

import (
    "log"
    "context"
    "net/http"
    "io/ioutil"
    "encoding/json"
    "golang.org/x/exp/slices"
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
func (s *Service) Create(ctx context.Context, ccaddr string, meta string) error {
    ctx, childSpan := tracer.Start(ctx, "ServiceCreate")
    defer childSpan.End()

    return s.repository.Create(ctx, &core.Entity{
        ID: ccaddr,
        Role: "default",
        Meta: meta,
    })
}

// Get returns stream information by ID
func (s *Service) Get(ctx context.Context, key string) (core.Entity, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceGet")
    defer childSpan.End()

    entity, err := s.repository.Get(ctx, key)
    if err != nil {
        return core.Entity{}, err
    }

    if slices.Contains(s.config.Concurrent.Admins, entity.ID) {
        entity.Role = "_admin"
    }

    return entity, nil
}

// List returns streamList by schema
func (s *Service) List(ctx context.Context, ) ([]SafeEntity, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceList")
    defer childSpan.End()

    return s.repository.GetList(ctx)
}

// PullRemoteEntities copies remote entities
func (s *Service) PullRemoteEntities(ctx context.Context, host core.Host) error {
    ctx, childSpan := tracer.Start(ctx, "ServicePullRemoteEntities")
    defer childSpan.End()

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
        err := s.repository.Upsert(ctx, &core.Entity{
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
func (s *Service) ResolveHost(ctx context.Context, user string) (string, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceResolveHost")
    defer childSpan.End()

    entity, err := s.repository.Get(ctx, user)
    if err != nil {
        return "", err
    }
    fqdn := entity.Host
    if fqdn == "" {
        fqdn = s.config.Concurrent.FQDN
    }
    return fqdn, nil
}


// IsUserExists returns true if user exists
func (s *Service) IsUserExists(ctx context.Context, user string) bool {
    ctx, childSpan := tracer.Start(ctx, "ServiceIsUserExists")
    defer childSpan.End()

    entity, err := s.repository.Get(ctx, user)
    if err != nil {
        return false
    }
    return entity.ID != "" && entity.Host == ""
}

