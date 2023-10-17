package domain

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"time"
)

// Service is the interface for host service
type Service interface {
	Upsert(ctx context.Context, host *core.Domain) error
	GetByFQDN(ctx context.Context, key string) (core.Domain, error)
	GetByCCID(ctx context.Context, key string) (core.Domain, error)
	List(ctx context.Context) ([]core.Domain, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, host *core.Domain) error
	UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error
}

type service struct {
	repository Repository
}

// NewService creates a new host service
func NewService(repository Repository) Service {
	return &service{repository}
}

// Upsert creates new host
func (s *service) Upsert(ctx context.Context, host *core.Domain) error {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	return s.repository.Upsert(ctx, host)
}

// GetByFQDN returns domain by FQDN
func (s *service) GetByFQDN(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.GetByFQDN(ctx, key)
}

// GetByCCID returns domain by CCID
func (s *service) GetByCCID(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByCCID")
	defer span.End()

	return s.repository.GetByCCID(ctx, key)
}

// List returns list of domains
func (s *service) List(ctx context.Context) ([]core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// Delete deletes a domain
func (s *service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}

// Update updates a domain
func (s *service) Update(ctx context.Context, host *core.Domain) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	return s.repository.Update(ctx, host)
}

// UpdateScrapeTime updates a domain's scrape time
func (s *service) UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateScrapeTime")
	defer span.End()

	return s.repository.UpdateScrapeTime(ctx, id, scrapeTime)
}
