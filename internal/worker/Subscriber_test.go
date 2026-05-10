package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/schemas"
)

func TestSubscriberCacheChunklineEventPrependsCachedChunk(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	const chunkSize = int64(300)
	chunkID := int64(42)
	createdAt := time.Unix(chunkID*chunkSize+10, 0).UTC()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/timeline", r.URL.Path)
		require.Equal(t, "application/chunkline+json", r.Header.Get("Accept"))
		require.NoError(t, json.NewEncoder(w).Encode(chunkline.Manifest{
			Version:   "1.0",
			ChunkSize: chunkSize,
		}))
	}))
	defer server.Close()

	timeline := server.URL + "/timeline"
	source := timeline + "/reference"
	target := "cckv://remote.example/concrnt.world/posts/post"

	existing := chunkline.BodyItem{
		Timestamp:   time.Unix(chunkID*chunkSize+1, 0).UTC(),
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

	subscriber := &Subscriber{
		Client:        client.New(""),
		Memcache:      mc,
		manifestCache: cache.New(10*time.Minute, 15*time.Minute),
	}
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
