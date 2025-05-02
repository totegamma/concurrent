package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	entity := mock_core.NewMockEntityService(ctrl)
	domain := mock_core.NewMockDomainService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	policy := mock_core.NewMockPolicyService(ctrl)
	config := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

	s := NewService(rdb, config, entity, domain, key, policy)
	assert.NotNil(t, s)
	assert.Implements(t, (*core.AuthService)(nil), s)
}

func TestService_IssuePassport(t *testing.T) {
	ctx := t.Context()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	rdb, cleanup := testutil.CreateRDB()
	defer cleanup()

	entity := mock_core.NewMockEntityService(ctrl)
	domain := mock_core.NewMockDomainService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	policy := mock_core.NewMockPolicyService(ctrl)
	// Use a valid dummy private key
	dummyPrivateKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: dummyPrivateKey}
	s := NewService(rdb, config, entity, domain, key, policy)

	requesterID := "con1requester"
	requesterEntity := core.Entity{ID: requesterID, Domain: config.FQDN} // Local entity
	remoteEntity := core.Entity{ID: "con1remote", Domain: "remote.example.com"}
	dummyKeys := []core.Key{{ID: "key1"}}

	// --- Success Case ---
	t.Run("Success", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), requesterID).Return(requesterEntity, nil).Times(1)

		passportB64, err := s.IssuePassport(ctx, requesterID, dummyKeys)
		assert.NoError(t, err)
		assert.NotEmpty(t, passportB64)

		// Decode and verify passport content (optional but good)
		passportBytes, err := base64.URLEncoding.DecodeString(passportB64)
		assert.NoError(t, err)
		var passport core.Passport
		err = json.Unmarshal(passportBytes, &passport)
		assert.NoError(t, err)
		assert.NotEmpty(t, passport.Document)
		assert.NotEmpty(t, passport.Signature)

		var passportDoc core.PassportDocument
		err = json.Unmarshal([]byte(passport.Document), &passportDoc)
		assert.NoError(t, err)
		assert.Equal(t, config.FQDN, passportDoc.Domain)
		assert.Equal(t, config.CSID, passportDoc.Signer)
		assert.Equal(t, requesterEntity, passportDoc.Entity)
		assert.Equal(t, dummyKeys, passportDoc.Keys)
		assert.Equal(t, "passport", passportDoc.Type)
	})

	// --- Failure Case: Entity Not Found ---
	t.Run("Fail_EntityNotFound", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), requesterID).Return(core.Entity{}, errors.New("not found")).Times(1)

		_, err := s.IssuePassport(ctx, requesterID, dummyKeys)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	// --- Failure Case: Remote Entity ---
	t.Run("Fail_RemoteEntity", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), remoteEntity.ID).Return(remoteEntity, nil).Times(1)

		_, err := s.IssuePassport(ctx, remoteEntity.ID, dummyKeys)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "You are not a local entity")
	})

	// --- Failure Case: Invalid Private Key (Simulated by using a bad key in config) ---
	t.Run("Fail_SignError", func(t *testing.T) {
		badConfig := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: "invalidkey"}
		badService := NewService(rdb, badConfig, entity, domain, key, policy)
		entity.EXPECT().Get(gomock.Any(), requesterID).Return(requesterEntity, nil).Times(1)

		_, err := badService.IssuePassport(ctx, requesterID, dummyKeys)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to convert private key")
	})
}

// Note: IdentifyIdentity and RateLimiter are middleware and typically tested via integration tests or handler tests
// that use the actual middleware, rather than direct unit tests on the service returning the middleware function.
