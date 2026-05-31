package gateway

import (
	"context"
	"crypto/sha256"
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

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
)

const (
	chunklineManifestCacheTTL = int32(24 * 60 * 60) // 1 day
	chunklineNegativeCacheTTL = int32(60 * 60)      // 1 hour
	chunklineIteratorCacheTTL = int32(24 * 60 * 60) // 1 day
)

type ChunklineGateway struct {
	client   *client.Client
	resolver *chunkline.Client
}

func NewChunklineGateway(
	cl *client.Client,
	mc *memcache.Client,
) *ChunklineGateway {
	r := &resolver{
		client: cl,
		mc:     mc,
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
	client *client.Client
	mc     *memcache.Client
}

func (r *resolver) ResolveTimelines(ctx context.Context, timelines []string) (map[string]chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "ChunklineResolver.ResolveTimelines")
	defer span.End()

	result := make(map[string]chunkline.Manifest)
	remaining := []string{}

	for _, tl := range timelines {
		cached, found, err := r.getCachedManifest(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read chunkline manifest cache for %s: %w", tl, err))
		}
		if found {
			result[tl] = cached
		} else {
			remaining = append(remaining, tl)
		}
	}

	for _, tl := range remaining {
		negativeCached, err := r.isNegativeCached(tl)
		if err != nil {
			span.RecordError(fmt.Errorf("failed to read chunkline negative cache for %s: %w", tl, err))
		}
		if negativeCached {
			span.AddEvent(fmt.Sprintf("Skipping timeline %s due to negative cache", tl))
			continue
		}

		var manifest chunkline.Manifest
		err = r.client.GetResource(ctx, tl, "application/chunkline+json", nil, &manifest)
		if err != nil {
			span.RecordError(errors.Join(fmt.Errorf("failed to fetch chunkline manifest for %s", tl), err))
			if cacheErr := r.setNegativeCache(tl); cacheErr != nil {
				span.RecordError(fmt.Errorf("failed to write chunkline negative cache for %s: %w", tl, cacheErr))
			}
			continue
		}
		result[tl] = manifest
		if err := r.setCachedManifest(tl, manifest); err != nil {
			span.RecordError(fmt.Errorf("failed to write chunkline manifest cache for %s: %w", tl, err))
		}
	}
	return result, nil

}

func (r *resolver) getCachedManifest(timeline string) (chunkline.Manifest, bool, error) {
	var manifest chunkline.Manifest

	item, err := r.mc.Get(chunklineCacheKey("manifest", timeline))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return manifest, false, nil
	}
	if err != nil {
		return manifest, false, err
	}

	if err := json.Unmarshal(item.Value, &manifest); err != nil {
		_ = r.mc.Delete(chunklineCacheKey("manifest", timeline))
		return manifest, false, err
	}

	return manifest, true, nil
}

func (r *resolver) setCachedManifest(timeline string, manifest chunkline.Manifest) error {

	value, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	return r.mc.Set(&memcache.Item{
		Key:        chunklineCacheKey("manifest", timeline),
		Value:      value,
		Expiration: chunklineManifestCacheTTL,
	})
}

func (r *resolver) isNegativeCached(timeline string) (bool, error) {

	_, err := r.mc.Get(chunklineCacheKey("negative", timeline))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *resolver) setNegativeCache(timeline string) error {

	return r.mc.Set(&memcache.Item{
		Key:        chunklineCacheKey("negative", timeline),
		Value:      []byte("1"),
		Expiration: chunklineNegativeCacheTTL,
	})
}

func chunklineCacheKey(kind string, timeline string) string {
	sum := sha256.Sum256([]byte(timeline))
	return fmt.Sprintf("chunkline:%s:%x", kind, sum)
}

func chunklineIteratorCacheKey(timeline string, chunkID int64) string {
	sum := sha256.Sum256([]byte(timeline))
	return fmt.Sprintf("chunkline:iterator:%x:%d", sum, chunkID)
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
		keys = append(keys, key)
	}

	cacheItems, err := r.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(fmt.Errorf("failed to read chunkline iterator cache: %w", err))
	}

	results := make(map[string]string)
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

		if item := cacheItems[chunklineIteratorCacheKey(tl, queryChunk)]; item != nil {
			iterator := strings.TrimSpace(string(item.Value))
			results[tl] = iterator
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

		if err := r.mc.Set(&memcache.Item{
			Key:        chunklineIteratorCacheKey(tl, queryChunk),
			Value:      []byte(itr),
			Expiration: chunklineIteratorCacheTTL,
		}); err != nil {
			span.RecordError(fmt.Errorf("failed to write chunkline iterator cache for timeline %s: %w", tl, err))
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
