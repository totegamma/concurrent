package association

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt/client/mock"
	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/concrnt/concrnt/x/association/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_association.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	domain := mock_core.NewMockDomainService(ctrl)
	profile := mock_core.NewMockProfileService(ctrl)
	timeline := mock_core.NewMockTimelineService(ctrl)
	subscription := mock_core.NewMockSubscriptionService(ctrl)
	message := mock_core.NewMockMessageService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	policy := mock_core.NewMockPolicyService(ctrl)
	dummyPrivateKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: dummyPrivateKey}

	s := NewService(repo, client, entity, domain, profile, timeline, subscription, message, key, policy, config)
	assert.NotNil(t, s)
	assert.Implements(t, (*core.AssociationService)(nil), s)
}

// Helper function to create a valid AssociationDocument string
func createAssociationDocStr(signer, owner, schema, target, variant string, timelines []string, body any) string {
	doc := core.AssociationDocument[any]{
		DocumentBase: core.DocumentBase[any]{
			Signer:   signer,
			Owner:    owner,
			Schema:   schema,
			SignedAt: time.Now(),
			Body:     body,
		},
		Target:    target,
		Variant:   variant,
		Timelines: timelines,
	}
	bytes, _ := json.Marshal(doc)
	return string(bytes)
}

// Helper function to create a valid DeleteDocument string
func createDeleteDocStr(signer, target string) string {
	doc := core.DeleteDocument{
		DocumentBase: core.DocumentBase[any]{
			Signer:   signer,
			Type:     "delete",
			SignedAt: time.Now(),
		},
		Target: target,
	}
	bytes, _ := json.Marshal(doc)
	return string(bytes)
}

