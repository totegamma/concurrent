package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/testutil"
)

func TestResolverUsesMemcachedChunkCaches(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	chunkID := int64(42)
	item := chunkline.BodyItem{
		Timestamp:   time.Unix(chunkID*600+10, 0).UTC(),
		Href:        "cckv://remote.example/documents/item",
		ContentType: "application/concrnt.document+json",
	}

	var itrHits int
	var bodyHits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/timeline":
			require.Equal(t, "application/chunkline+json", r.Header.Get("Accept"))
			manifest := chunkline.Manifest{
				Version:   "1.0",
				ChunkSize: 600,
				Descending: &chunkline.Endpoint{
					Iterator: "/itr/{chunk}",
					Body:     "/body/{chunk}",
				},
			}
			require.NoError(t, json.NewEncoder(w).Encode(manifest))
		case "/itr/42":
			itrHits++
			_, _ = w.Write([]byte("42"))
		case "/body/42":
			bodyHits++
			require.NoError(t, json.NewEncoder(w).Encode([]chunkline.BodyItem{item}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	timeline := server.URL + "/timeline"
	resolver := &resolver{
		client: client.New(""),
		cache:  cache.New(10*time.Minute, 15*time.Minute),
		mc:     mc,
	}

	itrs, err := resolver.LookupChunkItrs(ctx, []string{timeline}, time.Unix(chunkID*600+599, 0).UTC())
	require.NoError(t, err)
	require.Equal(t, map[string]string{timeline: "42"}, itrs)

	chunks, err := resolver.LoadChunkBodies(ctx, itrs)
	require.NoError(t, err)
	require.Equal(t, []chunkline.BodyItem{item}, chunks[timeline].Items)

	itrs, err = resolver.LookupChunkItrs(ctx, []string{timeline}, time.Unix(chunkID*600+599, 0).UTC())
	require.NoError(t, err)
	require.Equal(t, map[string]string{timeline: "42"}, itrs)

	chunks, err = resolver.LoadChunkBodies(ctx, itrs)
	require.NoError(t, err)
	require.Equal(t, []chunkline.BodyItem{item}, chunks[timeline].Items)

	require.Equal(t, 1, itrHits)
	require.Equal(t, 1, bodyHits)

	cachedItr, err := mc.Get(chunkline.IteratorCacheKey(timeline, chunkID))
	require.NoError(t, err)
	require.Equal(t, "42", string(cachedItr.Value))

	cachedBody, err := mc.Get(chunkline.BodyCacheKey(timeline, chunkID))
	require.NoError(t, err)
	decoded, err := chunkline.DecodeBodyCache(cachedBody.Value)
	require.NoError(t, err)
	require.Equal(t, []chunkline.BodyItem{item}, decoded)
}

func TestResolverSkipsCurrentChunkCacheWithoutSubscription(t *testing.T) {
	timeline := "https://remote.example/timeline"
	manifest := chunkline.Manifest{ChunkSize: 600}
	currentChunk := manifest.Time2Chunk(time.Now().UTC())

	resolver := &resolver{}
	require.False(t, resolver.shouldCacheChunk(timeline, currentChunk, manifest))

	resolver.subscriptions = staticSubscriptions{timeline + "*"}
	require.True(t, resolver.shouldCacheChunk(timeline, currentChunk, manifest))
}

func TestResolverCachesCurrentChunkOnlyAfterSubscribed(t *testing.T) {
	mc, cleanup := testutil.CreateMC()
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	manifest := chunkline.Manifest{
		Version:   "1.0",
		ChunkSize: 60,
		Descending: &chunkline.Endpoint{
			Iterator: "/itr/{chunk}",
			Body:     "/body/{chunk}",
		},
	}
	currentChunk := manifest.Time2Chunk(now)
	item := chunkline.BodyItem{
		Timestamp:   now,
		Href:        "cckv://remote.example/documents/item",
		ContentType: "application/concrnt.document+json",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/timeline":
			require.NoError(t, json.NewEncoder(w).Encode(manifest))
		case "/itr/" + strconv.FormatInt(currentChunk, 10):
			_, _ = w.Write([]byte(strconv.FormatInt(currentChunk, 10)))
		case "/body/" + strconv.FormatInt(currentChunk, 10):
			require.NoError(t, json.NewEncoder(w).Encode([]chunkline.BodyItem{item}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	timeline := server.URL + "/timeline"
	subscriptions := &mutableSubscriptions{}
	resolver := &resolver{
		client:        client.New(""),
		cache:         cache.New(10*time.Minute, 15*time.Minute),
		mc:            mc,
		subscriptions: subscriptions,
	}

	itrs, err := resolver.LookupChunkItrs(ctx, []string{timeline}, now)
	require.NoError(t, err)
	_, err = resolver.LoadChunkBodies(ctx, itrs)
	require.NoError(t, err)
	_, err = mc.Get(chunkline.BodyCacheKey(timeline, currentChunk))
	require.ErrorIs(t, err, memcache.ErrCacheMiss)

	subscriptions.subs = []string{timeline + "*"}
	itrs, err = resolver.LookupChunkItrs(ctx, []string{timeline}, now)
	require.NoError(t, err)
	_, err = resolver.LoadChunkBodies(ctx, itrs)
	require.NoError(t, err)

	cachedBody, err := mc.Get(chunkline.BodyCacheKey(timeline, currentChunk))
	require.NoError(t, err)
	decoded, err := chunkline.DecodeBodyCache(cachedBody.Value)
	require.NoError(t, err)
	require.Equal(t, []chunkline.BodyItem{item}, decoded)
}

type staticSubscriptions []string

func (s staticSubscriptions) GetCurrentSubscriptions() []string {
	return []string(s)
}

type mutableSubscriptions struct {
	subs []string
}

func (s *mutableSubscriptions) GetCurrentSubscriptions() []string {
	return append([]string(nil), s.subs...)
}
