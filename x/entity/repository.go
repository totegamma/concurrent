package entity

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/schema"
	"gorm.io/gorm"
)

// Repository is the interface for host repository
type Repository interface {
	GetEntity(ctx context.Context, key string) (core.Entity, error)
	GetEntityMeta(ctx context.Context, key string) (core.EntityMeta, error)
	CreateEntity(ctx context.Context, entity core.Entity) (core.Entity, error)
	CreateEntityMeta(ctx context.Context, meta core.EntityMeta) (core.EntityMeta, error)
	CreateEntityWithMeta(ctx context.Context, entity core.Entity, meta core.EntityMeta) (core.Entity, core.EntityMeta, error)
	UpdateEntity(ctx context.Context, entity *core.Entity) error
	SetTombstone(ctx context.Context, id, document, signature string) error
	GetList(ctx context.Context) ([]core.Entity, error)
	ListModified(ctx context.Context, modified time.Time) ([]core.Entity, error)
	Delete(ctx context.Context, key string) error
	Count(ctx context.Context) (int64, error)
	GetAddress(ctx context.Context, ccid string) (core.Address, error)
	UpdateAddress(ctx context.Context, ccid string, domain string, signedAt time.Time) error
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema schema.Service
}

// NewRepository creates a new host repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema schema.Service) Repository {

	var count int64
	err := db.Model(&core.Entity{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count entities",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "entity_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, mc, schema}
}

// Count returns the total number of entities
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCount")
	defer span.End()

	item, err := r.mc.Get("entity_count")
	if err != nil {
		span.RecordError(err)
		return 0, err
	}

	count, err := strconv.ParseInt(string(item.Value), 10, 64)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return count, nil
}

// GetAddress returns the address of a entity
func (r *repository) GetAddress(ctx context.Context, ccid string) (core.Address, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetAddress")
	defer span.End()

	var addr core.Address
	err := r.db.WithContext(ctx).First(&addr, "id = ?", ccid).Error
	return addr, err
}

// SetAddress sets the address of a entity
func (r *repository) SetAddress(ctx context.Context, ccid string, address string) error {
	ctx, span := tracer.Start(ctx, "RepositorySetAddress")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Entity{}).Where("id = ?", ccid).Update("address", address).Error
}

// UpdateAddress updates the address of a entity
func (r *repository) UpdateAddress(ctx context.Context, ccid string, domain string, signedAt time.Time) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateAddress")
	defer span.End()

	// create if not exists
	var addr core.Address
	err := r.db.WithContext(ctx).First(&addr, "id = ?", ccid).Error
	if err != nil {
		return r.db.WithContext(ctx).Create(&core.Address{
			ID:       ccid,
			Domain:   domain,
			SignedAt: signedAt,
		}).Error
	}

	return r.db.WithContext(ctx).Model(&core.Address{}).Where("id = ?", ccid).Update("domain", domain).Error
}

// SetTombstone sets the tombstone of a entity
func (r *repository) SetTombstone(ctx context.Context, id, document, signature string) error {
	ctx, span := tracer.Start(ctx, "RepositorySetThumbstone")
	defer span.End()

	err := r.db.Model(&core.Entity{}).Where("id = ?", id).Updates(map[string]interface{}{
		"tombstone_payload":   document,
		"tombstone_signature": signature,
	}).Error

	return err
}

// Get returns a entity by key
func (r *repository) GetEntity(ctx context.Context, key string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var entity core.Entity
	err := r.db.WithContext(ctx).First(&entity, "id = ?", key).Error
	return entity, err
}

func (r *repository) GetEntityMeta(ctx context.Context, key string) (core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetMeta")
	defer span.End()

	var meta core.EntityMeta
	err := r.db.WithContext(ctx).First(&meta, "id = ?", key).Error
	return meta, err
}

// Create creates new entity
func (r *repository) CreateEntity(ctx context.Context, entity core.Entity) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return core.Entity{}, err
	}

	r.mc.Increment("entity_count", 1)

	return entity, nil
}

// CreateEntityMeta creates new entity meta
func (r *repository) CreateEntityMeta(ctx context.Context, meta core.EntityMeta) (core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateMeta")
	defer span.End()

	err := r.db.WithContext(ctx).Create(meta).Error

	return meta, err
}

func (r *repository) CreateEntityWithMeta(ctx context.Context, entity core.Entity, meta core.EntityMeta) (core.Entity, core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateWithMeta")
	defer span.End()

	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		if err := tx.Create(&meta).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return core.Entity{}, core.EntityMeta{}, err
	}

	r.mc.Increment("entity_count", 1)

	return entity, meta, nil
}

// GetList returns all entities
func (r *repository) GetList(ctx context.Context) ([]core.Entity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetList")
	defer span.End()

	var entities []core.Entity
	err := r.db.WithContext(ctx).Model(&core.Entity{}).Find(&entities).Error
	return entities, err
}

// ListModified returns all entities which modified after given time
func (r *repository) ListModified(ctx context.Context, time time.Time) ([]core.Entity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryListModified")
	defer span.End()

	var entities []core.Entity
	err := r.db.WithContext(ctx).Model(&core.Entity{}).Where("m_date > ?", time).Find(&entities).Error
	return entities, err
}

// Delete deletes a entity
func (r *repository) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	err := r.db.WithContext(ctx).Delete(&core.Entity{}, "id = ?", id).Error

	if err == nil {
		r.mc.Decrement("entity_count", 1)
	}

	return err
}

// Update updates a entity
func (r *repository) UpdateEntity(ctx context.Context, entity *core.Entity) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpdate")
	defer span.End()

	return r.db.WithContext(ctx).Where("id = ?", entity.ID).Updates(&entity).Error
}
