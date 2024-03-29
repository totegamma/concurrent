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
	"github.com/totegamma/concurrent/x/timeline"
)

type Service interface {
	Commit(ctx context.Context, document, signature, option string) (any, error)
}

type service struct {
	key         key.Service
	entity      entity.Service
	message     message.Service
	association association.Service
	profile     profile.Service
	timeline    timeline.Service
}

func NewService(
	key key.Service,
	entity entity.Service,
	message message.Service,
	association association.Service,
	profile profile.Service,
	timeline timeline.Service,
) Service {
	return &service{
		key:         key,
		entity:      entity,
		message:     message,
		association: association,
		profile:     profile,
		timeline:    timeline,
	}
}

func (s *service) Commit(ctx context.Context, document string, signature string, option string) (any, error) {
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
	case "message":
		return s.message.Create(ctx, document, signature)
	case "association":
		return s.association.Create(ctx, document, signature)
	case "profile":
		return s.profile.Create(ctx, document, signature)
	case "affiliation":
		return s.entity.Affiliation(ctx, document, signature, option)
	case "tombstone":
		return s.entity.Tombstone(ctx, document, signature)
	case "extension":
		return s.entity.Extension(ctx, document, signature)
	case "timeline":
		return s.timeline.CreateTimeline(ctx, document, signature)
	case "delete":
		var doc core.DeleteDocument
		err := json.Unmarshal([]byte(document), &doc)
		if err != nil {
			return nil, err
		}
		typ := doc.Body.TargetID[0]
		switch typ {
		case 'm': // message
			return s.message.Delete(ctx, document)
		case 'a': // association
			return s.association.Delete(ctx, document)
		case 'p': // profile
			return s.profile.Delete(ctx, document)
		default:
			return nil, fmt.Errorf("unknown document type: %s", string(typ))
		}
	}
	return nil, fmt.Errorf("unknown document type: %s", base.Type)
}
