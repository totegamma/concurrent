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

// Get returns stream information by FQDN
func (s *Service) GetByFQDN(ctx context.Context, key string) (core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceGet")
    defer childSpan.End()

    return s.repository.GetByFQDN(ctx, key)
}

// GetByCCID returns stream information by CCID
func (s *Service) GetByCCID(ctx context.Context, key string) (core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceGetByCCID")
    defer childSpan.End()

    return s.repository.GetByCCID(ctx, key)
}


// List returns streamList by schema
func (s *Service) List(ctx context.Context, ) ([]core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceList")
    defer childSpan.End()

    return s.repository.GetList(ctx)
}

// Delete deletes a host
func (s *Service) Delete(ctx context.Context, id string) error {
    ctx, childSpan := tracer.Start(ctx, "ServiceDelete")
    defer childSpan.End()

    return s.repository.Delete(ctx, id)
}

