package userkv

import (
    "context"
)

// Service is userkv service
type Service struct {
    repository *Repository
}

// NewService is for wire.go
func NewService(repository *Repository) *Service {
    return &Service{repository: repository}
}

// Get returns a userkv by ID
func (s *Service) Get(ctx context.Context, userID string, key string) (string, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceGet")
    defer childSpan.End()

    return s.repository.Get(ctx, userID + ":" + key)
}

// Upsert updates a userkv
func (s *Service) Upsert(ctx context.Context, userID string, key string, value string) error {
    ctx, childSpan := tracer.Start(ctx, "ServiceUpsert")
    defer childSpan.End()

    return s.repository.Upsert(ctx, userID + ":" + key, value)
}

