package gateway

import (
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

type ChunklineGateway struct {
	resolver   *chunkline.Client
	subscriber *worker.Subscriber
}

func NewChunklineGateway(
	cl *client.Client,
	mc *memcache.Client,
	subscriber *worker.Subscriber,
	pubsub worker.PubSub,
) *ChunklineGateway {
	r := &resolver{
		client:     cl,
		mc:         mc,
		subscriber: subscriber,
		pubsub:     pubsub,
	}
	subscriber.RegisterClient(r)
	go r.cacheUpdater()
	return &ChunklineGateway{
		resolver:   chunkline.NewClient(r),
		subscriber: subscriber,
	}
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client     *client.Client
	mc         *memcache.Client
	subscriber *worker.Subscriber
	pubsub     worker.PubSub
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

	currentSubscriptions := r.subscriber.CurrentSubscriptions(r)

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

	currentSubscriptions := r.subscriber.CurrentSubscriptions(r)

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

		// もしキャッシュ対象が最新チャンクであれば、現在の購読状態を確認し、購読中でなければキャッシュを保存しない
		if chunkID == manifests[tl].Time2Chunk(time.Now()) {
			isSubscribed := slices.Contains(currentSubscriptions, tl)
			if !isSubscribed {
				continue // skip caching if not subscribed to the latest chunk
			}
		}

		bytes, err := json.Marshal(items)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to marshal body chunk for caching for %s: %w", tl, err))
			continue
		}

		cacheStr := "," + string(bytes[1:len(bytes)-1])
		cacheItem := &memcache.Item{
			Key:        bodyCacheKey(tl, itr),
			Value:      []byte(cacheStr),
			Expiration: bodyCacheTTL,
		}
		err = r.mc.Set(cacheItem)
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
	ongoing := r.subscriber.CurrentSubscriptions(r)

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

func (r *resolver) cacheUpdater() {

	ctx := context.Background()

	events := make(chan concrnt.Event)

	go r.pubsub.SubscribeAll(ctx, events)

	for event := range events {

		if event.Type != "created" {
			continue
		}

		timeline := event.Source
		manifest, err := r.resolveTimeline(ctx, timeline)
		if err != nil {
			slog.Error("failed to resolve timeline for caching", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}

		epoch := manifest.Time2Chunk(event.Timestamp)
		itrKey := itrCacheKey(timeline, epoch)
		bodyKey := bodyCacheKey(timeline, strconv.FormatInt(epoch, 10))

		// update iterator cache
		err = r.mc.Replace(&memcache.Item{Key: itrKey, Value: []byte(strconv.FormatInt(epoch, 10))})
		if err != nil {
			slog.Error("failed to replace iterator in cache", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}

		// update body cache
		bodyItem := chunkline.BodyItem{
			Timestamp: event.Timestamp,
			Href:      event.URI,
		}

		serializedItem, err := json.Marshal(bodyItem)
		if err != nil {
			slog.Error("failed to marshal body item for caching", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}
		val := "," + string(serializedItem)

		err = r.mc.Prepend(&memcache.Item{Key: bodyKey, Value: []byte(val)})
		if err != nil {
			slog.Error("failed to prepend body item in cache", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}
	}
}
