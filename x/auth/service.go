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
	IssuePassport(ctx context.Context, request string, remote string) (string, error)
	Restrict(principal Principal) echo.MiddlewareFunc
}

type service struct {
    repository Repository
	config util.Config
	entity entity.Service
	domain domain.Service
    jwtService jwt.Service
}

// NewService creates a new auth service
func NewService(repository Repository, config util.Config, entity entity.Service, domain domain.Service, jwtService jwt.Service) Service {
	return &service{repository, config, entity, domain, jwtService}
}

// GetPassport takes client signed JWT and returns server signed JWT
func (s *service) IssuePassport(ctx context.Context, request string, remote string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIssueJWT")
	defer span.End()

	// check jwt basic info
	claims, err := jwt.Validate(request)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

    // check if jwt is not used before
    ok, err := s.jwtService.CheckJTI(ctx, claims.JWTID)
    if err != nil {
        span.RecordError(err)
        return "", err
    }
    if !ok {
        return "", fmt.Errorf("jti is not valid")
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

	// create new jwt
	response, err := jwt.Create(jwt.Claims{
		Issuer:         s.config.Concurrent.CCID,
		Subject:        "CC_PASSPORT",
		Audience:       remote,
        Principal:      claims.Issuer,
		ExpirationTime: strconv.FormatInt(time.Now().Add(6*time.Hour).Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
		Tag:            ent.Tag,
	}, s.config.Concurrent.PrivateKey)

	if err != nil {
		span.RecordError(err)
		return "", err
	}

    // invalidate old jwt
    expireAt, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
    if err != nil {
        span.RecordError(err)
        return "", err
    }
    err = s.jwtService.InvalidateJTI(ctx, claims.JWTID, time.Unix(expireAt, 0))
    if err != nil {
        span.RecordError(err)
        return "", err
    }

	return response, nil
}
