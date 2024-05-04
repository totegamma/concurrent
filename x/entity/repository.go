package entity

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/core"
	"gorm.io/gorm"
)

// Repository is the interface for host repository
type Repository interface {
	Get(ctx context.Context, key string) (core.Entity, error)
	GetByAlias(ctx context.Context, alias string) (core.Entity, error)
	SetAlias(ctx context.Context, id, alias string) error
	GetMeta(ctx context.Context, key string) (core.EntityMeta, error)
	Create(ctx context.Context, entity core.Entity) (core.Entity, error)
	CreateMeta(ctx context.Context, meta core.EntityMeta) (core.EntityMeta, error)
	CreateWithMeta(ctx context.Context, entity core.Entity, meta core.EntityMeta) (core.Entity, core.EntityMeta, error)
	UpdateScore(ctx context.Context, id string, score int) error
	UpdateTag(ctx context.Context, id, tag string) error
	SetTombstone(ctx context.Context, id, document, signature string) error
	GetList(ctx context.Context) ([]core.Entity, error)
	Delete(ctx context.Context, key string) error
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema core.SchemaService
}

// NewRepository creates a new host repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema core.SchemaService) Repository {

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
	ctx, span := tracer.Start(ctx, "Entity.Repository.Count")
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

// SetTombstone sets the tombstone of a entity
func (r *repository) SetTombstone(ctx context.Context, id, document, signature string) error {
	ctx, span := tracer.Start(ctx, "Entity.Repository.SetTombstone")
	defer span.End()

	err := r.db.Model(&core.Entity{}).Where("id = ?", id).Updates(map[string]interface{}{
		"tombstone_payload":   document,
		"tombstone_signature": signature,
	}).Error

	return err
}

// Get returns a entity by key
func (r *repository) Get(ctx context.Context, key string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.Get")
	defer span.End()

	var entity core.Entity
	err := r.db.WithContext(ctx).First(&entity, "id = ?", key).Error
	return entity, err
}

func (r *repository) GetByAlias(ctx context.Context, alias string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.GetByAlias")
	defer span.End()

	var entity core.Entity
	err := r.db.WithContext(ctx).First(&entity, "alias = ?", alias).Error
	return entity, err
}

func (r *repository) SetAlias(ctx context.Context, id, alias string) error {
	ctx, span := tracer.Start(ctx, "Entity.Repository.SetAlias")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Entity{}).Where("id = ?", id).Update("alias", alias).Error
}

func (r *repository) GetMeta(ctx context.Context, key string) (core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.GetMeta")
	defer span.End()

	var meta core.EntityMeta
	err := r.db.WithContext(ctx).First(&meta, "id = ?", key).Error
	return meta, err
}

// Create creates new entity
func (r *repository) Create(ctx context.Context, entity core.Entity) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.Create")
	defer span.End()

	if err := r.db.WithContext(ctx).Create(&entity).Error; err != nil {
		return core.Entity{}, err
	}

	r.mc.Increment("entity_count", 1)

	return entity, nil
}

// CreateEntityMeta creates new entity meta
func (r *repository) CreateMeta(ctx context.Context, meta core.EntityMeta) (core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.CreateMeta")
	defer span.End()

	err := r.db.WithContext(ctx).Create(meta).Error

	return meta, err
}

func (r *repository) CreateWithMeta(ctx context.Context, entity core.Entity, meta core.EntityMeta) (core.Entity, core.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "Entity.Repository.CreateWithMeta")
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
	ctx, span := tracer.Start(ctx, "Entity.Repository.GetList")
	defer span.End()

	var entities []core.Entity
	err := r.db.WithContext(ctx).Model(&core.Entity{}).Find(&entities).Error
	return entities, err
}

// Delete deletes a entity
func (r *repository) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Entity.Repository.Delete")
	defer span.End()

	err := r.db.WithContext(ctx).Delete(&core.Entity{}, "id = ?", id).Error

	if err == nil {
		r.mc.Decrement("entity_count", 1)
	}

	return err
}

func (r *repository) UpdateScore(ctx context.Context, id string, score int) error {
	ctx, span := tracer.Start(ctx, "Entity.Repository.UpdateScore")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Entity{}).Where("id = ?", id).Update("score", score).Error
}

func (r *repository) UpdateTag(ctx context.Context, id, tag string) error {
	ctx, span := tracer.Start(ctx, "Entity.Repository.UpdateTag")
	defer span.End()

	return r.db.WithContext(ctx).Model(&core.Entity{}).Where("id = ?", id).Update("tag", tag).Error
}
