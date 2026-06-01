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
	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
)

const (
	chunklineManifestCacheTTL = int32(24 * 60 * 60) // 1 day
	chunklineNegativeCacheTTL = int32(60 * 60)      // 1 hour
	chunklineIteratorCacheTTL = int32(24 * 60 * 60) // 1 day
	chunklineBodyCacheTTL     = int32(24 * 60 * 60) // 1 day
)

type memcacheStore interface {
	Get(key string) (*memcache.Item, error)
	GetMulti(keys []string) (map[string]*memcache.Item, error)
	Set(item *memcache.Item) error
	Delete(key string) error
}

type ChunklineCacheService struct {
	client *client.Client
	mc     memcacheStore
}

func NewChunklineCacheService(cl *client.Client, mc *memcache.Client) *ChunklineCacheService {
	return &ChunklineCacheService{
		client: cl,
		mc:     mc,
	}
}

func newChunklineCacheService(cl *client.Client, mc memcacheStore) *ChunklineCacheService {
	return &ChunklineCacheService{
		client: cl,
		mc:     mc,
	}
}

func (s *ChunklineCacheService) CacheCreatedEvent(ctx context.Context, event concrnt.Event) error {
	if event.Type != "created" || event.Source == "" || event.URI == "" {
		return nil
	}

	item, ok := bodyItemFromCreatedEvent(event)
	if !ok {
		return nil
	}

	manifest, _, err := s.getManifest(ctx, event.Source, false)
	if err != nil {
		return err
	}

	eventChunk := manifest.Time2Chunk(item.Timestamp)
	if manifest.LastChunk != nil && eventChunk > *manifest.LastChunk {
		manifest, _, err = s.getManifest(ctx, event.Source, true)
		if err != nil {
			return err
		}
	}

	latestChunk := latestChunkID(manifest, item.Timestamp)
	if eventChunk != latestChunk {
		return nil
	}

	body, found, err := s.getBodyCache(event.Source, eventChunk)
	if err != nil {
		_ = s.deleteBodyCache(event.Source, eventChunk)
		found = false
	}
	if !found {
		body, err = s.fetchChunkBody(ctx, event.Source, manifest, strconv.FormatInt(eventChunk, 10))
		if err != nil {
			return err
		}
	}

	duplicate := false
	for _, existing := range body {
		if existing.ID() == item.ID() {
			duplicate = true
			break
		}
	}

	if !duplicate {
		body = append([]chunkline.BodyItem{item}, body...)
	}
	if err := s.setBodyCache(event.Source, eventChunk, body); err != nil {
		return err
	}
	return s.setIteratorCache(event.Source, eventChunk, strconv.FormatInt(eventChunk, 10))
}

func (s *ChunklineCacheService) EnsureLatestCache(ctx context.Context, timeline string) error {
	manifest, _, err := s.getManifest(ctx, timeline, false)
	if err != nil {
		return err
	}

	chunkID := latestChunkID(manifest, time.Now())
	_, found, err := s.getBodyCache(timeline, chunkID)
	if err != nil {
		_ = s.deleteBodyCache(timeline, chunkID)
		found = false
	}
	if !found {
		if err := s.setBodyCache(timeline, chunkID, []chunkline.BodyItem{}); err != nil {
			return err
		}
	}
	return s.setIteratorCache(timeline, chunkID, strconv.FormatInt(chunkID, 10))
}

func (s *ChunklineCacheService) LatestCacheExists(ctx context.Context, timeline string) (bool, error) {
	manifest, _, err := s.getManifest(ctx, timeline, false)
	if err != nil {
		return false, err
	}

	chunkID := latestChunkID(manifest, time.Now())
	_, err = s.mc.Get(chunklineBodyCacheKey(timeline, chunkID))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *ChunklineCacheService) LatestChunkStaleForRetention(ctx context.Context, timeline string, now time.Time) (bool, error) {
	manifest, _, err := s.getManifest(ctx, timeline, false)
	if err != nil {
		return false, err
	}
	if manifest.ChunkSize <= 0 {
		return false, nil
	}

	chunkID := latestChunkID(manifest, now)
	elapsed := now.Sub(manifest.Chunk2Time(chunkID))
	return elapsed >= time.Duration(float64(manifest.ChunkSize)*0.9*float64(time.Second)), nil
}

func (s *ChunklineCacheService) DeleteLatestCache(ctx context.Context, timeline string) error {
	manifest, _, err := s.getManifest(ctx, timeline, false)
	if err != nil {
		return err
	}

	chunkID := latestChunkID(manifest, time.Now())
	var result error
	if err := s.mc.Delete(chunklineBodyCacheKey(timeline, chunkID)); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		result = errors.Join(result, err)
	}
	if err := s.mc.Delete(chunklineIteratorCacheKey(timeline, chunkID)); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		result = errors.Join(result, err)
	}
	return result
}

func (s *ChunklineCacheService) getManifest(ctx context.Context, timeline string, refresh bool) (chunkline.Manifest, bool, error) {
	if !refresh {
		cached, found, err := s.getCachedManifest(timeline)
		if err != nil {
			return chunkline.Manifest{}, false, err
		}
		if found {
			return cached, true, nil
		}
	}

	var manifest chunkline.Manifest
	opts := &client.Options{NoCache: refresh}
	if err := s.client.GetResource(ctx, timeline, "application/chunkline+json", opts, &manifest); err != nil {
		return manifest, false, err
	}
	if err := s.setCachedManifest(timeline, manifest); err != nil {
		return manifest, false, err
	}
	return manifest, false, nil
}

