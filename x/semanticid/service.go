package semanticid

import (
	"context"
	"github.com/concrnt/concrnt/core"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) core.SemanticIDService {
	return &service{repo}
}

// Name creates or updates a semantic ID mapping.
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

// Lookup retrieves the target ID associated with a semantic ID and owner.
func (s *service) Lookup(ctx context.Context, id, owner string) (string, error) {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Lookup")
	defer span.End()

	item, err := s.repo.Get(ctx, id, owner)
	if err != nil {
		return "", err
	}

	return item.Target, nil
}

// Delete removes a semantic ID mapping by its ID and owner.
func (s *service) Delete(ctx context.Context, id, owner string) error {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Delete")
	defer span.End()

	return s.repo.Delete(ctx, id, owner)
}

// Clean removes all semantic ID mappings owned by the specified ccid.
func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "SemanticID.Service.Clean")
	defer span.End()

	return s.repo.Clean(ctx, ccid)
}
