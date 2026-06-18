package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/stretchr/testify/require"
)

type fakeMemcache struct {
	mu    sync.Mutex
	items map[string]*memcache.Item
}

func newFakeMemcache() *fakeMemcache {
	return &fakeMemcache{items: make(map[string]*memcache.Item)}
}

func (m *fakeMemcache) Get(key string) (*memcache.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	if !ok {
		return nil, memcache.ErrCacheMiss
	}
	return cloneMemcacheItem(item), nil
}

func (m *fakeMemcache) GetMulti(keys []string) (map[string]*memcache.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]*memcache.Item)
	for _, key := range keys {
		if item, ok := m.items[key]; ok {
			result[key] = cloneMemcacheItem(item)
		}
	}
	return result, nil
}

func (m *fakeMemcache) Set(item *memcache.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[item.Key] = cloneMemcacheItem(item)
	return nil
}

func (m *fakeMemcache) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.items[key]; !ok {
		return memcache.ErrCacheMiss
	}
	delete(m.items, key)
	return nil
}

func cloneMemcacheItem(item *memcache.Item) *memcache.Item {
	cloned := *item
	cloned.Value = append([]byte(nil), item.Value...)
	return &cloned
}

type subscriptionsProvider []string

func (p subscriptionsProvider) GetCurrentSubscriptions() []string {
	return []string(p)
}

func TestResolverDoesNotUseOrWriteLatestCacheWithoutSubscription(t *testing.T) {
	timeline, cl, mc := newChunklineTestServer(t, 100, []chunkline.BodyItem{
		{Timestamp: time.Unix(100*600, 0), Href: "new"},
	})
	require.NoError(t, mc.Set(&memcache.Item{Key: chunklineIteratorCacheKey(timeline, 100), Value: []byte("stale")}))
	require.NoError(t, mc.Set(&memcache.Item{Key: chunklineBodyCacheKey(timeline, 100), Value: mustEncodeBodyCache(t, []chunkline.BodyItem{{Href: "stale"}})}))

	resolver := &resolver{
		client:               cl,
		cache:                newChunklineCacheService(cl, mc),
		currentSubscriptions: subscriptionsProvider{},
	}

	itrs, err := resolver.LookupChunkItrs(context.Background(), []string{timeline}, time.Unix(100*600, 0))
	require.NoError(t, err)
	require.Equal(t, "100", itrs[timeline])

	bodies, err := resolver.LoadChunkBodies(context.Background(), map[string]string{timeline: "100"})
	require.NoError(t, err)
	require.Len(t, bodies[timeline].Items, 1)
	require.Equal(t, "new", bodies[timeline].Items[0].Href)

	item, err := mc.Get(chunklineIteratorCacheKey(timeline, 100))
	require.NoError(t, err)
	require.Equal(t, "stale", string(item.Value))
}

func TestResolverWritesLatestCacheWithSubscriptionAndOldCacheWithoutSubscription(t *testing.T) {
	timeline, cl, mc := newChunklineTestServer(t, 100, []chunkline.BodyItem{
		{Timestamp: time.Unix(100*600, 0), Href: "latest"},
	})
	resolver := &resolver{
		client:               cl,
		cache:                newChunklineCacheService(cl, mc),
		currentSubscriptions: subscriptionsProvider{timeline},
	}

	_, err := resolver.LookupChunkItrs(context.Background(), []string{timeline}, time.Unix(100*600, 0))
	require.NoError(t, err)
	_, err = resolver.LoadChunkBodies(context.Background(), map[string]string{timeline: "100"})
	require.NoError(t, err)

	_, err = mc.Get(chunklineIteratorCacheKey(timeline, 100))
	require.NoError(t, err)
	_, err = mc.Get(chunklineBodyCacheKey(timeline, 100))
	require.NoError(t, err)

	resolver.currentSubscriptions = subscriptionsProvider{}
	_, err = resolver.LookupChunkItrs(context.Background(), []string{timeline}, time.Unix(99*600, 0))
	require.NoError(t, err)
	_, err = resolver.LoadChunkBodies(context.Background(), map[string]string{timeline: "99"})
	require.NoError(t, err)

	_, err = mc.Get(chunklineIteratorCacheKey(timeline, 99))
	require.NoError(t, err)
	_, err = mc.Get(chunklineBodyCacheKey(timeline, 99))
	require.NoError(t, err)
}

