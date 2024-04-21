//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mock/service.go

package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"strconv"
	"strings"
	"time"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/util"
	"golang.org/x/exp/slices"
)

// Service is the interface for entity service
type Service interface {
	Affiliation(ctx context.Context, document, signature, meta string) (core.Entity, error)
	Tombstone(ctx context.Context, document, signature string) (core.Entity, error)

	Get(ctx context.Context, ccid string) (core.Entity, error)
	GetWithHint(ctx context.Context, ccid, hint string) (core.Entity, error)
	List(ctx context.Context) ([]core.Entity, error)
	UpdateScore(ctx context.Context, id string, score int) error
	UpdateTag(ctx context.Context, id, tag string) error
	IsUserExists(ctx context.Context, user string) bool
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
	PullEntityFromRemote(ctx context.Context, id, domain string) (core.Entity, error)
}

type service struct {
	repository Repository
	client     client.Client
	config     util.Config
	jwtService jwt.Service
}

// NewService creates a new entity service
func NewService(repository Repository, client client.Client, config util.Config, jwtService jwt.Service) Service {
	return &service{
		repository,
		client,
		config,
		jwtService,
	}
}

// PullEntityFromRemote pulls entity from remote
func (s *service) PullEntityFromRemote(ctx context.Context, id, hintDomain string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryPullEntityFromRemote")
	defer span.End()

	targetDomain, err := s.client.ResolveAddress(ctx, hintDomain, id)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	entity, err := s.client.GetEntity(ctx, targetDomain, id)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	err = util.VerifySignature([]byte(entity.AffiliationDocument), []byte(entity.AffiliationSignature), id)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	created, err := s.Affiliation(ctx, entity.AffiliationDocument, entity.AffiliationSignature, "")
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	return created, nil
}

// Total returns the count number of entities
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repository.Count(ctx)
}

