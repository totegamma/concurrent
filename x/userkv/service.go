package userkv

import (
	"context"
)

type IService interface {
	Get(ctx context.Context, userID string, key string) (string, error)
	Upsert(ctx context.Context, userID string, key string, value string) error
}

// Service is userkv service
type Service struct {
	repository IRepository
}

// NewService is for wire.go
func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

// Get returns a userkv by ID
func (s *Service) Get(ctx context.Context, userID string, key string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.Get(ctx, userID+":"+key)
}

// Upsert updates a userkv
func (s *Service) Upsert(ctx context.Context, userID string, key string, value string) error {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	return s.repository.Upsert(ctx, userID+":"+key, value)
}
