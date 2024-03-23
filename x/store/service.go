package store

import (
	"context"
	"encoding/json"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
)

type Service interface {
	Commit(ctx context.Context, document string, signature string) (any, error)
}

type service struct {
	key     key.Service
	message message.Service
}

func NewService() Service {
	return &service{}
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
	}

	return nil, nil
}
