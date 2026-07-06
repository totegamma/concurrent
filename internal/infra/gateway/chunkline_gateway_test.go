package gateway

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/internal/testutil"
)

const testTimeline = "cckv://alice/home"

func newTestResolver(t *testing.T) (*resolver, chunkline.Manifest) {
	t.Helper()
	mc, cleanup := testutil.CreateMC()
	t.Cleanup(cleanup)

	manifest := chunkline.Manifest{
		Version:    "0.1",
		ChunkSize:  600,
		Descending: &chunkline.Endpoint{Iterator: "itr/{chunk}", Body: "body/{chunk}"},
	}
	seedManifest(t, mc, testTimeline, manifest)

	return &resolver{mc: mc}, manifest
}

func seedManifest(t *testing.T, mc *memcache.Client, timeline string, manifest chunkline.Manifest) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	err = mc.Set(&memcache.Item{Key: manifestCacheKey(timeline), Value: raw, Expiration: manifestCacheTTL})
	if err != nil {
		t.Fatal(err)
	}
}

func seedChunk(t *testing.T, mc *memcache.Client, timeline string, chunk int64, items []chunkline.BodyItem) {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	body := "," + string(raw[1:len(raw)-1])
	err = mc.Set(&memcache.Item{Key: bodyCacheKey(timeline, strconv.FormatInt(chunk, 10)), Value: []byte(body), Expiration: bodyCacheTTL})
	if err != nil {
		t.Fatal(err)
	}
	err = mc.Set(&memcache.Item{Key: itrCacheKey(timeline, chunk), Value: []byte(strconv.FormatInt(chunk, 10)), Expiration: itrCacheTTL})
	if err != nil {
		t.Fatal(err)
	}
}

func cachedBody(t *testing.T, mc *memcache.Client, timeline string, chunk int64) string {
	t.Helper()
	item, err := mc.Get(bodyCacheKey(timeline, strconv.FormatInt(chunk, 10)))
	if err != nil {
		t.Fatalf("failed to get cached body: %v", err)
	}
	return string(item.Value)
}

func testEvent(uri string, ts time.Time) concrnt.Event {
	return concrnt.Event{
		Type:      "created",
		Source:    testTimeline,
		URI:       uri,
		Timestamp: ts,
	}
}

// Applying the same event twice (two overlapping cache updaters during a
// leadership handover) must prepend it exactly once.
func TestApplyEventToCacheOnce(t *testing.T) {
	r, manifest := newTestResolver(t)

	now := time.Now()
	epoch := manifest.Time2Chunk(now)
	seedChunk(t, r.mc, testTimeline, epoch, []chunkline.BodyItem{
		{Timestamp: now.Add(-time.Minute), Href: "cckv://alice/home/existing"},
	})

	event := testEvent("cckv://alice/home/new", now)
	r.applyEventToCache(event, epoch)
	r.applyEventToCache(event, epoch)

	body := cachedBody(t, r.mc, testTimeline, epoch)
	if got := strings.Count(body, `"cckv://alice/home/new"`); got != 1 {
		t.Fatalf("expected event exactly once in cached body, got %d: %s", got, body)
	}
	if !strings.Contains(body, "existing") {
		t.Fatalf("existing record lost from cached body: %s", body)
	}
}

// A record already present via an origin fetch (whose serialization may
// differ) must not be prepended again.
func TestApplyEventToCacheAlreadyInOriginBody(t *testing.T) {
	r, manifest := newTestResolver(t)

	now := time.Now()
	epoch := manifest.Time2Chunk(now)
	seedChunk(t, r.mc, testTimeline, epoch, []chunkline.BodyItem{
		{Timestamp: now.Add(-time.Minute), Href: "cckv://alice/home/rec1"},
	})
	before := cachedBody(t, r.mc, testTimeline, epoch)

	r.applyEventToCache(testEvent("cckv://alice/home/rec1", now), epoch)

	if after := cachedBody(t, r.mc, testTimeline, epoch); after != before {
		t.Fatalf("body changed for an already-present record:\nbefore: %s\nafter:  %s", before, after)
	}
}

// Concurrent distinct events must each land exactly once (exercises the
// ErrCASConflict retry path against a real memcached).
func TestApplyEventToCacheConcurrentDistinctEvents(t *testing.T) {
	r, manifest := newTestResolver(t)

	now := time.Now()
	epoch := manifest.Time2Chunk(now)
	seedChunk(t, r.mc, testTimeline, epoch, []chunkline.BodyItem{
		{Timestamp: now.Add(-time.Minute), Href: "cckv://alice/home/existing"},
	})

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.applyEventToCache(testEvent(fmt.Sprintf("cckv://alice/home/rec%d", i), now), epoch)
		}(i)
	}
	wg.Wait()

	body := cachedBody(t, r.mc, testTimeline, epoch)
	for i := range n {
		if got := strings.Count(body, fmt.Sprintf(`"cckv://alice/home/rec%d"`, i)); got != 1 {
			t.Fatalf("expected rec%d exactly once, got %d: %s", i, got, body)
		}
	}
}

// With no cached body there is nothing to maintain: no key may be created.
func TestApplyEventToCacheNoBody(t *testing.T) {
	r, manifest := newTestResolver(t)

	now := time.Now()
	epoch := manifest.Time2Chunk(now)

	r.applyEventToCache(testEvent("cckv://alice/home/new", now), epoch)

	_, err := r.mc.Get(bodyCacheKey(testTimeline, strconv.FormatInt(epoch, 10)))
	if err != memcache.ErrCacheMiss {
		t.Fatalf("expected no body key to be created, got err=%v", err)
	}
}

func TestPurgeLatest(t *testing.T) {
	r, manifest := newTestResolver(t)

	now := time.Now()
	current := manifest.Time2Chunk(now)
	item := []chunkline.BodyItem{{Timestamp: now, Href: "cckv://alice/home/rec"}}

	assertChunk := func(chunk int64, wantCached bool) {
		t.Helper()
		_, bodyErr := r.mc.Get(bodyCacheKey(testTimeline, strconv.FormatInt(chunk, 10)))
		_, itrErr := r.mc.Get(itrCacheKey(testTimeline, chunk))
		if wantCached && (bodyErr != nil || itrErr != nil) {
			t.Fatalf("expected chunk %d to stay cached, got body=%v itr=%v", chunk, bodyErr, itrErr)
		}
		if !wantCached && (bodyErr != memcache.ErrCacheMiss || itrErr != memcache.ErrCacheMiss) {
			t.Fatalf("expected chunk %d to be purged, got body=%v itr=%v", chunk, bodyErr, itrErr)
		}
	}

	for _, chunk := range []int64{current, current - 1, current - 2} {
		seedChunk(t, r.mc, testTimeline, chunk, item)
	}
	r.PurgeLatest([]string{testTimeline}, 1)
	assertChunk(current, false)
	assertChunk(current-1, true)
	assertChunk(current-2, true)

	for _, chunk := range []int64{current, current - 1, current - 2} {
		seedChunk(t, r.mc, testTimeline, chunk, item)
	}
	r.PurgeLatest([]string{testTimeline}, 2)
	assertChunk(current, false)
	assertChunk(current-1, false)
	assertChunk(current-2, true)
}
