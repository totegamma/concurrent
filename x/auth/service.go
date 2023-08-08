package auth

import (
	"context"
	"fmt"
	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/host"
	"github.com/totegamma/concurrent/x/util"
	"strconv"
	"time"
)

// Service is entity service
type Service struct {
	config util.Config
	entity *entity.Service
	host   *host.Service
}

// NewService is for wire.go
func NewService(config util.Config, entity *entity.Service, host *host.Service) *Service {
	return &Service{config, entity, host}
}

// IssueJWT takes client signed JWT and returns server signed JWT
func (s *Service) IssueJWT(ctx context.Context, request string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIssueJWT")
	defer span.End()

	// check jwt basic info
	claims, err := util.ValidateJWT(request)
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
		return "", fmt.Errorf("jwt is not for this host")
	}

	// check if issuer exists in this host
	ent, err := s.entity.Get(ctx, claims.Issuer)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// check if the entity is local user
	if ent.Host != "" {
		return "", fmt.Errorf("requester is not a local user")
	}

	// create new jwt
	response, err := util.CreateJWT(util.JwtClaims{
		Issuer:         s.config.Concurrent.CCAddr,
		Subject:        "CONCURRENT_API",
		Audience:       claims.Issuer,
		ExpirationTime: strconv.FormatInt(time.Now().Add(6*time.Hour).Unix(), 10),
		NotBefore:      strconv.FormatInt(time.Now().Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
	}, s.config.Concurrent.Prvkey)

	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// TODO: register jti

	return response, nil
}
