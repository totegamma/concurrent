package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

type service struct {
	repository Repository
	client     client.Client
	config     util.Config
}

// NewService creates a new host service
func NewService(repository Repository, client client.Client, config util.Config) core.DomainService {
	return &service{repository, client, config}
}

// Upsert creates new host
func (s *service) Upsert(ctx context.Context, host core.Domain) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.Upsert")
	defer span.End()

	return s.repository.Upsert(ctx, host)
}

// GetByFQDN returns domain by FQDN
func (s *service) GetByFQDN(ctx context.Context, fqdn string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.GetByFQDN")
	defer span.End()

	domain, err := s.repository.GetByFQDN(ctx, fqdn)
	if err == nil {
		if domain.Dimension != s.config.Concurrent.Dimension {
			return core.Domain{}, fmt.Errorf("domain is not in the same dimension")
		}
		return domain, nil
	}

	domain, err = s.client.GetDomain(ctx, fqdn)
	if err != nil {
		return core.Domain{}, err
	}

	_, err = s.repository.Upsert(ctx, domain)
	if err != nil {
		return core.Domain{}, err
	}

	if domain.Dimension != s.config.Concurrent.Dimension {
		return core.Domain{}, fmt.Errorf("domain is not in the same dimension")
	}

	return domain, nil
}

// GetByCCID returns domain by CCID
func (s *service) GetByCCID(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.GetByCCID")
	defer span.End()

	return s.repository.GetByCCID(ctx, key)
}

// List returns list of domains
func (s *service) List(ctx context.Context) ([]core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.List")
	defer span.End()

	return s.repository.GetList(ctx)
}

// Delete deletes a domain
func (s *service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Domain.Service.Delete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}

// Update updates a domain
func (s *service) Update(ctx context.Context, host core.Domain) error {
	ctx, span := tracer.Start(ctx, "Domain.Service.Update")
	defer span.End()

	return s.repository.Update(ctx, host)
}

// UpdateScrapeTime updates a domain's scrape time
func (s *service) UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error {
	ctx, span := tracer.Start(ctx, "Domain.Service.UpdateScrapeTime")
	defer span.End()

	return s.repository.UpdateScrapeTime(ctx, id, scrapeTime)
}
