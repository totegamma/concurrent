//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mock/service.go
package key

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for auth service
type Service interface {
	Enact(ctx context.Context, mode core.CommitMode, payload, signature string) (core.Key, error)
	Revoke(ctx context.Context, mode core.CommitMode, payload, signature string) (core.Key, error)
	ValidateDocument(ctx context.Context, document, signature string, keys []core.Key) error
	ResolveSubkey(ctx context.Context, keyID string) (string, error)
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

// Enact validates new subkey and save it if valid
func (s *service) Enact(ctx context.Context, mode core.CommitMode, payload, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.EnactKey")
	defer span.End()

	object := core.EnactKey{}
	err := json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	signerKey := object.KeyID
	if signerKey == "" {
		signerKey = object.Signer
	}

	if object.Signer != object.Root {
		return core.Key{}, fmt.Errorf("Root is not matched with the signer")
	}

	if signerKey != object.Parent {
		return core.Key{}, fmt.Errorf("Parent is not matched with the signer")
	}

	key := core.Key{
		ID:             object.Target,
		Root:           object.Root,
		Parent:         object.Parent,
		EnactDocument:  payload,
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

// Revoke validates new subkey and save it if valid
func (s *service) Revoke(ctx context.Context, mode core.CommitMode, payload, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.RevokeKey")
	defer span.End()

	object := core.RevokeKey{}
	err := json.Unmarshal([]byte(payload), &object)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	if object.Type != "revoke" {
		return core.Key{}, fmt.Errorf("Invalid type: %s", object.Type)
	}

	targetKeyResolution, err := s.GetKeyResolution(ctx, object.Target)
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

	revoked, err := s.repository.Revoke(ctx, object.Target, payload, signature, object.SignedAt)
	if err != nil {
		span.RecordError(err)
		return core.Key{}, err
	}

	return revoked, nil
}

func (s *service) ValidateDocument(ctx context.Context, document, signature string, keys []core.Key) error {
	ctx, span := tracer.Start(ctx, "Key.Service.ValidateDocument")
	defer span.End()

	object := core.DocumentBase[any]{}
	err := json.Unmarshal([]byte(document), &object)
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
		err = util.VerifySignature([]byte(document), signatureBytes, object.Signer)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[master] failed to verify signature")
		}
	} else { // サブキーの場合: 親キーを取得して検証

		signer, err := s.entity.Get(ctx, object.Signer)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to resolve host")
		}

		ccid := ""

		if signer.Domain == s.config.Concurrent.FQDN {
			ccid, err = s.ResolveSubkey(ctx, object.KeyID)
			if err != nil {
				span.RecordError(err)
				return errors.Wrap(err, "[sub] failed to resolve subkey")
			}
		} else {
			ccid, err = ValidateKeyResolution(keys)
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
		err = util.VerifySignature([]byte(document), signatureBytes, object.KeyID)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to verify signature")
		}
	}

	return nil
}

func ValidateKeyResolution(keys []core.Key) (string, error) {

	var rootKey string
	var nextKey string
	for _, key := range keys {
		if (nextKey != "") && (nextKey != key.ID) {
			return "", fmt.Errorf("Key %s is not a child of %s", key.ID, nextKey)
		}

		signature, err := hex.DecodeString(key.EnactSignature)
		if err != nil {
			return "", err
		}
		err = util.VerifySignature([]byte(key.EnactDocument), signature, key.Parent)
		if err != nil {
			return "", err
		}

		var enact core.EnactKey
		err = json.Unmarshal([]byte(key.EnactDocument), &enact)
		if err != nil {
			return "", err
		}

		if enact.Signer != key.Parent {
			return "", fmt.Errorf("enact signer is not matched with the parent")
		}

		if enact.Target != key.ID {
			return "", fmt.Errorf("KeyID in payload is not matched with the keyID")
		}

		if enact.Parent != key.Parent {
			return "", fmt.Errorf("Parent in payload is not matched with the parent")
		}

		if enact.Root != key.Root {
			return "", fmt.Errorf("Root in payload is not matched with the root")
		}

		if rootKey == "" {
			rootKey = key.Root
		} else {
			if rootKey != key.Root {
				return "", fmt.Errorf("Root is not matched with the previous key")
			}
		}

		if key.RevokeDocument != nil {
			return "", fmt.Errorf("Key %s is revoked", key.ID)
		}

		nextKey = key.Parent
	}

	return rootKey, nil
}

func (s *service) ResolveSubkey(ctx context.Context, keyID string) (string, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.ResolveSubkey")
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
	ctx, span := tracer.Start(ctx, "Key.Service.GetKeyResolution")
	defer span.End()

	var keys []core.Key
	var currentDepth = 0
	for {
		if core.IsCCID(keyID) {
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
	ctx, span := tracer.Start(ctx, "Key.Service.GetAllKeys")
	defer span.End()

	return s.repository.GetAll(ctx, owner)
}

func IsKeyValid(ctx context.Context, key core.Key) bool {
	return key.RevokeDocument == nil
}
