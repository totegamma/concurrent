package key

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/concrnt/concrnt/core"
)

type service struct {
	repository Repository
	config     core.Config
}

// NewService creates a new auth service
func NewService(repository Repository, config core.Config) core.KeyService {
	return &service{repository, config}
}

// Enact validates new subkey and save it if valid
func (s *service) Enact(ctx context.Context, mode core.CommitMode, payload, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.EnactKey")
	defer span.End()

	object := core.EnactDocument{}
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

	object := core.RevokeDocument{}
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

// ResolveSubkey resolves a subkey (CKID) to its root key (CCID) by traversing the key hierarchy.
// It also validates that each key in the chain is not revoked.
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

// GetRemoteKeyResolution retrieves the key resolution chain for a key ID from a remote domain.
func (s *service) GetRemoteKeyResolution(ctx context.Context, remote string, keyID string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.GetRemoteKey")
	defer span.End()

	return s.repository.GetRemoteKeyResolution(ctx, remote, keyID)
}

// GetKeyResolution retrieves the key resolution chain for a local key ID (CKID) up to its root (CCID).
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

// GetAllKeys retrieves all keys associated with a specific owner (root CCID).
func (s *service) GetAllKeys(ctx context.Context, owner string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Service.GetAllKeys")
	defer span.End()

	return s.repository.GetAll(ctx, owner)
}

// Clean deletes all keys associated with a specific owner (root CCID).
func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Key.Service.Clean")
	defer span.End()

	return s.repository.Clean(ctx, ccid)
}

// IsKeyValid checks if a key is currently valid (i.e., not revoked).
func IsKeyValid(ctx context.Context, key core.Key) bool {
	return key.RevokeDocument == nil
}