func (s *ChunklineCacheService) getCachedManifest(timeline string) (chunkline.Manifest, bool, error) {
	var manifest chunkline.Manifest

	item, err := s.mc.Get(chunklineCacheKey("manifest", timeline))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return manifest, false, nil
	}
	if err != nil {
		return manifest, false, err
	}

	if err := json.Unmarshal(item.Value, &manifest); err != nil {
		_ = s.mc.Delete(chunklineCacheKey("manifest", timeline))
		return manifest, false, err
	}

	return manifest, true, nil
}

func (s *ChunklineCacheService) setCachedManifest(timeline string, manifest chunkline.Manifest) error {
	value, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	return s.mc.Set(&memcache.Item{
		Key:        chunklineCacheKey("manifest", timeline),
		Value:      value,
		Expiration: chunklineManifestCacheTTL,
	})
}

func (s *ChunklineCacheService) isNegativeCached(timeline string) (bool, error) {
	_, err := s.mc.Get(chunklineCacheKey("negative", timeline))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *ChunklineCacheService) setNegativeCache(timeline string) error {
	return s.mc.Set(&memcache.Item{
		Key:        chunklineCacheKey("negative", timeline),
		Value:      []byte("1"),
		Expiration: chunklineNegativeCacheTTL,
	})
}

func (s *ChunklineCacheService) getBodyCache(timeline string, chunkID int64) ([]chunkline.BodyItem, bool, error) {
	item, err := s.mc.Get(chunklineBodyCacheKey(timeline, chunkID))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	items, err := DecodeBodyCache(item.Value)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

func (s *ChunklineCacheService) setBodyCache(timeline string, chunkID int64, items []chunkline.BodyItem) error {
	cacheBody, err := EncodeBodyCache(items)
	if err != nil {
		return err
	}

	return s.mc.Set(&memcache.Item{
		Key:        chunklineBodyCacheKey(timeline, chunkID),
		Value:      cacheBody,
		Expiration: chunklineBodyCacheTTL,
	})
}

func (s *ChunklineCacheService) deleteBodyCache(timeline string, chunkID int64) error {
	err := s.mc.Delete(chunklineBodyCacheKey(timeline, chunkID))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil
	}
	return err
}

func (s *ChunklineCacheService) setIteratorCache(timeline string, chunkID int64, iterator string) error {
	return s.mc.Set(&memcache.Item{
		Key:        chunklineIteratorCacheKey(timeline, chunkID),
		Value:      []byte(iterator),
		Expiration: chunklineIteratorCacheTTL,
	})
}

func (s *ChunklineCacheService) fetchChunkBody(ctx context.Context, timeline string, manifest chunkline.Manifest, itr string) ([]chunkline.BodyItem, error) {
	if manifest.Descending == nil || manifest.Descending.Body == "" {
		return nil, fmt.Errorf("timeline %s does not support descending body retrieval", timeline)
	}

	endpoint, err := s.resolveEndpoint(ctx, timeline, strings.ReplaceAll(manifest.Descending.Body, "{chunk}", itr))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.GetClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 response for timeline %s: %d", timeline, resp.StatusCode)
	}

	var items []chunkline.BodyItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ChunklineCacheService) fetchIterator(ctx context.Context, timeline string, manifest chunkline.Manifest, chunkID int64) (string, error) {
	if manifest.Descending == nil || manifest.Descending.Iterator == "" {
		return "", fmt.Errorf("timeline %s does not support descending iteration", timeline)
	}

	endpoint, err := s.resolveEndpoint(ctx, timeline, strings.ReplaceAll(manifest.Descending.Iterator, "{chunk}", fmt.Sprintf("%d", chunkID)))
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return "", err
	}

	resp, err := s.client.GetClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-200 response for timeline %s: %d", timeline, resp.StatusCode)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes)), nil
}

func (s *ChunklineCacheService) resolveEndpoint(ctx context.Context, timeline string, refPath string) (*url.URL, error) {
	ref, err := url.Parse(refPath)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(timeline)
	if err != nil {
		return nil, err
	}

	endpoint := base.ResolveReference(ref)
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		host, err := s.client.ResolveResourceHost(ctx, timeline)
		if err != nil {
			return nil, err
		}
		endpoint.Scheme = "https"
		endpoint.Host = host
	}

	return endpoint, nil
}

func chunklineCacheKey(kind string, timeline string) string {
	sum := sha256.Sum256([]byte(timeline))
	return fmt.Sprintf("chunkline:%s:%x", kind, sum)
}

func chunklineIteratorCacheKey(timeline string, chunkID int64) string {
	sum := sha256.Sum256([]byte(timeline))
	return fmt.Sprintf("chunkline:iterator:%x:%d", sum, chunkID)
}

func chunklineBodyCacheKey(timeline string, chunkID int64) string {
	sum := sha256.Sum256([]byte(timeline))
	return fmt.Sprintf("chunkline:body:%x:%d", sum, chunkID)
}

func latestChunkID(manifest chunkline.Manifest, reference time.Time) int64 {
	if manifest.LastChunk != nil {
		return *manifest.LastChunk
	}
	return manifest.Time2Chunk(reference)
}

func bodyItemFromCreatedEvent(event concrnt.Event) (chunkline.BodyItem, bool) {
	sd, ok := event.References[event.URI]
	if !ok {
		return chunkline.BodyItem{}, false
	}

	var doc concrnt.Document[json.RawMessage]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		return chunkline.BodyItem{}, false
	}
	if doc.CreatedAt.IsZero() || doc.Schema == "" {
		return chunkline.BodyItem{}, false
	}

	return chunkline.BodyItem{
		Timestamp:   doc.CreatedAt,
		ContentType: doc.Schema,
		Href:        event.URI,
	}, true
}
