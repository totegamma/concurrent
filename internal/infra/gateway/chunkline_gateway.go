package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/worker"
)

const (
	manifestCacheTTL = 60 * 60 * 24 * 7 // 7 days
	itrCacheTTL      = 60 * 60 * 24 * 2 // 2 days
	bodyCacheTTL     = 60 * 60 * 24 * 2 // 2 days
)

// SubscriptionDemand reports the realtime subscription demand of this replica,
// used to decide whether a latest chunk will be kept fresh by the cache
// updater and is therefore safe to cache.
type SubscriptionDemand interface {
	CurrentSubscriptions() []string
}

type ChunklineGateway struct {
	resolver *chunkline.Client
	r        *resolver
}

func NewChunklineGateway(
	cl *client.Client,
	mc *memcache.Client,
	demand SubscriptionDemand,
	pubsub worker.PubSub,
) *ChunklineGateway {
	r := &resolver{
		client: cl,
		mc:     mc,
		demand: demand,
		pubsub: pubsub,
	}
	return &ChunklineGateway{
		resolver: chunkline.NewClient(r),
		r:        r,
	}
}

// StartWorker registers the resolver's cache keep-alive demand on the
// subscriber, hooks cache purging into the subscription open/close edges, and
// starts the cache updater. Run this only on the replica that runs the
// singleton workers (the leader) — the cache updater maintains the shared
// memcached.
func (g *ChunklineGateway) StartWorker(ctx context.Context, subscriber *worker.Subscriber) {
	g.r.open = subscriber
	subscriber.RegisterClient(g.r)
	subscriber.SetCachePurger(g.r)
	go g.r.cacheUpdater(ctx)
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// OpenSubscriptions reports the prefixes whose upstream subscription is
// currently established (as opposed to merely demanded).
type OpenSubscriptions interface {
	OpenPrefixes() []string
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client *client.Client
	mc     *memcache.Client
	demand SubscriptionDemand
	pubsub worker.PubSub
	open   OpenSubscriptions // set by StartWorker on the leader; nil elsewhere
}

func manifestCacheKey(timeline string) string {
	return "chunkline_manifest:" + timeline
}

func itrCacheKey(timeline string, chunkID int64) string {
	return fmt.Sprintf("chunkline_itr:%s:%d", timeline, chunkID)
}

func bodyCacheKey(timeline string, chunkID string) string {
	return fmt.Sprintf("chunkline_body:%s:%s", timeline, chunkID)
}

func (r *resolver) resolveTimeline(ctx context.Context, timeline string) (chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.resolveTimeline")
	defer span.End()

	cacheKey := manifestCacheKey(timeline)
	item, err := r.mc.Get(cacheKey)
	if err == nil {
		var manifest chunkline.Manifest
		err := json.Unmarshal(item.Value, &manifest)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to unmarshal cached manifest for %s: %w", timeline, err))
		} else {
			return manifest, nil
		}
	} else if !errors.Is(err, memcache.ErrCacheMiss) {
		span.RecordError(fmt.Errorf("failed to get manifest from cache for %s: %w", timeline, err))
	}

	var manifest chunkline.Manifest
	err = r.client.GetResource(ctx, timeline, "application/chunkline+json", nil, &manifest)
	if err != nil {
		span.RecordError(fmt.Errorf("failed to fetch chunkline manifest for %s: %w", timeline, err))
		return chunkline.Manifest{}, err
	}

	bytes, err := json.Marshal(manifest)
	if err != nil {
		span.RecordError(fmt.Errorf("failed to marshal manifest for caching for %s: %w", timeline, err))
	} else {
		cacheItem := &memcache.Item{
			Key:        cacheKey,
			Value:      bytes,
			Expiration: manifestCacheTTL,
		}
		err = r.mc.Set(cacheItem)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to set manifest in cache for %s: %w", timeline, err))
		}
	}

	return manifest, nil
}

