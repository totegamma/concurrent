package timeline

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/core/mock"
	"github.com/totegamma/concurrent/x/timeline/mock"
	"go.uber.org/mock/gomock"
)

func TestGetRecentItemsSimple(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pivotEpoch := "6000"
	pivotTime := core.EpochTime("6300")
	prevEpoch := "5400"

	mockRepo := mock_timeline.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "t00000000000000000000000000").
		Return("t00000000000000000000000000@local.example.com", nil).AnyTimes()
	mockRepo.EXPECT().
		LookupChunkItrs(gomock.Any(), []string{"t00000000000000000000000000@local.example.com"}, pivotEpoch).
		Return(map[string]string{"t00000000000000000000000000@local.example.com": pivotEpoch}, nil)
	mockRepo.EXPECT().
		LoadChunkBodies(gomock.Any(), map[string]string{"t00000000000000000000000000@local.example.com": pivotEpoch}).
		Return(map[string]core.Chunk{
			"t00000000000000000000000000@local.example.com": {
				Epoch: pivotEpoch,
				Items: []core.TimelineItem{
					{
						ResourceID: "m00000000000000000000006302",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6302"),
					},
					{
						ResourceID: "m00000000000000000000006301",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6301"),
					},
					{
						ResourceID: "m00000000000000000000006300",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6300"),
					},
					{
						ResourceID: "m00000000000000000000006299",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6299"),
					},
					{
						ResourceID: "m00000000000000000000006298",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6298"),
					},
				},
			},
		}, nil)
	mockRepo.EXPECT().
		LookupChunkItrs(gomock.Any(), []string{"t00000000000000000000000000@local.example.com"}, prevEpoch).
		Return(map[string]string{"t00000000000000000000000000@local.example.com": prevEpoch}, nil)
	mockRepo.EXPECT().
		LoadChunkBodies(gomock.Any(), map[string]string{"t00000000000000000000000000@local.example.com": prevEpoch}).
		Return(nil, errors.New("not found"))
	mockRepo.EXPECT().ListRecentlyRemovedItems(gomock.Any(), gomock.Any()).Return(map[string][]string{}, nil)

	mockEntity := mock_core.NewMockEntityService(ctrl)
	mockDomain := mock_core.NewMockDomainService(ctrl)
	mockSemantic := mock_core.NewMockSemanticIDService(ctrl)
	mockSubscription := mock_core.NewMockSubscriptionService(ctrl)
	mockPolicy := mock_core.NewMockPolicyService(ctrl)

	service := NewService(
		mockRepo,
		mockEntity,
		mockDomain,
		mockSemantic,
		mockSubscription,
		mockPolicy,
		core.Config{
			FQDN: "local.example.com",
		},
	)

	ctx := context.Background()

	items, err := service.GetRecentItems(ctx, []string{"t00000000000000000000000000"}, pivotTime, 16)
	assert.NoError(t, err)

	assert.Len(t, items, 2)
}

