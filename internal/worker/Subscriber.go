package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/concrnt/concrnt"
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

// SubscriberClient is the subset of client.Client the Subscriber needs.
type SubscriberClient interface {
	ResolveResourceHost(ctx context.Context, uri string) (string, error)
	Realtime(ctx context.Context, fqdn string) (*websocket.Conn, error)
}

// CachePurger drops caches that are only valid while an upstream subscription
// is open. The subscriber invokes it on both subscription edges: on close
// (depth 1) because the cache stops being maintained, and on open (depth 2,
// covering a gap that started in the previous chunk period) because whatever
// was cached before or during the unsubscribed gap may be missing records.
type CachePurger interface {
	PurgeLatest(prefixes []string, depth int)
}

type SubState struct {
	Prefixes   []string
	Connection *websocket.Conn    // nil until the dial completes
	CancelFunc context.CancelFunc // cancels this connection's goroutines; nil until connected
	Dialing    bool               // a dial for this host is in flight

	// writeMu serializes writes on Connection (listen requests vs pings);
	// gorilla/websocket forbids concurrent writers. Always taken after s.mu
	// is released, never while holding it.
	writeMu sync.Mutex
}

type PubSub interface {
	Publish(ctx context.Context, channel string, event concrnt.Event) error
	Subscribe(ctx context.Context, prefixes []string, response chan<- concrnt.Event) error
	SubscribeAll(ctx context.Context, response chan<- concrnt.Event) error
}

type Subscriber struct {
	mu            sync.Mutex
	runCtx        context.Context // current lead-term context, set by Start; nil before first lead
	Subscriptions map[string]*SubState
	Clients       map[string]SubscribeClient
	Config        *domain.Config
	Client        SubscriberClient
	pubsub        PubSub
	purger        CachePurger // optional, set by SetCachePurger
}

// listenUpdate is a pending "listen" request to send once s.mu is released.
type listenUpdate struct {
	host     string
	state    *SubState
	conn     *websocket.Conn
	prefixes []string
	added    []string // prefixes newly opened by this update (purged depth 2 after the listen)
}

