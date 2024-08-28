package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/client/mock"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/core/mock"
	"github.com/totegamma/concurrent/internal/testutil"
	"go.uber.org/mock/gomock"
)

var ctx = context.Background()

func TestLookupLocalItrs(t *testing.T) {
	var cleanup_db func()
	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	var cleanup_rdb func()
	rdb, cleanup_rdb := testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc := testutil.CreateMC()
	defer cleanup_mc()

	pivotTime := time.Now()
	pivotEpoch := core.Time2Chunk(pivotTime)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSchema := mock_core.NewMockSchemaService(ctrl)
	mockSchema.EXPECT().UrlToID(gomock.Any(), gomock.Any()).Return(uint(0), nil).AnyTimes()
	mockSchema.EXPECT().IDToUrl(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	mockClient := mock_client.NewMockClient(ctrl)

	repo := repository{
		db:     db,
		rdb:    rdb,
		mc:     mc,
		client: mockClient,
		schema: mockSchema,
		config: core.Config{
			FQDN: "example.com",
		},
	}

	// Timelineを作成
	_, err := repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t00000000000000000000000000",
		Indexable: true,
		Author:    "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	_, err = repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t11111111111111111111111111",
		Indexable: true,
		Author:    "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	itemPivotTime := pivotTime.Add(-time.Minute * 0)
	itemPivotEpoch := core.Time2Chunk(itemPivotTime)

	// Itemを追加
	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "m00000000000000000000000000",
		TimelineID: "t00000000000000000000000000",
		Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		CDate:      itemPivotTime,
	})

	_, err = repo.CreateItem(ctx, core.TimelineItem{
		ResourceID: "m11111111111111111111111111",
		TimelineID: "t11111111111111111111111111",
		Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		CDate:      itemPivotTime,
	})

	// 取得
	itrs, err := repo.lookupLocalItrs(
		ctx,
		[]string{"t00000000000000000000000000@example.com", "t11111111111111111111111111@example.com"},
		pivotEpoch,
	)
	assert.NoError(t, err)
	assert.Len(t, itrs, 2)
	if assert.Contains(t, itrs, "t00000000000000000000000000@example.com") {
		assert.Equal(t, itemPivotEpoch, itrs["t00000000000000000000000000@example.com"])
	}
	if assert.Contains(t, itrs, "t11111111111111111111111111@example.com") {
		assert.Equal(t, itemPivotEpoch, itrs["t11111111111111111111111111@example.com"])
	}

	// ちゃんとキャッシュされているか確認
	mcKey1 := tlItrCachePrefix + "t00000000000000000000000000@example.com:" + pivotEpoch
	mcKey2 := tlItrCachePrefix + "t11111111111111111111111111@example.com:" + pivotEpoch
	mcVal1, err := mc.Get(mcKey1)
	if assert.NoError(t, err) {
		assert.Equal(t, itemPivotEpoch, string(mcVal1.Value))
	}
	mcVal2, err := mc.Get(mcKey2)
	if assert.NoError(t, err) {
		assert.Equal(t, itemPivotEpoch, string(mcVal2.Value))
	}
}

func TestLoadLocalBody(t *testing.T) {
	var cleanup_db func()
	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	var cleanup_rdb func()
	rdb, cleanup_rdb := testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc := testutil.CreateMC()
	defer cleanup_mc()

	pivotEpoch := core.Time2Chunk(time.Now())
	pivotTime := core.Chunk2RecentTime(pivotEpoch)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSchema := mock_core.NewMockSchemaService(ctrl)
	mockSchema.EXPECT().UrlToID(gomock.Any(), gomock.Any()).Return(uint(0), nil).AnyTimes()
	mockSchema.EXPECT().IDToUrl(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	mockClient := mock_client.NewMockClient(ctrl)

	repo := repository{
		db:     db,
		rdb:    rdb,
		mc:     mc,
		client: mockClient,
		schema: mockSchema,
		config: core.Config{
			FQDN: "example.com",
		},
	}

	// シナリオ1: 1チャンク内のアイテム数がdefaultChunkSizeより少ない場合
	// Timelineを作成
	_, err := repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t00000000000000000000000000",
		Indexable: true,
		Author:    "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	// Itemを追加
	for i := 0; i < 40; i++ {
		resourceID := fmt.Sprintf("m%026d", i)
		_, err = repo.CreateItem(ctx, core.TimelineItem{
			ResourceID: resourceID,
			TimelineID: "t00000000000000000000000000",
			Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
			CDate:      pivotTime.Add(-time.Minute * time.Duration(i)),
		})
	}

	// 取得
	chunk0, err := repo.loadLocalBody(
		ctx,
		"t00000000000000000000000000@example.com",
		pivotEpoch,
	)
	assert.NoError(t, err)
	assert.Equal(t, chunk0.Epoch, pivotEpoch)
	assert.Len(t, chunk0.Items, 32) // defaultChunkSizeの数入っているはず

	// ちゃんとキャッシュされているか確認
	mcKey0 := tlBodyCachePrefix + "t00000000000000000000000000@example.com:" + pivotEpoch
	mcVal0, err := mc.Get(mcKey0)
	if assert.NoError(t, err) {

		cacheStr := string(mcVal0.Value)
		cacheStr = cacheStr[:len(cacheStr)-1]
		cacheStr = "[" + cacheStr + "]"

		var items []core.TimelineItem
		err = json.Unmarshal([]byte(cacheStr), &items)
		if assert.NoError(t, err) {
			assert.Len(t, items, 32)
			assert.Equal(t, "m00000000000000000000000031", items[0].ResourceID) // 逆順になっているので最後のリソースIDが最初になる
		}
	}

	// シナリオ2: 1チャンク内のアイテム数がdefaultChunkSizeより多い場合
	// Timelineを作成
	_, err = repo.UpsertTimeline(ctx, core.Timeline{
		ID:        "t11111111111111111111111111",
		Indexable: true,
		Author:    "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
		Schema:    "https://example.com/testschema.json",
		Document:  "{}",
	})
	assert.NoError(t, err)

	// Itemを追加
	for i := 0; i < 40; i++ {
		resourceID := fmt.Sprintf("m%026d", i)
		_, err = repo.CreateItem(ctx, core.TimelineItem{
			ResourceID: resourceID,
			TimelineID: "t11111111111111111111111111",
			Owner:      "con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2",
			CDate:      pivotTime.Add(-time.Minute * time.Duration(i) / 10), //ツメツメで入れる
		})
	}

	// 取得
	chunk1, err := repo.loadLocalBody(
		ctx,
		"t11111111111111111111111111@example.com",
		pivotEpoch,
	)
	assert.NoError(t, err)
	assert.Equal(t, chunk1.Epoch, pivotEpoch)
	assert.Len(t, chunk1.Items, 40) // 全アイテムが入っているはず

	// ちゃんとキャッシュされているか確認
	mcKey1 := tlBodyCachePrefix + "t11111111111111111111111111@example.com:" + pivotEpoch
	mcVal1, err := mc.Get(mcKey1)
	if assert.NoError(t, err) {

		cacheStr := string(mcVal1.Value)
		cacheStr = cacheStr[:len(cacheStr)-1]
		cacheStr = "[" + cacheStr + "]"

		var items []core.TimelineItem
		err = json.Unmarshal([]byte(cacheStr), &items)
		if assert.NoError(t, err) {
			assert.Len(t, items, 40)
			assert.Equal(t, "m00000000000000000000000039", items[0].ResourceID) // 逆順になっているので最後のリソースIDが最初になる
		}
	}

}
