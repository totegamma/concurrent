package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

type recordingPrefetcher struct {
	timelines []string
}

func (p *recordingPrefetcher) PrefetchChunks(ctx context.Context, timelines []string) {
	p.timelines = append(p.timelines, timelines...)
}

func TestSubscriberActivatesSubscriptionFromSubscribedEvent(t *testing.T) {
	ctx := context.Background()
	timeline := "cckv://remote.example/concrnt.world/timeline"
	prefetcher := &recordingPrefetcher{}
	subscriber := NewSubscriber(nil, nil, nil, nil, prefetcher)

	require.Empty(t, subscriber.GetCurrentSubscriptions())

	subscriber.activateSubscription(ctx, "remote.example", []string{timeline + "*"})

	require.True(t, slices.Contains(subscriber.GetCurrentSubscriptions(), timeline+"*"))
	require.Equal(t, []string{timeline}, prefetcher.timelines)

	subscriber.deactivateSubscription(ctx, "remote.example")
	require.Empty(t, subscriber.GetCurrentSubscriptions())
}

func TestSubscriberCacheChunklineEventPrependsCachedChunk(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	const testChunkSize = int64(300)
	chunkID := int64(42)
	createdAt := time.Unix(chunkID*testChunkSize+10, 0).UTC()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/timeline", r.URL.Path)
		require.Equal(t, "application/chunkline+json", r.Header.Get("Accept"))
		require.NoError(t, json.NewEncoder(w).Encode(chunkline.Manifest{
			Version:   "1.0",
			ChunkSize: testChunkSize,
		}))
	}))
	defer server.Close()

	timeline := server.URL + "/timeline"
	source := timeline + "/reference"
	target := "cckv://remote.example/concrnt.world/posts/post"

	existing := chunkline.BodyItem{
		Timestamp:   time.Unix(chunkID*testChunkSize+1, 0).UTC(),
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
		manifestCache: cache.New(manifestCacheExpiration, manifestCacheCleanup),
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

func TestSubscriberCacheChunklineEventCreatesEntryOnCacheMiss(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	const testChunkSize = int64(300)
	chunkID := int64(42)
	createdAt := time.Unix(chunkID*testChunkSize+10, 0).UTC()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(chunkline.Manifest{
			Version:   "1.0",
			ChunkSize: testChunkSize,
		}))
	}))
	defer server.Close()

	timeline := server.URL + "/timeline"
	source := timeline + "/reference"
	target := "cckv://remote.example/concrnt.world/posts/post"

	refDoc, err := json.Marshal(concrnt.Document[schemas.Reference]{
		Key:       source,
		Value:     schemas.Reference{Href: target},
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
		manifestCache: cache.New(manifestCacheExpiration, manifestCacheCleanup),
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

	cachedBody, err := mc.Get(chunkline.BodyCacheKey(timeline, chunkID))
	require.NoError(t, err)
	items, err := chunkline.DecodeBodyCache(cachedBody.Value)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, target, items[0].Href)
	require.Equal(t, createdAt, items[0].Timestamp)
}

func TestBodyItemFromEventUsesNestedReferenceDocumentTimestamp(t *testing.T) {
	referenceURI := "cckv://remote.example/concrnt.world/refs/ref"
	targetURI := "cckv://remote.example/concrnt.world/posts/post"
	referenceCreatedAt := time.Unix(1200, 0).UTC()
	targetCreatedAt := time.Unix(600, 0).UTC()

	targetDoc, err := json.Marshal(concrnt.Document[any]{
		Key:       targetURI,
		Value:     map[string]any{},
		CreatedAt: targetCreatedAt,
	})
	require.NoError(t, err)

	referenceDoc, err := json.Marshal(concrnt.Document[schemas.Reference]{
		Key: referenceURI,
		Value: schemas.Reference{
			Href: targetURI,
		},
		Schema:    schemas.ReferenceURL,
		CreatedAt: referenceCreatedAt,
	})
	require.NoError(t, err)

	item, ok := bodyItemFromEvent(concrnt.Event{
		Type: "created",
		URI:  referenceURI,
		References: map[string]concrnt.SignedDocument{
			referenceURI: {
				Document: string(referenceDoc),
				References: map[string]concrnt.SignedDocument{
					targetURI: {Document: string(targetDoc)},
				},
			},
		},
	})

	require.True(t, ok)
	require.Equal(t, targetURI, item.Href)
	require.Equal(t, targetCreatedAt, item.Timestamp)
}
