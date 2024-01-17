package jwt

import (
	"context"
	"time"
)

// Service is the interface for auth service
type Service interface {
    CheckJTI(ctx context.Context, jti string) (bool, error)
    InvalidateJTI(ctx context.Context, jti string, exp time.Time) error
}

type service struct {
    repository Repository
}

// NewService creates a new auth service
func NewService(repository Repository) Service {
	return &service{repository}
}

// CheckJTI checks if jti is valid
func (s *service) CheckJTI(ctx context.Context, jti string) (bool, error) {
    ctx, span := tracer.Start(ctx, "ServiceCheckJTI")
    defer span.End()

    // check if jti exists
    return s.repository.CheckJTI(ctx, jti)
}

// InvalidateJTI invalidates jti
func (s *service) InvalidateJTI(ctx context.Context, jti string, exp time.Time) error {
    ctx, span := tracer.Start(ctx, "ServiceInvalidateJTI")
    defer span.End()

    // invalidate jti
    return s.repository.InvalidateJTI(ctx, jti, exp)
}
