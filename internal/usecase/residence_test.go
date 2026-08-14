package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
)

// encodeMetaInfo turns the client-supplied registration meta (any JSON value,
// typically the register-template form data object) into the jsonb string
// stored in EntityMeta.Info.
func TestEncodeMetaInfo(t *testing.T) {
	t.Run("object is marshaled as-is", func(t *testing.T) {
		info, err := encodeMetaInfo(map[string]any{"name": "alice", "consent": true})
		if err != nil {
			t.Fatalf("encodeMetaInfo returned error: %v", err)
		}
		if info != `{"consent":true,"name":"alice"}` {
			t.Fatalf("info = %s", info)
		}
	})

	t.Run("nil becomes jsonb null", func(t *testing.T) {
		info, err := encodeMetaInfo(nil)
		if err != nil {
			t.Fatalf("encodeMetaInfo returned error: %v", err)
		}
		if info != "null" {
			t.Fatalf("info = %s", info)
		}
	})

	t.Run("raw message passes through compacted", func(t *testing.T) {
		info, err := encodeMetaInfo(json.RawMessage(`{"note": "created by conctl"}`))
		if err != nil {
			t.Fatalf("encodeMetaInfo returned error: %v", err)
		}
		if info != `{"note":"created by conctl"}` {
			t.Fatalf("info = %s", info)
		}
	})
}

// registrationResidenceRepo serves a fixed meta (or an error) and records
// UpdateMetaInfo calls without touching inviter.
type registrationResidenceRepo struct {
	ResidenceRepository
	meta    *domain.EntityMeta
	metaErr error
}

func (r *registrationResidenceRepo) GetMeta(ctx context.Context, ccid string) (*domain.EntityMeta, error) {
	if r.metaErr != nil {
		return nil, r.metaErr
	}
	return r.meta, nil
}

func (r *registrationResidenceRepo) UpdateMetaInfo(ctx context.Context, ccid string, info string) error {
	if r.metaErr != nil {
		return r.metaErr
	}
	r.meta.Info = info
	return nil
}

// GetRegistration/UpdateRegistration are the v1 GET/PUT /entity/meta
// equivalents: requester-only access to their own registration info, with
// inviter fixed at registration time.
func TestRegistrationMeta(t *testing.T) {
	cfg := &domain.Config{FQDN: "example.com"}
	ccid := "con1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	inviter := "con1pppppppppppppppppppppppppppppppppppppppp"
	authedCtx := context.WithValue(context.Background(), interop.RequesterCtxKey, domain.Entity{ID: ccid})

	t.Run("unauthenticated get rejected", func(t *testing.T) {
		uc := NewResidenceUsecase(&registrationResidenceRepo{}, nil, cfg)
		_, err := uc.GetRegistration(context.Background())
		if err == nil || !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
	})

	t.Run("unauthenticated update rejected", func(t *testing.T) {
		repo := &registrationResidenceRepo{meta: &domain.EntityMeta{ID: ccid, Info: "null"}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		err := uc.UpdateRegistration(context.Background(), map[string]any{"email": "a@example.com"})
		if err == nil || !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if repo.meta.Info != "null" {
			t.Fatal("UpdateMetaInfo should not be called")
		}
	})

	t.Run("service account (non-entity requester) rejected", func(t *testing.T) {
		uc := NewResidenceUsecase(&registrationResidenceRepo{}, nil, cfg)
		saCtx := context.WithValue(context.Background(), interop.RequesterCtxKey, "con1sssssssssssssssssssssssssssssssssssssss")
		_, err := uc.GetRegistration(saCtx)
		if err == nil || !errors.Is(err, domain.ErrPermissionDenied) {
			t.Fatalf("expected permission error, got %v", err)
		}
	})

	t.Run("unregistered requester gets not found", func(t *testing.T) {
		repo := &registrationResidenceRepo{metaErr: domain.NotFoundError{Resource: "entity meta"}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		_, err := uc.GetRegistration(authedCtx)
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
		// the message and code are API contract: they mark the 404 as
		// concrnt's application-level "not registered", not a proxy/misconfig
		// 404
		assertRegistrationNotFound := func(err error) {
			t.Helper()
			if err.Error() != "Registration Not Found" {
				t.Fatalf("message = %q", err.Error())
			}
			var nf domain.NotFoundError
			if !errors.As(err, &nf) || nf.Code != domain.ErrorCodeRegistrationNotFound {
				t.Fatalf("code = %q", nf.Code)
			}
		}
		assertRegistrationNotFound(err)
		err = uc.UpdateRegistration(authedCtx, nil)
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
		assertRegistrationNotFound(err)
	})

	t.Run("get returns own meta", func(t *testing.T) {
		repo := &registrationResidenceRepo{meta: &domain.EntityMeta{ID: ccid, Inviter: &inviter, Info: `{"email":"a@example.com"}`}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		meta, err := uc.GetRegistration(authedCtx)
		if err != nil {
			t.Fatalf("GetRegistration returned error: %v", err)
		}
		if meta.ID != ccid || meta.Inviter != &inviter || meta.Info != `{"email":"a@example.com"}` {
			t.Fatalf("meta = %+v", meta)
		}
	})

	t.Run("update replaces info and keeps inviter", func(t *testing.T) {
		repo := &registrationResidenceRepo{meta: &domain.EntityMeta{ID: ccid, Inviter: &inviter, Info: `{"email":"a@example.com"}`}}
		uc := NewResidenceUsecase(repo, nil, cfg)
		if err := uc.UpdateRegistration(authedCtx, map[string]any{"email": "b@example.com"}); err != nil {
			t.Fatalf("UpdateRegistration returned error: %v", err)
		}
		if repo.meta.Info != `{"email":"b@example.com"}` {
			t.Fatalf("info = %s", repo.meta.Info)
		}
		if repo.meta.Inviter != &inviter {
			t.Fatal("inviter must be preserved")
		}
	})
}

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
