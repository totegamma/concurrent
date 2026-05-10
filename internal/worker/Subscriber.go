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
)

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

type SubState struct {
	Prefixes   []string
	Connection *websocket.Conn
	CancelFunc context.CancelFunc
}

type Subscriber struct {
	Subscriptions map[string]*SubState
	Config        *domain.Config
	Client        *client.Client
	Signal        *service.SignalService
	Memcache      *memcache.Client
}

func NewSubscriber(
	config *domain.Config,
	client *client.Client,
	signal *service.SignalService,
	mc *memcache.Client,
) *Subscriber {
	return &Subscriber{
		Subscriptions: make(map[string]*SubState),
		Config:        config,
		Client:        client,
		Signal:        signal,
		Memcache:      mc,
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
		if manifest.ChunkSize <= 0 {
			slog.WarnContext(ctx, "skip chunkline cache update with invalid chunk size", slog.String("timeline", timeline))
			continue
		}

		chunkID := manifest.Time2Chunk(item.Timestamp)
		itrKey := chunkline.IteratorCacheKey(timeline, chunkID)
		bodyKey := chunkline.BodyCacheKey(timeline, chunkID)
		// Update only cache entries created by previous chunkline requests.
		if err := s.Memcache.Replace(&memcache.Item{Key: itrKey, Value: []byte(fmt.Sprintf("%d", chunkID))}); err != nil && err != memcache.ErrCacheMiss {
			slog.ErrorContext(ctx, "failed to update chunkline iterator cache", slog.String("error", err.Error()))
		}
		cachedBody, err := s.Memcache.Get(bodyKey)
		if err != nil {
			if err != memcache.ErrCacheMiss {
				slog.ErrorContext(ctx, "failed to load chunkline body cache", slog.String("error", err.Error()))
			}
			continue
		}
		if len(cachedBody.Value) == 0 || cachedBody.Value[0] != ',' {
			slog.WarnContext(ctx, "skip updating malformed chunkline body cache", slog.String("key", bodyKey))
			continue
		}

		value, err := chunkline.EncodeBodyCache([]chunkline.BodyItem{item})
		if err != nil {
			slog.ErrorContext(ctx, "failed to encode chunkline body cache update", slog.String("error", err.Error()))
			continue
		}
		if err := s.Memcache.Prepend(&memcache.Item{Key: bodyKey, Value: value}); err != nil && err != memcache.ErrCacheMiss {
			slog.ErrorContext(ctx, "failed to update chunkline body cache", slog.String("error", err.Error()))
		}
	}
}

func (s *Subscriber) loadChunklineManifest(ctx context.Context, timeline string) (chunkline.Manifest, error) {
	if s.Client == nil {
		return chunkline.Manifest{}, fmt.Errorf("client is not configured")
	}

	var manifest chunkline.Manifest
	err := s.Client.GetResource(ctx, timeline, "application/chunkline+json", nil, &manifest)
	return manifest, err
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

func (s *Subscriber) epochRoutine() {
	for {
		// 次の実行時刻を計算
		nextRun := time.Now().Truncate(time.Hour).Add(time.Minute * 10)
		if time.Now().After(nextRun) {
			// 現在時刻がnextRunを過ぎている場合、次の10分単位の時刻を計算
			elapsed := time.Since(nextRun)
			nextRun = nextRun.Add(time.Minute * 10 * ((elapsed / (time.Minute * 10)) + 1))
		}

		// 次の実行時刻まで待機
		time.Sleep(time.Until(nextRun))

		// ctx, span := tracer.Start(ctx, "Agent.chunkUpdaterRoutine")
		// defer span.End()

		// span.SetAttributes(attribute.String("currentChunk", currentChunk))

		slog.Info(
			"update chunkline subscriptions",
			slog.String("module", "agent"),
			slog.String("group", "realtime"),
		)

		s.deleteExcessSubscriptions()
	}
}

func (s *Subscriber) deleteExcessSubscriptions() {
	currentSubs := s.Signal.GetCurrentSubscriptions()

	closeDomains := make(map[string]bool)
	updatedDomains := make(map[string]bool)

	for domain, state := range s.Subscriptions {
		var newPrefixes []string
		for _, prefix := range state.Prefixes {
			if slices.Contains(currentSubs, prefix) {
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
		s.subscribeRemote(context.Background(), domain, s.Subscriptions[domain].Prefixes)
	}

	slog.Info(
		fmt.Sprintf("Subscriptions cleaned up: %v", maps.Keys(closeDomains)),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)

}
