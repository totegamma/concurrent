package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/schemas"
)

// EntityRepository defines persistence/lookup for entities.
type EntityRepository interface {
	SaveMeta(ctx context.Context, meta domain.EntityMeta) error
	SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.Document[schemas.Entity], error)
	Get(ctx context.Context, ccid string, hint *string) (*domain.Entity, error)
	GetSD(ctx context.Context, ccid string, hint *string) (*concrnt.SignedDocument, error)
	GetDocument(ctx context.Context, ccid string, hint *string) (*concrnt.Document[schemas.Entity], error)
	GetByAlias(ctx context.Context, alias string) (*domain.Entity, error)
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
	_, err := uc.repo.SaveEntity(ctx, sd)
	if err != nil {
		return nil, err
	}

	return &sd, nil
}

func (uc *EntityUsecase) Get(ctx context.Context, key string, resolver *string) (*domain.Entity, error) {

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
	entity, err := uc.repo.Get(ctx, ccid, nil)
	if err != nil {
		return false, err
	}

	return uc.IsLocal(ctx, *entity), nil
}
