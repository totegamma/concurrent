package timeline

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/socket/mock"
	"github.com/totegamma/concurrent/x/util"
	"go.uber.org/mock/gomock"
)

var ctx = context.Background()
var mc *memcache.Client
var repo Repository
var pivot time.Time

func TestRepository(t *testing.T) {

	log.Println("Test Start")

	var cleanup_db func()
	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	var cleanup_rdb func()
	rdb, cleanup_rdb := testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc = testutil.CreateMC()
	defer cleanup_mc()

	pivot = time.Now()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockManager := mock_socket.NewMockManager(ctrl)
	mockManager.EXPECT().GetAllRemoteSubs().Return([]string{}).AnyTimes()

	repo = NewRepository(db, rdb, mc, mockManager, util.Config{})

	// :: Timelineを作成 ::
	timeline := core.Timeline{
		ID:         "00000000000000000000",
		Visible:    true,
		Author:     "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema:     "https://example.com/testschema.json",
		Payload:    "{}",
	}

	created, err := repo.CreateTimeline(ctx, timeline)
	if assert.NoError(t, err) {
		assert.Equal(t, created.ID, timeline.ID)
		assert.Equal(t, created.Visible, timeline.Visible)
		assert.Equal(t, created.Author, timeline.Author)
		assert.Contains(t, created.Maintainer, timeline.Author)
		assert.Contains(t, created.Writer, timeline.Author)
		assert.Contains(t, created.Reader, timeline.Author)
		assert.Equal(t, created.Schema, timeline.Schema)
		assert.Equal(t, created.Payload, timeline.Payload)
		assert.NotZero(t, created.CDate)
		assert.NotZero(t, created.MDate)
	}

	// :: Itemを作成 ::
	item := core.TimelineItem{
		Type:     "message",
		ObjectID: "af7bcaa8-820a-4ce2-ab17-1b3f6bf14d9b",
		TimelineID: "00000000000000000000",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 0),
	}

	createdItem, err := repo.CreateItem(ctx, item)
	if assert.NoError(t, err) {
		assert.Equal(t, createdItem.Type, item.Type)
		assert.Equal(t, createdItem.ObjectID, item.ObjectID)
		assert.Equal(t, createdItem.TimelineID, item.TimelineID)
		assert.Equal(t, createdItem.Owner, item.Owner)
		assert.NotZero(t, createdItem.CDate)
	}

	// :: ChunkIteratorが取得できることを確認 ::
	pivotChunk := core.Time2Chunk(pivot)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "3c850e58-efca-4656-bbe4-2e5642dbbbe8",
		TimelineID: "00000000000000000000",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	// trial1: cache miss test
	result, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result, 1)
	}

	itemKey := "timeline:body:all:00000000000000000000:" + core.Time2Chunk(createdItem.CDate)
	assert.Equal(t, result["00000000000000000000"], itemKey)

	// trial2: cache hit test
	result2, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result2, 1)
		assert.Equal(t, result2["00000000000000000000"], itemKey)
	}

	// :: Timeline1を作成してItemを追加 ::
	_, err = repo.CreateTimeline(ctx, core.Timeline{
		ID:         "11111111111111111111",
		Visible:    true,
		Author:     "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema:     "https://example.com/testschema.json",
		Payload:    "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "50797d45-23d2-471e-9e48-b4b8a6cdc840",
		TimelineID: "11111111111111111111",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 0),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "9aad0952-7a50-419c-96c1-565a1da95c47",
		TimelineID: "11111111111111111111",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	// Insertによるキャッシュ更新を一旦クリア
	mc.DeleteAll()

	// GetChunksFromCacheでキャッシュがないはずなので何も帰ってこないことを確認
	chunks, err := repo.GetChunksFromCache(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	assert.NoError(t, err)
	assert.Len(t, chunks, 0)

	// GetChunksFromDBで要素を取得する
	chunks, err = repo.GetChunksFromDB(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 2)
		assert.Len(t, chunks["00000000000000000000"].Items, 2)
		assert.Len(t, chunks["11111111111111111111"].Items, 2)
	}

	// GetChunksFromCacheでキャッシュがあるはずなのでキャッシュから取得する
	chunks, err = repo.GetChunksFromCache(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 2)
		assert.Len(t, chunks["00000000000000000000"].Items, 2)
		assert.Len(t, chunks["11111111111111111111"].Items, 2)
	}

	// TimelineItemの順番のテスト

	_, err = repo.CreateTimeline(ctx, core.Timeline{
		ID:         "22222222222222222222",
		Visible:    true,
		Author:     "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader:     []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema:     "https://example.com/testschema.json",
		Payload:    "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "d6087868-c30b-439d-9c2c-646fdd48ecc4",
		TimelineID: "22222222222222222222",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "797e1f95-542e-485b-8051-a87c1ad1fe06",
		TimelineID: "22222222222222222222",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 5),
	})
	assert.NoError(t, err)

	mc.DeleteAll()

	chunks, err = repo.GetChunksFromDB(ctx, []string{"22222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["22222222222222222222"].Items, 2)
		assert.Equal(t, "797e1f95-542e-485b-8051-a87c1ad1fe06", chunks["22222222222222222222"].Items[0].ObjectID)
		assert.Equal(t, "d6087868-c30b-439d-9c2c-646fdd48ecc4", chunks["22222222222222222222"].Items[1].ObjectID)
	}

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		Type:     "message",
		ObjectID: "01eb39b4-0a5b-4461-a091-df9a97c7b2fd",
		TimelineID: "22222222222222222222",
		Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate:    pivot.Add(-time.Minute * 1),
	})
	assert.NoError(t, err)

	chunks, err = repo.GetChunksFromDB(ctx, []string{"22222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["22222222222222222222"].Items, 3)
		assert.Equal(t, "01eb39b4-0a5b-4461-a091-df9a97c7b2fd", chunks["22222222222222222222"].Items[0].ObjectID)
		assert.Equal(t, "797e1f95-542e-485b-8051-a87c1ad1fe06", chunks["22222222222222222222"].Items[1].ObjectID)
		assert.Equal(t, "d6087868-c30b-439d-9c2c-646fdd48ecc4", chunks["22222222222222222222"].Items[2].ObjectID)
	}

	remoteKey0 := "timeline:body:all:00000000000000000000@remote.com:" + core.Time2Chunk(pivot.Add(-time.Minute*10))
	remoteKey1 := "timeline:body:all:11111111111111111111@remote.com:" + core.Time2Chunk(pivot.Add(-time.Minute*30))

	// test SaveToCache
	testchunks := make(map[string]Chunk)
	testchunks["00000000000000000000@remote.com"] = Chunk{
		Key: remoteKey0,
		Items: []core.TimelineItem{
			{
				Type:     "message",
				ObjectID: "00000000000000000000",
				TimelineID: "00000000000000000000@remote.com",
				Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
				CDate:    pivot.Add(-time.Minute * 10),
			},
		},
	}
	testJson0, err := json.Marshal(testchunks["00000000000000000000@remote.com"].Items[0])
	testJson0 = append(testJson0, ',')
	testchunks["11111111111111111111@remote.com"] = Chunk{
		Key: remoteKey1,
		Items: []core.TimelineItem{
			{
				Type:     "message",
				ObjectID: "22222222222222222222",
				TimelineID: "11111111111111111111@remote.com",
				Owner:    "CC62b953CCCE898b955f256976d61BdEE04353C042",
				CDate:    pivot.Add(-time.Minute * 30),
			},
		},
	}
	testJson1, err := json.Marshal(testchunks["11111111111111111111@remote.com"].Items[0])
	testJson1 = append(testJson1, ',')

	err = repo.SaveToCache(ctx, testchunks, pivot)
	if assert.NoError(t, err) {
		itrkey0 := "timeline:itr:all:00000000000000000000@remote.com:" + pivotChunk
		remoteCache0, err := mc.Get(itrkey0)
		if assert.NoError(t, err) {
			assert.Equal(t, remoteKey0, string(remoteCache0.Value))
		}

		itrKey1 := "timeline:itr:all:11111111111111111111@remote.com:" + pivotChunk
		remoteCache1, err := mc.Get(itrKey1)
		if assert.NoError(t, err) {
			assert.Equal(t, remoteKey1, string(remoteCache1.Value))
		}

		remoteCache0, err = mc.Get(remoteKey0)
		if assert.NoError(t, err) {
			assert.Equal(t, string(testJson0), string(remoteCache0.Value))
		}

		remoteCache1, err = mc.Get(remoteKey1)
		if assert.NoError(t, err) {
			assert.Equal(t, string(testJson1), string(remoteCache1.Value))
		}
	}
}
