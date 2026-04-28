package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/cdid"
	"github.com/totegamma/concrnt-playground/client"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/infra/database/models"
	"github.com/totegamma/concrnt-playground/schemas"
)

type EntityRepository struct {
	db     *gorm.DB
	client *client.Client
	config domain.Config
}

func NewEntityRepository(db *gorm.DB, cl *client.Client, config domain.Config) *EntityRepository {
	return &EntityRepository{db: db, client: cl, config: config}
}

func (r *EntityRepository) SaveMeta(ctx context.Context, meta domain.EntityMeta) error {
	ctx, span := tracer.Start(ctx, "EntityRepository.Register")
	defer span.End()

	modelMeta := models.EntityMeta{
		ID:      meta.ID,
		Inviter: meta.Inviter,
		Info:    meta.Info,
	}

	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"inviter", "info"}),
	}).Create(&modelMeta).Error; err != nil {
		return err
	}

	return nil
}

func (r *EntityRepository) SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.Document[schemas.Entity], error) {
	ctx, span := tracer.Start(ctx, "EntityRepository.SaveEntity")
	defer span.End()

	var entity concrnt.Document[schemas.Entity]
	if err := json.Unmarshal([]byte(sd.Document), &entity); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if entity.Value.Domain == r.config.FQDN {
		// if local, check if author is registered
		var meta models.EntityMeta
		err := r.db.Where("id = ?", entity.Author).Take(&meta).Error
		if err != nil {
			span.RecordError(err)
			return nil, errors.New("user is not registered for this domain")
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

	commitLog := models.CommitLog{
		ID:       documentID,
		Document: sd.Document,
		Proof:    string(proof),
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		if err := tx.Clauses(clause.OnConflict{
			DoNothing: true,
		}).Create(&commitLog).Error; err != nil {
			span.RecordError(err)
			return err
		}

		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "commit_log_id"}, {Name: "owner"}},
			DoNothing: true,
		}).Create(&models.CommitOwner{
			CommitLogID: commitLog.ID,
			Owner:       entity.Author,
		}).Error
		if err != nil {
			span.RecordError(err)
			return err
		}

		if entity.Value.Alias != nil {
			name := "_concrnt." + *entity.Value.Alias
			txtrecords, err := net.DefaultResolver.LookupTXT(ctx, name)
			if err != nil {
				span.RecordError(err)
				return err
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
				return errors.New("alias ownership verification failed")
			}
		}

		modelEntity := models.Entity{
			ID:         entity.Author,
			Alias:      entity.Value.Alias,
			Domain:     entity.Value.Domain,
			DocumentID: documentID,
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"alias", "domain", "document_id"}),
		}).Create(&modelEntity).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &entity, nil
}

func (r *EntityRepository) Get(ctx context.Context, ccid string, hint *string) (*domain.Entity, error) {
	ctx, span := tracer.Start(ctx, "EntityRepository.Get")
	defer span.End()

	entity, err := gorm.G[models.Entity](r.db).
		Where("entities.id = ?", ccid).
		Take(ctx)
	if err == nil {
		return &domain.Entity{
			ID:        entity.ID,
			Domain:    entity.Domain,
			Alias:     entity.Alias,
			TagString: entity.Tag,
		}, nil
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	doc, err := r.GetDocument(ctx, ccid, hint)
	if err != nil {
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
	ctx, span := tracer.Start(ctx, "EntityRepository.GetByAlias")
	defer span.End()

	alias = strings.TrimPrefix(alias, "@")

	entity, err := gorm.G[models.Entity](r.db).
		Where("entities.alias = ?", alias).
		Take(ctx)
	if err == nil {
		return &domain.Entity{
			ID:        entity.ID,
			Domain:    entity.Domain,
			Alias:     entity.Alias,
			TagString: entity.Tag,
		}, nil
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
	if owner != "" {
		return nil, errors.New("no valid CCURI found in TXT records")
	}

	return r.Get(ctx, owner, hint)
}

func (r *EntityRepository) GetSD(ctx context.Context, ccid string, hint *string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "EntityRepository.GetSD")
	defer span.End()

	//var docString string
	entity, err := gorm.G[models.Entity](r.db).
		//Select("commit_logs.document", "commit_logs.proof"). // TODO: specify columns to avoid loading unnecessary data
		Joins(clause.Has("Document"), nil).
		Where("entities.id = ?", ccid).
		Take(ctx)
	if err == nil {
		var proof concrnt.Proof
		err = json.Unmarshal([]byte(entity.Document.Proof), &proof)
		if err != nil {
			return nil, err
		}
		return &concrnt.SignedDocument{
			Document: entity.Document.Document,
			Proof:    proof,
		}, nil
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	var sd concrnt.SignedDocument
	err = r.client.GetResource(ctx, "cckv://"+ccid, "application/json", nil, &sd)
	if err != nil {
		return nil, err
	}

	// TODO: 署名検証

	_, err = r.SaveEntity(ctx, sd)
	if err != nil {
		return nil, err
	}

	return &sd, nil

}

func (r *EntityRepository) GetDocument(ctx context.Context, ccid string, hint *string) (*concrnt.Document[schemas.Entity], error) {
	ctx, span := tracer.Start(ctx, "EntityRepository.GetDocument")
	defer span.End()

	//var docString string
	entity, err := gorm.G[models.Entity](r.db).
		// Select("commit_logs.document").
		Joins(clause.Has("Document"), nil).
		Where("entities.id = ?", ccid).
		Take(ctx)
	if err == nil {
		var doc concrnt.Document[schemas.Entity]
		if err := json.Unmarshal([]byte(entity.Document.Document), &doc); err != nil {
			return nil, err
		}
		return &doc, nil
	}

	if hint == nil || *hint == r.config.FQDN {
		return nil, domain.NotFoundError{Resource: ccid}
	}

	var sd concrnt.SignedDocument
	err = r.client.GetResource(ctx, "cckv://"+ccid, "application/json", nil, &sd)
	if err != nil {
		return nil, err
	}

	// TODO: 署名検証

	remoteEntity, err := r.SaveEntity(ctx, sd)
	if err != nil {
		return nil, err
	}

	return remoteEntity, nil

}
