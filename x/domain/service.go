package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/core"
)

type service struct {
	repository Repository
	client     client.Client
	config     core.Config
}

// NewService creates a new host service
func NewService(repository Repository, client client.Client, config core.Config) core.DomainService {
	return &service{repository, client, config}
}

// Upsert creates new host
func (s *service) Upsert(ctx context.Context, host core.Domain) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.Upsert")
	defer span.End()

	return s.repository.Upsert(ctx, host)
}

// Get returns a domain by ID. It handles FQDN, CCID, and CSID formats.
// For CSID, it returns the local domain information if the CSID matches the server's CSID.
func (s *service) Get(ctx context.Context, id string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.Get")
	defer span.End()

	if core.IsCCID(id) {
		return s.repository.GetByCCID(ctx, id)
	}

	if core.IsCSID(id) {
		if id == s.config.CSID {
			return core.Domain{
				ID:        s.config.FQDN,
				Dimension: s.config.Dimension,
				CSID:      s.config.CSID,
				CCID:      s.config.CCID,
			}, nil
		}
		return s.repository.GetByCSID(ctx, id)
	}

	return s.repository.GetByFQDN(ctx, id)
}

// GetByFQDN returns domain by FQDN
func (s *service) GetByFQDN(ctx context.Context, fqdn string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.GetByFQDN")
	defer span.End()

	domain, err := s.repository.GetByFQDN(ctx, fqdn)
	if err == nil {
		return domain, nil
	}

	domain, err = s.client.GetDomain(ctx, fqdn, nil)
	if err != nil {
		return core.Domain{}, err
	}

	if domain.Dimension != s.config.Dimension {
		return core.Domain{}, fmt.Errorf("domain is not in the same dimension")
	}

	_, err = s.repository.Upsert(ctx, domain)
	if err != nil {
		return core.Domain{}, err
	}

	return domain, nil
}

// ForceFetch fetches domain information directly from the remote domain specified by FQDN,
// bypassing any cached data in the local repository, and updates the local repository.
func (s *service) ForceFetch(ctx context.Context, fqdn string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Domain.Service.ForceFetch")
	defer span.End()

	domain, err := s.client.GetDomain(ctx, fqdn, nil)
	if err != nil {
		return core.Domain{}, err
	}

	if domain.Dimension != s.config.Dimension {
		return core.Domain{}, fmt.Errorf("domain is not in the same dimension")
	}

	_, err = s.repository.Upsert(ctx, domain)
	if err != nil {
		return core.Domain{}, err
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
