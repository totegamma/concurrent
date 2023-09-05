package collection

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

type IRepository interface {
	CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	GetCollection(ctx context.Context, id string) (core.Collection, error)
	UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	DeleteCollection(ctx context.Context, id string) error

	CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
	UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
}

type Repository struct {
	db *gorm.DB
}

// NewRepository is used for wire.go
func NewRepository(db *gorm.DB) IRepository {
	return &Repository{db: db}
}

func (r *Repository) CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	return obj, r.db.Create(&obj).Error
}

func (r *Repository) GetCollection(ctx context.Context, id string) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetCollection")
	defer span.End()

	var obj core.Collection
	return obj, r.db.WithContext(ctx).Preload("Items").First(&obj, "id = ?", id).Error
}

func (r *Repository) UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateCollection")
	defer span.End()

	return obj, r.db.WithContext(ctx).Save(&obj).Error
}

func (r *Repository) DeleteCollection(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteCollection")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Collection{}, "id = ?", id).Error
}

func (r *Repository) CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateItem")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&item)
	return item, err.Error
}

func (r *Repository) GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetItem")
	defer span.End()

	var obj core.CollectionItem
	return obj, r.db.WithContext(ctx).First(&obj, "collection = ? and id = ?", id, itemId).Error
}

func (r *Repository) UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateItem")
	defer span.End()

	err := r.db.WithContext(ctx).Save(&item).Error

	return item, err
}

func (r *Repository) DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
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
