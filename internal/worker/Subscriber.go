package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/concrnt/concrnt/schemas"
	"github.com/gorilla/websocket"
	"github.com/patrickmn/go-cache"
)

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

const (
	manifestCacheExpiration = 10 * time.Minute
	manifestCacheCleanup    = 15 * time.Minute
)

type SubState struct {
	Prefixes   []string
	Connection *websocket.Conn
	CancelFunc context.CancelFunc
}

// ChunklinePrefetcher warms the memcached chunkline cache for newly
// subscribed timelines so that subsequent realtime updates can prepend
// onto an existing entry.
type ChunklinePrefetcher interface {
	PrefetchChunks(ctx context.Context, timelines []string)
}

type Subscriber struct {
	Subscriptions map[string]*SubState
	Config        *domain.Config
	Client        *client.Client
	Signal        *service.SignalService
	Memcache      *memcache.Client
	Prefetcher    ChunklinePrefetcher
	manifestCache *cache.Cache
}

func NewSubscriber(
	config *domain.Config,
	client *client.Client,
	signal *service.SignalService,
	mc *memcache.Client,
	prefetcher ChunklinePrefetcher,
) *Subscriber {
	return &Subscriber{
		Subscriptions: make(map[string]*SubState),
		Config:        config,
		Client:        client,
		Signal:        signal,
		Memcache:      mc,
		Prefetcher:    prefetcher,
		manifestCache: cache.New(manifestCacheExpiration, manifestCacheCleanup),
	}
}

func (s *Subscriber) Start(ctx context.Context) {
	go s.keeperRoutine(ctx)
	go s.epochRoutine()
}

func (s *Subscriber) keeperRoutine(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for range ticker.C {
		s.createInsufficientSubscriptions(ctx)
		for domain := range s.Subscriptions {
			if s.Subscriptions[domain].Connection == nil {
				slog.Info(
					fmt.Sprintf("broken connection found: %s", domain),
					slog.String("module", "worker"),
					slog.String("group", "realtime"),
				)
				s.subscribeRemote(ctx, domain, s.Subscriptions[domain].Prefixes)
			}
		}
	}
}

func (s *Subscriber) createInsufficientSubscriptions(ctx context.Context) {

	currentSubscriptions := s.Signal.GetCurrentSubscriptions()
	changedRemotes := make([]string, 0)

	for _, prefix := range currentSubscriptions {
		host, err := s.Client.ResolveResourceHost(ctx, prefix)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to resolve resource host for prefix %s: %v", prefix, err),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)
			continue
		}

		if host == s.Config.FQDN {
			continue
		}

		if _, ok := s.Subscriptions[host]; !ok {
			s.Subscriptions[host] = &SubState{
				Prefixes: []string{prefix},
			}
			if !slices.Contains(changedRemotes, host) {
				changedRemotes = append(changedRemotes, host)
			}
		} else {
			if !slices.Contains(s.Subscriptions[host].Prefixes, prefix) {
				s.Subscriptions[host].Prefixes = append(s.Subscriptions[host].Prefixes, prefix)
				if !slices.Contains(changedRemotes, host) {
					changedRemotes = append(changedRemotes, host)
				}
			}
		}
	}

	for _, host := range changedRemotes {
		slog.Debug(
			fmt.Sprintf("subscription updated: %s > %v", host, s.Subscriptions[host].Prefixes),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		s.subscribeRemote(ctx, host, s.Subscriptions[host].Prefixes)
	}
}

