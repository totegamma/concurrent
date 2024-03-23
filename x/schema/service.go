package schema

import (
	"context"
)

type Service interface {
	UrlToID(ctx context.Context, url string) (uint, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
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
