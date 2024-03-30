package profile

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/key"
)

// Service is the interface for profile service
type Service interface {
	Create(ctx context.Context, objectStr string, signature string) (core.Profile, error)
	Update(ctx context.Context, objectStr string, signature string) (core.Profile, error)
	Delete(ctx context.Context, document string) (core.Profile, error)

	Count(ctx context.Context) (int64, error)
	Get(ctx context.Context, id string) (core.Profile, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error)
	GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error)
	GetBySchema(ctx context.Context, schema string) ([]core.Profile, error)
}

type service struct {
	repo Repository
	key  key.Service
}

// NewService creates a new profile service
func NewService(repo Repository, key key.Service) Service {
	return &service{repo, key}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repo.Count(ctx)
}

// GetByAuthorAndSchema returns profiles by owner and schema
func (s *service) GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByAuthorAndSchema")
	defer span.End()

	return s.repo.GetByAuthorAndSchema(ctx, owner, schema)
}

// GetByAuthor returns profiles by owner
func (s *service) GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByAuthor")
	defer span.End()

	return s.repo.GetByAuthor(ctx, owner)
}

// GetBySchema returns profiles by schema
func (s *service) GetBySchema(ctx context.Context, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetBySchema")
	defer span.End()

	return s.repo.GetBySchema(ctx, schema)
}

// PutProfile creates new profile if the signature is valid
func (s *service) Create(ctx context.Context, objectStr string, signature string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServicePutProfile")
	defer span.End()

	var object core.UpsertProfile[any]
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	profile := core.Profile{
		ID:        object.ID,
		Author:    object.Signer,
		Schema:    object.Schema,
		Document:  objectStr,
		Signature: signature,
	}

	err = s.repo.Upsert(ctx, profile)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	return profile, nil
}

// PutProfile creates new profile if the signature is valid
func (s *service) Update(ctx context.Context, objectStr string, signature string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServicePutProfile")
	defer span.End()

	var object core.UpsertProfile[any]
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	profile := core.Profile{
		ID:        object.ID,
		Author:    object.Signer,
		Schema:    object.Schema,
		Document:  objectStr,
		Signature: signature,
	}

	err = s.repo.Upsert(ctx, profile)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	return profile, nil
}

// Delete deletes profile
func (s *service) Delete(ctx context.Context, documentStr string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	var document core.DeleteDocument
	err := json.Unmarshal([]byte(documentStr), &document)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	deleteTarget, err := s.Get(ctx, document.Body.TargetID)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if deleteTarget.Author != document.Signer {
		err = errors.New("unauthorized")
		span.RecordError(err)
		return core.Profile{}, err
	}

	return s.repo.Delete(ctx, document.Body.TargetID)
}

func (s *service) Get(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repo.Get(ctx, id)
}
