package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/schemas"
)

func TestSubscriberCacheChunklineEventPrependsCachedChunk(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	timeline := "cckv://remote.example/concrnt.world/profiles/main/home-timeline"
	source := timeline + "/reference"
	target := "cckv://remote.example/concrnt.world/posts/post"
	chunkID := int64(42)
	createdAt := time.Unix(chunkID*600+10, 0).UTC()

	existing := chunkline.BodyItem{
		Timestamp:   time.Unix(chunkID*600+1, 0).UTC(),
		Href:        "cckv://remote.example/concrnt.world/posts/old",
		ContentType: "application/concrnt.document+json",
	}
	bodyCache, err := chunkline.EncodeBodyCache([]chunkline.BodyItem{existing})
	require.NoError(t, err)
	require.NoError(t, mc.Set(&memcache.Item{Key: chunkline.IteratorCacheKey(timeline, chunkID), Value: []byte("41")}))
	require.NoError(t, mc.Set(&memcache.Item{Key: chunkline.BodyCacheKey(timeline, chunkID), Value: bodyCache}))

	refDoc, err := json.Marshal(concrnt.Document[schemas.Reference]{
		Key: source,
		Value: schemas.Reference{
			Href: target,
		},
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	targetDoc, err := json.Marshal(concrnt.Document[any]{
		Key:       target,
		Value:     map[string]any{},
		CreatedAt: createdAt,
	})
	require.NoError(t, err)

	subscriber := &Subscriber{Memcache: mc}
	subscriber.cacheChunklineEvent(ctx, []string{timeline}, concrnt.Event{
		Type:   "created",
		Source: source,
		URI:    source,
		References: map[string]concrnt.SignedDocument{
			source: {Document: string(refDoc)},
			target: {Document: string(targetDoc)},
		},
	})

	cachedItr, err := mc.Get(chunkline.IteratorCacheKey(timeline, chunkID))
	require.NoError(t, err)
	require.Equal(t, "42", string(cachedItr.Value))

	cachedBody, err := mc.Get(chunkline.BodyCacheKey(timeline, chunkID))
	require.NoError(t, err)
	items, err := chunkline.DecodeBodyCache(cachedBody.Value)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, target, items[0].Href)
	require.Equal(t, createdAt, items[0].Timestamp)
	require.Equal(t, existing, items[1])
}
