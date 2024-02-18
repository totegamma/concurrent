package auth

import (
	"context"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/xid"

	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for auth service
type Service interface {
	IssuePassport(ctx context.Context, request string, remote string) (string, error)
	IdentifyIdentity(next echo.HandlerFunc) echo.HandlerFunc
}

type service struct {
	config util.Config
	entity entity.Service
	domain domain.Service
	key    key.Service
}

// NewService creates a new auth service
func NewService(config util.Config, entity entity.Service, domain domain.Service, key key.Service) Service {
	return &service{config, entity, domain, key}
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
		Domain:         s.config.Concurrent.FQDN,
	}, s.config.Concurrent.PrivateKey)

	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return response, nil
}
