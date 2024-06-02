package userkv

import (
	"context"
)

// Service is the interface for userkv service
type Service interface {
	Get(ctx context.Context, userID string, key string) (string, error)
	Upsert(ctx context.Context, userID string, key string, value string) error
	Clean(ctx context.Context, ccid string) error
}

type service struct {
	repository Repository
}

// NewService creates a new userkv service
func NewService(repository Repository) Service {
	return &service{repository: repository}
}

// Get returns a userkv by ID
func (s *service) Get(ctx context.Context, userID string, key string) (string, error) {
	ctx, span := tracer.Start(ctx, "UserKV.Service.Get")
	defer span.End()

	return s.repository.Get(ctx, userID, key)
}

// Upsert updates a userkv
func (s *service) Upsert(ctx context.Context, userID string, key string, value string) error {
	ctx, span := tracer.Start(ctx, "UserKV.Service.Upsert")
	defer span.End()

	return s.repository.Upsert(ctx, userID, key, value)
}

// Clean deletes all userkvs for a given owner
func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "UserKV.Service.Clean")
	defer span.End()

	return s.repository.Clean(ctx, ccid)
}
