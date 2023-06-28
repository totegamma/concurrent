package host

import (
    "context"
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)

// Repository is host repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Get returns a host by ID
func (r *Repository) Get(ctx context.Context, key string) (core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    var host core.Host
    err := r.db.WithContext(ctx).First(&host, "id = ?", key).Error
    return host, err
}

// Upsert updates a stream
func (r *Repository) Upsert(ctx context.Context, host *core.Host) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsert")
    defer childSpan.End()

    return r.db.WithContext(ctx).Save(&host).Error
}

// GetList returns list of schemas by schema
func (r *Repository) GetList(ctx context.Context, ) ([]core.Host, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetList")
    defer childSpan.End()

    var hosts []core.Host
    err := r.db.WithContext(ctx).Find(&hosts).Error
    return hosts, err
}

// Delete deletes a host
func (r *Repository) Delete(ctx context.Context, id string) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryDelete")
    defer childSpan.End()

    return r.db.WithContext(ctx).Delete(&core.Host{}, "id = ?", id).Error
}

