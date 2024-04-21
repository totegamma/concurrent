package timeline

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/client/mock"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/schema/mock"
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

	mockSchema := mock_schema.NewMockService(ctrl)
	mockSchema.EXPECT().UrlToID(gomock.Any(), gomock.Any()).Return(uint(0), nil).AnyTimes()
	mockSchema.EXPECT().IDToUrl(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	mockClient := mock_client.NewMockClient(ctrl)

	repo = NewRepository(db, rdb, mc, mockClient, mockSchema, mockManager, util.Config{})

	// :: Timelineを作成 ::
	timeline := core.Timeline{
		ID:        "t00000000000000000000000000",
		Indexable: true,
		Author:    "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	}

	created, err := repo.UpsertTimeline(ctx, timeline)
	if assert.NoError(t, err) {
		assert.Equal(t, created.ID, timeline.ID)
		assert.Equal(t, created.Indexable, timeline.Indexable)
		assert.Equal(t, created.Author, timeline.Author)
		assert.Equal(t, created.Schema, timeline.Schema)
		assert.Equal(t, created.Document, timeline.Document)
		assert.NotZero(t, created.CDate)
		assert.NotZero(t, created.MDate)
	}

	// :: Itemを作成 ::
	item := core.TimelineItem{
		ResourceID: "mRGZKRZ5YTMTNDE9J0676P1TQAW",
		TimelineID: "t00000000000000000000000000",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 0),
	}

	createdItem, err := repo.CreateItem(ctx, item)
	if assert.NoError(t, err) {
		assert.Equal(t, createdItem.ResourceID, item.ResourceID)
		assert.Equal(t, createdItem.TimelineID, item.TimelineID)
		assert.Equal(t, createdItem.Owner, item.Owner)
		assert.NotZero(t, createdItem.CDate)
	}

	// :: ChunkIteratorが取得できることを確認 ::
	pivotChunk := core.Time2Chunk(pivot)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "mRV75ZS5R588QDNQ00676P1X440",
		TimelineID: "t00000000000000000000000000",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	// trial1: cache miss test
	result, err := repo.GetChunkIterators(ctx, []string{"t00000000000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result, 1)
	}

	itemKey := "timeline:body:all:t00000000000000000000000000:" + core.Time2Chunk(createdItem.CDate)
	assert.Equal(t, result["t00000000000000000000000000"], itemKey)

	// trial2: cache hit test
	result2, err := repo.GetChunkIterators(ctx, []string{"t00000000000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result2, 1)
		assert.Equal(t, result2["t00000000000000000000000000"], itemKey)
	}

	// :: Timeline1を作成してItemを追加 ::
	_, err = repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t11111111111111111111111111",
		Indexable: true,
		Author:    "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "m5JY6724DKGDBCMP60676P2055M",
		TimelineID: "t11111111111111111111111111",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 0),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "m5KV37HA63HVE7KNP0676P228RM",
		TimelineID: "t11111111111111111111111111",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	// Insertによるキャッシュ更新を一旦クリア
	mc.DeleteAll()

	// GetChunksFromCacheでキャッシュがないはずなので何も帰ってこないことを確認
	chunks, err := repo.GetChunksFromCache(ctx, []string{"t00000000000000000000000000", "t11111111111111111111111111"}, pivotChunk)
	assert.NoError(t, err)
	assert.Len(t, chunks, 0)

	// GetChunksFromDBで要素を取得する
	chunks, err = repo.GetChunksFromDB(ctx, []string{"t00000000000000000000000000", "t11111111111111111111111111"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 2)
		assert.Len(t, chunks["t00000000000000000000000000"].Items, 2)
		assert.Len(t, chunks["t11111111111111111111111111"].Items, 2)
	}

	// GetChunksFromCacheでキャッシュがあるはずなのでキャッシュから取得する
	chunks, err = repo.GetChunksFromCache(ctx, []string{"t00000000000000000000000000", "t11111111111111111111111111"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 2)
		assert.Len(t, chunks["t00000000000000000000000000"].Items, 2)
		assert.Len(t, chunks["t11111111111111111111111111"].Items, 2)
	}

	// TimelineItemの順番のテスト

	_, err = repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t22222222222222222222222222",
		Indexable: true,
		Author:    "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "mA1HJCH9NK9MPMV7D0676P25PSR",
		TimelineID: "t22222222222222222222222222",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "mW4H1PZZ223D1B6ED0676P27J50",
		TimelineID: "t22222222222222222222222222",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 5),
	})
	assert.NoError(t, err)

	mc.DeleteAll()

	chunks, err = repo.GetChunksFromDB(ctx, []string{"t22222222222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["t22222222222222222222222222"].Items, 2)
		assert.Equal(t, "mW4H1PZZ223D1B6ED0676P27J50", chunks["t22222222222222222222222222"].Items[0].ResourceID)
		assert.Equal(t, "mA1HJCH9NK9MPMV7D0676P25PSR", chunks["t22222222222222222222222222"].Items[1].ResourceID)
	}

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "mT46G7BT5TJQQS4WY0676P2A9ZM",
		TimelineID: "t22222222222222222222222222",
		Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		CDate:      pivot.Add(-time.Minute * 1),
	})
	assert.NoError(t, err)

	chunks, err = repo.GetChunksFromDB(ctx, []string{"t22222222222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["t22222222222222222222222222"].Items, 3)
		assert.Equal(t, "mT46G7BT5TJQQS4WY0676P2A9ZM", chunks["t22222222222222222222222222"].Items[0].ResourceID)
		assert.Equal(t, "mW4H1PZZ223D1B6ED0676P27J50", chunks["t22222222222222222222222222"].Items[1].ResourceID)
		assert.Equal(t, "mA1HJCH9NK9MPMV7D0676P25PSR", chunks["t22222222222222222222222222"].Items[2].ResourceID)
	}

	remoteKey0 := "timeline:body:all:t00000000000000000000000000@remote.com:" + core.Time2Chunk(pivot.Add(-time.Minute*10))
	remoteKey1 := "timeline:body:all:t11111111111111111111111111@remote.com:" + core.Time2Chunk(pivot.Add(-time.Minute*30))

	// test SaveToCache
	testchunks := make(map[string]core.Chunk)
	testchunks["t00000000000000000000000000@remote.com"] = core.Chunk{
		Key: remoteKey0,
		Items: []core.TimelineItem{
			{
				ResourceID: "mDMZMRRS7N16E1PDN0676P2QH6C",
				TimelineID: "t00000000000000000000000000@remote.com",
				Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
				CDate:      pivot.Add(-time.Minute * 10),
			},
		},
	}
	testJson0, err := json.Marshal(testchunks["t00000000000000000000000000@remote.com"].Items[0])
	testJson0 = append(testJson0, ',')
	testchunks["t11111111111111111111111111@remote.com"] = core.Chunk{
		Key: remoteKey1,
		Items: []core.TimelineItem{
			{
				ResourceID: "mD895NMA837R0C6B90676P2S1J4",
				TimelineID: "t11111111111111111111111111@remote.com",
				Owner:      "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
				CDate:      pivot.Add(-time.Minute * 30),
			},
		},
	}
	testJson1, err := json.Marshal(testchunks["t11111111111111111111111111@remote.com"].Items[0])
	testJson1 = append(testJson1, ',')

	err = repo.SaveToCache(ctx, testchunks, pivot)
	if assert.NoError(t, err) {
		itrkey0 := "timeline:itr:all:t00000000000000000000000000@remote.com:" + pivotChunk
		remoteCache0, err := mc.Get(itrkey0)
		if assert.NoError(t, err) {
			assert.Equal(t, remoteKey0, string(remoteCache0.Value))
		}

		itrKey1 := "timeline:itr:all:t11111111111111111111111111@remote.com:" + pivotChunk
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