func TestService_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_association.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	domain := mock_core.NewMockDomainService(ctrl)
	profile := mock_core.NewMockProfileService(ctrl)
	timeline := mock_core.NewMockTimelineService(ctrl)
	subscription := mock_core.NewMockSubscriptionService(ctrl)
	message := mock_core.NewMockMessageService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	policy := mock_core.NewMockPolicyService(ctrl)
	dummyPrivateKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: dummyPrivateKey}
	s := NewService(repo, client, entity, domain, profile, timeline, subscription, message, key, policy, config)
	ctx := t.Context()

	signerID := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
	ownerID := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
	targetMessageID := "m" + "targetMessageID1234567890"
	targetProfileID := "p" + "targetProfileID1234567890"
	schema := "testSchema"
	variant := "testVariant"
	timelineID := "t" + "timelineID12345678901234"
	remoteTimelineID := "t" + "remoteTimeline1234567890@remote.example.com"
	sig := "testSig"

	signerEntity := core.Entity{ID: signerID, Domain: config.FQDN}
	ownerEntity := core.Entity{ID: ownerID, Domain: config.FQDN}
	targetMessage := core.Message{ID: targetMessageID, Timelines: []string{timelineID, remoteTimelineID}, Policy: ""}
	targetProfile := core.Profile{ID: targetProfileID, Policy: ""}
	localTimeline := core.Timeline{ID: timelineID, Policy: ""}

	docStr := createAssociationDocStr(signerID, ownerID, schema, targetMessageID, variant, []string{timelineID, remoteTimelineID}, map[string]string{"body": "test"})

	// --- Success Case: Target Message, Local Owner ---
	t.Run("Create_TargetMessage_LocalOwner", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		entity.EXPECT().Get(gomock.Any(), ownerID).Return(ownerEntity, nil).AnyTimes()
		message.EXPECT().GetAsUser(gomock.Any(), targetMessageID, signerEntity).Return(targetMessage, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), timelineID).Return(localTimeline, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), remoteTimelineID).Return(core.Timeline{}, errors.New("not found")).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "timeline.message.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "message.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "message.association.attach", nil).Return(true).Times(1)
		repo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, assoc core.Association) (core.Association, error) {
				assert.Equal(t, signerID, assoc.Author)
				assert.Equal(t, ownerID, assoc.Owner)
				assert.Equal(t, schema, assoc.Schema)
				assert.Equal(t, targetMessageID, assoc.Target)
				assert.True(t, strings.HasPrefix(assoc.ID, "a"))
				assoc.ID = "a" + "generatedID1234567890123"
				return assoc, nil
			}).Times(1)
		timeline.EXPECT().NormalizeTimelineID(gomock.Any(), timelineID).Return(timelineID+"@"+config.FQDN, nil).AnyTimes()
		timeline.EXPECT().NormalizeTimelineID(gomock.Any(), remoteTimelineID).Return(remoteTimelineID, nil).AnyTimes()
		timeline.EXPECT().PostItem(gomock.Any(), timelineID, gomock.Any(), docStr, sig).Return(core.TimelineItem{}, nil).AnyTimes()
		timeline.EXPECT().PublishEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		domain.EXPECT().GetByFQDN(gomock.Any(), "remote.example.com").Return(core.Domain{}, nil).AnyTimes()
		client.EXPECT().Commit(gomock.Any(), "remote.example.com", gomock.Any(), nil, gomock.Any()).Return(&http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(""))}, nil).AnyTimes()
		timeline.EXPECT().GetOwners(gomock.Any(), gomock.Any()).Return([]string{ownerID}, nil).Times(1)

		assoc, affected, err := s.Create(ctx, core.CommitModeExecute, docStr, sig)
		assert.NoError(t, err)
		assert.NotEmpty(t, assoc.ID)
		assert.Equal(t, []string{ownerID}, affected)
	})

	// --- Success Case: Target Profile, Local Owner ---
	t.Run("Create_TargetProfile_LocalOwner", func(t *testing.T) {
		docStrProfile := createAssociationDocStr(signerID, ownerID, schema, targetProfileID, variant, []string{}, map[string]string{"body": "profile assoc"})
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		entity.EXPECT().Get(gomock.Any(), ownerID).Return(ownerEntity, nil).AnyTimes()
		profile.EXPECT().Get(gomock.Any(), targetProfileID).Return(targetProfile, nil).Times(1)
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "profile.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "profile.association.attach", nil).Return(true).Times(1)
		repo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, assoc core.Association) (core.Association, error) {
				assoc.ID = "a" + "generatedIDProfile123"
				return assoc, nil
			}).Times(1)
		timeline.EXPECT().GetOwners(gomock.Any(), gomock.Any()).Return([]string{}, nil).Times(1)

		assoc, affected, err := s.Create(ctx, core.CommitModeExecute, docStrProfile, sig)
		assert.NoError(t, err)
		assert.NotEmpty(t, assoc.ID)
		assert.Empty(t, affected)
	})

	// --- Failure Case: Policy Denied ---
	t.Run("Create_PolicyDenied", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		entity.EXPECT().Get(gomock.Any(), ownerID).Return(ownerEntity, nil).AnyTimes()
		message.EXPECT().GetAsUser(gomock.Any(), targetMessageID, signerEntity).Return(targetMessage, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), timelineID).Return(localTimeline, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), remoteTimelineID).Return(core.Timeline{}, errors.New("not found")).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "timeline.message.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "message.association.attach").Return(core.PolicyEvalResultDeny, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "message.association.attach", nil).Return(false).Times(1)
		// Repo should not be called
		repo.EXPECT().Create(gomock.Any(), gomock.Any()).Times(0)

		_, _, err := s.Create(ctx, core.CommitModeExecute, docStr, sig)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, core.ErrorPermissionDenied))
	})

	// --- Failure Case: Already Exists ---
	t.Run("Create_AlreadyExists", func(t *testing.T) {
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		entity.EXPECT().Get(gomock.Any(), ownerID).Return(ownerEntity, nil).AnyTimes()
		message.EXPECT().GetAsUser(gomock.Any(), targetMessageID, signerEntity).Return(targetMessage, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), timelineID).Return(localTimeline, nil).AnyTimes()
		timeline.EXPECT().GetTimelineAutoDomain(gomock.Any(), remoteTimelineID).Return(core.Timeline{}, errors.New("not found")).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "timeline.message.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "message.association.attach").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "message.association.attach", nil).Return(true).Times(1)
		repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(core.Association{}, core.NewErrorAlreadyExists()).Times(1)

		_, _, err := s.Create(ctx, core.CommitModeExecute, docStr, sig)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, core.ErrorAlreadyExists))
	})

}

