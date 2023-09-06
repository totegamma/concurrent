package collection

import (
	"context"
	"fmt"
	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/core"
)

// Repository is the interface for collection repository
type Service interface {
	CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	GetCollection(ctx context.Context, id string) (core.Collection, error)
	UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	DeleteCollection(ctx context.Context, id string) error

	CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
	UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
}

type service struct {
	repo Repository
}

// NewRepository creates a new collection repository
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// CreateCollection creates new collection
func (s *service) CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreateCollection")
	defer span.End()

	if obj.ID != "" {
		return core.Collection{}, fmt.Errorf("id must be empty")
	}

	obj.ID = xid.New().String()

	return s.repo.CreateCollection(ctx, obj)
}

// GetCollection returns a Collection by ID
func (s *service) GetCollection(ctx context.Context, id string) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetCollection")
	defer span.End()

	return s.repo.GetCollection(ctx, id)
}

// UpdateCollection updates a collection
func (s *service) UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdateCollection")
	defer span.End()

	return s.repo.UpdateCollection(ctx, obj)
}

// DeleteCollection deletes a collection by ID
func (s *service) DeleteCollection(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDeleteCollection")
	defer span.End()

	return s.repo.DeleteCollection(ctx, id)
}

// CreateItem creates new collection item
func (s *service) CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreateItem")
	defer span.End()

	if item.ID != "" {
		return core.CollectionItem{}, fmt.Errorf("id must be empty")
	}

	item.ID = xid.New().String()

	return s.repo.CreateItem(ctx, item)
}

// GetItem returns a CollectionItem by ID
func (s *service) GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetItem")
	defer span.End()

	return s.repo.GetItem(ctx, id, itemId)
}

// UpdateItem updates a collection item
func (s *service) UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdateItem")
	defer span.End()

	return s.repo.UpdateItem(ctx, item)
}

// DeleteItem deletes a collection item by ID
func (s *service) DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceDeleteItem")
	defer span.End()

	return s.repo.DeleteItem(ctx, id, itemId)
}
