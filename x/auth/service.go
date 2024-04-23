package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
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
	IssuePassport(ctx context.Context, requester string, key []core.Key) (string, error)
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
func (s *service) IssuePassport(ctx context.Context, requester string, keys []core.Key) (string, error) {
	ctx, span := tracer.Start(ctx, "Auth.Service.IssuePassport")
	defer span.End()

	entity, err := s.entity.Get(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	if entity.Domain != s.config.Concurrent.FQDN {
		return "", fmt.Errorf("You are not a local entity")
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

	signatureBytes, err := util.SignBytes(document, s.config.Concurrent.PrivateKey)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	signature := hex.EncodeToString(signatureBytes)

	passport := core.Passport{
		Document:  string(document),
		Signature: signature,
	}

	passportBytes, err := json.Marshal(passport)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	websafePassport := base64.URLEncoding.EncodeToString(passportBytes)

	return websafePassport, nil
}
