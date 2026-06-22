package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/gorilla/websocket"
)

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

type SubscribeClient interface {
	CurrentSubscriptions() []string
}

type SubState struct {
	Prefixes   []string
	Connection *websocket.Conn
	CancelFunc context.CancelFunc
}

type PubSub interface {
	Publish(ctx context.Context, channel string, event concrnt.Event) error
	Subscribe(ctx context.Context, prefixes []string, response chan<- concrnt.Event) error
	SubscribeAll(ctx context.Context, response chan<- concrnt.Event) error
}

type Subscriber struct {
	Subscriptions map[string]*SubState
	Clients       map[string]SubscribeClient
	Config        *domain.Config
	Client        *client.Client
	pubsub        PubSub
}

func NewSubscriber(
	config *domain.Config,
	client *client.Client,
	pubsub PubSub,
) *Subscriber {
	return &Subscriber{
		Subscriptions: make(map[string]*SubState),
		Clients:       make(map[string]SubscribeClient),
		Config:        config,
		Client:        client,
		pubsub:        pubsub,
	}
}

func (s *Subscriber) Start(ctx context.Context) {
	go s.keeperRoutine(ctx)
}

func (s *Subscriber) RegisterClient(client SubscribeClient) string {
	id := fmt.Sprintf("%p", client)
	s.Clients[id] = client
	slog.Info(
		fmt.Sprintf("client registered: %s", id),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)
	return id
}

func (s *Subscriber) CurrentSubscriptions(except ...SubscribeClient) []string {
	subscriptionSet := make(map[string]bool)

	for _, client := range s.Clients {
		if slices.Contains(except, client) {
			continue
		}

		for _, prefix := range client.CurrentSubscriptions() {
			subscriptionSet[prefix] = true
		}
	}

	subscriptions := make([]string, 0, len(subscriptionSet))
	for prefix := range subscriptionSet {
		subscriptions = append(subscriptions, prefix)
	}

	return subscriptions
}

func (s *Subscriber) keeperRoutine(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for range ticker.C {

		fmt.Println("Keeper routine ticked")
		fmt.Println("Current clients: ", maps.Keys(s.Clients))
		fmt.Println("Current subscriptions: ", maps.Keys(s.Subscriptions))

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
		s.deleteExcessSubscriptions()
	}
}

func (s *Subscriber) CollectCurrentSubscriptions() []string {
	subscriptionSet := make(map[string]bool)
	for _, client := range s.Clients {
		for _, prefix := range client.CurrentSubscriptions() {
			subscriptionSet[prefix] = true
		}
	}

	subscriptions := make([]string, 0, len(subscriptionSet))
	for prefix := range subscriptionSet {
		subscriptions = append(subscriptions, prefix)
	}

	return subscriptions
}

func (s *Subscriber) createInsufficientSubscriptions(ctx context.Context) {

	fmt.Println("CreateInsufficientSubscriptions called")

	currentSubscriptions := s.CollectCurrentSubscriptions()
	changedRemotes := make([]string, 0)

	fmt.Printf("Current subscriptions: %v\n", currentSubscriptions)

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

	fmt.Println("Changed remotes: ", changedRemotes)

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

					err = s.pubsub.Publish(ctx, event.Source, event)
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

func (s *Subscriber) deleteExcessSubscriptions() {
	currentSubs := s.CollectCurrentSubscriptions()

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