func (r *resolver) ResolveTimelines(ctx context.Context, timelines []string) (map[string]chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.ResolveTimelines")
	defer span.End()

	keys := make([]string, len(timelines))
	for i, tl := range timelines {
		keys[i] = manifestCacheKey(tl)
	}

	cacheItems, err := r.mc.GetMulti(keys)
	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		span.RecordError(fmt.Errorf("failed to get manifests from cache: %w", err))
	}

	result := make(map[string]chunkline.Manifest)
	remaining := []string{}

	for i, tl := range timelines {
		cacheKey := keys[i]
		if item, found := cacheItems[cacheKey]; found {
			var manifest chunkline.Manifest
			err := json.Unmarshal(item.Value, &manifest)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to unmarshal cached manifest for %s: %w", tl, err))
				remaining = append(remaining, tl)
				continue
			}
			result[tl] = manifest
		} else {
			remaining = append(remaining, tl)
		}
	}

	for _, tl := range remaining {
		var manifest chunkline.Manifest
		err := r.client.GetResource(ctx, tl, "application/chunkline+json", nil, &manifest)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to fetch chunkline manifest for %s: %w", tl, err))
			continue
		}
		result[tl] = manifest

		bytes, err := json.Marshal(manifest)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to marshal manifest for caching for %s: %w", tl, err))
			continue
		}

		cacheItem := &memcache.Item{
			Key:        manifestCacheKey(tl),
			Value:      bytes,
			Expiration: manifestCacheTTL,
		}
		err = r.mc.Set(cacheItem)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to set manifest in cache for %s: %w", tl, err))
		}
	}

	return result, nil

}

func (r *resolver) GetRemovedItems(ctx context.Context, timelines []string) (map[string][]string, error) {
	result := make(map[string][]string)
	for _, tl := range timelines {
		result[tl] = []string{}
	}
	return result, nil
}

func (r *resolver) LookupChunkItrs(ctx context.Context, timelines []string, until time.Time) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.LookupChunkItrs")
	defer span.End()

	manifests, err := r.ResolveTimelines(ctx, timelines)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	queries := make(map[string]int64)
	keys := make([]string, 0, len(timelines))

	for _, tl := range timelines {
		manifest, ok := manifests[tl]
		if !ok {
			err := fmt.Errorf("missing chunkline manifest for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if manifest.Descending.Iterator == "" {
			err := fmt.Errorf("timeline %s does not support descending iteration", tl)
			span.RecordError(err)
			continue
		}

		queryChunk := manifest.Time2Chunk(until)
		if manifest.LastChunk != nil && queryChunk > *manifest.LastChunk {
			queryChunk = *manifest.LastChunk
		}

		if manifest.FirstChunk != nil && queryChunk < *manifest.FirstChunk {
			err := fmt.Errorf("query chunk %d is before first chunk %d for timeline %s", queryChunk, *manifest.FirstChunk, tl)
			span.RecordError(err)
			continue
		}

		queries[tl] = queryChunk
		keys = append(keys, itrCacheKey(tl, queryChunk))
	}

	cachedItems, err := r.mc.GetMulti(keys)
	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		span.RecordError(fmt.Errorf("failed to get iterators from cache: %w", err))
	}

	results := make(map[string]string)

	requestsByDomain := make(map[string]map[string]string) // domain -> timeline -> query path

	remainings := make(map[string]int64)

	for tl, chunkID := range queries {
		cacheKey := itrCacheKey(tl, chunkID)
		if item, found := cachedItems[cacheKey]; found {
			results[tl] = string(item.Value)
			continue
		}

		manifest, ok := manifests[tl]
		if !ok {
			err := fmt.Errorf("missing chunkline manifest for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if manifest.Descending.Iterator == "" {
			err := fmt.Errorf("timeline %s does not support descending iteration", tl)
			span.RecordError(err)
			continue
		}

		relPath, err := concrnt.RenderURITemplate(manifest.Descending.Iterator, map[string]string{
			"chunk": fmt.Sprintf("%d", chunkID),
		})
		if err != nil {
			span.RecordError(fmt.Errorf("failed to render iterator URI for timeline %s: %w", tl, err))
			continue
		}

		rel, err := url.Parse(relPath)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid iterator URI template for timeline %s: %w", tl, err))
			continue
		}

		base, err := url.Parse(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid timeline URI %s: %w", tl, err))
			continue
		}

		endpoint := base.ResolveReference(rel)
		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			host, err := r.client.ResolveResourceHost(ctx, tl)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to resolve host for timeline %s: %w", tl, err))
				continue
			}
			endpoint.Scheme = "https"
			endpoint.Host = host
		}

		domain := endpoint.Host
		if _, exists := requestsByDomain[domain]; !exists {
			requestsByDomain[domain] = make(map[string]string)
		}
		requestsByDomain[domain][tl] = endpoint.String()
		remainings[tl] = chunkID
	}

	if len(requestsByDomain) == 0 {
		return results, nil // all results were cached
	}

	responces, err := r.client.BatchGet(ctx, requestsByDomain)
	if err != nil {
		span.RecordError(fmt.Errorf("batch request for iterators failed: %w", err))
		return results, nil // return what we have from cache
	}

	currentSubscriptions := r.demand.CurrentSubscriptions()

	for tl, chunkID := range remainings {
		resp, ok := responces[tl]
		if !ok {
			err := fmt.Errorf("missing response for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("non-200 response for timeline %s: %d", tl, resp.StatusCode)
			span.RecordError(err)
			continue
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read response body for timeline %s: %w", tl, err))
			continue
		}

		iterator := strings.TrimSpace(string(bytes))
		results[tl] = iterator

		// もしキャッシュ対象が最新チャンクであれば、現在の購読状態を確認し、購読中でなければキャッシュを保存しない
		if chunkID == manifests[tl].Time2Chunk(time.Now()) {
			isSubscribed := slices.Contains(currentSubscriptions, tl)
			if !isSubscribed {
				continue // skip caching if not subscribed to the latest chunk
			}
		}

		cacheItem := &memcache.Item{
			Key:        itrCacheKey(tl, chunkID),
			Value:      []byte(iterator),
			Expiration: itrCacheTTL,
		}
		err = r.mc.Set(cacheItem)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to set iterator in cache for %s: %w", tl, err))
		}
	}

	return results, nil
}

