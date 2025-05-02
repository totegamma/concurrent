package ack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/concrnt/concrnt/client/mock"
	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/concrnt/concrnt/x/ack/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_ack.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	config := core.Config{FQDN: "local.example.com"}

	s := NewService(repo, client, entity, key, config)
	assert.NotNil(t, s)
	assert.Implements(t, (*core.AckService)(nil), s)
}

func TestService_Ack(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_ack.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	config := core.Config{FQDN: "local.example.com"}
	s := NewService(repo, client, entity, key, config)
	ctx := context.Background()

	user1 := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
	user2 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"

	testDoc := core.AckDocument{
		DocumentBase: core.DocumentBase[any]{Type: "ack"},
		From:         user1,
		To:           user2,
	}
	docBytes, _ := json.Marshal(testDoc)
	docStr := string(docBytes)
	sig := "testSig"

	testUnackDoc := core.AckDocument{
		DocumentBase: core.DocumentBase[any]{Type: "unack"},
		From:         user1,
		To:           user2,
	}
	unackDocBytes, _ := json.Marshal(testUnackDoc)
	unackDocStr := string(unackDocBytes)

	expectedAck := core.Ack{
		From:      testDoc.From,
		To:        testDoc.To,
		Document:  docStr,
		Signature: sig,
		Valid:     true,
	}

	expectedUnack := core.Ack{
		From:      testUnackDoc.From,
		To:        testUnackDoc.To,
		Document:  unackDocStr,
		Signature: sig,
		Valid:     false,
	}

	localEntity := core.Entity{
		ID:     testDoc.To,
		Domain: config.FQDN,
	}

	remoteEntity := core.Entity{
		ID:     testDoc.To,
		Domain: "remote.example.com",
	}

	// --- Ack - Local Entity ---
	t.Run("Ack_Local", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testDoc.To).Return(localEntity, nil).Times(1)
		repo.EXPECT().Ack(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ack *core.Ack) (core.Ack, error) {
				assert.Equal(t, testDoc.From, ack.From)
				assert.Equal(t, testDoc.To, ack.To)
				assert.Equal(t, docStr, ack.Document)
				assert.Equal(t, sig, ack.Signature)
				return expectedAck, nil
			}).Times(1)

		result, err := s.Ack(ctx, core.CommitModeExecute, docStr, sig)
		assert.NoError(t, err)
		assert.Equal(t, expectedAck, result)
	})

	// --- Ack - Remote Entity ---
	t.Run("Ack_Remote", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testDoc.To).Return(remoteEntity, nil).Times(1)
		client.EXPECT().Commit(gomock.Any(), remoteEntity.Domain, gomock.Any(), nil, gomock.Any()).
			Return(&http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(""))}, nil).Times(1)
		repo.EXPECT().Ack(gomock.Any(), gomock.Any()).Return(expectedAck, nil).Times(1)

		result, err := s.Ack(ctx, core.CommitModeExecute, docStr, sig)
		assert.NoError(t, err)
		assert.Equal(t, expectedAck, result)
	})

	// --- Unack - Local Entity ---
	t.Run("Unack_Local", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testUnackDoc.To).Return(localEntity, nil).Times(1)
		repo.EXPECT().Unack(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ack *core.Ack) (core.Ack, error) {
				assert.Equal(t, testUnackDoc.From, ack.From)
				assert.Equal(t, testUnackDoc.To, ack.To)
				assert.Equal(t, unackDocStr, ack.Document)
				assert.Equal(t, sig, ack.Signature)
				return expectedUnack, nil
			}).Times(1)

		result, err := s.Ack(ctx, core.CommitModeExecute, unackDocStr, sig)
		assert.NoError(t, err)
		assert.Equal(t, expectedUnack, result)
	})

	// --- Unack - Remote Entity ---
	t.Run("Unack_Remote", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testUnackDoc.To).Return(remoteEntity, nil).Times(1)
		client.EXPECT().Commit(gomock.Any(), remoteEntity.Domain, gomock.Any(), nil, gomock.Any()).
			Return(&http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(""))}, nil).Times(1)
		repo.EXPECT().Unack(gomock.Any(), gomock.Any()).Return(expectedUnack, nil).Times(1)

		result, err := s.Ack(ctx, core.CommitModeExecute, unackDocStr, sig)
		assert.NoError(t, err)
		assert.Equal(t, expectedUnack, result)
	})

	// --- Error Cases ---
	t.Run("Ack_InvalidDocType", func(t *testing.T) {
		invalidDoc := `{"type": "invalid", "from": "a", "to": "b"}`
		_, err := s.Ack(ctx, core.CommitModeExecute, invalidDoc, sig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid object type")
	})

	t.Run("Ack_InvalidJson", func(t *testing.T) {
		invalidJson := `{"type": "ack",`
		_, err := s.Ack(ctx, core.CommitModeExecute, invalidJson, sig)
		assert.Error(t, err)
	})

	t.Run("Ack_EntityGetError", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testDoc.To).Return(core.Entity{}, errors.New("entity error")).Times(1)
		_, err := s.Ack(ctx, core.CommitModeExecute, docStr, sig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "entity error")
	})

	t.Run("Ack_RemoteCommitError", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testDoc.To).Return(remoteEntity, nil).Times(1)
		client.EXPECT().Commit(gomock.Any(), remoteEntity.Domain, gomock.Any(), nil, gomock.Any()).
			Return(nil, errors.New("client commit error")).Times(1)
		// Repo should not be called
		repo.EXPECT().Ack(gomock.Any(), gomock.Any()).Times(0)

		_, err := s.Ack(ctx, core.CommitModeExecute, docStr, sig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client commit error")
	})

	t.Run("Ack_RepoAckError", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), testDoc.To).Return(localEntity, nil).Times(1)
		repo.EXPECT().Ack(gomock.Any(), gomock.Any()).Return(core.Ack{}, errors.New("repo ack error")).Times(1)

		_, err := s.Ack(ctx, core.CommitModeExecute, docStr, sig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "repo ack error")
	})

}

