package schema

import (
	"context"

	"github.com/concrnt/concrnt/core"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) core.SchemaService {
	return &service{repo: repo}
}

// UrlToID converts a schema URL to its internal ID, creating the schema record if it doesn't exist.
func (s *service) UrlToID(ctx context.Context, url string) (uint, error) {
	ctx, span := tracer.Start(ctx, "Schema.Service.UrlToID")
	defer span.End()

	schema, err := s.repo.Upsert(ctx, url)
	if err != nil {
		return 0, err
	}
	return schema.ID, nil
}

// IDToUrl converts an internal schema ID back to its URL.
func (s *service) IDToUrl(ctx context.Context, id uint) (string, error) {
	ctx, span := tracer.Start(ctx, "Schema.Service.IDToUrl")
	defer span.End()

	schema, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return schema.URL, nil
}
