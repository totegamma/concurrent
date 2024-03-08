package stream

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain/mock"
	"github.com/totegamma/concurrent/x/entity/mock"
	"github.com/totegamma/concurrent/x/socket/mock"
	"github.com/totegamma/concurrent/x/util"
	"go.uber.org/mock/gomock"
)

func TestService(t *testing.T) {

	log.Println("Test Start")

	ctx := context.Background()

	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	rdb, cleanup_rdb := testutil.CreateRDB()
	defer cleanup_rdb()

	mc, cleanup_mc := testutil.CreateMC()
	defer cleanup_mc()

	pivot := time.Now()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := mock_socket.NewMockManager(ctrl)
	mockManager.EXPECT().GetAllRemoteSubs().Return([]string{}).AnyTimes()

	mockDomain := mock_domain.NewMockService(ctrl)
	mockEntity := mock_entity.NewMockService(ctrl)

	repo := NewRepository(db, rdb, mc, mockManager, util.Config{})

	config := util.Config{
		Concurrent: util.Concurrent{
			FQDN: "example.com",
		},
	}

	service := NewService(repo, mockEntity, mockDomain, config)

	created, err := service.CreateStream(ctx, core.Stream{
		Visible:    true,
		Author:     "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema:     "https://example.com/testschema.json",
		Payload:    "{}",
	})

	if assert.NoError(t, err) {
		assert.NotNil(t, created)
	}

	const schema1 = "https://schema.concurrent.world/message1.json"
	const schema2 = "https://schema.concurrent.world/message2.json"

	var body interface{}

	streamID := created.ID

	err = service.PostItem(ctx, streamID, core.StreamItem{
		Type:     "message",
		ObjectID: "af7bcaa8-820a-4ce2-ab17-1b3f6bf14d9b",
		Schema:   schema1,
		StreamID: streamID,
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Author:   "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 0),
	}, body)

	assert.NoError(t, err)

	err = service.PostItem(ctx, streamID, core.StreamItem{
		Type:     "message",
		ObjectID: "dd9b1f04-fb48-4f7e-a799-cb77cf557375",
		Schema:   schema2,
		StreamID: streamID,
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Author:   "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 0),
	}, body)

	assert.NoError(t, err)

	items, err := service.GetRecentItems(ctx, []string{streamID}, "", pivot, 16)
	if assert.NoError(t, err) {
		assert.Equal(t, 2, len(items))
	}

	items1, err := service.GetRecentItems(ctx, []string{streamID}, schema1, pivot, 16)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(items1))
		assert.Equal(t, "af7bcaa8-820a-4ce2-ab17-1b3f6bf14d9b", items1[0].ObjectID)
	}

	items2, err := service.GetRecentItems(ctx, []string{streamID}, schema2, pivot, 16)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(items2))
		assert.Equal(t, "dd9b1f04-fb48-4f7e-a799-cb77cf557375", items2[0].ObjectID)
	}

}
