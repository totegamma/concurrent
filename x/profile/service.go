package profile

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel/codes"
)

type service struct {
	repo       Repository
	entity     core.EntityService
	policy     core.PolicyService
	semanticid core.SemanticIDService
}

// NewService creates a new profile service
func NewService(
	repo Repository,
	entity core.EntityService,
	policy core.PolicyService,
	semanticid core.SemanticIDService,
) core.ProfileService {
	return &service{
		repo,
		entity,
		policy,
		semanticid,
	}
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

	if !core.IsCCID(owner) {
		ownerentity, err := s.entity.GetByAlias(ctx, owner)
		if err != nil {
			return core.Profile{}, err
		}
		owner = ownerentity.ID
	}

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

	var doc core.ProfileDocument[any]
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

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if doc.ID == "" {
		hash := core.GetHash([]byte(document))
		hash10 := [10]byte{}
		copy(hash10[:], hash[:10])
		doc.ID = cdid.New(hash10, doc.SignedAt).String()

		_, err := s.repo.Get(ctx, doc.ID)
		if err == nil {
			span.RecordError(err)
			return core.Profile{}, errors.New("profile already exists")
		}

		policyResult, err := s.policy.TestWithPolicyURL(
			ctx,
			"",
			core.RequestContext{
				Requester: signer,
				Document:  doc,
			},
			"profile.create",
		)
		if err != nil {
			span.RecordError(err)
			return core.Profile{}, err
		}

		result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "profile.create", nil)
		if !result {
			return core.Profile{}, errors.New("policy failed")
		}

	} else {

		existance, err := s.repo.Get(ctx, doc.ID)
		if err != nil {
			span.RecordError(err)
			return core.Profile{}, err
		}

		var params map[string]any = make(map[string]any)
		if existance.PolicyParams != nil {
			json.Unmarshal([]byte(*existance.PolicyParams), &params)
		}

		policyResult, err := s.policy.TestWithPolicyURL(
			ctx,
			existance.Policy,
			core.RequestContext{
				Requester: signer,
				Self:      existance,
				Document:  doc,
				Params:    params,
			},
			"profile.update",
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "profile.update", nil)
		if !result {
			return core.Profile{}, errors.New("policy failed")
		}
	}

	var policyparams *string = nil
	if doc.PolicyParams != "" {
		policyparams = &doc.PolicyParams
	}

	profile := core.Profile{
		ID:           doc.ID,
		Author:       doc.Signer,
		Schema:       doc.Schema,
		Document:     document,
		Policy:       doc.Policy,
		PolicyParams: policyparams,
		Signature:    signature,
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
func (s *service) Delete(ctx context.Context, mode core.CommitMode, document string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Service.Delete")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	deleteTarget, err := s.Get(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	var params map[string]any = make(map[string]any)
	if deleteTarget.PolicyParams != nil {
		json.Unmarshal([]byte(*deleteTarget.PolicyParams), &params)
	}

	policyResult, err := s.policy.TestWithPolicyURL(
		ctx,
		deleteTarget.Policy,
		core.RequestContext{
			Requester: signer,
			Self:      deleteTarget,
			Document:  doc,
		},
		"profile.delete",
	)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "profile.delete", nil)
	if !result {
		return core.Profile{}, errors.New("policy failed")
	}

	return s.repo.Delete(ctx, doc.Target)
}

// Clean deletes all profiles by author
func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Profile.Service.Clean")
	defer span.End()

	return s.repo.Clean(ctx, ccid)
}
