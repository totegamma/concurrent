package firestore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gcfirestore "cloud.google.com/go/firestore"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
)

type ServerRepository struct {
	*store
	config *domain.Config
	remote *client.Client
}

func NewServerRepository(config *domain.Config, ds *gcfirestore.Client, namespace string, cl *client.Client) usecase.ServerRepository {
	return &ServerRepository{
		store:  newStore(ds, namespace),
		config: config,
		remote: cl,
	}
}

func (r *ServerRepository) Resolve(ctx context.Context, identifier string, hint *string) (*domain.Server, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Server.Resolve")
	defer span.End()

	if concrnt.IsCSID(identifier) {
		return r.getAndCacheByCSID(ctx, identifier, hint)
	}
	return r.getAndCacheByFQDN(ctx, identifier)
}

func (r *ServerRepository) getAndCacheByCSID(ctx context.Context, csid string, hint *string) (*domain.Server, error) {
	servers, _, err := getAll[serverModel](ctx, r.query(kindServer).Where("csid", "==", csid).Limit(1))
	if err != nil {
		return nil, err
	}
	if len(servers) > 0 && servers[0].WellKnown != "" {
		return modelToDomainServer(servers[0])
	}

	if hint == nil {
		return nil, domain.ErrNotFound
	}

	wkc, err := r.remote.GetServer(ctx, csid, hint)
	if err != nil {
		return nil, err
	}

	model, err := serverModelFromWellKnown(&wkc)
	if err != nil {
		return nil, err
	}
	if _, err := r.doc(kindServer, wkc.Domain).Set(ctx, &model); err != nil {
		return nil, err
	}
	return modelToDomainServer(model)
}

func (r *ServerRepository) getAndCacheByFQDN(ctx context.Context, fqdn string) (*domain.Server, error) {
	var model serverModel
	err := r.get(ctx, r.doc(kindServer, fqdn), &model)
	if err == nil && model.WellKnown != "" {
		return modelToDomainServer(model)
	}
	if err != nil && !isNoSuchEntity(err) {
		return nil, err
	}

	wkc, err := r.remote.GetServer(ctx, fqdn, nil)
	if err != nil {
		return nil, err
	}

	model, err = serverModelFromWellKnown(&wkc)
	if err != nil {
		return nil, err
	}
	if _, err := r.doc(kindServer, wkc.Domain).Set(ctx, &model); err != nil {
		return nil, err
	}
	return modelToDomainServer(model)
}

func (r *ServerRepository) List(ctx context.Context) ([]*concrnt.WellKnownConcrnt, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Server.List")
	defer span.End()

	servers, _, err := getAll[serverModel](ctx, r.query(kindServer))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make([]*concrnt.WellKnownConcrnt, 0, len(servers))
	for _, server := range servers {
		if server.WellKnown == "" {
			span.RecordError(fmt.Errorf("server with empty well-known: %s", server.ID))
			continue
		}
		var wkc concrnt.WellKnownConcrnt
		if err := json.Unmarshal([]byte(server.WellKnown), &wkc); err != nil {
			span.RecordError(err)
			continue
		}
		result = append(result, &wkc)
	}
	return result, nil
}

func serverModelFromWellKnown(wkc *concrnt.WellKnownConcrnt) (serverModel, error) {
	serialized, err := json.Marshal(wkc)
	if err != nil {
		return serverModel{}, err
	}
	now := time.Now().UTC()
	return serverModel{
		ID:        wkc.Domain,
		CSID:      wkc.CSID,
		Layer:     wkc.Layer,
		Tag:       "",
		WellKnown: string(serialized),
		CDate:     now,
		MDate:     now,
	}, nil
}

func modelToDomainServer(model serverModel) (*domain.Server, error) {
	var wkc concrnt.WellKnownConcrnt
	if err := json.Unmarshal([]byte(model.WellKnown), &wkc); err != nil {
		return nil, err
	}
	return &domain.Server{
		TagString: model.Tag,
		WellKnown: wkc,
	}, nil
}

var _ usecase.ServerRepository = (*ServerRepository)(nil)