func (s *service) Affiliation(ctx context.Context, document, signature, option string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceAffiliation")
	defer span.End()

	var doc core.EntityAffiliation
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, errors.Wrap(err, "Failed to unmarshal document")
	}

	existence, exists := s.repository.Get(ctx, doc.Signer)
	if exists == nil {
		var existenceAffiliation core.EntityAffiliation
		err = json.Unmarshal([]byte(existence.AffiliationDocument), &existenceAffiliation)
		if err != nil {
			span.RecordError(err)
			return core.Entity{}, errors.Wrap(err, "Failed to unmarshal existence affiliation document")
		}

		if !existenceAffiliation.SignedAt.After(doc.SignedAt) {
			return existence, nil
		}
	}

	if doc.Domain == s.config.Concurrent.FQDN {
		if s.config.Profile.SiteKey != "" {
			captchaVerified, ok := ctx.Value(core.CaptchaVerifiedKey).(bool)
			if !ok || !captchaVerified {
				return core.Entity{}, errors.New("Captcha verification failed")
			}
		}

		var opts affiliationOption
		err = json.Unmarshal([]byte(option), &opts)
		if err != nil {
			span.RecordError(err)
			return core.Entity{}, errors.Wrap(err, "Failed to unmarshal option")
		}

		switch s.config.Concurrent.Registration {
		case "open":
			entity, _, err := s.repository.CreateWithMeta(
				ctx,
				core.Entity{
					ID:                   doc.Signer,
					Domain:               doc.Domain,
					Tag:                  "",
					Score:                0,
					IsScoreFixed:         false,
					AffiliationDocument:  document,
					AffiliationSignature: signature,
				},
				core.EntityMeta{
					ID:      doc.Signer,
					Info:    opts.Info,
					Inviter: nil,
				},
			)

			if err != nil {
				return core.Entity{}, errors.Wrap(err, "Failed to create entity")
			}

			return entity, nil
		case "invite":
			if opts.Invitation == "" {
				return core.Entity{}, fmt.Errorf("invitation code is required")
			}

			claims, err := jwt.Validate(opts.Invitation)
			if err != nil {
				span.RecordError(err)
				return core.Entity{}, err
			}
			if claims.Subject != "CONCURRENT_INVITE" {
				return core.Entity{}, fmt.Errorf("invalid invitation code")
			}

			ok, err := s.jwtService.CheckJTI(ctx, claims.JWTID)
			if err != nil {
				span.RecordError(err)
				return core.Entity{}, err
			}
			if !ok {
				return core.Entity{}, fmt.Errorf("token is already used")
			}

			inviter, err := s.repository.Get(ctx, claims.Issuer)
			if err != nil {
				span.RecordError(err)
				return core.Entity{}, err
			}

			inviterTags := strings.Split(inviter.Tag, ",")
			if !slices.Contains(inviterTags, "_invite") {
				return core.Entity{}, fmt.Errorf("inviter is not allowed to invite")
			}

			registered, _, err := s.repository.CreateWithMeta(
				ctx,
				core.Entity{
					ID:                   doc.Signer,
					Domain:               doc.Domain,
					Tag:                  "",
					Score:                0,
					IsScoreFixed:         false,
					AffiliationDocument:  document,
					AffiliationSignature: signature,
				},
				core.EntityMeta{
					ID:      doc.Signer,
					Info:    opts.Info,
					Inviter: &claims.Issuer,
				},
			)

			if err != nil {
				span.RecordError(err)
				return core.Entity{}, err
			}

			expireAt, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
			if err != nil {
				span.RecordError(err)
				return registered, err
			}
			err = s.jwtService.InvalidateJTI(ctx, claims.JWTID, time.Unix(expireAt, 0))

			if err != nil {
				span.RecordError(err)
				return core.Entity{}, err
			}

			return registered, nil

		default:
			return core.Entity{}, fmt.Errorf("registration is not open")
		}
	} else {
		newEntity := core.Entity{
			ID:                   doc.Signer,
			Domain:               doc.Domain,
			AffiliationDocument:  document,
			AffiliationSignature: signature,
		}

		if exists == nil {
			newEntity.Tag = existence.Tag
			newEntity.IsScoreFixed = existence.IsScoreFixed
			newEntity.Score = existence.Score
		}

		created, err := s.repository.Create(ctx, newEntity)
		if err != nil {
			span.RecordError(err)
			return core.Entity{}, err
		}

		return created, nil
	}
}

func (s *service) Tombstone(ctx context.Context, document, signature string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceTombstone")
	defer span.End()

	var doc core.EntityTombstone
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, errors.Wrap(err, "Failed to unmarshal document")
	}

	err = s.repository.SetTombstone(ctx, doc.Signer, document, signature)

	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	return core.Entity{}, nil
}

// Get returns entity by ccid
func (s *service) Get(ctx context.Context, key string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	entity, err := s.repository.Get(ctx, key)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	return entity, nil
}

// GetWithHint returns entity by ccid with hint
func (s *service) GetWithHint(ctx context.Context, ccid, hint string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetWithHint")
	defer span.End()

	entity, err := s.repository.Get(ctx, ccid)
	if err == nil {
		return entity, nil
	}

	if hint == "" {
		return core.Entity{}, errors.New("hint is required")
	}

	entity, err = s.PullEntityFromRemote(ctx, ccid, hint)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	return entity, nil
}

// List returns all entities
func (s *service) List(ctx context.Context) ([]core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// IsUserExists returns true if user exists
func (s *service) IsUserExists(ctx context.Context, user string) bool {
	ctx, span := tracer.Start(ctx, "ServiceIsUserExists")
	defer span.End()

	_, err := s.repository.Get(ctx, user)
	if err != nil {
		return false
	}
	return true
}

// UpdateScore updates entity score
func (s *service) UpdateScore(ctx context.Context, id string, score int) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateScore")
	defer span.End()

	return s.repository.UpdateScore(ctx, id, score)
}

// UpdateTag updates entity tag
func (s *service) UpdateTag(ctx context.Context, id, tag string) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateTag")
	defer span.End()

	return s.repository.UpdateTag(ctx, id, tag)
}

// Delete deletes entity
func (s *service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}
