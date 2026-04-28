package usecase

import (
	"context"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/service"
)

// ServerRepository defines persistence/lookup for remote servers.
type ServerRepository interface {
	Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error)
	List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error)
}

type ServerUsecase struct {
	repo   ServerRepository
	config *domain.Config
	info   concrnt.SoftwareInfo
	mm     *service.ModuleManager
}

func NewServerUsecase(
	repo ServerRepository,
	config *domain.Config,
	info concrnt.SoftwareInfo,
	mm *service.ModuleManager,

) *ServerUsecase {
	return &ServerUsecase{
		repo:   repo,
		config: config,
		info:   info,
		mm:     mm,
	}
}

func (uc *ServerUsecase) Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error) {

	if (identifier == uc.config.FQDN) || (identifier == uc.config.CSID) {
		return uc.GetThisServer()
	}

	sv, err := uc.repo.Resolve(ctx, identifier, hint)
	if err != nil {
		return nil, err
	}
	return sv, nil
}

func (uc *ServerUsecase) List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error) {
	return uc.repo.List(ctx)
}

func (uc *ServerUsecase) GetThisServer() (*domain.Server, error) {
	wellknown := concrnt.WellKnownConcrnt{
		Version:      "2.0",
		Domain:       uc.config.FQDN,
		CSID:         uc.config.CSID,
		Layer:        uc.config.Layer,
		Dimension:    uc.config.Dimension,
		Endpoints:    uc.mm.GetEndpoints(),
		SoftwareInfo: uc.info,
		Meta:         uc.config.Meta,
	}

	return &domain.Server{
		TagString: "",
		WellKnown: wellknown,
	}, nil
}