func TestResolveTimelinesUsesBatchEndpoint(t *testing.T) {
	const domain = "example.test"

	manifests := map[string]chunkline.Manifest{
		"cckv://example.test/timeline/0": {
			Version:   "1.0",
			ChunkSize: 600,
		},
		"cckv://example.test/timeline/1": {
			Version:   "1.0",
			ChunkSize: 300,
		},
	}

	var batchHits atomic.Int64
	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve?uri={uri}",
					"net.concrnt.core.batch":   "/batch",
				},
			}
			require.NoError(t, json.NewEncoder(w).Encode(wkc))
		case "/resolve":
			require.Equal(t, "application/chunkline+json", r.Header.Get("Accept"))

			timeline, err := url.QueryUnescape(r.URL.Query().Get("uri"))
			require.NoError(t, err)
			manifest, ok := manifests[timeline]
			if !ok {
				http.NotFound(w, r)
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(manifest))
		case "/batch":
			batchHits.Add(1)
			serveGatewayTestBatch(t, handler, w, r)
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cl := client.New(domain)
	cl.AddHostRemapping(domain, server.URL)
	resolver := &resolver{
		client: cl,
		cache:  newChunklineCacheService(cl, newFakeMemcache()),
	}

	timelines := []string{"cckv://example.test/timeline/0", "cckv://example.test/timeline/1"}
	got, err := resolver.ResolveTimelines(context.Background(), timelines)
	require.NoError(t, err)
	require.Equal(t, int64(1), batchHits.Load())
	require.Equal(t, manifests[timelines[0]].ChunkSize, got[timelines[0]].ChunkSize)
	require.Equal(t, manifests[timelines[1]].ChunkSize, got[timelines[1]].ChunkSize)
}

func TestLookupChunkItrsAndLoadChunkBodiesUseBatchEndpoint(t *testing.T) {
	const domain = "example.test"
	lastChunk := int64(100)
	timelines := []string{"cckv://example.test/timeline/0", "cckv://example.test/timeline/1"}
	manifests := map[string]chunkline.Manifest{
		timelines[0]: {
			Version:   "1.0",
			ChunkSize: 600,
			LastChunk: &lastChunk,
			Descending: &chunkline.Endpoint{
				Iterator: "/itr/{chunk}",
				Body:     "/body/{chunk}",
			},
		},
		timelines[1]: {
			Version:   "1.0",
			ChunkSize: 600,
			LastChunk: &lastChunk,
			Descending: &chunkline.Endpoint{
				Iterator: "/itr/{chunk}",
				Body:     "/body/{chunk}",
			},
		},
	}

	var batchHits atomic.Int64
	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve?uri={uri}",
					"net.concrnt.core.batch":   "/batch",
				},
			}
			require.NoError(t, json.NewEncoder(w).Encode(wkc))
		case r.URL.Path == "/resolve":
			timeline, err := url.QueryUnescape(r.URL.Query().Get("uri"))
			require.NoError(t, err)
			manifest, ok := manifests[timeline]
			if !ok {
				http.NotFound(w, r)
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(manifest))
		case r.URL.Path == "/batch":
			batchHits.Add(1)
			serveGatewayTestBatch(t, handler, w, r)
		case strings.HasPrefix(r.URL.Path, "/itr/"):
			_, err := w.Write([]byte(r.URL.Path[len("/itr/"):]))
			require.NoError(t, err)
		case strings.HasPrefix(r.URL.Path, "/body/"):
			require.NoError(t, json.NewEncoder(w).Encode([]chunkline.BodyItem{
				{Timestamp: time.Unix(lastChunk*600, 0), Href: r.URL.Path},
			}))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cl := client.New(domain)
	cl.AddHostRemapping(domain, server.URL)
	resolver := &resolver{
		client: cl,
		cache:  newChunklineCacheService(cl, newFakeMemcache()),
	}

	itrs, err := resolver.LookupChunkItrs(context.Background(), timelines, time.Unix(lastChunk*600, 0))
	require.NoError(t, err)
	require.Equal(t, "100", itrs[timelines[0]])
	require.Equal(t, "100", itrs[timelines[1]])

	bodies, err := resolver.LoadChunkBodies(context.Background(), itrs)
	require.NoError(t, err)
	require.Len(t, bodies[timelines[0]].Items, 1)
	require.Len(t, bodies[timelines[1]].Items, 1)
	require.Equal(t, int64(3), batchHits.Load())
}

