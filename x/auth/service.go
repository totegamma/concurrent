package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/labstack/echo/v4"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
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
func (s *service) IssuePassport(ctx context.Context, requester, keyID string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIssueJWT")
	defer span.End()

	entity, err := s.entity.Get(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	if entity.Domain != s.config.Concurrent.FQDN {
		return "", fmt.Errorf("You are not a local entity")
	}

	var keys []core.Key

	if keyID == "" {
		keys, err = s.key.GetKeyResolution(ctx, keyID)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
	}

	documentObj := core.PassportDocument{
		Domain: s.config.Concurrent.FQDN,
		Entity: entity,
		Keys:   keys,
	}

	document, err := json.Marshal(documentObj)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	signature, err := util.SignBytes(document, s.config.Concurrent.PrivateKey)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	passport := core.Passport{
		Document:  string(document),
		Signature: string(signature),
	}

	passportBytes, err := json.Marshal(passport)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return string(passportBytes), nil
}
