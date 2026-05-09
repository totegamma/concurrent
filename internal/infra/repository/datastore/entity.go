package datastore

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	gcdatastore "cloud.google.com/go/datastore"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/schemas"
)

type EntityRepository struct {
	*store
	remote *client.Client
	config domain.Config
}

func NewEntityRepository(ds *gcdatastore.Client, namespace string, cl *client.Client, config domain.Config) usecase.EntityRepository {
	return &EntityRepository{
		store:  newStore(ds, namespace),
		remote: cl,
		config: config,
	}
}

func (r *EntityRepository) SaveMeta(ctx context.Context, meta domain.EntityMeta) error {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.SaveMeta")
	defer span.End()

	key := r.key(kindEntityMeta, meta.ID)
	now := time.Now().UTC()
	model := entityMetaModel{
		ID:    meta.ID,
		Info:  meta.Info,
		CDate: now,
		MDate: now,
	}
	if meta.Inviter != nil {
		model.Inviter = *meta.Inviter
	}

	var existing entityMetaModel
	err := r.client.Get(ctx, key, &existing)
	if err == nil {
		model.CDate = existing.CDate
	} else if err != nil && !isNoSuchEntity(err) {
		span.RecordError(err)
		return err
	}

	if _, err := r.client.Put(ctx, key, &model); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (r *EntityRepository) SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.Document[schemas.Entity], error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if entity.Value.Domain == r.config.FQDN {
		var meta entityMetaModel
		err := r.client.Get(ctx, r.key(kindEntityMeta, entity.Author), &meta)
		if err != nil {
			if isNoSuchEntity(err) {
				err = errors.New("user is not registered for this domain")
			}
			span.RecordError(err)
			return nil, err
		}
	}

	proof, err := json.Marshal(sd.Proof)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	hash := concrnt.GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, entity.CreatedAt).String()

	if entity.Value.Alias != nil {
		name := "_concrnt." + *entity.Value.Alias
		txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
		if err != nil {
			span.RecordError(err)
			return nil, err
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
			return nil, errors.New("alias ownership verification failed")
		}
	}

	_, err = r.client.RunInTransaction(ctx, func(tx *gcdatastore.Transaction) error {
		if _, err := r.createCommitLogAndOwners(ctx, tx, usecase.CommitWrite{
			ID:       documentID,
			Document: sd.Document,
			Proof:    string(proof),
			Owners:   []string{entity.Author},
		}); err != nil {
			return err
		}

		now := time.Now().UTC()
		model := entityModel{
			ID:         entity.Author,
			Domain:     entity.Value.Domain,
			DocumentID: documentID,
			MDate:      now,
		}
		if entity.Value.Alias != nil {
			model.Alias = *entity.Value.Alias
		}

		key := r.key(kindEntity, entity.Author)
		var existing entityModel
		err := tx.Get(key, &existing)
		if err == nil {
			model.CDate = existing.CDate
			model.Tag = existing.Tag
		} else if isNoSuchEntity(err) {
			model.CDate = now
		} else {
			return err
		}

		_, err = tx.Put(key, &model)
		return err
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &entity, nil
}

func (r *EntityRepository) Get(ctx context.Context, ccid string, hint *string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.Get")
	defer span.End()

	var model entityModel
	err := r.client.Get(ctx, r.key(kindEntity, ccid), &model)
	if err == nil {
		return modelToDomainEntity(model), nil
	}
	if !isNoSuchEntity(err) {
		span.RecordError(err)
		return nil, err
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	doc, err := r.GetDocument(ctx, ccid, hint)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &domain.Entity{
		ID:        ccid,
		Domain:    doc.Value.Domain,
		Alias:     doc.Value.Alias,
		TagString: "",
	}, nil
}

func (r *EntityRepository) GetByAlias(ctx context.Context, alias string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.GetByAlias")
	defer span.End()

	alias = strings.TrimPrefix(alias, "@")
	var models []entityModel
	_, err := r.client.GetAll(ctx, r.query(kindEntity).Filter("alias =", alias).Limit(1), &models)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if len(models) > 0 {
		return modelToDomainEntity(models[0]), nil
	}

	name := "_concrnt." + alias
	txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var owner string
	var hint *string
	for _, record := range txtrecords {
		parsed, err := concrnt.ParseCCURI(record)
		if err == nil {
			owner = parsed.Owner
			hint = parsed.Hint
			break
		}
	}
	if owner == "" {
		return nil, errors.New("no valid CCURI found in TXT records")
	}

	return r.Get(ctx, owner, hint)
}

func (r *EntityRepository) GetSD(ctx context.Context, ccid string, hint *string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.GetSD")
	defer span.End()

	var model entityModel
	err := r.client.Get(ctx, r.key(kindEntity, ccid), &model)
	if err == nil {
		commit, err := r.getCommit(ctx, model.DocumentID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		return signedDocumentFromCommit(commit, nil, nil)
	}
	if !isNoSuchEntity(err) {
		span.RecordError(err)
		return nil, err
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	var sd concrnt.SignedDocument
	err = r.remote.GetResource(ctx, "cckv://"+ccid, "application/json", &client.Options{
		Resolver: *hint,
	}, &sd)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if _, err := r.SaveEntity(ctx, sd); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &sd, nil
}

func (r *EntityRepository) GetDocument(ctx context.Context, ccid string, hint *string) (*concrnt.Document[schemas.Entity], error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.GetDocument")
	defer span.End()

	var model entityModel
	err := r.client.Get(ctx, r.key(kindEntity, ccid), &model)
	if err == nil {
		commit, err := r.getCommit(ctx, model.DocumentID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		var doc concrnt.Document[schemas.Entity]
		if err := json.Unmarshal([]byte(commit.Document), &doc); err != nil {
			span.RecordError(err)
			return nil, err
		}
		return &doc, nil
	}
	if !isNoSuchEntity(err) {
		span.RecordError(err)
		return nil, err
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	var sd concrnt.SignedDocument
	err = r.remote.GetResource(ctx, "cckv://"+ccid, "application/json", &client.Options{
		Resolver: *hint,
	}, &sd)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	remoteEntity, err := r.SaveEntity(ctx, sd)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return remoteEntity, nil
}

func (r *EntityRepository) GetMeta(ctx context.Context, ccid string) (*domain.EntityMeta, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Entity.GetMeta")
	defer span.End()

	var model entityMetaModel
	err := r.client.Get(ctx, r.key(kindEntityMeta, ccid), &model)
	if err != nil {
		if isNoSuchEntity(err) {
			return nil, domain.NotFoundError{Resource: ccid}
		}
		span.RecordError(err)
		return nil, err
	}

	var inviter *string
	if model.Inviter != "" {
		inviter = &model.Inviter
	}
	return &domain.EntityMeta{
		ID:      model.ID,
		Inviter: inviter,
		Info:    model.Info,
	}, nil
}

func modelToDomainEntity(model entityModel) *domain.Entity {
	var alias *string
	if model.Alias != "" {
		alias = &model.Alias
	}
	return &domain.Entity{
		ID:        model.ID,
		Domain:    model.Domain,
		Alias:     alias,
		TagString: model.Tag,
	}
}

var _ usecase.EntityRepository = (*EntityRepository)(nil)
