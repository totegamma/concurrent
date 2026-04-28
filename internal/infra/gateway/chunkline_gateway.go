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

	"github.com/patrickmn/go-cache"

	"github.com/totegamma/concrnt-playground/chunkline"
	"github.com/totegamma/concrnt-playground/client"
)

type ChunklineGateway struct {
	client   *client.Client
	cache    *cache.Cache
	resolver *chunkline.Client
}

func NewChunklineGateway(cl *client.Client) *ChunklineGateway {
	r := &resolver{
		client: cl,
		cache:  cache.New(10*time.Minute, 15*time.Minute),
	}
	return &ChunklineGateway{
		client:   cl,
		cache:    r.cache,
		resolver: chunkline.NewClient(r),
	}
}

func (g *ChunklineGateway) QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {
	return g.resolver.QueryDescending(ctx, uris, until, limit)
}

// resolver implements chunkline resolver callbacks.
type resolver struct {
	client *client.Client
	cache  *cache.Cache
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

		results[tl] = strings.TrimSpace(string(bytes))
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
