package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
)

type CurrentSubscriptionsProvider interface {
	GetCurrentSubscriptions() []string
}

type ChunklineGateway struct {
	client   *client.Client
	resolver *chunkline.Client
}

func NewChunklineGateway(
	cl *client.Client,
	mc *memcache.Client,
	currentSubscriptions CurrentSubscriptionsProvider,
) *ChunklineGateway {
	return NewChunklineGatewayWithCacheService(cl, NewChunklineCacheService(cl, mc), currentSubscriptions)
}

func NewChunklineGatewayWithCacheService(
	cl *client.Client,
	cache *ChunklineCacheService,
	currentSubscriptions CurrentSubscriptionsProvider,
) *ChunklineGateway {
	r := &resolver{
		client:               cl,
		cache:                cache,
		currentSubscriptions: currentSubscriptions,
	}
	return &ChunklineGateway{
		client:   cl,
		resolver: chunkline.NewClient(r),
	}
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client               *client.Client
	cache                *ChunklineCacheService
	currentSubscriptions CurrentSubscriptionsProvider
}

func (r *resolver) ResolveTimelines(ctx context.Context, timelines []string) (map[string]chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.ResolveTimelines")
	defer span.End()

	result := make(map[string]chunkline.Manifest)
	remaining := []string{}

	for _, tl := range timelines {
		cached, found, err := r.cache.getCachedManifest(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read chunkline manifest cache for %s: %w", tl, err))
		}
		if found {
			result[tl] = cached
		} else {
			remaining = append(remaining, tl)
		}
	}

	fetchTargets := make([]string, 0, len(remaining))
	for _, tl := range remaining {
		negativeCached, err := r.cache.isNegativeCached(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read chunkline negative cache for %s: %w", tl, err))
		}
		if negativeCached {
			span.AddEvent(fmt.Sprintf("Skipping timeline %s due to negative cache", tl))
			continue
		}

		fetchTargets = append(fetchTargets, tl)
	}

	if len(fetchTargets) == 0 {
		return result, nil
	}

	manifests := make([]chunkline.Manifest, len(fetchTargets))
	targets := make([]any, len(fetchTargets))
	for i := range manifests {
		targets[i] = &manifests[i]
	}

	err := r.client.GetResourceBatch(ctx, fetchTargets, "application/chunkline+json", nil, targets)
	if err == nil {
		for i, tl := range fetchTargets {
			manifest := manifests[i]
			result[tl] = manifest
			if err := r.cache.setCachedManifest(tl, manifest); err != nil {
				span.RecordError(fmt.Errorf("failed to write chunkline manifest cache for %s: %w", tl, err))
			}
		}
		return result, nil
	}

	span.RecordError(errors.Join(fmt.Errorf("failed to fetch chunkline manifests batch"), err))

	for _, tl := range fetchTargets {
		var manifest chunkline.Manifest
		err = r.client.GetResource(ctx, tl, "application/chunkline+json", nil, &manifest)
		if err != nil {
			span.RecordError(errors.Join(fmt.Errorf("failed to fetch chunkline manifest for %s", tl), err))
			if cacheErr := r.cache.setNegativeCache(tl); cacheErr != nil {
				span.RecordError(fmt.Errorf("failed to write chunkline negative cache for %s: %w", tl, cacheErr))
			}
			continue
		}
		result[tl] = manifest
		if err := r.cache.setCachedManifest(tl, manifest); err != nil {
			span.RecordError(fmt.Errorf("failed to write chunkline manifest cache for %s: %w", tl, err))
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

	if len(timelines) == 0 {
		return make(map[string]string), nil
	}

	manifests, err := r.ResolveTimelines(ctx, timelines)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	keys := make([]string, 0, len(timelines))
	cacheable := make(map[string]bool, len(timelines))
	for _, tl := range timelines {
		manifest, ok := manifests[tl]
		if !ok {
			continue
		}

		queryChunk := manifest.Time2Chunk(until)
		if manifest.LastChunk != nil && queryChunk > *manifest.LastChunk {
			queryChunk = *manifest.LastChunk
		}

		key := chunklineIteratorCacheKey(tl, queryChunk)
		if r.shouldCacheChunk(tl, manifest, queryChunk) {
			keys = append(keys, key)
			cacheable[key] = true
		}
	}

	cacheItems, err := r.cache.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(fmt.Errorf("failed to read chunkline iterator cache: %w", err))
	}

	results := make(map[string]string)
	fetchTasks := make([]chunklineBatchTask, 0)
	fetchTaskChunk := make(map[string]int64, len(timelines))
	fetchTaskManifest := make(map[string]chunkline.Manifest, len(timelines))
	for _, tl := range timelines {

		manifest, ok := manifests[tl]
		if !ok {
			err := fmt.Errorf("missing chunkline manifest for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if manifest.Descending == nil || manifest.Descending.Iterator == "" {
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

		cacheKey := chunklineIteratorCacheKey(tl, queryChunk)
		if cacheable[cacheKey] {
			if item := cacheItems[cacheKey]; item != nil {
				iterator := strings.TrimSpace(string(item.Value))
				results[tl] = iterator
				continue
			}
		}

		endpoint, err := r.cache.resolveEndpoint(ctx, tl, strings.ReplaceAll(manifest.Descending.Iterator, "{chunk}", fmt.Sprintf("%d", queryChunk)))
		if err != nil {
			span.RecordError(fmt.Errorf("failed to resolve chunkline iterator endpoint for timeline %s: %w", tl, err))
			continue
		}
		timeline := tl
		chunkID := queryChunk
		manifestForFetch := manifest
		fetchTasks = append(fetchTasks, chunklineBatchTask{
			key:      timeline,
			endpoint: endpoint,
			fallback: func(ctx context.Context) ([]byte, error) {
				itr, err := r.cache.fetchIterator(ctx, timeline, manifestForFetch, chunkID)
				if err != nil {
					return nil, err
				}
				return []byte(itr), nil
			},
		})
		fetchTaskChunk[timeline] = queryChunk
		fetchTaskManifest[timeline] = manifest
	}

	fetched, fetchErrs := r.fetchChunklineBatch(ctx, fetchTasks)
	for _, task := range fetchTasks {
		tl := task.key
		if err := fetchErrs[tl]; err != nil {
			span.RecordError(fmt.Errorf("failed to fetch chunkline iterator for timeline %s: %w", tl, err))
			continue
		}

		itr := strings.TrimSpace(string(fetched[tl]))
		results[tl] = itr

		manifest := fetchTaskManifest[tl]
		queryChunk := fetchTaskChunk[tl]
		if r.shouldCacheChunk(tl, manifest, queryChunk) {
			if err := r.cache.setIteratorCache(tl, queryChunk, itr); err != nil {
				span.RecordError(fmt.Errorf("failed to write chunkline iterator cache for timeline %s: %w", tl, err))
			}
		}
	}
	return results, nil
}

func (r *resolver) shouldCacheChunk(timeline string, manifest chunkline.Manifest, chunkID int64) bool {
	latestChunk := latestChunkID(manifest, time.Now())
	if chunkID != latestChunk {
		return true
	}
	if r.currentSubscriptions == nil {
		return false
	}
	for _, current := range r.currentSubscriptions.GetCurrentSubscriptions() {
		if current == timeline {
			return true
		}
	}
	return false
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
	cacheable := make(map[string]bool, len(query))
	for tl, itr := range query {
		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
			continue
		}

		manifest, ok := manifests[tl]
		if !ok {
			continue
		}

		key := chunklineBodyCacheKey(tl, chunkID)
		if r.shouldCacheChunk(tl, manifest, chunkID) {
			keys = append(keys, key)
			cacheable[key] = true
		}
	}

	cacheItems, err := r.cache.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(fmt.Errorf("failed to read chunkline body cache: %w", err))
	}

	result := make(map[string]chunkline.BodyChunk)
	fetchTasks := make([]chunklineBatchTask, 0)
	fetchTaskChunk := make(map[string]int64, len(query))
	fetchTaskManifest := make(map[string]chunkline.Manifest, len(query))
	for tl, itr := range query {

		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
			continue
		}

		cacheKey := chunklineBodyCacheKey(tl, chunkID)
		if cacheable[cacheKey] {
			if item := cacheItems[cacheKey]; item != nil {
				items, err := DecodeBodyCache(item.Value)
				if err != nil {
					span.RecordError(fmt.Errorf("failed to decode chunkline body cache for timeline %s: %w", tl, err))
					_ = r.cache.mc.Delete(chunklineBodyCacheKey(tl, chunkID))
				} else {
					result[tl] = chunkline.BodyChunk{
						URI:     tl,
						ChunkID: chunkID,
						Items:   items,
					}
					continue
				}
			}
		}

		manifest, ok := manifests[tl]
		if !ok {
			err := fmt.Errorf("missing chunkline manifest for timeline %s", tl)
			span.RecordError(err)
			continue
		}

		if manifest.Descending == nil || manifest.Descending.Body == "" {
			err := fmt.Errorf("timeline %s does not support descending body retrieval", tl)
			span.RecordError(err)
			continue
		}

		endpoint, err := r.cache.resolveEndpoint(ctx, tl, strings.ReplaceAll(manifest.Descending.Body, "{chunk}", itr))
		if err != nil {
			span.RecordError(fmt.Errorf("failed to resolve chunkline body endpoint for timeline %s: %w", tl, err))
			continue
		}
		timeline := tl
		iterator := itr
		manifestForFetch := manifest
		fetchTasks = append(fetchTasks, chunklineBatchTask{
			key:      timeline,
			endpoint: endpoint,
			fallback: func(ctx context.Context) ([]byte, error) {
				items, err := r.cache.fetchChunkBody(ctx, timeline, manifestForFetch, iterator)
				if err != nil {
					return nil, err
				}
				return json.Marshal(items)
			},
		})
		fetchTaskChunk[timeline] = chunkID
		fetchTaskManifest[timeline] = manifest
	}

	fetched, fetchErrs := r.fetchChunklineBatch(ctx, fetchTasks)
	for _, task := range fetchTasks {
		tl := task.key
		if err := fetchErrs[tl]; err != nil {
			span.RecordError(fmt.Errorf("failed to fetch chunkline body for timeline %s: %w", tl, err))
			continue
		}

		var items []chunkline.BodyItem
		if err := json.Unmarshal(fetched[tl], &items); err != nil {
			span.RecordError(fmt.Errorf("failed to decode chunkline body for timeline %s: %w", tl, err))
			continue
		}

		chunkID := fetchTaskChunk[tl]
		result[tl] = chunkline.BodyChunk{
			URI:     tl,
			ChunkID: chunkID,
			Items:   items,
		}

		manifest := fetchTaskManifest[tl]
		if !r.shouldCacheChunk(tl, manifest, chunkID) {
			continue
		}

		if err := r.cache.setBodyCache(tl, chunkID, items); err != nil {
			span.RecordError(fmt.Errorf("failed to write chunkline body cache for timeline %s: %w", tl, err))
		}

	}
	return result, nil
}

func EncodeBodyCache(items []chunkline.BodyItem) ([]byte, error) {
	if len(items) == 0 {
		return []byte{}, nil
	}
	body, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if len(body) < 2 {
		return nil, fmt.Errorf("invalid chunkline body JSON")
	}
	return []byte("," + string(body[1:len(body)-1])), nil
}

func DecodeBodyCache(body []byte) ([]chunkline.BodyItem, error) {
	if len(body) == 0 {
		return []chunkline.BodyItem{}, nil
	}
	if body[0] != ',' {
		return nil, fmt.Errorf("invalid chunkline body cache")
	}
	if len(body) == 1 {
		return []chunkline.BodyItem{}, nil
	}
	// Tolerate a trailing comma that may appear if a prepend was performed
	// onto a previously-empty (",") cache entry.
	end := len(body)
	if body[end-1] == ',' {
		end--
	}
	cacheStr := "[" + string(body[1:end]) + "]"

	var items []chunkline.BodyItem
	err := json.Unmarshal([]byte(cacheStr), &items)
	return items, err
}
