package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/jwt"
	"github.com/concrnt/concrnt/schemas"
)

// EntityRepository defines persistence/lookup for entities.
type EntityRepository interface {
	SaveMeta(ctx context.Context, meta domain.EntityMeta) error
	SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.Document[schemas.Entity], error)
	Get(ctx context.Context, ccid string, hint *string) (*domain.Entity, error)
	GetSD(ctx context.Context, ccid string, hint *string) (*concrnt.SignedDocument, error)
	GetDocument(ctx context.Context, ccid string, hint *string) (*concrnt.Document[schemas.Entity], error)
	GetByAlias(ctx context.Context, alias string) (*domain.Entity, error)
	GetMeta(ctx context.Context, ccid string) (*domain.EntityMeta, error)
}

type EntityUsecase struct {
	repo   EntityRepository
	config *domain.Config
}

func NewEntityUsecase(
	repo EntityRepository,
	config *domain.Config,
) *EntityUsecase {
	return &EntityUsecase{
		repo:   repo,
		config: config,
	}
}

func (uc *EntityUsecase) Register(ctx context.Context, req concrnt.RegisterRequest[domain.EntityMeta]) error {
	ctx, span := tracer.Start(ctx, "EntityUsecase.Register")
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
			inviterEntity, err := uc.repo.Get(ctx, claims.Issuer, nil)
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
	existing, err := uc.repo.GetDocument(ctx, ccid, nil)
	if err == nil {
		latest = existing.CreatedAt
	}

	if entity.CreatedAt.Before(latest) {
		err := errors.New("incoming document is older than existing document")
		span.RecordError(err)
		return err
	}

	req.Meta.ID = ccid
	req.Meta.Inviter = inviter
	err = uc.repo.SaveMeta(ctx, req.Meta)
	if err != nil {
		span.RecordError(err)
		return err
	}

	_, err = uc.repo.SaveEntity(ctx, req.SignedDocument)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

func (uc *EntityUsecase) SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "EntityUsecase.SaveEntity")
	defer span.End()

	_, err := uc.repo.SaveEntity(ctx, sd)
	if err != nil {
		return nil, err
	}

	return &sd, nil
}

func (uc *EntityUsecase) Get(ctx context.Context, key string, resolver *string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "EntityUsecase.Get")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(key)
	if err != nil {
		return nil, err
	}

	id := parsed.Owner

	if strings.HasPrefix(id, "@") {
		return uc.repo.GetByAlias(ctx, id)
	} else {
		return uc.repo.Get(ctx, id, resolver)
	}
}

func (uc *EntityUsecase) GetSD(ctx context.Context, ccid string, resolver *string) (*concrnt.SignedDocument, error) {
	return uc.repo.GetSD(ctx, ccid, resolver)
}

func (uc *EntityUsecase) IsLocal(ctx context.Context, entity domain.Entity) bool {
	return entity.Domain == uc.config.FQDN
}

func (uc *EntityUsecase) IsLocalByCCID(ctx context.Context, ccid string) (bool, error) {
	ctx, span := tracer.Start(ctx, "EntityUsecase.IsLocalByCCID")
	defer span.End()

	entity, err := uc.repo.Get(ctx, ccid, nil)
	if err != nil {
		return false, err
	}

	return uc.IsLocal(ctx, *entity), nil
}
