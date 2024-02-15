package auth

import (
	"context"
	"encoding/json"
	"fmt"
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
	RevokeKey(ctx context.Context, payload, signature string) (core.Key, error)
	ValidateSignedObject(ctx context.Context, payload, signature string) error
	ResolveSubkey(ctx context.Context, keyID string) (string, error)
	GetKeyResolution(ctx context.Context, keyID string) ([]core.Key, error)
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

	err := s.ValidateSignedObject(ctx, payload, signature)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	object := core.SignedObject[core.Enact]{}
	err = json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

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

// RevokeKey validates new subkey and save it if valid
func (s *service) RevokeKey(ctx context.Context, payload, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "ServiceRevokeKey")
	defer span.End()

	err := s.ValidateSignedObject(ctx, payload, signature)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	object := core.SignedObject[core.Revoke]{}
	err = json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	if object.Type != "revoke" {
		return core.Key{}, fmt.Errorf("Invalid type: %s", object.Type)
	}

	targetKeyResolution, err := s.GetKeyResolution(ctx, object.Body.CKID)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	performerKey := object.KeyID
	if performerKey == "" {
		performerKey = object.Signer
	}

	performerKeyResolution, err := s.GetKeyResolution(ctx, performerKey)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	if len(targetKeyResolution) < len(performerKeyResolution) {
		return core.Key{}, fmt.Errorf("KeyDepth is not enough. target: %d, performer: %d", len(targetKeyResolution), len(performerKeyResolution))
	}

	revoked, err := s.repository.Revoke(ctx, object.Body.CKID, payload, signature)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	return revoked, nil
}

func (s *service) ValidateSignedObject(ctx context.Context, payload, signature string) error {
	ctx, span := tracer.Start(ctx, "ServiceValidate")
	defer span.End()

	object := core.SignedObject[any]{}
	err := json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// マスターキーの場合: そのまま検証して終了
	if object.KeyID == "" {
		err := util.VerifySignature(payload, object.Signer, signature)
		if err != nil {
			span.RecordError(err)
			return err
		}
	} else { // サブキーの場合: 親キーを取得して検証
		_, err := s.ResolveSubkey(ctx, object.KeyID)
		if err != nil {
			span.RecordError(err)
			return err
		}
		err = util.VerifySignature(payload, object.KeyID, signature)
		if err != nil {
			span.RecordError(err)
			return err
		}
	}

	return nil
}

func (s *service) ResolveSubkey(ctx context.Context, keyID string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceIsKeyChainValid")
	defer span.End()

	rootKey := keyID
	validationTrace := keyID

	keychain, err := s.GetKeyResolution(ctx, keyID)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	for _, key := range keychain {
		rootKey = key.ID
		validationTrace += " -> " + key.ID
		if !isKeyValid(ctx, key) {
			return "", fmt.Errorf("Key %s is revoked. trace: %s", keyID, validationTrace)
		}
	}

	return rootKey, nil
}

func (s *service) GetKeyResolution(ctx context.Context, keyID string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetKeyResolution")
	defer span.End()

	var keys []core.Key
	for {
		if isCCID(keyID) {
			return keys, nil
		}

		key, err := s.repository.Get(ctx, keyID)
		if err != nil {
			return nil, err
		}

		keys = append(keys, key)
		keyID = key.Parent
	}
}

func isKeyValid(ctx context.Context, key core.Key) bool {
	return key.RevokePayload == "null"
}

func isCKID(keyID string) bool {
	return keyID[:2] == "CK"
}

func isCCID(keyID string) bool {
	return keyID[:2] == "CC"
}
