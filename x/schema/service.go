package schema

import (
	"context"

	"github.com/totegamma/concurrent/x/core"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) core.SchemaService {
	return &service{repo: repo}
}

func (s *service) UrlToID(ctx context.Context, url string) (uint, error) {
	ctx, span := tracer.Start(ctx, "Schema.Service.UrlToID")
	defer span.End()

	schema, err := s.repo.Upsert(ctx, url)
	if err != nil {
		return 0, err
	}
	return schema.ID, nil
}

func (s *service) IDToUrl(ctx context.Context, id uint) (string, error) {
	ctx, span := tracer.Start(ctx, "Schema.Service.IDToUrl")
	defer span.End()

	schema, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return schema.URL, nil
}
