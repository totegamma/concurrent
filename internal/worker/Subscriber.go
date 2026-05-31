package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/gorilla/websocket"
)

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

type SubState struct {
	RequestedPrefixes  []string
	SubscribedPrefixes []string
	Connection         *websocket.Conn
	CancelFunc         context.CancelFunc
}

type Subscriber struct {
	Subscriptions map[string]*SubState
	Config        *domain.Config
	Client        *client.Client
	Signal        *service.SignalService

	mu                   sync.RWMutex
	subscriptionRequests []string
}

func NewSubscriber(
	config *domain.Config,
	client *client.Client,
	signal *service.SignalService,
) *Subscriber {
	return &Subscriber{
		Subscriptions: make(map[string]*SubState),
		Config:        config,
		Client:        client,
		Signal:        signal,
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
		s.ReconcileSubscriptions(ctx, s.getSubscriptionRequests())
	}
}

func (s *Subscriber) ReconcileSubscriptions(ctx context.Context, currentRequests []string) {
	currentRequests = uniqueSortedStrings(currentRequests)

	s.mu.Lock()
	s.subscriptionRequests = cloneStrings(currentRequests)
	s.mu.Unlock()

	desired := make(map[string][]string)

	for _, prefix := range currentRequests {
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

		desired[host] = appendUnique(desired[host], prefix)
	}

	changedRemotes, closedRemotes := s.applyDesiredSubscriptions(desired)

	for _, host := range changedRemotes {
		prefixes := desired[host]
		slog.Debug(
			fmt.Sprintf("subscription updated: %s > %v", host, prefixes),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		s.subscribeRemote(ctx, host, prefixes)
	}

	if len(closedRemotes) > 0 {
		slog.Info(
			fmt.Sprintf("Subscriptions cleaned up: %v", closedRemotes),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}
}

func (s *Subscriber) subscribeRemote(ctx context.Context, domain string, prefixes []string) {
	prefixes = uniqueSortedStrings(prefixes)

	s.mu.Lock()
	state, ok := s.Subscriptions[domain]
	if !ok {
		state = &SubState{}
		s.Subscriptions[domain] = state
	}
	state.RequestedPrefixes = cloneStrings(prefixes)
	if len(state.SubscribedPrefixes) > 0 {
		state.SubscribedPrefixes = intersectStrings(state.SubscribedPrefixes, state.RequestedPrefixes)
	}
	connection := state.Connection
	s.mu.Unlock()

	if connection == nil {
		c, err := s.Client.Realtime(ctx, domain)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to connect to remote server %v", domain),
				slog.String("error", err.Error()),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)

			s.removeSubscription(domain)
			return
		}

		workerCtx, cancel := context.WithCancel(ctx)

		s.mu.Lock()
		state, ok = s.Subscriptions[domain]
		if !ok {
			state = &SubState{}
			s.Subscriptions[domain] = state
		}
		state.Connection = c
		state.CancelFunc = cancel
		s.mu.Unlock()

		messageChan := make(chan []byte)

		go func(ctx context.Context, c *websocket.Conn, messageChan chan<- []byte) {
			defer func() {
				cancel()
				if c != nil {
					c.Close()
				}
				s.removeSubscriptionConnection(domain, c)
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
				s.removeSubscriptionConnection(domain, c)
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
						fmt.Sprintf("remote message received: %s", previewMessage(message)),
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

					if event.Type == "subscribed" {
						s.setSubscribedPrefixes(domain, event.Prefixes)
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

					// TODO: add cache update logic here
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

		connection = c
	}

	request := concrnt.RealtimeRequest{
		Type:     "listen",
		Prefixes: prefixes,
	}
	err := connection.WriteJSON(request)
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", domain),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)

		s.removeSubscription(domain)
		return
	}
	slog.Debug(
		fmt.Sprintf("remote connection updated: %s > %v", domain, prefixes),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)

}

func Time2Chunk(t time.Time) string {
	const chunkLength = 600
	return fmt.Sprintf("%d", (t.Unix()/chunkLength)*chunkLength)
}

func (s *Subscriber) epochRoutine() {
	currentChunk := Time2Chunk(time.Now())
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

		// まだだったら待ちなおす
		newChunk := Time2Chunk(time.Now())
		if newChunk == currentChunk {
			continue
		}

		// ctx, span := tracer.Start(ctx, "Agent.chunkUpdaterRoutine")
		// defer span.End()

		// span.SetAttributes(attribute.String("currentChunk", currentChunk))

		slog.Info(
			fmt.Sprintf("update chunks: %s -> %s", currentChunk, newChunk),
			slog.String("module", "agent"),
			slog.String("group", "realtime"),
		)

		s.ReconcileSubscriptions(context.Background(), s.getSubscriptionRequests())

		currentChunk = newChunk
	}
}

func (s *Subscriber) GetLocalSubscriptions(prefixes []string) []string {
	local := make([]string, 0)

	for _, prefix := range prefixes {
		host, err := s.Client.ResolveResourceHost(context.Background(), prefix)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to resolve resource host for prefix %s: %v", prefix, err),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)
			continue
		}

		if host == s.Config.FQDN {
			local = append(local, prefix)
		}
	}

	return uniqueSortedStrings(local)
}

