package host

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"time"
)

// Service is stream service
type Service struct {
	repository *Repository
}

// NewService is for wire.go
func NewService(repository *Repository) *Service {
	return &Service{repository}
}

// Upsert updates stream information
func (s *Service) Upsert(ctx context.Context, host *core.Host) error {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	return s.repository.Upsert(ctx, host)
}

// Get returns stream information by FQDN
func (s *Service) GetByFQDN(ctx context.Context, key string) (core.Host, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.GetByFQDN(ctx, key)
}

// GetByCCID returns stream information by CCID
func (s *Service) GetByCCID(ctx context.Context, key string) (core.Host, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByCCID")
	defer span.End()

	return s.repository.GetByCCID(ctx, key)
}

// List returns streamList by schema
func (s *Service) List(ctx context.Context) ([]core.Host, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// Delete deletes a host
func (s *Service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}

// Update updates a host
func (s *Service) Update(ctx context.Context, host *core.Host) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	return s.repository.Update(ctx, host)
}

// UpdateScrapeTime updates scrape time
func (s *Service) UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateScrapeTime")
	defer span.End()

	return s.repository.UpdateScrapeTime(ctx, id, scrapeTime)
}
