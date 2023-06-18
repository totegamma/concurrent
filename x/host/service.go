package host

import (
    "context"
    "github.com/totegamma/concurrent/x/core"
)

// Service is stream service
type Service struct {
    repository *Repository
}

// NewService is for wire.go
func NewService(repository *Repository) *Service {
    return &Service{ repository }
}


// Upsert updates stream information
func (s *Service) Upsert(ctx context.Context, host *core.Host) error {
    ctx, childSpan := tracer.Start(ctx, "ServiceUpsert")
    defer childSpan.End()

    return s.repository.Upsert(ctx, host)
}

// Get returns stream information by ID
func (s *Service) Get(ctx context.Context, key string) (core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceGet")
    defer childSpan.End()

    return s.repository.Get(ctx, key)
}

// List returns streamList by schema
func (s *Service) List(ctx context.Context, ) ([]core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceList")
    defer childSpan.End()

    return s.repository.GetList(ctx)
}

