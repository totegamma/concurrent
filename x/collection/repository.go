package collection

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for collection repository
type Repository interface {
	CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	GetCollection(ctx context.Context, id string) (core.Collection, error)
	UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	DeleteCollection(ctx context.Context, id string) error

	CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
	UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new collection repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// CreateCollection creates new collection
func (r *repository) CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	return obj, r.db.Create(&obj).Error
}

// GetCollection returns a Collection by ID
func (r *repository) GetCollection(ctx context.Context, id string) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetCollection")
	defer span.End()

	var obj core.Collection
	return obj, r.db.WithContext(ctx).Preload("Items").First(&obj, "id = ?", id).Error
}

// UpdateCollection updates a collection
func (r *repository) UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateCollection")
	defer span.End()

	return obj, r.db.WithContext(ctx).Save(&obj).Error
}

// DeleteCollection deletes a collection by ID
func (r *repository) DeleteCollection(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteCollection")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Collection{}, "id = ?", id).Error
}

// CreateItem creates new collection item
func (r *repository) CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateItem")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&item)
	return item, err.Error
}

// GetItem returns a collection item by ID
func (r *repository) GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetItem")
	defer span.End()

	var obj core.CollectionItem
	return obj, r.db.WithContext(ctx).First(&obj, "collection = ? and id = ?", id, itemId).Error
}

// UpdateItem updates a collection item
func (r *repository) UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateItem")
	defer span.End()

	err := r.db.WithContext(ctx).Save(&item).Error

	return item, err
}

// DeleteItem deletes a collection item by ID
func (r *repository) DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteItem")
	defer span.End()

	// get deleted
	var deleted core.CollectionItem
	err := r.db.WithContext(ctx).First(&deleted, "collection = ? and id = ?", id, itemId).Error
	if err != nil {
		return deleted, err
	}

	err = r.db.WithContext(ctx).Where("collection = ? and id = ?", id, itemId).Delete(&deleted).Error

	return deleted, err
}