func (r *resolver) LoadChunkBodies(ctx context.Context, query map[string]string) (map[string]chunkline.BodyChunk, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.LoadChunkBodies")
	defer span.End()

	uris := []string{}
	for itr := range query {
		uris = append(uris, itr)
	}

	manifests, err := r.ResolveTimelines(ctx, uris)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	keys := make([]string, 0, len(query))

	for tl, itr := range query {
		cacheKey := bodyCacheKey(tl, itr)
		keys = append(keys, cacheKey)
	}

	results := make(map[string]chunkline.BodyChunk)

	cachedItems, err := r.mc.GetMulti(keys)
	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		span.RecordError(fmt.Errorf("failed to get chunk bodies from cache: %w", err))
	}

	requestsByDomain := make(map[string]map[string]string) // domain -> timeline -> query path

	remaining := make(map[string]string)

	for tl, itr := range query {
		cacheKey := bodyCacheKey(tl, itr)
		if item, found := cachedItems[cacheKey]; found {
			var bodyItems []chunkline.BodyItem
			cacheStr := string(item.Value)
			cacheStr = cacheStr[1:]
			cacheStr = "[" + cacheStr + "]"
			err := json.Unmarshal([]byte(cacheStr), &bodyItems)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to unmarshal cached body chunk for %s: %w", tl, err))
				fmt.Printf("invalid cache format for key %s: %s\n", cacheKey, cacheStr)
				continue
			}
			chunkID, err := strconv.ParseInt(itr, 10, 64)
			if err != nil {
				span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
				continue
			}
			results[tl] = chunkline.BodyChunk{
				URI:     tl,
				ChunkID: chunkID,
				Items:   bodyItems,
			}
		}

		manifest, ok := manifests[tl]
		if !ok {
			err := fmt.Errorf("missing chunkline manifest for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if manifest.Descending.Body == "" {
			err := fmt.Errorf("timeline %s does not support descending body retrieval", tl)
			span.RecordError(err)
			continue
		}

		refPath := strings.ReplaceAll(manifest.Descending.Body, "{chunk}", itr)
		ref, err := url.Parse(refPath)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid body URI template for timeline %s: %w", tl, err))
			continue
		}

		base, err := url.Parse(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid timeline URI %s: %w", tl, err))
			continue
		}

		endpoint := base.ResolveReference(ref)
		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			host, err := r.client.ResolveResourceHost(ctx, tl)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to resolve host for timeline %s: %w", tl, err))
				continue
			}
			endpoint.Scheme = "https"
			endpoint.Host = host
		}

		domain := endpoint.Host
		if _, exists := requestsByDomain[domain]; !exists {
			requestsByDomain[domain] = make(map[string]string)
		}
		requestsByDomain[domain][tl] = endpoint.String()
		remaining[tl] = itr
	}

	if len(requestsByDomain) == 0 {
		return results, nil // all results were cached
	}

	responses, err := r.client.BatchGet(ctx, requestsByDomain)
	if err != nil {
		span.RecordError(fmt.Errorf("batch request for chunk bodies failed: %w", err))
		return results, nil // return what we have from cache
	}

	currentSubscriptions := r.demand.CurrentSubscriptions()

	for tl, itr := range remaining {
		resp, ok := responses[tl]
		if !ok {
			err := fmt.Errorf("missing response for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("non-200 response for timeline %s: %d", tl, resp.StatusCode)
			span.RecordError(err)
			continue
		}

		var items []chunkline.BodyItem
		err = json.NewDecoder(resp.Body).Decode(&items)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to decode chunk body for timeline %s: %w", tl, err))
			continue
		}

		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
			continue
		}

		bodyChunk := chunkline.BodyChunk{
			URI:     tl,
			ChunkID: chunkID,
			Items:   items,
		}
		results[tl] = bodyChunk

		isLatestChunk := chunkID == manifests[tl].Time2Chunk(time.Now())

		// もしキャッシュ対象が最新チャンクであれば、現在の購読状態を確認し、購読中でなければキャッシュを保存しない
		if isLatestChunk {
			isSubscribed := slices.Contains(currentSubscriptions, tl)
			if !isSubscribed {
				continue // skip caching if not subscribed to the latest chunk
			}
		}

		serialized, err := json.Marshal(items)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to marshal body chunk for caching for %s: %w", tl, err))
			continue
		}

		cacheStr := "," + string(serialized[1:len(serialized)-1])
		cacheItem := &memcache.Item{
			Key:        bodyCacheKey(tl, itr),
			Value:      []byte(cacheStr),
			Expiration: bodyCacheTTL,
		}
		if isLatestChunk {
			// Add, not Set: a live latest chunk may already be maintained by
			// the cache updater, and clobbering it with this (possibly older)
			// origin fetch would drop the records prepended since
			err = r.mc.Add(cacheItem)
			if errors.Is(err, memcache.ErrNotStored) {
				err = nil
			}
		} else {
			err = r.mc.Set(cacheItem)
		}
		if err != nil {
			span.RecordError(fmt.Errorf("failed to set body chunk in cache for %s: %w", tl, err))
		}

		// もしキャッシュ対象が最新チャンクなのであれば、itrも更新する必要がある
		if chunkID == manifests[tl].Time2Chunk(time.Now()) {
			cacheItem := &memcache.Item{
				Key:        itrCacheKey(tl, chunkID),
				Value:      []byte(itr),
				Expiration: itrCacheTTL,
			}
			err = r.mc.Set(cacheItem)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to set iterator in cache for %s: %w", tl, err))
			}
		}
	}

	return results, nil
}