func TestGetRecentItemsLoadMore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pivotEpoch := "6000"
	pivotTime := core.EpochTime("6300")
	prevEpoch := "5400"

	mockRepo := mock_timeline.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "t00000000000000000000000000").
		Return("t00000000000000000000000000@local.example.com", nil).AnyTimes()
	mockRepo.EXPECT().
		LookupChunkItrs(gomock.Any(), []string{"t00000000000000000000000000@local.example.com"}, pivotEpoch).
		Return(map[string]string{"t00000000000000000000000000@local.example.com": pivotEpoch}, nil)

	chunk6000 := []core.TimelineItem{}
	for i := 0; i < 16; i++ {
		epoch := 6308 - i
		chunk6000 = append(chunk6000, core.TimelineItem{
			ResourceID: fmt.Sprintf("m0000000000000000000000%04d", epoch),
			TimelineID: "t00000000000000000000000000",
			Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
			CDate:      core.EpochTime(strconv.Itoa(epoch)),
		})
	}

	chunk5400 := []core.TimelineItem{}
	for i := 0; i < 16; i++ {
		epoch := 5399 - i
		chunk5400 = append(chunk5400, core.TimelineItem{
			ResourceID: fmt.Sprintf("m0000000000000000000000%04d", epoch),
			TimelineID: "t00000000000000000000000000",
			Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
			CDate:      core.EpochTime(strconv.Itoa(epoch)),
		})
	}

	mockRepo.EXPECT().
		LoadChunkBodies(gomock.Any(), map[string]string{"t00000000000000000000000000@local.example.com": pivotEpoch}).
		Return(map[string]core.Chunk{
			"t00000000000000000000000000@local.example.com": {
				Epoch: pivotEpoch,
				Items: chunk6000,
			},
		}, nil)

	mockRepo.EXPECT().
		LookupChunkItrs(gomock.Any(), []string{"t00000000000000000000000000@local.example.com"}, prevEpoch).
		Return(map[string]string{"t00000000000000000000000000@local.example.com": prevEpoch}, nil)
	mockRepo.EXPECT().
		LoadChunkBodies(gomock.Any(), map[string]string{"t00000000000000000000000000@local.example.com": prevEpoch}).
		Return(map[string]core.Chunk{
			"t00000000000000000000000000@local.example.com": {
				Epoch: pivotEpoch,
				Items: chunk5400,
			},
		}, nil)
	mockRepo.EXPECT().ListRecentlyRemovedItems(gomock.Any(), gomock.Any()).Return(map[string][]string{}, nil)

	mockEntity := mock_core.NewMockEntityService(ctrl)
	mockDomain := mock_core.NewMockDomainService(ctrl)
	mockSemantic := mock_core.NewMockSemanticIDService(ctrl)
	mockSubscription := mock_core.NewMockSubscriptionService(ctrl)
	mockPolicy := mock_core.NewMockPolicyService(ctrl)

	service := NewService(
		mockRepo,
		mockEntity,
		mockDomain,
		mockSemantic,
		mockSubscription,
		mockPolicy,
		core.Config{
			FQDN: "local.example.com",
		},
	)

	ctx := context.Background()

	items, err := service.GetRecentItems(ctx, []string{"t00000000000000000000000000"}, pivotTime, 16)
	assert.NoError(t, err)

	assert.Len(t, items, 16)

	expected := []string{}
	for i := 0; i < 7; i++ {
		epoch := 6299 - i
		expected = append(expected, fmt.Sprintf("m0000000000000000000000%04d", epoch))
	}
	for i := 0; i < 9; i++ {
		epoch := 5399 - i
		expected = append(expected, fmt.Sprintf("m0000000000000000000000%04d", epoch))
	}

	for i, item := range items {
		assert.Equal(t, expected[i], item.ResourceID)
	}
}