func (s *Subscriber) subscribeRemote(ctx context.Context, domain string, prefixes []string) {
	state, ok := s.Subscriptions[domain]
	if !ok {
		state = &SubState{
			Prefixes: []string{},
		}
		s.Subscriptions[domain] = state
	}
	if state.Connection == nil {
		c, err := s.Client.Realtime(ctx, domain)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to connect to remote server %v", domain),
				slog.String("error", err.Error()),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)

			delete(s.Subscriptions, domain)
			return
		}

		workerCtx, cancel := context.WithCancel(ctx)
		state.Connection = c
		state.CancelFunc = cancel

		messageChan := make(chan []byte)

		go func(ctx context.Context, c *websocket.Conn, messageChan chan<- []byte) {
			defer func() {
				cancel()
				if c != nil {
					c.Close()
				}
				delete(s.Subscriptions, domain)
				slog.Debug(
					fmt.Sprintf("remote connection closed(listener): %s", domain),
					slog.String("module", "worker"),
					slog.String("group", "remote ws.listener"),
				)
			}()
			for {
				// check if the connection is still alive
				if c == nil {
					slog.Info(
						fmt.Sprintf("connection is nil (domain: %s)", domain),
						slog.String("module", "worker"),
						slog.String("group", "realtime"),
					)
					return
				}
				_, message, err := c.ReadMessage()
				if err != nil {

					if ctx.Err() != nil {
						break
					}

					slog.Error(
						fmt.Sprintf("fail to read message: %v", err),
						slog.String("module", "worker"),
						slog.String("group", "realtime"),
					)
					break
				}
				messageChan <- message
			}
		}(workerCtx, c, messageChan)

		go func(ctx context.Context, c *websocket.Conn, messageChan <-chan []byte) {
			pingTicker := time.NewTicker(pingInterval)
			defer func() {
				cancel()
				if c != nil {
					c.Close()
				}
				pingTicker.Stop()
				delete(s.Subscriptions, domain)
				slog.Debug(
					fmt.Sprintf("remote connection closed(relayer): %s", domain),
					slog.String("module", "worker"),
					slog.String("group", "remote ws.publisher"),
				)
			}()

			var lastPong time.Time = time.Now()
			c.SetPongHandler(func(string) error {
				lastPong = time.Now()
				return nil
			})

			for {
				select {
				case <-ctx.Done():
					return
				case message := <-messageChan:
					slog.Debug(
						fmt.Sprintf("remote message received: %s", message[:64]),
						slog.String("module", "worker"),
						slog.String("group", "realtime"),
					)

					var event concrnt.Event
					err = json.Unmarshal(message, &event)
					if err != nil {
						slog.Error(
							"fail to Unmarshall redis message",
							slog.String("error", err.Error()),
							slog.String("module", "worker"),
							slog.String("group", "realtime"),
						)
						continue
					}

					err = s.Signal.Publish(ctx, event.Source, event)
					if err != nil {
						slog.Error(
							"fail to publish event to local signal service",
							slog.String("error", err.Error()),
							slog.String("module", "worker"),
							slog.String("group", "realtime"),
						)
						continue
					}

					s.cacheChunklineEvent(ctx, state.Prefixes, event)
				case <-pingTicker.C:
					if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
						slog.Error(
							fmt.Sprintf("fail to send ping message: %v", err),
							slog.String("module", "worker"),
							slog.String("group", "realtime"),
						)
						return
					}
					if lastPong.Before(time.Now().Add(-disconnectTimeout)) {
						slog.Warn(
							fmt.Sprintf("no pong received for %v, closing connection", disconnectTimeout),
							slog.String("module", "worker"),
							slog.String("group", "realtime"),
						)
						return
					}
				}
			}
		}(workerCtx, c, messageChan)
	}

	request := concrnt.RealtimeRequest{
		Type:     "listen",
		Prefixes: prefixes,
	}
	err := state.Connection.WriteJSON(request)
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", domain),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)

		delete(s.Subscriptions, domain)
		return
	}
	slog.Debug(
		fmt.Sprintf("remote connection updated: %s > %v", domain, prefixes),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)

	if s.Prefetcher != nil && len(prefixes) > 0 {
		timelines := make([]string, 0, len(prefixes))
		for _, prefix := range prefixes {
			tl := strings.TrimSuffix(prefix, "*")
			if tl != "" {
				timelines = append(timelines, tl)
			}
		}
		if len(timelines) > 0 {
			s.Prefetcher.PrefetchChunks(ctx, timelines)
		}
	}

}

