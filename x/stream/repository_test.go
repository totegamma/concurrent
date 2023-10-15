package stream

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
)


var ctx = context.Background()
var mc *memcache.Client
var repo Repository
var pivot time.Time

func TestMain(m *testing.M) {
	log.Println("Test Start")
	db_resource, db_pool := testutil.CreateDBContainer()
	defer testutil.CloseContainer(db_resource, db_pool)

	db := testutil.ConnectDB(db_resource, db_pool)

	testutil.SetupDB(db)

	mc_resource, mc_pool := testutil.CreateMemcachedContainer()
	defer testutil.CloseContainer(mc_resource, mc_pool)

	mc = testutil.ConnectMemcached(mc_resource, mc_pool)

	repo = NewRepository(db, mc)

	pivot = time.Now()

	m.Run()

	log.Println("Test End")
}

func TestRepository(t *testing.T) {

	// :: Streamを作成 ::
	stream := core.Stream{
		ID: "00000000000000000000",
		Visible: true,
		Author: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema: "https://example.com/testschema.json",
		Payload: "{}",
	}

	created, err := repo.CreateStream(ctx, stream)
	if assert.NoError(t, err) {
		assert.Equal(t, created.ID, stream.ID)
		assert.Equal(t, created.Visible, stream.Visible)
		assert.Equal(t, created.Author, stream.Author)
		assert.Contains(t, created.Maintainer, stream.Author)
		assert.Contains(t, created.Writer, stream.Author)
		assert.Contains(t, created.Reader, stream.Author)
		assert.Equal(t, created.Schema, stream.Schema)
		assert.Equal(t, created.Payload, stream.Payload)
		assert.NotZero(t, created.CDate)
		assert.NotZero(t, created.MDate)
	}

	// :: Itemを作成 ::
	item := core.StreamItem {
		Type: "message",
		ObjectID: "af7bcaa8-820a-4ce2-ab17-1b3f6bf14d9b",
		StreamID: "00000000000000000000",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 0),
	}

	createdItem, err := repo.CreateItem(ctx, item)
	if assert.NoError(t, err) {
		assert.Equal(t, createdItem.Type, item.Type)
		assert.Equal(t, createdItem.ObjectID, item.ObjectID)
		assert.Equal(t, createdItem.StreamID, item.StreamID)
		assert.Equal(t, createdItem.Owner, item.Owner)
		assert.NotZero(t, createdItem.CDate)
	}

	// :: ChunkIteratorが取得できることを確認 ::
	pivotChunk := Time2Chunk(pivot)

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "3c850e58-efca-4656-bbe4-2e5642dbbbe8",
		StreamID: "00000000000000000000",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)
	
	// trial1: cache miss test
	result, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result, 1)
	}

	itemKey := "stream:body:all:00000000000000000000:" + Time2Chunk(createdItem.CDate)
	assert.Equal(t, result["00000000000000000000"], itemKey)

	// trial2: cache hit test
	result2, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, result2, 1)
		assert.Equal(t, result2["00000000000000000000"], itemKey)
	}

	// :: Stream1を作成してItemを追加 ::
	_, err = repo.CreateStream(ctx, core.Stream {
		ID: "11111111111111111111",
		Visible: true,
		Author: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema: "https://example.com/testschema.json",
		Payload: "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "50797d45-23d2-471e-9e48-b4b8a6cdc840",
		StreamID: "11111111111111111111",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 0),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "9aad0952-7a50-419c-96c1-565a1da95c47",
		StreamID: "11111111111111111111",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 10),
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
		assert.Len(t, chunks["00000000000000000000"], 2)
		assert.Len(t, chunks["11111111111111111111"], 2)
	}

	// GetChunksFromCacheでキャッシュがあるはずなのでキャッシュから取得する
	chunks, err = repo.GetChunksFromCache(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 2)
		assert.Len(t, chunks["00000000000000000000"], 2)
		assert.Len(t, chunks["11111111111111111111"], 2)
	}

	// StreamItemの順番のテスト

	_, err = repo.CreateStream(ctx, core.Stream {
		ID: "22222222222222222222",
		Visible: true,
		Author: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema: "https://example.com/testschema.json",
		Payload: "{}",
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "d6087868-c30b-439d-9c2c-646fdd48ecc4",
		StreamID: "22222222222222222222",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 10),
	})
	assert.NoError(t, err)

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "797e1f95-542e-485b-8051-a87c1ad1fe06",
		StreamID: "22222222222222222222",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 5),
	})
	assert.NoError(t, err)

	mc.DeleteAll()

	chunks, err = repo.GetChunksFromDB(ctx, []string{"22222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["22222222222222222222"], 2)
		assert.Equal(t, "797e1f95-542e-485b-8051-a87c1ad1fe06", chunks["22222222222222222222"][0].ObjectID)
		assert.Equal(t, "d6087868-c30b-439d-9c2c-646fdd48ecc4", chunks["22222222222222222222"][1].ObjectID)
	}


	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "01eb39b4-0a5b-4461-a091-df9a97c7b2fd",
		StreamID: "22222222222222222222",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 1),
	})
	assert.NoError(t, err)

	chunks, err = repo.GetChunksFromDB(ctx, []string{"22222222222222222222"}, pivotChunk)
	if assert.NoError(t, err) {
		assert.Len(t, chunks, 1)
		assert.Len(t, chunks["22222222222222222222"], 3)
		assert.Equal(t, "01eb39b4-0a5b-4461-a091-df9a97c7b2fd", chunks["22222222222222222222"][0].ObjectID)
		assert.Equal(t, "797e1f95-542e-485b-8051-a87c1ad1fe06", chunks["22222222222222222222"][1].ObjectID)
		assert.Equal(t, "d6087868-c30b-439d-9c2c-646fdd48ecc4", chunks["22222222222222222222"][2].ObjectID)
	}
}