func (r *resolver) CurrentSubscriptions() []string {
	// start from the prefixes whose upstream subscription is actually open:
	// "open + latest chunk cached => keep" is self-sustaining regardless of
	// which replica wrote the cache, whereas demand-based input would drop
	// the timeline in the same tick its demand lapses, orphaning the cache
	var ongoing []string
	if r.open != nil {
		ongoing = r.open.OpenPrefixes()
	} else {
		ongoing = r.demand.CurrentSubscriptions()
	}

	timelines, err := r.ResolveTimelines(context.Background(), ongoing)
	if err != nil {
		return ongoing // fallback to raw URIs if manifest resolution fails
	}

	keep := []string{}
	for _, tl := range ongoing {
		manifest, ok := timelines[tl]
		if !ok {
			continue
		}

		currentChunk := manifest.Time2Chunk(time.Now())

		// bodyの最新チャンクのキャッシュが存在する場合は、残しておかないとキャッシュが古いままになってしまう
		latestChunkKey := bodyCacheKey(tl, fmt.Sprintf("%d", currentChunk))
		_, err := r.mc.Get(latestChunkKey)
		if err == nil {
			keep = append(keep, tl)
			continue
		}

		// 最新チャンク-1がなければ、確定で終了して良い
		prevChunkKey := bodyCacheKey(tl, fmt.Sprintf("%d", currentChunk-1))
		_, err = r.mc.Get(prevChunkKey)
		if err != nil {
			continue
		}

		// 前チャンクがあり最新がまだ作成されてない場合、現在チャンクの80%が経過するまでは待機しておく
		now := time.Now()
		currentChunkStart := manifest.Chunk2Time(currentChunk)
		currentChunkEnd := manifest.Chunk2Time(currentChunk + 1)
		currentChunkDuration := currentChunkEnd.Sub(currentChunkStart)
		if now.Before(currentChunkStart.Add(currentChunkDuration * 8 / 10)) {
			keep = append(keep, tl)
			continue
		}
	}

	return keep
}