func (s *Subscriber) cacheChunklineEvent(ctx context.Context, prefixes []string, event concrnt.Event) {
	if s.Memcache == nil || event.Type != "created" || event.URI == "" {
		return
	}

	source := event.Source
	if source == "" {
		source = event.URI
	}

	item, ok := bodyItemFromEvent(event)
	if !ok {
		return
	}

	for _, prefix := range prefixes {
		timeline := strings.TrimSuffix(prefix, "*")
		if timeline == "" || !strings.HasPrefix(source, timeline) {
			continue
		}

		manifest, err := s.loadChunklineManifest(ctx, timeline)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load chunkline manifest for cache update", slog.String("timeline", timeline), slog.String("error", err.Error()))
			continue
		}

		chunkID := manifest.Time2Chunk(item.Timestamp)
		itrKey := chunkline.IteratorCacheKey(timeline, chunkID)
		bodyKey := chunkline.BodyCacheKey(timeline, chunkID)
		// Update only cache entries created by previous chunkline requests.
		if err := s.Memcache.Replace(&memcache.Item{Key: itrKey, Value: []byte(fmt.Sprintf("%d", chunkID))}); err != nil && err != memcache.ErrCacheMiss {
			slog.ErrorContext(ctx, "failed to update chunkline iterator cache", slog.String("error", err.Error()))
		}

		value, err := chunkline.EncodeBodyCache([]chunkline.BodyItem{item})
		if err != nil {
			slog.ErrorContext(ctx, "failed to encode chunkline body cache update", slog.String("error", err.Error()))
			continue
		}
		prependErr := s.Memcache.Prepend(&memcache.Item{Key: bodyKey, Value: value})
		if prependErr == nil {
			continue
		}
		if prependErr != memcache.ErrCacheMiss && prependErr != memcache.ErrNotStored {
			slog.ErrorContext(ctx, "failed to prepend chunkline body cache", slog.String("error", prependErr.Error()))
			continue
		}
		// Cache miss: create a fresh entry holding just this item so that
		// future events can prepend onto it while the timeline stays subscribed.
		if err := s.Memcache.Add(&memcache.Item{Key: bodyKey, Value: value, Expiration: chunkline.CacheTTL}); err != nil && err != memcache.ErrNotStored {
			slog.ErrorContext(ctx, "failed to create chunkline body cache", slog.String("error", err.Error()))
		}
	}
}

func (s *Subscriber) loadChunklineManifest(ctx context.Context, timeline string) (chunkline.Manifest, error) {
	if s.Client == nil {
		return chunkline.Manifest{}, fmt.Errorf("client is not configured")
	}
	if s.manifestCache != nil {
		if cached, ok := s.manifestCache.Get(timeline); ok {
			if manifest, ok := cached.(chunkline.Manifest); ok {
				return manifest, nil
			}
		}
	}

	var manifest chunkline.Manifest
	if err := s.Client.GetResource(ctx, timeline, "application/chunkline+json", nil, &manifest); err != nil {
		return chunkline.Manifest{}, err
	}
	if manifest.ChunkSize <= 0 {
		return chunkline.Manifest{}, fmt.Errorf("chunkline manifest has invalid chunk size %d: must be greater than 0", manifest.ChunkSize)
	}
	if s.manifestCache != nil {
		s.manifestCache.Set(timeline, manifest, cache.DefaultExpiration)
	}
	return manifest, nil
}

