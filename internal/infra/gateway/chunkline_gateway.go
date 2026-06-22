package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
)

type ChunklineGateway struct {
	resolver *chunkline.Client
}

func NewChunklineGateway(cl *client.Client, mc *memcache.Client) *ChunklineGateway {
	r := &resolver{
		client: cl,
		mc:     mc,
	}
	return &ChunklineGateway{
		resolver: chunkline.NewClient(r),
	}
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client *client.Client
	mc     *memcache.Client
}


func manifestCacheKey(timeline string) string {
	return "chunkline_manifest:" + timeline
}

func itrCacheKey(timeline string, chunkID int64) string {
	return fmt.Sprintf("chunkline_itr:%s:%d", timeline, chunkID)
}

func bodyCacheKey(timeline string, chunkID int64) string {
	return fmt.Sprintf("chunkline_body:%s:%d", timeline, chunkID)
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
			Expiration: int32(time.Hour.Seconds()),
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

	type ItrQuery struct {
		Manifest chunkline.Manifest
		ChunkID  int64
	}

	queries := make(map[string]ItrQuery)
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

		queries[tl] = ItrQuery {
			Manifest: manifest,
			ChunkID:  queryChunk,
		}
		keys = append(keys, itrCacheKey(tl, queryChunk))
	}

	cachedItems, err := r.mc.GetMulti(keys)
	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		span.RecordError(fmt.Errorf("failed to get iterators from cache: %w", err))
	}

	results := make(map[string]string)

	requestsByDomain := make(map[string]map[string]string) // domain -> timeline -> query path

	for tl, query := range queries {
		cacheKey := itrCacheKey(tl, query.ChunkID)
		if item, found := cachedItems[cacheKey]; found {
			results[tl] = string(item.Value)
			continue
		}

		relPath, err := concrnt.RenderURITemplate(query.Manifest.Descending.Iterator, map[string]string{
			"chunk": fmt.Sprintf("%d", query.ChunkID),
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
	}

	if len(requestsByDomain) == 0 {
		return results, nil // all results were cached
	}

	responces, err := r.client.BatchGet(ctx, requestsByDomain)
	if err != nil {
		span.RecordError(fmt.Errorf("batch request for iterators failed: %w", err))
		return results, nil // return what we have from cache
	}

	for tl, endpoint := range queries {
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

		cacheItem := &memcache.Item{
			Key:        itrCacheKey(tl, endpoint.ChunkID),
			Value:      []byte(iterator),
			Expiration: int32(time.Minute.Seconds()),
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

	result := make(map[string]chunkline.BodyChunk)
	for tl, itr := range query {

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

		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			span.RecordError(fmt.Errorf("invalid chunk ID %s for timeline %s: %w", itr, tl, err))
			continue
		}

		result[tl] = chunkline.BodyChunk{
			URI:     tl,
			ChunkID: chunkID,
			Items:   items,
		}

	}
	return result, nil
}