func (r *resolver) cacheUpdater(ctx context.Context) {

	events := make(chan concrnt.Event)

	go r.pubsub.SubscribeAll(ctx, events)

	for {
		var event concrnt.Event
		select {
		case <-ctx.Done():
			return
		case event = <-events:
		}

		if event.Type != "created" {
			continue
		}

		timeline := event.Source
		manifest, err := r.resolveTimeline(ctx, timeline)
		if err != nil {
			slog.Error("failed to resolve timeline for caching", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}

		r.applyEventToCache(event, manifest.Time2Chunk(event.Timestamp))
	}
}

// applyEventToCache prepends the event to the cached chunk body exactly once.
// The CAS loop makes the mutation idempotent under concurrent updaters (a
// leadership-handover overlap): whichever writer loses the race re-reads the
// body and finds the record already present. There is no claim to leak, so a
// writer crashing mid-update cannot suppress the surviving one — pubsub never
// redelivers, so a suppressed update would be missing from the cached chunk
// for up to bodyCacheTTL.
func (r *resolver) applyEventToCache(event concrnt.Event, epoch int64) {
	timeline := event.Source
	itrKey := itrCacheKey(timeline, epoch)
	bodyKey := bodyCacheKey(timeline, strconv.FormatInt(epoch, 10))

	bodyItem := chunkline.BodyItem{
		Timestamp: event.Timestamp,
		Href:      event.URI,
	}
	serializedItem, err := json.Marshal(bodyItem)
	if err != nil {
		slog.Error("failed to marshal body item for caching", slog.String("timeline", timeline), slog.String("error", err.Error()))
		return
	}

	// membership needle: matching on href alone also detects the record when
	// it is already part of an origin-fetched body whose timestamp
	// serialization differs from ours
	needle := serializedItem
	if event.URI != "" {
		hrefJSON, err := json.Marshal(event.URI)
		if err == nil {
			needle = []byte(`"href":` + string(hrefJSON))
		}
	}

	// every CAS conflict means another writer made progress, so the retry cap
	// only needs to exceed the realistic number of concurrent updaters (2-3
	// during a leadership handover)
	applied := false
	for attempt := 0; attempt < 10; attempt++ {
		item, err := r.mc.Get(bodyKey) // issues "gets": CasID is populated
		if errors.Is(err, memcache.ErrCacheMiss) {
			return // nothing cached to maintain
		}
		if err != nil {
			slog.Error("failed to get body chunk for cache update", slog.String("timeline", timeline), slog.String("error", err.Error()))
			return
		}

		if bytes.Contains(item.Value, needle) {
			applied = true // already applied by a concurrent updater or an origin fetch
			break
		}

		item.Value = append(append([]byte(","), serializedItem...), item.Value...)
		item.Expiration = bodyCacheTTL
		err = r.mc.CompareAndSwap(item)
		if err == nil {
			applied = true
			break
		}
		if errors.Is(err, memcache.ErrCASConflict) {
			continue // raced with another writer; re-read and re-check
		}
		// ErrNotStored: evicted between gets and cas — the cache is gone,
		// nothing left to maintain
		slog.Error("failed to update body chunk in cache", slog.String("timeline", timeline), slog.String("error", err.Error()))
		return
	}
	if !applied {
		slog.Error("giving up body chunk cache update after repeated CAS conflicts", slog.String("timeline", timeline))
		return
	}

	err = r.mc.Replace(&memcache.Item{Key: itrKey, Value: []byte(strconv.FormatInt(epoch, 10)), Expiration: itrCacheTTL})
	if err != nil && !errors.Is(err, memcache.ErrNotStored) {
		slog.Error("failed to replace iterator in cache", slog.String("timeline", timeline), slog.String("error", err.Error()))
	}
}

// PurgeLatest drops the cached latest chunk (and, for depth 2, the previous
// one) of each timeline. The subscriber calls this when an upstream
// subscription opens or closes: a cached latest chunk is only valid while the
// leader is receiving events for it, so both edges invalidate whatever was
// cached before or during the unsubscribed gap.
func (r *resolver) PurgeLatest(prefixes []string, depth int) {
	if len(prefixes) == 0 {
		return
	}

	// deliberately not the caller's context: purge-on-close also runs while
	// the lead context is already cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	manifests, err := r.ResolveTimelines(ctx, prefixes)
	if err != nil {
		slog.Error("failed to resolve timelines for cache purge", slog.String("error", err.Error()))
		return
	}

	for _, tl := range prefixes {
		manifest, ok := manifests[tl]
		if !ok {
			continue
		}
		current := manifest.Time2Chunk(time.Now())
		for i := range int64(depth) {
			chunk := current - i
			if err := r.mc.Delete(itrCacheKey(tl, chunk)); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
				slog.Error("failed to purge iterator cache", slog.String("timeline", tl), slog.String("error", err.Error()))
			}
			if err := r.mc.Delete(bodyCacheKey(tl, strconv.FormatInt(chunk, 10))); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
				slog.Error("failed to purge body cache", slog.String("timeline", tl), slog.String("error", err.Error()))
			}
		}
	}
}
