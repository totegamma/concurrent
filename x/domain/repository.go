package domain

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
	"time"
)

// Repository is the interface for host repository
type Repository interface {
    GetByFQDN(ctx context.Context, key string) (core.Domain, error)
    GetByCCID(ctx context.Context, ccid string) (core.Domain, error)
    Upsert(ctx context.Context, host *core.Domain) error
    GetList(ctx context.Context) ([]core.Domain, error)
    Delete(ctx context.Context, id string) (error)
    UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error
    Update(ctx context.Context, host *core.Domain) error
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new host repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// GetByFQDN returns a host by FQDN
func (r *repository) GetByFQDN(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetByFQDN")
	defer span.End()

	var host core.Domain
	err := r.db.WithContext(ctx).First(&host, "id = ?", key).Error
	return host, err
}

// GetByCCID returns a host by CCID
func (r *repository) GetByCCID(ctx context.Context, ccid string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetByCCID")
	defer span.End()

	var host core.Domain
	err := r.db.WithContext(ctx).First(&host, "cc_id = ?", ccid).Error
	return host, err
}

// Upsert creates new host
func (r *repository) Upsert(ctx context.Context, host *core.Domain) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	return r.db.WithContext(ctx).Save(&host).Error
}

// GetList returns list of schemas by schema
func (r *repository) GetList(ctx context.Context) ([]core.Domain, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetList")
	defer span.End()

	var hosts []core.Domain
	err := r.db.WithContext(ctx).Find(&hosts).Error
	return hosts, err
}

// Delete deletes a host
func (r *repository) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Domain{}, "id = ?", id).Error
}

// UpdateScrapeTime updates scrape time
func (r *repository) UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateScrapeTime")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Domain{}).Where("id = ?", id).Update("last_scraped", scrapeTime).Error
}

// Update updates a host
func (r *repository) Update(ctx context.Context, host *core.Domain) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpdate")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Domain{}).Where("id = ?", host.ID).Updates(&host).Error
}