func NewSubscriber(
	config *domain.Config,
	client SubscriberClient,
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

// Start begins the keeper loop for one lead term. It may be called again on
// re-election; each call overwrites runCtx with the fresh lead context, so
// connections are always bound to the current term rather than to whichever
// caller happened to trigger the dial.
func (s *Subscriber) Start(ctx context.Context) {
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()
	go s.keeperRoutine(ctx)
}

// SetCachePurger hooks cache invalidation into the subscription open/close
// edges. Wire it before Start.
func (s *Subscriber) SetCachePurger(p CachePurger) {
	s.mu.Lock()
	s.purger = p
	s.mu.Unlock()
}

// purge must be called without holding s.mu (the purger does network I/O).
func (s *Subscriber) purge(prefixes []string, depth int) {
	if len(prefixes) == 0 {
		return
	}
	s.mu.Lock()
	p := s.purger
	s.mu.Unlock()
	if p == nil {
		return
	}
	p.PurgeLatest(prefixes, depth)
}

// OpenPrefixes reports the union of prefixes whose upstream connection is
// currently established (as opposed to merely demanded or still dialing).
func (s *Subscriber) OpenPrefixes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	set := make(map[string]bool)
	for _, state := range s.Subscriptions {
		if state.Connection == nil {
			continue
		}
		for _, prefix := range state.Prefixes {
			set[prefix] = true
		}
	}

	prefixes := make([]string, 0, len(set))
	for prefix := range set {
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func (s *Subscriber) RegisterClient(client SubscribeClient) string {
	id := fmt.Sprintf("%p", client)
	s.mu.Lock()
	s.Clients[id] = client
	s.mu.Unlock()
	slog.Info(
		fmt.Sprintf("client registered: %s", id),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)
	return id
}

// snapshotClients copies the client list so CurrentSubscriptions() calls
// (which may do I/O) run without holding the subscriber lock.
func (s *Subscriber) snapshotClients() []SubscribeClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	clients := make([]SubscribeClient, 0, len(s.Clients))
	for _, client := range s.Clients {
		clients = append(clients, client)
	}
	return clients
}

func (s *Subscriber) CurrentSubscriptions() []string {
	subscriptionSet := make(map[string]bool)

	for _, client := range s.snapshotClients() {
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

	for {
		// one demand snapshot per tick, shared by create and delete so they
		// cannot disagree within a tick (and peers are only polled once)
		demand := s.CurrentSubscriptions()
		s.createInsufficientSubscriptions(ctx, demand)
		s.repairBrokenSubscriptions()
		s.deleteExcessSubscriptions(demand)

		select {
		case <-ctx.Done():
			s.closeAllSubscriptions()
			return
		case <-ticker.C:
		}
	}
}

func (s *Subscriber) repairBrokenSubscriptions() {
	s.mu.Lock()
	defer s.mu.Unlock()

	runCtx := s.runCtx
	if runCtx == nil || runCtx.Err() != nil {
		return
	}

	for host, state := range s.Subscriptions {
		if state.Connection == nil && !state.Dialing {
			slog.Info(
				fmt.Sprintf("broken connection found: %s", host),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)
			state.Dialing = true
			go s.dialAndRun(runCtx, host, state)
		}
	}
}

func (s *Subscriber) closeAllSubscriptions() {
	var dropped []string
	s.mu.Lock()

	// runCtx is deliberately left untouched: a new term's Start may already
	// have replaced it. In-flight dials self-clean via the identity check in
	// dialAndRun.
	for domain, state := range s.Subscriptions {
		if state.CancelFunc != nil {
			state.CancelFunc()
		}
		if state.Connection != nil {
			state.Connection.Close()
		}
		dropped = append(dropped, state.Prefixes...)
		delete(s.Subscriptions, domain)
	}
	s.mu.Unlock()

	s.purge(dropped, 1)

	slog.Info(
		"all remote subscriptions closed",
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)
}

// resolvePrefixHosts maps each prefix to its remote host, dropping local and
// unresolvable prefixes. Called without holding the lock (does network I/O).
func (s *Subscriber) resolvePrefixHosts(ctx context.Context, prefixes []string) map[string][]string {
	result := make(map[string][]string)
	for _, prefix := range prefixes {
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

		if !slices.Contains(result[host], prefix) {
			result[host] = append(result[host], prefix)
		}
	}
	return result
}

// EnsureSubscriptions makes sure an upstream subscription exists for every
// given prefix. The caller's ctx bounds only host resolution; connections are
// bound to the lead context captured at Start, so an ensure forwarded over
// HTTP does not tear its connection down when the request ends.
func (s *Subscriber) EnsureSubscriptions(ctx context.Context, subscriptions []string) {
	s.ensureHosts(s.resolvePrefixHosts(ctx, subscriptions))
}

func (s *Subscriber) createInsufficientSubscriptions(ctx context.Context, demand []string) {
	s.ensureHosts(s.resolvePrefixHosts(ctx, demand))
}

func (s *Subscriber) ensureHosts(prefixesByHost map[string][]string) {
	s.mu.Lock()

	runCtx := s.runCtx
	if runCtx == nil || runCtx.Err() != nil {
		s.mu.Unlock()
		slog.Warn(
			"subscription ensure requested while not leading, ignoring",
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return
	}

	var updates []listenUpdate
	for host, prefixes := range prefixesByHost {
		state, ok := s.Subscriptions[host]
		if !ok {
			state = &SubState{
				Prefixes: slices.Clone(prefixes),
				Dialing:  true,
			}
			s.Subscriptions[host] = state
			go s.dialAndRun(runCtx, host, state)
			continue
		}

		var added []string
		for _, prefix := range prefixes {
			if !slices.Contains(state.Prefixes, prefix) {
				state.Prefixes = append(state.Prefixes, prefix)
				added = append(added, prefix)
			}
		}

		if state.Dialing {
			// the in-flight dial snapshots Prefixes after connecting, so the
			// merged prefixes above will be included in its listen request
			continue
		}
		if state.Connection == nil {
			state.Dialing = true
			go s.dialAndRun(runCtx, host, state)
			continue
		}
		if len(added) > 0 {
			slog.Debug(
				fmt.Sprintf("subscription updated: %s > %v", host, state.Prefixes),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)
			updates = append(updates, listenUpdate{
				host:     host,
				state:    state,
				conn:     state.Connection,
				prefixes: slices.Clone(state.Prefixes),
				added:    added,
			})
		}
	}
	s.mu.Unlock()

	for _, u := range updates {
		s.sendListen(u)
		// only after the listen is on the wire: purging earlier would let a
		// racing read re-cache a pre-subscription body
		s.purge(u.added, 2)
	}
}

// dialAndRun performs the network dial for state outside the lock, then either
// installs the connection or backs out if the entry was replaced or the term
// ended while dialing.
func (s *Subscriber) dialAndRun(ctx context.Context, host string, state *SubState) {
	conn, err := s.Client.Realtime(ctx, host)

	s.mu.Lock()
	state.Dialing = false
	if err != nil {
		if s.Subscriptions[host] == state {
			// same policy as before: drop the entry, the keeper recreates it
			// next tick while demand persists
			delete(s.Subscriptions, host)
		}
		s.mu.Unlock()
		slog.Error(
			fmt.Sprintf("fail to connect to remote server %v", host),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return
	}
	if ctx.Err() != nil || s.Subscriptions[host] != state {
		// demoted, or delete-excess/closeAll removed us while dialing
		s.mu.Unlock()
		conn.Close()
		return
	}

	workerCtx, cancel := context.WithCancel(ctx)
	state.Connection = conn
	state.CancelFunc = cancel
	prefixes := slices.Clone(state.Prefixes) // includes anything merged during the dial
	messageChan := make(chan []byte)
	go s.runListener(workerCtx, cancel, host, state, conn, messageChan)
	go s.runRelayer(workerCtx, cancel, host, state, conn, messageChan)
	s.mu.Unlock()

	s.sendListen(listenUpdate{host: host, state: state, conn: conn, prefixes: prefixes})
	// every prefix of a fresh connection is newly opened; purge only after
	// the listen is on the wire (see ensureHosts)
	s.purge(prefixes, 2)
}

func (s *Subscriber) sendListen(u listenUpdate) {
	request := concrnt.RealtimeRequest{
		Type:     "listen",
		Prefixes: u.prefixes,
	}
	u.state.writeMu.Lock()
	err := u.conn.WriteJSON(request)
	u.state.writeMu.Unlock()
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", u.host),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		// closing the conn routes cleanup through the connection goroutines'
		// identity-checked defers; no manual map surgery here
		u.conn.Close()
		return
	}
	slog.Debug(
		fmt.Sprintf("remote connection updated: %s > %v", u.host, u.prefixes),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)
}

// dropOwnSubscription removes the map entry only if it still belongs to this
// connection, so a stale goroutine can never delete a fresh replacement.
func (s *Subscriber) dropOwnSubscription(host string, state *SubState, conn *websocket.Conn) {
	var dropped []string
	s.mu.Lock()
	if cur, ok := s.Subscriptions[host]; ok && cur == state && cur.Connection == conn {
		dropped = slices.Clone(cur.Prefixes)
		delete(s.Subscriptions, host)
	}
	s.mu.Unlock()
	s.purge(dropped, 1)
}

func (s *Subscriber) runListener(ctx context.Context, cancel context.CancelFunc, host string, state *SubState, c *websocket.Conn, messageChan chan<- []byte) {
	defer func() {
		cancel()
		c.Close()
		s.dropOwnSubscription(host, state, c)
		slog.Debug(
			fmt.Sprintf("remote connection closed(listener): %s", host),
			slog.String("module", "worker"),
			slog.String("group", "remote ws.listener"),
		)
	}()
	for {
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
		select {
		case messageChan <- message:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Subscriber) runRelayer(ctx context.Context, cancel context.CancelFunc, host string, state *SubState, c *websocket.Conn, messageChan <-chan []byte) {
	pingTicker := time.NewTicker(pingInterval)
	defer func() {
		cancel()
		c.Close()
		pingTicker.Stop()
		s.dropOwnSubscription(host, state, c)
		slog.Debug(
			fmt.Sprintf("remote connection closed(relayer): %s", host),
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
				fmt.Sprintf("remote message received: %s", message[:min(64, len(message))]),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)

			var event concrnt.Event
			err := json.Unmarshal(message, &event)
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

		case <-pingTicker.C:
			state.writeMu.Lock()
			err := c.WriteMessage(websocket.PingMessage, []byte{})
			state.writeMu.Unlock()
			if err != nil {
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
}

func (s *Subscriber) deleteExcessSubscriptions(currentSubs []string) {
	var dropped []string
	s.mu.Lock()

	closeDomains := make(map[string]bool)
	updatedDomains := make(map[string]bool)

	for domain, state := range s.Subscriptions {
		var newPrefixes []string
		for _, prefix := range state.Prefixes {
			if slices.Contains(currentSubs, prefix) {
				newPrefixes = append(newPrefixes, prefix)
			} else {
				dropped = append(dropped, prefix)
			}
		}

		if len(newPrefixes) != len(state.Prefixes) {
			updatedDomains[domain] = true
		}

		state.Prefixes = newPrefixes

		if len(newPrefixes) == 0 {
			closeDomains[domain] = true
		}
	}

	for domain := range closeDomains {
		if state, ok := s.Subscriptions[domain]; ok {
			if state.CancelFunc != nil {
				state.CancelFunc()
			}
			if state.Connection != nil {
				state.Connection.Close()
			}
			// a Dialing entry only needs the delete: dialAndRun's identity
			// check closes the late connection
		}

		delete(s.Subscriptions, domain)
		delete(updatedDomains, domain)
	}

	var updates []listenUpdate
	for domain := range updatedDomains {
		state := s.Subscriptions[domain]
		if state.Dialing || state.Connection == nil {
			// an in-flight dial sends the already-pruned state.Prefixes
			continue
		}
		updates = append(updates, listenUpdate{
			host:     domain,
			state:    state,
			conn:     state.Connection,
			prefixes: slices.Clone(state.Prefixes),
		})
	}
	s.mu.Unlock()

	s.purge(dropped, 1)

	for _, u := range updates {
		s.sendListen(u)
	}

	if len(closeDomains) > 0 {
		slog.Info(
			fmt.Sprintf("Subscriptions cleaned up: %v", maps.Keys(closeDomains)),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}
}
