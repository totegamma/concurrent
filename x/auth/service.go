package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/totegamma/concurrent/core"
)

type service struct {
	rdb    *redis.Client
	config core.Config
	entity core.EntityService
	domain core.DomainService
	key    core.KeyService
	policy core.PolicyService
}

// NewService creates a new auth service
func NewService(
	rdb *redis.Client,
	config core.Config,
	entity core.EntityService,
	domain core.DomainService,
	key core.KeyService,
	policy core.PolicyService,
) core.AuthService {
	return &service{rdb, config, entity, domain, key, policy}
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

	if entity.Domain != s.config.FQDN {
		return "", fmt.Errorf("You are not a local entity")
	}

	documentObj := core.PassportDocument{
		Domain: s.config.FQDN,
		Entity: entity,
		Keys:   keys,
		DocumentBase: core.DocumentBase[any]{
			Signer:   s.config.CSID,
			Type:     "passport",
			SignedAt: time.Now(),
		},
	}

	document, err := json.Marshal(documentObj)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	signatureBytes, err := core.SignBytes(document, s.config.PrivateKey)
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