func TestCacheCreatedEventPrependsLatestItemAndDeduplicates(t *testing.T) {
	timeline, cl, mc := newChunklineTestServer(t, 100, []chunkline.BodyItem{
		{Timestamp: time.Unix(100*600+1, 0), Href: "older"},
	})
	cache := newChunklineCacheService(cl, mc)

	createdAt := time.Unix(100*600+2, 0)
	event := createdChunklineEvent(timeline, "new", createdAt)

	require.NoError(t, cache.CacheCreatedEvent(context.Background(), event))
	items, found, err := cache.getBodyCache(timeline, 100)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, items, 2)
	require.Equal(t, "new", items[0].Href)
	require.Equal(t, "older", items[1].Href)

	require.NoError(t, cache.CacheCreatedEvent(context.Background(), event))
	items, found, err = cache.getBodyCache(timeline, 100)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, items, 2)
}

func newChunklineTestServer(t *testing.T, lastChunk int64, body []chunkline.BodyItem) (string, *client.Client, *fakeMemcache) {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	timeline := server.URL + "/timeline"
	manifest := chunkline.Manifest{
		Version:   "1.0",
		ChunkSize: 600,
		LastChunk: &lastChunk,
		Descending: &chunkline.Endpoint{
			Iterator: "/itr/{chunk}",
			Body:     "/body/{chunk}",
		},
	}

	mux.HandleFunc("/timeline", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(manifest))
	})
	mux.HandleFunc("/itr/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path[len("/itr/"):]))
	})
	mux.HandleFunc("/body/", func(w http.ResponseWriter, r *http.Request) {
		chunkID, err := strconv.ParseInt(r.URL.Path[len("/body/"):], 10, 64)
		require.NoError(t, err)
		if chunkID == lastChunk {
			require.NoError(t, json.NewEncoder(w).Encode(body))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode([]chunkline.BodyItem{
			{Timestamp: time.Unix(chunkID*600, 0), Href: "old"},
		}))
	})

	return timeline, client.New(""), newFakeMemcache()
}

func serveGatewayTestBatch(t *testing.T, handler http.Handler, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.True(t, strings.EqualFold(mediaType, "multipart/mixed"))

	boundary := params["boundary"]
	require.NotEmpty(t, boundary)

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+boundary)
	mw := multipart.NewWriter(w)
	require.NoError(t, mw.SetBoundary(boundary))
	defer mw.Close()

	mr := multipart.NewReader(r.Body, boundary)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		contentID := part.Header.Get("Content-ID")
		req, err := http.ReadRequest(bufio.NewReader(part))
		require.NoError(t, err)

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		resp := recorder.Result()

		pw, err := mw.CreatePart(map[string][]string{
			"Content-Type": {"application/http"},
			"Content-ID":   {contentID},
		})
		require.NoError(t, err)
		require.NoError(t, resp.Write(pw))
		require.NoError(t, resp.Body.Close())
	}
}

func mustEncodeBodyCache(t *testing.T, items []chunkline.BodyItem) []byte {
	t.Helper()

	body, err := EncodeBodyCache(items)
	require.NoError(t, err)
	return body
}

func createdChunklineEvent(timeline string, uri string, createdAt time.Time) concrnt.Event {
	doc := concrnt.Document[json.RawMessage]{
		Kind:      "record",
		Schema:    "application/test",
		CreatedAt: createdAt,
	}
	body, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}

	return concrnt.Event{
		Type:   "created",
		Source: timeline,
		URI:    uri,
		References: map[string]concrnt.SignedDocument{
			uri: {Document: string(body)},
		},
	}
}

var _ memcacheStore = (*fakeMemcache)(nil)
