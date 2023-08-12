package entity

import (
	"context"
	"fmt"
	"time"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"golang.org/x/exp/slices"
)

// Service is entity service
type Service struct {
	repository *Repository
	config     util.Config
}

// NewService is for wire.go
func NewService(repository *Repository, config util.Config) *Service {
	return &Service{repository, config}
}

// Create updates stream information
func (s *Service) Create(ctx context.Context, ccid string, meta string) error {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

	return s.repository.Create(ctx, &core.Entity{
		ID:   ccid,
		Role: "default",
		Meta: meta,
	})
}

// Create updates stream information
func (s *Service) Register(ctx context.Context, ccid string, meta string, inviter string) error {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

	if s.config.Concurrent.Registration == "open" {
		return s.repository.Create(ctx, &core.Entity{
			ID:      ccid,
			Role:    "default",
			Meta:    meta,
			Inviter: "",
		})
	} else if s.config.Concurrent.Registration == "invite" {
		if inviter == "" {
			return fmt.Errorf("invitation code is required")
		}

		_, err := s.repository.Get(ctx, inviter)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// TODO: validate inviter role

		return s.repository.Create(ctx, &core.Entity{
			ID:      ccid,
			Role:    "default",
			Meta:    meta,
			Inviter: inviter,
		})
	} else {
		if inviter == "" {
			return fmt.Errorf("registration is not allowed")
		}

		_, err := s.repository.Get(ctx, inviter)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("registration is not allowed")
		}

		if !slices.Contains(s.config.Concurrent.Admins, inviter) {
			return fmt.Errorf("registration is not allowed")
		}

		return s.repository.Create(ctx, &core.Entity{
			ID:      ccid,
			Role:    "default",
			Meta:    meta,
			Inviter: inviter,
		})
	}
}

// Get returns stream information by ID
func (s *Service) Get(ctx context.Context, key string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	entity, err := s.repository.Get(ctx, key)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	if slices.Contains(s.config.Concurrent.Admins, entity.ID) {
		entity.Role = "_admin"
	}

	return entity, nil
}

// List returns streamList by schema
func (s *Service) List(ctx context.Context) ([]SafeEntity, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// ListModified returns stream which modified after given time
func (s *Service) ListModified(ctx context.Context, time time.Time) ([]SafeEntity, error) {
	ctx, span := tracer.Start(ctx, "ServiceListModified")
	defer span.End()

	return s.repository.ListModified(ctx, time)
}

// ResolveHost resolves host from user address
func (s *Service) ResolveHost(ctx context.Context, user string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceResolveHost")
	defer span.End()

	entity, err := s.repository.Get(ctx, user)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	fqdn := entity.Host
	if fqdn == "" {
		fqdn = s.config.Concurrent.FQDN
	}
	return fqdn, nil
}

// Update updates entity
func (s *Service) Update(ctx context.Context, entity *core.Entity) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	return s.repository.Update(ctx, entity)
}

// Upsert upserts entity
func (s *Service) Upsert(ctx context.Context, entity *core.Entity) error {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	return s.repository.Upsert(ctx, entity)
}

// IsUserExists returns true if user exists
func (s *Service) IsUserExists(ctx context.Context, user string) bool {
	ctx, span := tracer.Start(ctx, "ServiceIsUserExists")
	defer span.End()

	entity, err := s.repository.Get(ctx, user)
	if err != nil {
		return false
	}
	return entity.ID != "" && entity.Host == ""
}

// Delete deletes entity
func (s *Service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}
