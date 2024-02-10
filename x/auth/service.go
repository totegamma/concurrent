package auth

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/xid"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for auth service
type Service interface {
	IssuePassport(ctx context.Context, request string, remote string) (string, error)
	Restrict(principal Principal) echo.MiddlewareFunc

	EnactKey(ctx context.Context, payload, signature string) (core.Key, error)
}

type service struct {
	repository Repository
	config     util.Config
	entity     entity.Service
	domain     domain.Service
}

// NewService creates a new auth service
func NewService(repository Repository, config util.Config, entity entity.Service, domain domain.Service) Service {
	return &service{repository, config, entity, domain}
}

// GetPassport takes client signed JWT and returns server signed JWT
func (s *service) IssuePassport(ctx context.Context, requester, remote string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIssueJWT")
	defer span.End()

	// check if issuer exists in this domain
	ent, err := s.entity.Get(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// create new jwt
	response, err := jwt.Create(jwt.Claims{
		Issuer:         s.config.Concurrent.CCID,
		Subject:        "CC_PASSPORT",
		Audience:       remote,
		Principal:      requester,
		ExpirationTime: strconv.FormatInt(time.Now().Add(6*time.Hour).Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
		Tag:            ent.Tag,
	}, s.config.Concurrent.PrivateKey)

	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return response, nil
}

// EnactKey validates new subkey and save it if valid
func (s *service) EnactKey(ctx context.Context, payload, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "ServiceEnactKey")
	defer span.End()

	object := core.SignedObject[core.Enact]{}
	err := json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	// TODO: add validation logic

	key := core.Key{
		ID:             object.Body.CKID,
		Root:           object.Body.Root,
		Parent:         object.Body.Parent,
		EnactPayload:   payload,
		EnactSignature: signature,
	}

	created, err := s.repository.Enact(ctx, key)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	return created, nil
}
