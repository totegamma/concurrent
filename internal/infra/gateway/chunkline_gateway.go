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
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/patrickmn/go-cache"

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
)

type ChunklineGateway struct {
	client   *client.Client
	cache    *cache.Cache
	rcb      *resolver
	resolver *chunkline.Client
}

type subscriptionProvider interface {
	GetCurrentSubscriptions() []string
}

func NewChunklineGateway(cl *client.Client, mc *memcache.Client, subs subscriptionProvider) *ChunklineGateway {
	r := &resolver{
		client:        cl,
		cache:         cache.New(10*time.Minute, 15*time.Minute),
		mc:            mc,
		subscriptions: subs,
	}
	return &ChunklineGateway{
		client:   cl,
		cache:    r.cache,
		rcb:      r,
		resolver: chunkline.NewClient(r),
	}
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// PrefetchChunks warms the memcached chunkline cache for the given timelines by
// fetching the iterator and body for the current chunk from the remote source.
// It is intended to be called when a new realtime subscription is established.
func (g *ChunklineGateway) PrefetchChunks(ctx context.Context, timelines []string) {
	if len(timelines) == 0 || g.rcb == nil {
		return
	}
	itrs, err := g.rcb.LookupChunkItrs(ctx, timelines, time.Now().UTC())
	if err != nil {
		slog.DebugContext(ctx, "chunkline prefetch: iterator lookup failed", slog.String("error", err.Error()))
	}
	if len(itrs) == 0 {
		return
	}
	if _, err := g.rcb.LoadChunkBodies(ctx, itrs); err != nil {
		slog.DebugContext(ctx, "chunkline prefetch: body load failed", slog.String("error", err.Error()))
	}
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client        *client.Client
	cache         *cache.Cache
	mc            *memcache.Client
	subscriptions subscriptionProvider
}

func (r *resolver) ResolveTimelines(ctx context.Context, timelines []string) (map[string]chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.ResolveTimelines")
	defer span.End()

	result := make(map[string]chunkline.Manifest)
	remaining := []string{}

	for _, tl := range timelines {
		if cached, found := r.cache.Get(tl); found {
			result[tl] = cached.(chunkline.Manifest)
		} else {
			remaining = append(remaining, tl)
		}
	}

	for _, tl := range remaining {
		var manifest chunkline.Manifest
		err := r.client.GetResource(ctx, tl, "application/chunkline+json", nil, &manifest)
		if err != nil {
			span.RecordError(errors.Join(fmt.Errorf("failed to fetch chunkline manifest for %s", tl), err))
			continue
		}
		if manifest.ChunkSize <= 0 {
			span.RecordError(fmt.Errorf("chunkline manifest for %s has invalid chunk size %d", tl, manifest.ChunkSize))
			continue
		}
		result[tl] = manifest
		r.cache.Set(tl, manifest, cache.DefaultExpiration)
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

	results := make(map[string]string)
	keys := make([]string, 0, len(timelines))

	for _, tl := range timelines {
		manifest, ok := manifests[tl]
		if !ok {
			continue
		}
		queryChunk := manifest.Time2Chunk(until)
		if manifest.LastChunk != nil && queryChunk > *manifest.LastChunk {
			queryChunk = *manifest.LastChunk
		}
		key := chunkline.IteratorCacheKey(tl, queryChunk)
		keys = append(keys, key)
	}

	cacheItems := map[string]*memcache.Item{}
	if r.mc != nil && len(keys) > 0 {
		cacheItems, err = r.mc.GetMulti(keys)
		if err != nil {
			span.RecordError(err)
		}
	}

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

		if item := cacheItems[chunkline.IteratorCacheKey(tl, queryChunk)]; item != nil {
			results[tl] = string(item.Value)
			continue
		}

		refPath := strings.ReplaceAll(manifest.Descending.Iterator, "{chunk}", fmt.Sprintf("%d", queryChunk))

		ref, err := url.Parse(refPath)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid iterator URI template for timeline %s: %w", tl, err))
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

		req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to create request for timeline %s: %w", tl, err))
			continue
		}

		resp, err := r.client.GetClient().Do(req)
		if err != nil {
			span.RecordError(fmt.Errorf("HTTP request failed for timeline %s: %w", tl, err))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			span.RecordError(fmt.Errorf("non-200 response for timeline %s: %d", tl, resp.StatusCode))
			continue
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read response body for timeline %s: %w", tl, err))
			continue
		}

		itr := strings.TrimSpace(string(bytes))
		results[tl] = itr

		if r.mc != nil && r.shouldCacheChunk(tl, queryChunk, manifest) {
			err = r.mc.Set(&memcache.Item{
				Key:        chunkline.IteratorCacheKey(tl, queryChunk),
				Value:      []byte(itr),
				Expiration: chunkline.CacheTTL,
			})
			if err != nil {
				span.RecordError(err)
			}
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

	result := make(map[string]chunkline.BodyChunk)
	keys := make([]string, 0, len(query))
	for tl, itr := range query {
		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			continue
		}
		key := chunkline.BodyCacheKey(tl, chunkID)
		keys = append(keys, key)
	}

	cacheItems := map[string]*memcache.Item{}
	if r.mc != nil && len(keys) > 0 {
		cacheItems, err = r.mc.GetMulti(keys)
		if err != nil {
			span.RecordError(err)
		}
	}

	for tl, itr := range query {
		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
			continue
		}

		if item := cacheItems[chunkline.BodyCacheKey(tl, chunkID)]; item != nil {
			items, err := chunkline.DecodeBodyCache(item.Value)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to decode cached chunk body for timeline %s: %w", tl, err))
				continue
			}
			result[tl] = chunkline.BodyChunk{
				URI:     tl,
				ChunkID: chunkID,
				Items:   items,
			}
			continue
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

		req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to create request for timeline %s: %w", tl, err))
			continue
		}

		resp, err := r.client.GetClient().Do(req)
		if err != nil {
			span.RecordError(fmt.Errorf("HTTP request failed for timeline %s: %w", tl, err))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			span.RecordError(fmt.Errorf("non-200 response for timeline %s: %d", tl, resp.StatusCode))
			continue
		}

		var items []chunkline.BodyItem
		err = json.NewDecoder(resp.Body).Decode(&items)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to decode chunk body for timeline %s: %w", tl, err))
			continue
		}

		result[tl] = chunkline.BodyChunk{
			URI:     tl,
			ChunkID: chunkID,
			Items:   items,
		}

		if r.mc != nil && r.shouldCacheChunk(tl, chunkID, manifest) {
			cacheBody, err := chunkline.EncodeBodyCache(items)
			if err != nil {
				span.RecordError(fmt.Errorf("failed to encode chunk body cache for timeline %s: %w", tl, err))
				continue
			}
			err = r.mc.Set(&memcache.Item{
				Key:        chunkline.BodyCacheKey(tl, chunkID),
				Value:      cacheBody,
				Expiration: chunkline.CacheTTL,
			})
			if err != nil {
				span.RecordError(err)
			}
		}

	}
	return result, nil
}

func (r *resolver) shouldCacheChunk(timeline string, chunkID int64, manifest chunkline.Manifest) bool {
	if manifest.ChunkSize <= 0 {
		return false
	}
	if chunkID != manifest.Time2Chunk(time.Now().UTC()) {
		return true
	}
	return r.isTimelineSubscribed(timeline)
}

func (r *resolver) isTimelineSubscribed(timeline string) bool {
	if r.subscriptions == nil {
		return false
	}
	for _, subscription := range r.subscriptions.GetCurrentSubscriptions() {
		subscription = strings.TrimSuffix(subscription, "*")
		if subscription == timeline || strings.HasPrefix(timeline, subscription) {
			return true
		}
	}
	return false
}