func (s *Subscriber) GetRemoteSubscriptions() []string {
	completed := make([]string, 0)

	s.mu.RLock()
	for _, state := range s.Subscriptions {
		completed = append(completed, state.SubscribedPrefixes...)
	}
	s.mu.RUnlock()

	return uniqueSortedStrings(completed)
}

func (s *Subscriber) getSubscriptionRequests() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneStrings(s.subscriptionRequests)
}

func (s *Subscriber) applyDesiredSubscriptions(desired map[string][]string) ([]string, []string) {
	changedRemotes := make([]string, 0)
	closedRemotes := make([]string, 0)
	closeStates := make([]*SubState, 0)

	s.mu.Lock()
	for host, prefixes := range desired {
		prefixes = uniqueSortedStrings(prefixes)
		state, ok := s.Subscriptions[host]
		if !ok {
			s.Subscriptions[host] = &SubState{
				RequestedPrefixes: prefixes,
			}
			changedRemotes = append(changedRemotes, host)
			continue
		}

		if !equalStringSlices(state.RequestedPrefixes, prefixes) {
			state.RequestedPrefixes = cloneStrings(prefixes)
			state.SubscribedPrefixes = intersectStrings(state.SubscribedPrefixes, state.RequestedPrefixes)
			changedRemotes = append(changedRemotes, host)
			continue
		}

		if state.Connection == nil {
			changedRemotes = append(changedRemotes, host)
		}
	}

	for host, state := range s.Subscriptions {
		if _, ok := desired[host]; ok {
			continue
		}

		closeStates = append(closeStates, state)
		closedRemotes = append(closedRemotes, host)
		delete(s.Subscriptions, host)
	}
	s.mu.Unlock()

	for _, state := range closeStates {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		if state.Connection != nil {
			state.Connection.Close()
		}
	}

	sort.Strings(changedRemotes)
	sort.Strings(closedRemotes)

	return changedRemotes, closedRemotes
}

func (s *Subscriber) setSubscribedPrefixes(domain string, prefixes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.Subscriptions[domain]
	if !ok {
		return
	}

	state.SubscribedPrefixes = intersectStrings(uniqueSortedStrings(prefixes), state.RequestedPrefixes)
}

func (s *Subscriber) removeSubscription(domain string) {
	var state *SubState

	s.mu.Lock()
	if current, ok := s.Subscriptions[domain]; ok {
		state = current
		delete(s.Subscriptions, domain)
	}
	s.mu.Unlock()

	if state == nil {
		return
	}

	if state.CancelFunc != nil {
		state.CancelFunc()
	}
	if state.Connection != nil {
		state.Connection.Close()
	}
}

func (s *Subscriber) removeSubscriptionConnection(domain string, connection *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.Subscriptions[domain]
	if !ok || state.Connection != connection {
		return
	}

	delete(s.Subscriptions, domain)
}

func previewMessage(message []byte) string {
	if len(message) <= 64 {
		return string(message)
	}
	return string(message[:64])
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func uniqueSortedStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}

	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)

	return result
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	result := make([]string, len(values))
	copy(result, values)
	return result
}

func equalStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func intersectStrings(values []string, allowed []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if slices.Contains(allowed, value) && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	sort.Strings(result)

	return result
}
