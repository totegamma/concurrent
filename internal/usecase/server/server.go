package server

import (
	"context"
	"errors"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/service"
)

// Repository defines persistence/lookup for remote servers.
type Repository interface {
	Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error)
	List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error)
}

type Usecase struct {
	repo   Repository
	config *domain.Config
	info   concrnt.SoftwareInfo
	mm     *service.ModuleManager
	client *client.Client
}

func New(
	repo Repository,
	config *domain.Config,
	info concrnt.SoftwareInfo,
	mm *service.ModuleManager,
	cl *client.Client,

) *Usecase {
	return &Usecase{
		repo:   repo,
		config: config,
		info:   info,
		mm:     mm,
		client: cl,
	}
}

func (uc *Usecase) Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error) {

	if (identifier == uc.config.FQDN) || (identifier == uc.config.CSID) {
		return uc.GetThisServer()
	}

	sv, err := uc.repo.Resolve(ctx, identifier, hint)
	if err != nil {
		return nil, err
	}
	return sv, nil
}

func (uc *Usecase) List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error) {
	return uc.repo.List(ctx)
}

// ResolveBlob resolves a ccfs blob URI to its file location. On success it
// terminates with domain.RedirectError pointing at the file location: the
// local storage module for blobs owned by this server's residents, or the
// owner server's resolve endpoint otherwise.
func (uc *Usecase) ResolveBlob(ctx context.Context, parsed *concrnt.CCURI) error {
	host, err := uc.client.ResolveResourceHost(ctx, parsed.Raw)
	if err != nil {
		return domain.NotFoundError{Resource: parsed.Raw}
	}

	if host != uc.config.FQDN {
		// the blob lives on the owner's server; redirect to its resolve endpoint
		server, err := uc.Resolve(ctx, host, parsed.Hint)
		if err != nil {
			return domain.NotFoundError{Resource: parsed.Raw}
		}
		remoteResolve, ok := server.WellKnown.Endpoints["net.concrnt.core.resolve"]
		if !ok {
			return domain.NotFoundError{Resource: parsed.Raw}
		}
		path, err := concrnt.RenderURITemplate(remoteResolve, map[string]string{
			"uri": parsed.Raw,
		})
		if err != nil {
			return err
		}
		return domain.RedirectError{Location: "https://" + host + path}
	}

	storageModule, ok := uc.mm.GetEndpoints()["net.concrnt.storage.resolve"]
	if !ok {
		return errors.New("storage module not found")
	}
	path, err := concrnt.RenderURITemplate(storageModule, map[string]string{
		"hash": parsed.CDID,
	})
	if err != nil {
		return err
	}
	return domain.RedirectError{Location: path}
}

func (uc *Usecase) GetThisServer() (*domain.Server, error) {
	wellknown := concrnt.WellKnownConcrnt{
		Version:      "2.0",
		Domain:       uc.config.FQDN,
		CSID:         uc.config.CSID,
		Layer:        uc.config.Layer,
		Endpoints:    uc.mm.GetEndpoints(),
		SoftwareInfo: uc.info,
		Meta:         uc.config.Meta,
	}

	return &domain.Server{
		TagString: "",
		WellKnown: wellknown,
	}, nil
}
