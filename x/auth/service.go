package auth

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
	"github.com/totegamma/concurrent/x/jwt"
	"strconv"
	"time"
)

// Service is the interface for auth service
type Service interface {
	IssueJWT(ctx context.Context, request string) (string, error)
	Restrict(principal Principal) echo.MiddlewareFunc
}

type service struct {
    repository Repository
	config util.Config
	entity entity.Service
	domain domain.Service
}

// NewService creates a new auth service
func NewService(repository Repository, config util.Config, entity entity.Service, domain domain.Service) Service {
	return &service{repository, config, entity, domain}
}

// IssueJWT takes client signed JWT and returns server signed JWT
func (s *service) IssueJWT(ctx context.Context, request string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIssueJWT")
	defer span.End()

	// check jwt basic info
	claims, err := jwt.Validate(request)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// TODO: check jti not used recently

	// check sub
	if claims.Subject != "CONCURRENT_APICLAIM" {
		return "", fmt.Errorf("invalid jwt subject")
	}

	// check aud
	if claims.Audience != s.config.Concurrent.FQDN {
		return "", fmt.Errorf("jwt is not for this domain")
	}

	// check if issuer exists in this domain
	ent, err := s.entity.Get(ctx, claims.Issuer)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// check if the entity is local user
	// if ent.Domain != "" {
	// 	return "", fmt.Errorf("requester is not a local user")
	// }

	// create new jwt
	response, err := jwt.Create(jwt.Claims{
		Issuer:         s.config.Concurrent.CCID,
		Subject:        "CONCURRENT_API",
		Audience:       claims.Issuer,
		ExpirationTime: strconv.FormatInt(time.Now().Add(6*time.Hour).Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
		Tag:            ent.Tag,
	}, s.config.Concurrent.PrivateKey)

	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// TODO: register jti

	return response, nil
}
