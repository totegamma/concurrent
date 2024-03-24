package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/profile"
)

type Service interface {
	Commit(ctx context.Context, document string, signature string) (any, error)
}

type service struct {
	key         key.Service
	entity      entity.Service
	message     message.Service
	association association.Service
	profile     profile.Service
}

func NewService(
	key key.Service,
	entity entity.Service,
	message message.Service,
	association association.Service,
	profile profile.Service,
) Service {
	return &service{
		key:         key,
		entity:      entity,
		message:     message,
		association: association,
		profile:     profile,
	}
}

func (s *service) Commit(ctx context.Context, document string, signature string) (any, error) {
	ctx, span := tracer.Start(ctx, "store.service.Commit")
	defer span.End()

	var base core.DocumentBase[any]
	err := json.Unmarshal([]byte(document), &base)
	if err != nil {
		return nil, err
	}

	err = s.key.ValidateSignedObject(ctx, document, signature)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	switch base.Type {
	case "message.create":
		return s.message.Create(ctx, document, signature)
	case "message.delete":
		return s.message.Delete(ctx, document)
	case "association.create":
		return s.association.Create(ctx, document, signature)
	case "association.delete":
		return s.association.Delete(ctx, document)
	case "profile.create":
		return s.profile.Create(ctx, document, signature)
	case "profile.update":
		return s.profile.Update(ctx, document, signature)
	case "profile.delete":
		return s.profile.Delete(ctx, document)
	case "entity.affiliation":
		return s.entity.Affiliation(ctx, document, signature)
	}

	return nil, fmt.Errorf("unknown document type: %s", base.Type)
}