func TestService_Delete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_association.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	entity := mock_core.NewMockEntityService(ctrl)
	domain := mock_core.NewMockDomainService(ctrl)
	profile := mock_core.NewMockProfileService(ctrl)
	timeline := mock_core.NewMockTimelineService(ctrl)
	subscription := mock_core.NewMockSubscriptionService(ctrl)
	message := mock_core.NewMockMessageService(ctrl)
	key := mock_core.NewMockKeyService(ctrl)
	policy := mock_core.NewMockPolicyService(ctrl)
	dummyPrivateKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := core.Config{FQDN: "local.example.com", CCID: "localCCID", CSID: "localCSID", PrivateKey: dummyPrivateKey}
	s := NewService(repo, client, entity, domain, profile, timeline, subscription, message, key, policy, config)
	ctx := t.Context()

	signerID := "con1signer"
	targetAssocID := "a" + "targetAssocID12345678901"
	targetMessageID := "m" + "targetMessageID1234567890"
	timelineID := "t" + "timelineID12345678901234"
	sig := "testSig"

	signerEntity := core.Entity{ID: signerID, Domain: config.FQDN}
	targetAssoc := core.Association{
		ID:        targetAssocID,
		Author:    signerID,
		Target:    targetMessageID,
		Timelines: []string{timelineID},
	}
	targetMessage := core.Message{ID: targetMessageID, Timelines: []string{timelineID}}

	deleteDocStr := createDeleteDocStr(signerID, targetAssocID)

	// --- Success Case ---
	t.Run("Delete_Success", func(t *testing.T) {
		repo.EXPECT().Get(gomock.Any(), targetAssocID).Return(targetAssoc, nil).Times(1)
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "association.delete").Return(core.PolicyEvalResultAllow, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "association.delete", nil).Return(true).Times(1)
		repo.EXPECT().Delete(gomock.Any(), targetAssocID).Return(nil).Times(1)
		timeline.EXPECT().RemoveItemsByResourceID(gomock.Any(), targetAssocID).Return(nil).Times(1)
		timeline.EXPECT().PublishEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		message.EXPECT().GetAsUser(gomock.Any(), targetMessageID, signerEntity).Return(targetMessage, nil).AnyTimes()
		timeline.EXPECT().NormalizeTimelineID(gomock.Any(), timelineID).Return(timelineID+"@"+config.FQDN, nil).AnyTimes()
		timeline.EXPECT().GetOwners(gomock.Any(), targetAssoc.Timelines).Return([]string{signerID}, nil).Times(1)

		deletedAssoc, affected, err := s.Delete(ctx, core.CommitModeExecute, deleteDocStr, sig)
		assert.NoError(t, err)
		assert.Equal(t, targetAssoc, deletedAssoc)
		assert.Equal(t, []string{signerID}, affected)
	})

	// --- Failure Case: Policy Denied ---
	t.Run("Delete_PolicyDenied", func(t *testing.T) {
		repo.EXPECT().Get(gomock.Any(), targetAssocID).Return(targetAssoc, nil).Times(1)
		entity.EXPECT().Get(gomock.Any(), signerID).Return(signerEntity, nil).AnyTimes()
		policy.EXPECT().TestWithPolicyURL(gomock.Any(), "", gomock.Any(), "association.delete").Return(core.PolicyEvalResultDeny, nil).AnyTimes()
		policy.EXPECT().Summerize(gomock.Any(), "association.delete", nil).Return(false).Times(1)
		// Repo Delete should not be called
		repo.EXPECT().Delete(gomock.Any(), targetAssocID).Times(0)

		_, _, err := s.Delete(ctx, core.CommitModeExecute, deleteDocStr, sig)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, core.ErrorPermissionDenied))
	})

	// --- Failure Case: Already Deleted ---
	t.Run("Delete_AlreadyDeleted", func(t *testing.T) {
		repo.EXPECT().Get(gomock.Any(), targetAssocID).Return(core.Association{}, core.ErrorNotFound).Times(1)

		_, _, err := s.Delete(ctx, core.CommitModeExecute, deleteDocStr, sig)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, core.ErrorAlreadyDeleted))
	})
}
