package entity

import (
    "time"
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
func (r *Repository) Get(ctx context.Context, key string) (core.Entity, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    var entity core.Entity
    err := r.db.WithContext(ctx).First(&entity, "id = ?", key).Error
    return entity, err 
}

// Create creates a entity
func (r *Repository) Create(ctx context.Context, entity *core.Entity) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryCreate")
    defer childSpan.End()

    return r.db.WithContext(ctx).Create(&entity).Error
}

// Upsert updates a entity
func (r *Repository) Upsert(ctx context.Context, entity *core.Entity) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsert")
    defer childSpan.End()

    return r.db.WithContext(ctx).Save(&entity).Error
}

// GetList returns all entities
func (r *Repository) GetList(ctx context.Context, ) ([]SafeEntity, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetList")
    defer childSpan.End()

    var entities []SafeEntity
    err := r.db.WithContext(ctx).Model(&core.Entity{}).Where("host IS NULL or host = ''").Find(&entities).Error
    return entities, err
}

// ListModified returns all entities which modified after given time
func (r *Repository) ListModified(ctx context.Context, time time.Time) ([]SafeEntity, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryListModified")
    defer childSpan.End()

    var entities []SafeEntity
    err := r.db.WithContext(ctx).Model(&core.Entity{}).Where("m_date > ?", time).Find(&entities).Error
    return entities, err
}

// Delete deletes a entity
func (r *Repository) Delete(ctx context.Context, id string) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryDelete")
    defer childSpan.End()

    return r.db.WithContext(ctx).Delete(&core.Entity{}, "id = ?", id).Error
}

// Update updates a entity
func (r *Repository) Update(ctx context.Context, entity *core.Entity) error {
    ctx, span := tracer.Start(ctx, "RepositoryUpdate")
    defer span.End()

    return r.db.WithContext(ctx).Where("id = ?", entity.ID).Updates(&entity).Error
}