func bodyItemFromEvent(event concrnt.Event) (chunkline.BodyItem, bool) {
	item := chunkline.BodyItem{
		Href:        event.URI,
		ContentType: "application/concrnt.document+json",
	}

	sd, ok := event.References[event.URI]
	if !ok {
		return item, false
	}

	var doc concrnt.Document[schemas.Reference]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err == nil {
		if !doc.CreatedAt.IsZero() {
			item.Timestamp = doc.CreatedAt
		}
		if doc.Value.Href != "" {
			item.Href = doc.Value.Href
			if targetSD, ok := event.References[doc.Value.Href]; ok {
				var targetDoc concrnt.Document[any]
				if err := json.Unmarshal([]byte(targetSD.Document), &targetDoc); err == nil && !targetDoc.CreatedAt.IsZero() {
					item.Timestamp = targetDoc.CreatedAt
				}
			}
		}
		return item, !item.Timestamp.IsZero()
	}

	var genericDoc concrnt.Document[any]
	if err := json.Unmarshal([]byte(sd.Document), &genericDoc); err == nil && !genericDoc.CreatedAt.IsZero() {
		item.Timestamp = genericDoc.CreatedAt
	}
	return item, !item.Timestamp.IsZero()
}

// chunkBoundaryThreshold is the fraction of a chunk_size that, once elapsed,
// causes us to defer dropping a subscription so we are still listening when
// the chunk rolls over.
const chunkBoundaryThreshold = 0.9

func (s *Subscriber) epochRoutine() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.deleteExcessSubscriptions()
	}
}

func (s *Subscriber) deleteExcessSubscriptions() {
	currentSubs := s.Signal.GetCurrentSubscriptions()
	ctx := context.Background()

	closeDomains := make(map[string]bool)
	updatedDomains := make(map[string]bool)

	for domain, state := range s.Subscriptions {
		var newPrefixes []string
		for _, prefix := range state.Prefixes {
			if slices.Contains(currentSubs, prefix) {
				newPrefixes = append(newPrefixes, prefix)
				continue
			}
			if s.shouldRetainPrefix(ctx, prefix) {
				newPrefixes = append(newPrefixes, prefix)
			}
		}

		if len(newPrefixes) != len(state.Prefixes) {
			updatedDomains[domain] = true
		}

		s.Subscriptions[domain].Prefixes = newPrefixes

		if len(newPrefixes) == 0 {
			closeDomains[domain] = true
		}
	}

	for domain := range closeDomains {
		if state, ok := s.Subscriptions[domain]; ok {
			if state.Connection != nil {
				state.CancelFunc()
				state.Connection.Close()
			}
		}

		delete(s.Subscriptions, domain)
		delete(updatedDomains, domain)
	}

	for domain := range updatedDomains {
		s.subscribeRemote(ctx, domain, s.Subscriptions[domain].Prefixes)
	}

	if len(closeDomains) > 0 {
		slog.Info(
			fmt.Sprintf("Subscriptions cleaned up: %v", maps.Keys(closeDomains)),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}
}

// shouldRetainPrefix decides whether a no-longer-requested prefix should be
// kept alive. We retain it when we are within the last 10% of the current
// chunk (so we do not drop the subscription right at chunk rollover) or when
// the current chunk still has cached body content that we want to keep
// freshening via incoming events.
func (s *Subscriber) shouldRetainPrefix(ctx context.Context, prefix string) bool {
	timeline := strings.TrimSuffix(prefix, "*")
	if timeline == "" {
		return false
	}

	manifest, err := s.loadChunklineManifest(ctx, timeline)
	if err != nil {
		// Without a manifest we cannot reason about the chunk window; drop.
		return false
	}

	now := time.Now().UTC()
	chunkID := manifest.Time2Chunk(now)
	elapsed := now.Unix() - chunkID*manifest.ChunkSize
	if elapsed >= int64(float64(manifest.ChunkSize)*chunkBoundaryThreshold) {
		return true
	}

	if s.Memcache == nil {
		return false
	}
	if _, err := s.Memcache.Get(chunkline.BodyCacheKey(timeline, chunkID)); err == nil {
		return true
	}
	return false
}
