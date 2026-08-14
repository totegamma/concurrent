package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/jwt"
	"github.com/concrnt/concrnt/schemas"
)

type ResidenceRepository interface {
	SaveMeta(ctx context.Context, meta domain.EntityMeta) error
	GetMeta(ctx context.Context, ccid string) (*domain.EntityMeta, error)
	UpdateMetaInfo(ctx context.Context, ccid string, info string) error
	DeleteMeta(ctx context.Context, ccid string) error

	GetEntityByCCID(ctx context.Context, ccid string) (*domain.Entity, error)
	GetEntityByAlias(ctx context.Context, alias string) (*domain.Entity, error)
}

type ResidenceUsecase struct {
	repo   ResidenceRepository
	record *RecordUsecase
	config *domain.Config
}

func NewResidenceUsecase(
	repo ResidenceRepository,
	record *RecordUsecase,
	config *domain.Config,
) *ResidenceUsecase {
	return &ResidenceUsecase{
		repo:   repo,
		record: record,
		config: config,
	}
}

// Unregister deletes the requester's residence meta from this server — the
// final cleanup step of a migration. The requester must have already moved
// their entity document to another domain: deleting the meta of a current
// resident would leave them unable to update their own entity document
// (saveEntity requires a meta for local-domain entity commits).
func (uc *ResidenceUsecase) Unregister(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "ResidenceUsecase.Unregister")
	defer span.End()

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		err := errors.New("requester not found in context")
		span.RecordError(err)
		return err
	}

	entity, err := uc.repo.GetEntityByCCID(ctx, requester.ID)
	if err != nil {
		// entityが見つからない場合はmetaだけ残っている状態なので削除に進む
		if !errors.Is(err, domain.ErrNotFound) {
			span.RecordError(err)
			return err
		}
	} else if entity.Domain == uc.config.FQDN {
		err := domain.PermissionError{Reason: "entity still resides on this domain; move to another domain first"}
		span.RecordError(err)
		return err
	}

	// DeleteMetaはmetaが存在しなくてもエラーにしない(冪等)
	if err := uc.repo.DeleteMeta(ctx, requester.ID); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

// GetRegistration returns the requester's own registration meta (info and
// inviter). The target is always the requester — other residents' meta is
// server-local private state and is never exposed.
func (uc *ResidenceUsecase) GetRegistration(ctx context.Context) (*domain.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "ResidenceUsecase.GetRegistration")
	defer span.End()

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		err := domain.PermissionError{Reason: "authentication required"}
		span.RecordError(err)
		return nil, err
	}

	meta, err := uc.repo.GetMeta(ctx, requester.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			err = errRegistrationNotFound
		}
		span.RecordError(err)
		return nil, err
	}

	return meta, nil
}

// errRegistrationNotFound is the 404 for a requester with no registration on
// this domain. The message and code are API contract: they let clients tell
// an application-level "not registered" apart from a bare 404 produced by a
// misconfigured server or proxy.
var errRegistrationNotFound = domain.NotFoundError{
	Resource: "registration",
	Message:  "Registration Not Found",
	Code:     domain.ErrorCodeRegistrationNotFound,
}

// UpdateRegistration replaces the requester's registration meta (info only —
// inviter is set at registration time and never updatable, as in v1).
func (uc *ResidenceUsecase) UpdateRegistration(ctx context.Context, meta any) error {
	ctx, span := tracer.Start(ctx, "ResidenceUsecase.UpdateRegistration")
	defer span.End()

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		err := domain.PermissionError{Reason: "authentication required"}
		span.RecordError(err)
		return err
	}

	info, err := encodeMetaInfo(meta)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := uc.repo.UpdateMetaInfo(ctx, requester.ID, info); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			err = errRegistrationNotFound
		}
		span.RecordError(err)
		return err
	}

	return nil
}

// encodeMetaInfo serializes the client-supplied registration meta (an
// arbitrary JSON value) into the string stored in EntityMeta.Info (jsonb).
func encodeMetaInfo(meta any) (string, error) {
	info, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(info), nil
}

func (uc *ResidenceUsecase) Register(ctx context.Context, ip string, req concrnt.RegisterRequest) error {
	ctx, span := tracer.Start(ctx, "RecordUsecase.Register")
	defer span.End()

	var inviter *string
	switch uc.config.Registration {
	case "invite":
		if req.InviteToken == nil {
			err := domain.PermissionError{Reason: "invitation code is required"}
			span.RecordError(err)
			return err
		}

		_, claims, err := jwt.Parse(*req.InviteToken)
		if err != nil {
			span.RecordError(err)
			return domain.PermissionError{Reason: "invalid invitation code"}
		}

		err = jwt.Validate(*req.InviteToken, claims.Issuer)
		if err != nil {
			span.RecordError(err)
			return domain.PermissionError{Reason: "invalid invitation code"}
		}
		if claims.Subject != "invite" {
			return domain.PermissionError{Reason: "invalid invitation code(subject)"}
		}

		if claims.Issuer != uc.config.CSID {
			inviterEntity, err := uc.repo.GetEntityByCCID(ctx, claims.Issuer)
			if err != nil {
				span.RecordError(err)
				return domain.PermissionError{Reason: "invalid invitation code(issuer)"}
			}

			tag := inviterEntity.Tag()
			if !tag.Has("_invite") {
				return domain.PermissionError{Reason: "invalid invitation code(issuer tag)"}
			}
		}

		inviter = &claims.Issuer

	case "open":
		// do nothing, allow registration
	default:
		err := domain.PermissionError{Reason: "registration is not allowed"}
		span.RecordError(err)
		return err
	}

	v := ctx.Value(interop.CaptchaVerifiedCtxKey)
	if v != nil {
		captchaVerified, ok := v.(bool)
		if !ok || !captchaVerified {
			err := errors.New("captcha verification failed")
			span.RecordError(err)
			return err
		}
	}

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(req.SignedDocument.Document), &entity); err != nil {
		span.RecordError(err)
		return err
	}

	ccid := entity.Author
	if entity.Value.Domain != uc.config.FQDN {
		err := errors.New("entity domain does not match server domain")
		span.RecordError(err)
		return err
	}

	var latest time.Time
	existing, err := uc.repo.GetEntityByCCID(ctx, ccid)
	if err == nil {
		var existingDoc concrnt.Document[schemas.Entity]
		err = json.Unmarshal([]byte(existing.SignedDocument.Document), &existingDoc)
		if err != nil {
			span.RecordError(err)
			return err
		}
		latest = existingDoc.CreatedAt
	}

	if entity.CreatedAt.Before(latest) {
		err := errors.New("incoming document is older than existing document")
		span.RecordError(err)
		return err
	}

	info, err := encodeMetaInfo(req.Meta)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = uc.repo.SaveMeta(ctx, domain.EntityMeta{
		ID:      ccid,
		Inviter: inviter,
		Info:    info,
	})
	if err != nil {
		span.RecordError(err)
		return err
	}

	_, err = uc.record.Commit(ctx, ip, req.SignedDocument, domain.CommitModeLocalOnlyExecute)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}
