package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
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

const peerPollTimeout = 2 * time.Second

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

// PeerDiscovery lists the internal base URLs of all live replicas. nil means
// standalone: there are no peers to poll.
type PeerDiscovery interface {
	Peers(ctx context.Context) ([]string, error)
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

// LeaderSubscriber is the Subscriber that actually dials upstream websockets.
// It must run on exactly one replica of the cluster (the leader): Start is
// called once per lead term.
//
// Two demand sources feed it: this replica's own registered SubscribeClients
// (Clients), and the union of every peer replica's own local demand, polled
// over HTTP once per keeper tick (see pollPeerDemand). The result is cached
// (demand) so that CurrentSubscriptions — called by workers over HTTP on
// every chunkline cache check — never itself triggers a peer poll.
type LeaderSubscriber struct {
	mu            sync.Mutex
	runCtx        context.Context // current lead-term context, set by Start; nil before first lead
	Subscriptions map[string]*SubState
	Clients       map[string]SubscribeClient
	Config        *domain.Config
	Client        SubscriberClient
	pubsub        PubSub
	purger        CachePurger // optional, set by SetCachePurger

	discovery  PeerDiscovery       // nil in standalone mode
	peerClient *http.Client
	peerDemand map[string][]string // last known demand per peer, keyed by peer base URL; membership pruned by discovery, not by time

	demand []string // cached aggregate: Clients' demand union peerDemand, refreshed once per keeper tick and nudged on ensure/disconnect
}

// listenUpdate is a pending "listen" request to send once s.mu is released.
type listenUpdate struct {
	host     string
	state    *SubState
	conn     *websocket.Conn
	prefixes []string
	added    []string // prefixes newly opened by this update (purged depth 2, merged into demand, after the listen)
}

func NewLeaderSubscriber(
	config *domain.Config,
	client SubscriberClient,
	pubsub PubSub,
	discovery PeerDiscovery,
) *LeaderSubscriber {
	return &LeaderSubscriber{
		Subscriptions: make(map[string]*SubState),
		Clients:       make(map[string]SubscribeClient),
		Config:        config,
		Client:        client,
		pubsub:        pubsub,
		discovery:     discovery,
		peerClient:    &http.Client{Timeout: peerPollTimeout},
		peerDemand:    make(map[string][]string),
	}
}

// Start begins the keeper loop for one lead term. It may be called again on
// re-election; each call overwrites runCtx with the fresh lead context, so
// connections are always bound to the current term rather than to whichever
// caller happened to trigger the dial.
func (s *LeaderSubscriber) Start(ctx context.Context) {
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()
	go s.keeperRoutine(ctx)
}

// SetCachePurger hooks cache invalidation into the subscription open/close
// edges. Wire it before Start.
func (s *LeaderSubscriber) SetCachePurger(p CachePurger) {
	s.mu.Lock()
	s.purger = p
	s.mu.Unlock()
}

// OpenPrefixes reports the union of prefixes whose upstream connection is
// currently established (as opposed to merely demanded or still dialing).
func (s *LeaderSubscriber) OpenPrefixes() []string {
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

func (s *LeaderSubscriber) RegisterClient(client SubscribeClient) string {
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

// CurrentSubscriptions returns the cached cluster-wide aggregate demand. It
// never does I/O: the aggregate is computed once per keeper tick by
// pollPeerDemand plus this replica's own Clients, and nudged immediately by
// EnsureSubscriptions (add) and upstream disconnects (remove) in between
// ticks. This is what workers poll over HTTP, so it must stay cheap.
func (s *LeaderSubscriber) CurrentSubscriptions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.demand)
}

// mergeDemand adds prefixes to the cached aggregate demand. Callers must hold s.mu.
func (s *LeaderSubscriber) mergeDemand(prefixes []string) {
	if len(prefixes) == 0 {
		return
	}
	set := make(map[string]bool, len(s.demand)+len(prefixes))
	for _, p := range s.demand {
		set[p] = true
	}
	for _, p := range prefixes {
		set[p] = true
	}
	s.demand = make([]string, 0, len(set))
	for p := range set {
		s.demand = append(s.demand, p)
	}
}

// pruneDemand removes prefixes from the cached aggregate demand. Callers must hold s.mu.
func (s *LeaderSubscriber) pruneDemand(prefixes []string) {
	if len(prefixes) == 0 {
		return
	}
	set := make(map[string]bool, len(s.demand))
	for _, p := range s.demand {
		set[p] = true
	}
	for _, p := range prefixes {
		delete(set, p)
	}
	s.demand = make([]string, 0, len(set))
	for p := range set {
		s.demand = append(s.demand, p)
	}
}

// pollPeerDemand returns the union of every peer replica's own local demand,
// fetched from each peer's GET /internal/demand (never /internal/subscriptions
// — that endpoint answers with this same aggregate, so polling it here would
// loop leader->peer->leader). A peer that drops out of discovery is forgotten
// immediately; a peer that stays listed but fails to answer keeps
// contributing its last known demand (redis pubsub delivery is fire-and-
// forget, so tearing down its subscriptions on a single failed poll would
// lose events). If discovery itself fails, the entire last known state is
// frozen rather than expired, since "peer gone" and "discovery broken" are
// then indistinguishable.
func (s *LeaderSubscriber) pollPeerDemand(ctx context.Context) []string {
	if s.discovery == nil {
		return nil
	}

	peers, err := s.discovery.Peers(ctx)
	if err != nil {
		slog.Warn(
			"failed to discover peers, freezing last known demand",
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		s.mu.Lock()
		defer s.mu.Unlock()
		set := make(map[string]bool)
		for _, prefixes := range s.peerDemand {
			for _, p := range prefixes {
				set[p] = true
			}
		}
		union := make([]string, 0, len(set))
		for p := range set {
			union = append(union, p)
		}
		return union
	}

	type polled struct {
		peer     string
		prefixes []string
		err      error
	}
	results := make(chan polled, len(peers))
	for _, peer := range peers {
		go func(peer string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer+"/internal/demand", nil)
			if err != nil {
				results <- polled{peer: peer, err: err}
				return
			}
			resp, err := s.peerClient.Do(req)
			if err != nil {
				results <- polled{peer: peer, err: err}
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				results <- polled{peer: peer, err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
				return
			}
			var prefixes []string
			if err := json.NewDecoder(resp.Body).Decode(&prefixes); err != nil {
				results <- polled{peer: peer, err: err}
				return
			}
			results <- polled{peer: peer, prefixes: prefixes}
		}(peer)
	}

	// collected without s.mu held: each result only arrives once its HTTP
	// round-trip finishes, and s.mu also guards CurrentSubscriptions (which
	// must stay a cheap, non-blocking read for workers polling it over HTTP)
	polls := make([]polled, 0, len(peers))
	for range peers {
		polls = append(polls, <-results)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range polls {
		if r.err != nil {
			slog.Warn(
				"failed to poll peer demand",
				slog.String("peer", r.peer),
				slog.String("error", r.err.Error()),
				slog.String("module", "worker"),
				slog.String("group", "realtime"),
			)
			continue // keep this peer's last known entry
		}
		s.peerDemand[r.peer] = r.prefixes
	}

	// discovery is authoritative: a peer missing from the live list is gone
	for peer := range s.peerDemand {
		if !slices.Contains(peers, peer) {
			delete(s.peerDemand, peer)
		}
	}

	set := make(map[string]bool)
	for _, prefixes := range s.peerDemand {
		for _, p := range prefixes {
			set[p] = true
		}
	}
	union := make([]string, 0, len(set))
	for p := range set {
		union = append(union, p)
	}
	return union
}

func (s *LeaderSubscriber) keeperRoutine(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		s.mu.Lock()
		clients := make([]SubscribeClient, 0, len(s.Clients))
		for _, client := range s.Clients {
			clients = append(clients, client)
		}
		s.mu.Unlock()

		// one demand snapshot per tick, shared by ensure and delete so they
		// cannot disagree within a tick (and peers are only polled once)
		demandSet := make(map[string]bool)
		for _, client := range clients {
			for _, prefix := range client.CurrentSubscriptions() {
				demandSet[prefix] = true
			}
		}
		for _, prefix := range s.pollPeerDemand(ctx) {
			demandSet[prefix] = true
		}
		demand := make([]string, 0, len(demandSet))
		for prefix := range demandSet {
			demand = append(demand, prefix)
		}

		s.mu.Lock()
		s.demand = demand
		s.mu.Unlock()

		s.EnsureSubscriptions(ctx, demand)
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

func (s *LeaderSubscriber) repairBrokenSubscriptions() {
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

func (s *LeaderSubscriber) closeAllSubscriptions() {
	var dropped []string
	s.mu.Lock()
	purger := s.purger

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
	s.pruneDemand(dropped)
	s.mu.Unlock()

	if purger != nil && len(dropped) > 0 {
		purger.PurgeLatest(dropped, 1)
	}

	slog.Info(
		"all remote subscriptions closed",
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)
}

// EnsureSubscriptions makes sure an upstream subscription exists for every
// given prefix, folding them into the cached aggregate demand once their
// listen request is confirmed on the wire. The caller's ctx bounds only host
// resolution; connections are bound to the lead context captured at Start, so
// an ensure forwarded over HTTP does not tear its connection down when the
// request ends.
func (s *LeaderSubscriber) EnsureSubscriptions(ctx context.Context, prefixes []string) {
	prefixesByHost := make(map[string][]string)
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

		if !slices.Contains(prefixesByHost[host], prefix) {
			prefixesByHost[host] = append(prefixesByHost[host], prefix)
		}
	}

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
	purger := s.purger

	var updates []listenUpdate
	for host, hostPrefixes := range prefixesByHost {
		state, ok := s.Subscriptions[host]
		if !ok {
			state = &SubState{
				Prefixes: slices.Clone(hostPrefixes),
				Dialing:  true,
			}
			s.Subscriptions[host] = state
			go s.dialAndRun(runCtx, host, state)
			continue
		}

		var added []string
		for _, prefix := range hostPrefixes {
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
			continue
		}
		slog.Debug(
			fmt.Sprintf("remote connection updated: %s > %v", u.host, u.prefixes),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)

		s.mu.Lock()
		s.mergeDemand(u.added)
		s.mu.Unlock()
		// only after the listen is on the wire: purging earlier would let a
		// racing read re-cache a pre-subscription body
		if purger != nil {
			purger.PurgeLatest(u.added, 2)
		}
	}
}

// dialAndRun performs the network dial for state outside the lock, then either
// installs the connection or backs out if the entry was replaced or the term
// ended while dialing.
func (s *LeaderSubscriber) dialAndRun(ctx context.Context, host string, state *SubState) {
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
	purger := s.purger
	messageChan := make(chan []byte)
	go s.runListener(workerCtx, cancel, host, state, conn, messageChan)
	go s.runRelayer(workerCtx, cancel, host, state, conn, messageChan)
	s.mu.Unlock()

	request := concrnt.RealtimeRequest{
		Type:     "listen",
		Prefixes: prefixes,
	}
	state.writeMu.Lock()
	err = conn.WriteJSON(request)
	state.writeMu.Unlock()
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", host),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		conn.Close()
		return
	}
	slog.Debug(
		fmt.Sprintf("remote connection updated: %s > %v", host, prefixes),
		slog.String("module", "worker"),
		slog.String("group", "realtime"),
	)

	s.mu.Lock()
	s.mergeDemand(prefixes)
	s.mu.Unlock()
	// every prefix of a fresh connection is newly opened; purge only after
	// the listen is on the wire (see EnsureSubscriptions)
	if purger != nil {
		purger.PurgeLatest(prefixes, 2)
	}
}

func (s *LeaderSubscriber) runListener(ctx context.Context, cancel context.CancelFunc, host string, state *SubState, c *websocket.Conn, messageChan chan<- []byte) {
	defer func() {
		cancel()
		c.Close()

		var dropped []string
		s.mu.Lock()
		purger := s.purger
		if cur, ok := s.Subscriptions[host]; ok && cur == state && cur.Connection == c {
			dropped = slices.Clone(cur.Prefixes)
			delete(s.Subscriptions, host)
		}
		s.pruneDemand(dropped)
		s.mu.Unlock()
		if purger != nil && len(dropped) > 0 {
			purger.PurgeLatest(dropped, 1)
		}

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

func (s *LeaderSubscriber) runRelayer(ctx context.Context, cancel context.CancelFunc, host string, state *SubState, c *websocket.Conn, messageChan <-chan []byte) {
	pingTicker := time.NewTicker(pingInterval)
	defer func() {
		cancel()
		c.Close()
		pingTicker.Stop()

		var dropped []string
		s.mu.Lock()
		purger := s.purger
		if cur, ok := s.Subscriptions[host]; ok && cur == state && cur.Connection == c {
			dropped = slices.Clone(cur.Prefixes)
			delete(s.Subscriptions, host)
		}
		s.pruneDemand(dropped)
		s.mu.Unlock()
		if purger != nil && len(dropped) > 0 {
			purger.PurgeLatest(dropped, 1)
		}

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

func (s *LeaderSubscriber) deleteExcessSubscriptions(currentSubs []string) {
	var dropped []string
	s.mu.Lock()
	purger := s.purger

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

	s.pruneDemand(dropped)

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

	if purger != nil && len(dropped) > 0 {
		purger.PurgeLatest(dropped, 1)
	}

	for _, u := range updates {
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
			u.conn.Close()
			continue
		}
		slog.Debug(
			fmt.Sprintf("remote connection updated: %s > %v", u.host, u.prefixes),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}

	if len(closeDomains) > 0 {
		slog.Info(
			fmt.Sprintf("Subscriptions cleaned up: %v", maps.Keys(closeDomains)),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}
}
