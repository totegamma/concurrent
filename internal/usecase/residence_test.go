package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
)

// unregisterResidenceRepo serves a fixed entity (or an error) and records
// DeleteMeta calls.
type unregisterResidenceRepo struct {
	ResidenceRepository
	entity     *domain.Entity
	entityErr  error
	deletedIDs []string
}

func (r *unregisterResidenceRepo) GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error) {
	if r.entityErr != nil {
		return nil, r.entityErr
	}
	return r.entity, nil
}

func (r *unregisterResidenceRepo) DeleteMeta(ctx context.Context, ccid string) error {
	r.deletedIDs = append(r.deletedIDs, ccid)
	return nil
}

// Unregister is the migration cleanup: it deletes the requester's residence
// meta, but only after their entity document has moved to another domain — a
// current resident must not be able to strand themselves.
func TestUnregister(t *testing.T) {
	cfg := &domain.Config{FQDN: "example.com"}
	ccid := "con1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	authedCtx := context.WithValue(context.Background(), interop.RequesterCtxKey, domain.Entity{ID: ccid})

	t.Run("unauthenticated rejected", func(t *testing.T) {
		repo := &unregisterResidenceRepo{}
		uc := NewResidenceUsecase(repo, nil, cfg)
		if err := uc.Unregister(context.Background()); err == nil {
			t.Fatal("expected error, got nil")
		}
		if len(repo.deletedIDs) != 0 {
			t.Fatal("DeleteMeta should not be called")
		}
	})

	t.Run("still resident rejected", func(t *testing.T) {
		repo := &unregisterResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: cfg.FQDN}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		err := uc.Unregister(authedCtx)
		if err == nil || !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if len(repo.deletedIDs) != 0 {
			t.Fatal("DeleteMeta should not be called for a current resident")
		}
	})

	t.Run("moved away deletes meta", func(t *testing.T) {
		repo := &unregisterResidenceRepo{entity: &domain.Entity{ID: ccid, Domain: "new.example.net"}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		if err := uc.Unregister(authedCtx); err != nil {
			t.Fatalf("Unregister returned error: %v", err)
		}
		if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != ccid {
			t.Fatalf("DeleteMeta calls = %v, want [%s]", repo.deletedIDs, ccid)
		}
	})

	t.Run("entity not found still deletes meta", func(t *testing.T) {
		repo := &unregisterResidenceRepo{entityErr: domain.NotFoundError{Resource: "entity"}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		if err := uc.Unregister(authedCtx); err != nil {
			t.Fatalf("Unregister returned error: %v", err)
		}
		if len(repo.deletedIDs) != 1 {
			t.Fatal("DeleteMeta was not called")
		}
	})

	t.Run("repository error propagates", func(t *testing.T) {
		repo := &unregisterResidenceRepo{entityErr: errors.New("db down")}
		uc := NewResidenceUsecase(repo, nil, cfg)
		if err := uc.Unregister(authedCtx); err == nil {
			t.Fatal("expected error, got nil")
		}
		if len(repo.deletedIDs) != 0 {
			t.Fatal("DeleteMeta should not be called on lookup failure")
		}
	})
}