func TestGetRecentItemsWide(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pivotEpoch := "6000"
	pivotTime := core.EpochTime("6300")

	mockRepo := mock_timeline.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "t00000000000000000000000000").
		Return("t00000000000000000000000000@local.example.com", nil).AnyTimes()
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "test@con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2").
		Return("t11111111111111111111111111@local.example.com", nil).AnyTimes()
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com").
		Return("taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com", nil).AnyTimes()
	mockRepo.EXPECT().
		GetNormalizationCache(gomock.Any(), "test@con1jmcread5dear85emug5gh3wvaf6st9av0kuxaj").
		Return("tbbbbbbbbbbbbbbbbbbbbbbbbbb@remote.example.com", nil).AnyTimes()

	mockRepo.EXPECT().
		LookupChunkItrs(gomock.Any(), []string{
			"t00000000000000000000000000@local.example.com",
			"t11111111111111111111111111@local.example.com",
			"taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com",
			"tbbbbbbbbbbbbbbbbbbbbbbbbbb@remote.example.com",
		}, pivotEpoch).
		Return(map[string]string{
			"t00000000000000000000000000@local.example.com":  pivotEpoch,
			"t11111111111111111111111111@local.example.com":  pivotEpoch,
			"taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com": pivotEpoch,
			"tbbbbbbbbbbbbbbbbbbbbbbbbbb@remote.example.com": pivotEpoch,
		}, nil)

	mockRepo.EXPECT().ListRecentlyRemovedItems(gomock.Any(), gomock.Any()).Return(map[string][]string{}, nil)

	mockRepo.EXPECT().
		LoadChunkBodies(gomock.Any(), map[string]string{
			"t00000000000000000000000000@local.example.com":  pivotEpoch,
			"t11111111111111111111111111@local.example.com":  pivotEpoch,
			"taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com": pivotEpoch,
			"tbbbbbbbbbbbbbbbbbbbbbbbbbb@remote.example.com": pivotEpoch,
		}).
		Return(map[string]core.Chunk{
			"t00000000000000000000000000@local.example.com": {
				Epoch: pivotEpoch,
				Items: []core.TimelineItem{
					{
						ResourceID: "m00000000000000000000000000",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6099"),
					},
					{
						ResourceID: "m00000000000000000000000001",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6098"),
					},
					{
						ResourceID: "m00000000000000000000000002",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6097"),
					},
					{
						ResourceID: "m00000000000000000000000003",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6096"),
					},
					{
						ResourceID: "m00000000000000000000000099",
						TimelineID: "t00000000000000000000000000",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6004"),
					},
				},
			},
			"t11111111111111111111111111@local.example.com": {
				Epoch: pivotEpoch,
				Items: []core.TimelineItem{
					{
						ResourceID: "m11111111111111111111111110",
						TimelineID: "t11111111111111111111111111",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6089"),
					},
					{
						ResourceID: "m11111111111111111111111111",
						TimelineID: "t11111111111111111111111111",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6088"),
					},
					{
						ResourceID: "m11111111111111111111111112",
						TimelineID: "t11111111111111111111111111",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6087"),
					},
					{
						ResourceID: "m11111111111111111111111113",
						TimelineID: "t11111111111111111111111111",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6086"),
					},
					{
						ResourceID: "m11111111111111111111111199",
						TimelineID: "t11111111111111111111111111",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6003"),
					},
				},
			},
			"taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com": {
				Epoch: pivotEpoch,
				Items: []core.TimelineItem{
					{
						ResourceID: "maaaaaaaaaaaaaaaaaaaaaaaaa0",
						TimelineID: "taaaaaaaaaaaaaaaaaaaaaaaaaa",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6079"),
					},
					{
						ResourceID: "maaaaaaaaaaaaaaaaaaaaaaaaa1",
						TimelineID: "taaaaaaaaaaaaaaaaaaaaaaaaaa",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6078"),
					},
					{
						ResourceID: "maaaaaaaaaaaaaaaaaaaaaaaaa2",
						TimelineID: "taaaaaaaaaaaaaaaaaaaaaaaaaa",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6077"),
					},
					{
						ResourceID: "maaaaaaaaaaaaaaaaaaaaaaaaa3",
						TimelineID: "taaaaaaaaaaaaaaaaaaaaaaaaaa",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6076"),
					},
					{
						ResourceID: "maaaaaaaaaaaaaaaaaaaaaaaa99",
						TimelineID: "taaaaaaaaaaaaaaaaaaaaaaaaaa",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6002"),
					},
				},
			},
			"tbbbbbbbbbbbbbbbbbbbbbbbbbb@remote.example.com": {
				Epoch: pivotEpoch,
				Items: []core.TimelineItem{
					{
						ResourceID: "mbbbbbbbbbbbbbbbbbbbbbbbbb0",
						TimelineID: "tbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6069"),
					},
					{
						ResourceID: "mbbbbbbbbbbbbbbbbbbbbbbbbb1",
						TimelineID: "tbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6068"),
					},
					{
						ResourceID: "mbbbbbbbbbbbbbbbbbbbbbbbbb2",
						TimelineID: "tbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6067"),
					},
					{
						ResourceID: "mbbbbbbbbbbbbbbbbbbbbbbbbb3",
						TimelineID: "tbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6066"),
					},
					{
						ResourceID: "mbbbbbbbbbbbbbbbbbbbbbbbb99",
						TimelineID: "tbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
						CDate:      core.EpochTime("6001"),
					},
				},
			},
		}, nil)

	mockEntity := mock_core.NewMockEntityService(ctrl)
	mockDomain := mock_core.NewMockDomainService(ctrl)
	mockSemantic := mock_core.NewMockSemanticIDService(ctrl)
	mockSubscription := mock_core.NewMockSubscriptionService(ctrl)
	mockPolicy := mock_core.NewMockPolicyService(ctrl)

	service := NewService(
		mockRepo,
		mockEntity,
		mockDomain,
		mockSemantic,
		mockSubscription,
		mockPolicy,
		core.Config{
			FQDN: "local.example.com",
		},
	)

	ctx := context.Background()

	items, err := service.GetRecentItems(
		ctx,
		[]string{
			"t00000000000000000000000000",
			"test@con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
			"taaaaaaaaaaaaaaaaaaaaaaaaaa@remote.example.com",
			"test@con1jmcread5dear85emug5gh3wvaf6st9av0kuxaj",
		},
		pivotTime,
		16,
	)
	assert.NoError(t, err)

	assert.Len(t, items, 16)

	expected := []string{
		"m00000000000000000000000000",
		"m00000000000000000000000001",
		"m00000000000000000000000002",
		"m00000000000000000000000003",
		"m11111111111111111111111110",
		"m11111111111111111111111111",
		"m11111111111111111111111112",
		"m11111111111111111111111113",
		"maaaaaaaaaaaaaaaaaaaaaaaaa0",
		"maaaaaaaaaaaaaaaaaaaaaaaaa1",
		"maaaaaaaaaaaaaaaaaaaaaaaaa2",
		"maaaaaaaaaaaaaaaaaaaaaaaaa3",
		"mbbbbbbbbbbbbbbbbbbbbbbbbb0",
		"mbbbbbbbbbbbbbbbbbbbbbbbbb1",
		"mbbbbbbbbbbbbbbbbbbbbbbbbb2",
		"mbbbbbbbbbbbbbbbbbbbbbbbbb3",
	}

	for i, item := range items {
		assert.Equal(t, expected[i], item.ResourceID)
	}
}
