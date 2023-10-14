package stream

import (
	"log"
	"time"
	"testing"
	"context"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
)


var ctx = context.Background()
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

	mc := testutil.ConnectMemcached(mc_resource, mc_pool)

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
	if err != nil {
		t.Errorf("CreateStream failed: %s", err)
	}

	if created.ID != stream.ID {
		t.Errorf("CreateStream failed: ID is not matched")
	}

	if created.Visible != stream.Visible {
		t.Errorf("CreateStream failed: Visible is not matched")
	}

	if created.Author != stream.Author {
		t.Errorf("CreateStream failed: Author is not matched")
	}

	if created.Maintainer[0] != stream.Maintainer[0] {
		t.Errorf("CreateStream failed: Maintainer is not matched")
	}

	if created.Writer[0] != stream.Writer[0] {
		t.Errorf("CreateStream failed: Writer is not matched")
	}

	if created.Reader[0] != stream.Reader[0] {
		t.Errorf("CreateStream failed: Reader is not matched")
	}

	if created.Schema != stream.Schema {
		t.Errorf("CreateStream failed: Schema is not matched")
	}

	if created.Payload != stream.Payload {
		t.Errorf("CreateStream failed: Payload is not matched")
	}

	if created.CDate.IsZero() {
		t.Errorf("CreateStream failed: CreatedAt is not set")
	}

	if created.MDate.IsZero() {
		t.Errorf("CreateStream failed: UpdatedAt is not set")
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
	if err != nil {
		t.Errorf("CreateItem failed: %s", err)
	}

	if createdItem.Type != item.Type {
		t.Errorf("CreateItem failed: Type is not matched")
	}

	if createdItem.ObjectID != item.ObjectID {
		t.Errorf("CreateItem failed: ObjectID is not matched")
	}

	if createdItem.StreamID != item.StreamID {
		t.Errorf("CreateItem failed: StreamID is not matched")
	}

	if createdItem.Owner != item.Owner {
		t.Errorf("CreateItem failed: Owner is not matched")
	}

	if createdItem.CDate.IsZero() {
		t.Errorf("CreateItem failed: CreatedAt is not set")
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
	if err != nil {
		t.Errorf("CreateItem1 failed: %s", err)
	}
	
	// trial1: cache miss test
	result, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if err != nil {
		t.Errorf("GetChunkIterators failed: %s", err)
	}

	if (len(result) != 1) {
		t.Errorf("GetChunkIterators failed: length is not matched. expected: 1, actual: %d", len(result))
	}

	itemKey := "stream:body:all:00000000000000000000:" + Time2Chunk(createdItem.CDate)

	if (result["00000000000000000000"] != itemKey) {
		t.Errorf("GetChunkIterators failed: chunk is not matched expected: %s, actual: %s", itemKey, result["00000000000000000000"])
	}

	// trial2: cache hit test
	result2, err := repo.GetChunkIterators(ctx, []string{"00000000000000000000"}, pivotChunk)
	if err != nil {
		t.Errorf("GetChunkIterators failed: %s", err)
	}

	if (len(result2) != 1) {
		t.Errorf("GetChunkIterators failed: length is not matched")
	}

	if (result2["00000000000000000000"] != itemKey) {
		t.Errorf("GetChunkIterators failed: chunk is not matched expected: %s, actual: %s", itemKey, result2["00000000000000000000"])
		t.Error(result["00000000000000000000"])
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
	if err != nil {
		t.Errorf("CreateStream failed: %s", err)
	}

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "50797d45-23d2-471e-9e48-b4b8a6cdc840",
		StreamID: "11111111111111111111",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 0),
	})
	if err != nil {
		t.Errorf("CreateItem1 failed: %s", err)
	}

	_, err = repo.CreateItem(ctx, core.StreamItem {
		Type: "message",
		ObjectID: "9aad0952-7a50-419c-96c1-565a1da95c47",
		StreamID: "11111111111111111111",
		Owner: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		CDate: pivot.Add(-time.Minute * 10),
	})
	if err != nil {
		t.Errorf("CreateItem1 failed: %s", err)
	}

	// :: GetMultiChunkのテスト ::
	chunks, err := repo.GetMultiChunk(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	if err != nil {
		t.Errorf("GetMultiChunk failed: %s", err)
	}

	if (len(chunks) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks))
		t.Error(chunks)
	}

	if (len(chunks["00000000000000000000"]) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks["00000000000000000000"]))
		t.Error(chunks["00000000000000000000"])
	}

	if (len(chunks["11111111111111111111"]) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks["11111111111111111111"]))
		t.Error(chunks["11111111111111111111"])
	}

	// :: GetChunkのテスト with cache ::
	chunks2, err := repo.GetMultiChunk(ctx, []string{"00000000000000000000", "11111111111111111111"}, pivotChunk)
	if err != nil {
		t.Errorf("GetMultiChunk failed: %s", err)
	}

	if (len(chunks2) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks2))
		t.Error(chunks)
	}

	if (len(chunks2["00000000000000000000"]) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks2["00000000000000000000"]))
		t.Error(chunks2["00000000000000000000"])
	}

	if (len(chunks2["11111111111111111111"]) != 2) {
		t.Errorf("GetMultiChunk failed: length is not matched. expected: 2, actual: %d", len(chunks2["11111111111111111111"]))
		t.Error(chunks2["11111111111111111111"])
	}

}
