package collection

import (
	"fmt"
	"context"
	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/core"
)

type IService interface {
	CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	GetCollection(ctx context.Context, id string) (core.Collection, error)
	UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error)
	DeleteCollection(ctx context.Context, id string) error

	CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
	UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error)
	DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error)
}

type Service struct {
	repo IRepository
}

func NewService(repo IRepository) IService {
	return &Service{repo: repo}
}

func (s *Service) CreateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreateCollection")
	defer span.End()

	if obj.ID != "" {
		return core.Collection{}, fmt.Errorf("id must be empty")
	}

	obj.ID = xid.New().String()

	return s.repo.CreateCollection(ctx, obj)
}

func (s *Service) GetCollection(ctx context.Context, id string) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetCollection")
	defer span.End()

	return s.repo.GetCollection(ctx, id)
}

func (s *Service) UpdateCollection(ctx context.Context, obj core.Collection) (core.Collection, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdateCollection")
	defer span.End()

	return s.repo.UpdateCollection(ctx, obj)
}

func (s *Service) DeleteCollection(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDeleteCollection")
	defer span.End()

	return s.repo.DeleteCollection(ctx, id)
}

func (s *Service) CreateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreateItem")
	defer span.End()

	if item.ID != "" {
		return core.CollectionItem{}, fmt.Errorf("id must be empty")
	}

	item.ID = xid.New().String()

	return s.repo.CreateItem(ctx, item)
}

func (s *Service) GetItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetItem")
	defer span.End()

	return s.repo.GetItem(ctx, id, itemId)
}

func (s *Service) UpdateItem(ctx context.Context, item core.CollectionItem) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdateItem")
	defer span.End()

	return s.repo.UpdateItem(ctx, item)
}

func (s *Service) DeleteItem(ctx context.Context, id string, itemId string) (core.CollectionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceDeleteItem")
	defer span.End()

	return s.repo.DeleteItem(ctx, id, itemId)
}

