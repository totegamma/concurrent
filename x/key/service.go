package key

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/pkg/errors"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for auth service
type Service interface {
	EnactKey(ctx context.Context, payload, signature string) (core.Key, error)
	RevokeKey(ctx context.Context, payload, signature string) (core.Key, error)
	ValidateSignedObject(ctx context.Context, payload, signature string) error
	ResolveSubkey(ctx context.Context, keyID string) (string, error)
	ResolveRemoteSubkey(ctx context.Context, keyID, domain string) (string, error)
	GetKeyResolution(ctx context.Context, keyID string) ([]core.Key, error)
	GetAllKeys(ctx context.Context, owner string) ([]core.Key, error)
}

type service struct {
	repository Repository
	entity     entity.Service
	config     util.Config
}

// NewService creates a new auth service
func NewService(repository Repository, entity entity.Service, config util.Config) Service {
	return &service{repository, entity, config}
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

	object := core.EnactKey{}
	err = json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	signerKey := object.KeyID
	if signerKey == "" {
		signerKey = object.Signer
	}

	if object.Signer != object.Body.Root {
		return core.Key{}, fmt.Errorf("Root is not matched with the signer")
	}

	if signerKey != object.Body.Parent {
		return core.Key{}, fmt.Errorf("Parent is not matched with the signer")
	}

	key := core.Key{
		ID:             object.Body.CKID,
		Root:           object.Body.Root,
		Parent:         object.Body.Parent,
		EnactPayload:   payload,
		EnactSignature: signature,
		ValidSince:     object.SignedAt,
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

	object := core.RevokeKey{}
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

	revoked, err := s.repository.Revoke(ctx, object.Body.CKID, payload, signature, object.SignedAt)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	return revoked, nil
}

func (s *service) ValidateSignedObject(ctx context.Context, payload, signature string) error {
	ctx, span := tracer.Start(ctx, "ServiceValidate")
	defer span.End()

	object := core.DocumentBase[any]{}
	err := json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return errors.Wrap(err, "failed to unmarshal payload")
	}

	// マスターキーの場合: そのまま検証して終了
	if object.KeyID == "" {
		signatureBytes, err := hex.DecodeString(signature)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[master] failed to decode signature")
		}
		err = util.VerifySignature([]byte(payload), signatureBytes, object.Signer)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[master] failed to verify signature")
		}
	} else { // サブキーの場合: 親キーを取得して検証

		domain, err := s.entity.ResolveHost(ctx, object.Signer, "")
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to resolve host")
		}

		ccid := ""

		if domain == s.config.Concurrent.FQDN {
			ccid, err = s.ResolveSubkey(ctx, object.KeyID)
			if err != nil {
				span.RecordError(err)
				return errors.Wrap(err, "[sub] failed to resolve subkey")
			}
		} else {
			ccid, err = s.ResolveRemoteSubkey(ctx, object.KeyID, domain)
			if err != nil {
				span.RecordError(err)
				return errors.Wrap(err, "[sub] failed to resolve remote subkey")
			}
		}

		if ccid != object.Signer {
			err := fmt.Errorf("Signer is not matched with the resolved signer")
			span.RecordError(err)
			return err
		}

		signatureBytes, err := hex.DecodeString(signature)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to decode signature")
		}
		err = util.VerifySignature([]byte(payload), signatureBytes, object.KeyID)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to verify signature")
		}
	}

	return nil
}

type keyResponse struct {
	Status  string     `json:"status"`
	Content []core.Key `json:"content"`
}

func (s *service) ResolveRemoteSubkey(ctx context.Context, keyID, domain string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRemoteKeyResolution")
	defer span.End()

	resovation, err := s.repository.GetRemoteKeyValidationCache(ctx, keyID)
	if err == nil {
		return resovation, nil
	}

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/key/"+keyID, nil)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteKey keyResponse
	json.Unmarshal(body, &remoteKey)

	keychain := remoteKey.Content

	if len(keychain) == 0 {
		return "", fmt.Errorf("Key not found")
	}

	rootKey := ""
	nextKey := ""
	for _, key := range keychain {

		if (nextKey != "") && (nextKey != key.ID) {
			return "", fmt.Errorf("Key %s is not a child of %s", key.ID, nextKey)
		}

		if IsCCID(key.ID) {
			break
		}

		// まず署名を検証
		err := s.ValidateSignedObject(ctx, key.EnactPayload, key.EnactSignature)
		if err != nil {
			return "", err
		}

		// 署名の内容が正しいか検証
		var enact core.EnactKey
		err = json.Unmarshal([]byte(key.EnactPayload), &enact)
		if err != nil {
			return "", err
		}

		if enact.Body.CKID != key.ID {
			return "", fmt.Errorf("KeyID in payload is not matched with the keyID")
		}

		if enact.Body.Parent != key.Parent {
			return "", fmt.Errorf("Parent in payload is not matched with the parent")
		}

		if enact.Body.Root != key.Root {
			return "", fmt.Errorf("Root in payload is not matched with the root")
		}

		if rootKey == "" {
			rootKey = key.Root
		} else {
			if rootKey != key.Root {
				return "", fmt.Errorf("Root is not matched with the previous key")
			}
		}

		if key.RevokePayload != "null" {
			return "", fmt.Errorf("Key %s is revoked", key.ID)
		}

		nextKey = key.Parent
	}

	s.repository.SetRemoteKeyValidationCache(ctx, keyID, rootKey)

	return rootKey, nil
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
		rootKey = key.Root
		validationTrace += " -> " + key.ID
		if !IsKeyValid(ctx, key) {
			return "", fmt.Errorf("Key %s is revoked. trace: %s", keyID, validationTrace)
		}
	}

	return rootKey, nil
}

func (s *service) GetKeyResolution(ctx context.Context, keyID string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetKeyResolution")
	defer span.End()

	var keys []core.Key
	var currentDepth = 0
	for {
		if IsCCID(keyID) {
			return keys, nil
		}

		key, err := s.repository.Get(ctx, keyID)
		if err != nil {
			return nil, err
		}

		keys = append(keys, key)
		keyID = key.Parent

		currentDepth++
		if currentDepth >= 8 {
			return nil, fmt.Errorf("KeyDepth is too deep")
		}
	}
}

func (s *service) GetAllKeys(ctx context.Context, owner string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetAll")
	defer span.End()

	return s.repository.GetAll(ctx, owner)
}

func IsKeyValid(ctx context.Context, key core.Key) bool {
	return key.RevokePayload == "null"
}

func IsCKID(keyID string) bool {
	return keyID[:3] == "cck"
}

func IsCCID(keyID string) bool {
	return keyID[:3] == "con"
}
