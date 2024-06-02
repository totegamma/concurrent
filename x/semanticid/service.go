package semanticid

import (
	"context"
	"github.com/totegamma/concurrent/core"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) core.SemanticIDService {
	return &service{repo}
}

func (s *service) Name(ctx context.Context, id, owner, target, document, signature string) (core.SemanticID, error) {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Name")
	defer span.End()

	created, err := s.repo.Upsert(ctx, core.SemanticID{
		ID:        id,
		Owner:     owner,
		Target:    target,
		Document:  document,
		Signature: signature,
	})

	if err != nil {
		return core.SemanticID{}, err
	}

	return created, nil
}

func (s *service) Lookup(ctx context.Context, id, owner string) (string, error) {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Lookup")
	defer span.End()

	item, err := s.repo.Get(ctx, id, owner)
	if err != nil {
		return "", err
	}

	return item.Target, nil
}

func (s *service) Delete(ctx context.Context, id, owner string) error {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Delete")
	defer span.End()

	return s.repo.Delete(ctx, id, owner)
}

func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Clean")
	defer span.End()

	return s.repo.Clean(ctx, ccid)
}
