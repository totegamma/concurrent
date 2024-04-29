package profile

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/totegamma/concurrent/x/cdid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/semanticid"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for profile service
type Service interface {
	Upsert(ctx context.Context, mode core.CommitMode, document, signature string) (core.Profile, error)
	Delete(ctx context.Context, mode core.CommitMode, document string) (core.Profile, error)

	Count(ctx context.Context) (int64, error)
	Get(ctx context.Context, id string) (core.Profile, error)
	GetBySemanticID(ctx context.Context, semanticID, owner string) (core.Profile, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error)
	GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error)
	GetBySchema(ctx context.Context, schema string) ([]core.Profile, error)
}

type service struct {
	repo       Repository
	key        key.Service
	semanticid semanticid.Service
}

// NewService creates a new profile service
func NewService(repo Repository, key key.Service, semanticid semanticid.Service) Service {
	return &service{repo, key, semanticid}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.Count")
	defer span.End()

	return s.repo.Count(ctx)
}

func (s *service) Get(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.Get")
	defer span.End()

	return s.repo.Get(ctx, id)
}

func (s *service) GetBySemanticID(ctx context.Context, semanticID, owner string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.GetBySemanticID")
	defer span.End()

	target, err := s.semanticid.Lookup(ctx, semanticID, owner)
	if err != nil {
		return core.Profile{}, err
	}

	return s.Get(ctx, target)
}

// GetByAuthorAndSchema returns profiles by owner and schema
func (s *service) GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.GetByAuthorAndSchema")
	defer span.End()

	return s.repo.GetByAuthorAndSchema(ctx, owner, schema)
}

// GetByAuthor returns profiles by owner
func (s *service) GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.GetByAuthor")
	defer span.End()

	return s.repo.GetByAuthor(ctx, owner)
}

// GetBySchema returns profiles by schema
func (s *service) GetBySchema(ctx context.Context, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.GetBySchema")
	defer span.End()

	return s.repo.GetBySchema(ctx, schema)
}

// Upsert creates new profile if the signature is valid
func (s *service) Upsert(ctx context.Context, mode core.CommitMode, document, signature string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.Upsert")
	defer span.End()

	var doc core.UpsertProfile[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if doc.SemanticID != "" {
		existingID, err := s.semanticid.Lookup(ctx, doc.SemanticID, doc.Signer)
		if err == nil {
			_, err = s.Get(ctx, existingID)
			if err != nil {
				s.semanticid.Delete(ctx, doc.SemanticID, doc.Signer)
			} else {
				if doc.ID == "" {
					doc.ID = existingID
				} else {
					if doc.ID != existingID {
						return core.Profile{}, errors.New("semantic ID mismatch")
					}
				}
			}
		}
	}

	if doc.ID == "" {
		hash := util.GetHash([]byte(document))
		hash10 := [10]byte{}
		copy(hash10[:], hash[:10])
		doc.ID = cdid.New(hash10, doc.SignedAt).String()
	}

	profile := core.Profile{
		ID:        doc.ID,
		Author:    doc.Signer,
		Schema:    doc.Schema,
		Document:  document,
		Signature: signature,
	}

	saved, err := s.repo.Upsert(ctx, profile)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if doc.SemanticID != "" {
		_, err = s.semanticid.Name(ctx, doc.SemanticID, doc.Signer, saved.ID, document, signature)
		if err != nil {
			span.RecordError(err)
			return core.Profile{}, err
		}
	}

	return saved, nil
}

// Delete deletes profile
func (s *service) Delete(ctx context.Context, mode core.CommitMode, documentStr string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.Delete")
	defer span.End()

	var document core.DeleteDocument
	err := json.Unmarshal([]byte(documentStr), &document)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	deleteTarget, err := s.Get(ctx, document.Target)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if deleteTarget.Author != document.Signer {
		err = errors.New("unauthorized")
		span.RecordError(err)
		return core.Profile{}, err
	}

	return s.repo.Delete(ctx, document.Target)
}