func TestService_GetAcker(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_ack.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	config := core.Config{}
	s := NewService(repo, client, entity, key, config)
	ctx := context.Background()
	user := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
	otherUser1 := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
	otherUser2 := "con1uwssx3d80e5wwl9fg8nnjpqms8rvs8p2lcldlr"

	expectedAcks := []core.Ack{{From: otherUser1, To: user, Document: "doc1", Signature: "sig1", Valid: true}, {From: otherUser2, To: user, Document: "doc2", Signature: "sig2", Valid: true}}

	// Success case
	repo.EXPECT().GetAcker(gomock.Any(), user).Return(expectedAcks, nil).Times(1)
	result, err := s.GetAcker(ctx, user)
	assert.NoError(t, err)
	assert.Equal(t, expectedAcks, result)

	// Error case
	repo.EXPECT().GetAcker(gomock.Any(), user).Return(nil, errors.New("repo error")).Times(1)
	_, err = s.GetAcker(ctx, user)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repo error")
}

func TestService_GetAcking(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_ack.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	config := core.Config{}
	s := NewService(repo, client, entity, key, config)
	ctx := context.Background()
	user := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
	target1 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
	target2 := "con1uwssx3d80e5wwl9fg8nnjpqms8rvs8p2lcldlr"

	expectedAcks := []core.Ack{{From: user, To: target1, Document: "doc3", Signature: "sig3", Valid: true}, {From: user, To: target2, Document: "doc4", Signature: "sig4", Valid: true}}

	// Success case
	repo.EXPECT().GetAcking(gomock.Any(), user).Return(expectedAcks, nil).Times(1)
	result, err := s.GetAcking(ctx, user)
	assert.NoError(t, err)
	assert.Equal(t, expectedAcks, result)

	// Error case
	repo.EXPECT().GetAcking(gomock.Any(), user).Return(nil, errors.New("repo error")).Times(1)
	_, err = s.GetAcking(ctx, user)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repo error")
}
