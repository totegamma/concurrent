package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/schemas"
)

func (uc *Usecase) saveEntity(ctx context.Context, tx RepositoryTx, ip string, sd concrnt.SignedDocument) (*commitApplyResult, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if sd.Proof.Type != concrnt.ProofTypeNone {
		entityServer, err := uc.server.Resolve(ctx, entity.Value.Domain, nil)
		if err != nil {
			err = errors.Join(domain.ValidationError{Field: "value.domain", Message: "failed to resolve the entity's domain"}, err)
			span.RecordError(err)
			return nil, err
		}
		if entityServer.Layer() != uc.config.Layer {
			err := domain.ValidationError{Field: "value.domain", Message: "the entity's domain is on a different layer"}
			span.RecordError(err)
			return nil, err
		}
		if serverTag := entityServer.Tag(); serverTag.Has("_blocked") {
			err := domain.ValidationError{Field: "value.domain", Message: "the entity's domain is blocked"}
			span.RecordError(err)
			return nil, err
		}
	}

	if entity.Value.Domain == uc.config.FQDN {
		// if local, check if author is registered
		_, err := uc.entity.GetMeta(ctx, entity.Author)
		if err != nil {
			span.RecordError(err)
			return nil, errors.New("user is not registered for this domain")
		}
	}

	if entity.Value.Alias != nil {
		name := "_concrnt." + *entity.Value.Alias
		txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
		if err != nil {
			span.RecordError(err)
			return nil, errors.New("alias ownership verification failed: TXT record not found for " + name)
		}

		verified := false
		for _, record := range txtrecords {
			parsed, err := concrnt.ParseCCURI(record)
			if err != nil {
				continue
			}
			if parsed.Owner == entity.Author {
				verified = true
				break
			}
		}

		if !verified {
			err := errors.New("alias ownership verification failed: no valid TXT record found for " + name)
			span.RecordError(err)
			return nil, err
		}
	}

	documentID, err := sd.CDID()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := uc.repo.CreateCommitLog(ctx, tx, documentID, ip, sd.Document, sd.Proof, entity.Author); err != nil {
		span.RecordError(err)
		return nil, err
	}

	applied, err := uc.repo.CreateEntity(ctx, tx, entity.Author, entity.Value.Alias, entity.Value.Domain, documentID, entity.CreatedAt)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !applied {
		return &commitApplyResult{result: &sd, noop: true}, nil
	}

	return &commitApplyResult{result: &sd}, nil
}

func (uc *Usecase) GetEntity(ctx context.Context, uri string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetEntity")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		return nil, err
	}

	if len(parsed.Owner) == 0 {
		err := fmt.Errorf("invalid URI: owner is required: %s", uri)
		span.RecordError(err)
		return nil, err
	}

	if parsed.Owner[0] == '@' { // alias
		alias := parsed.Owner[1:]
		sd, err := uc.entity.GetEntityByAlias(ctx, alias)
		if err == nil {
			return sd, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			span.RecordError(err)
			return nil, err
		}

		name := "_concrnt." + alias
		txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		redirect := ""
		for _, record := range txtrecords {
			parsed, err := concrnt.ParseCCURI(record)
			if err == nil && concrnt.IsCCID(parsed.Owner) && parsed.Hint != nil {
				redirect = record
				break
			}
		}
		if redirect == "" {
			return nil, errors.New("no valid CCURI found in TXT records")
		}

		return uc.GetEntity(ctx, redirect)
	} else {
		ccid := parsed.Owner
		entity, err := uc.entity.GetEntityByCCID(ctx, ccid)
		if err == nil {
			return entity, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			span.RecordError(err)
			return nil, err
		}

		if parsed.Hint == nil || *parsed.Hint == uc.config.FQDN {
			return nil, domain.NotFoundError{Resource: uri}
		}

		hint := *parsed.Hint

		var sd concrnt.SignedDocument
		err = uc.client.GetResource(ctx, uri, "application/json", &client.Options{
			Resolver: hint,
		}, &sd)
		if err != nil {
			return nil, err
		}

		_, err = uc.Commit(ctx, hint, sd, domain.CommitModeExecute)
		if err != nil {
			return nil, err
		}

		// commit済みなので今度は成功するはず
		entity, err = uc.entity.GetEntityByCCID(ctx, ccid)
		if err != nil {
			return nil, err
		}
		return entity, nil
	}
}

func (uc *Usecase) IsLocalEntity(ctx context.Context, entity *domain.Entity) bool {
	return uc.config.FQDN == entity.Domain
}

func (uc *Usecase) IsLocalEntityByCCID(ctx context.Context, entityID string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.IsLocalEntity")
	defer span.End()

	if concrnt.IsCCID(entityID) {
		entityID = concrnt.CCURI{
			Scheme: "cckv",
			Owner:  entityID,
		}.String()
	}

	entity, err := uc.GetEntity(ctx, entityID)
	if err != nil {
		span.RecordError(err)
		return false, err
	}

	return uc.IsLocalEntity(ctx, entity), nil
}
