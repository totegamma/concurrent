package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	peerPollTimeout = 2 * time.Second
	// peerDemandGrace keeps a peer's last known demand after a failed poll, so
	// a transient poll failure doesn't tear down remote subscriptions (redis
	// pubsub is fire-and-forget: a reconnect gap loses events).
	peerDemandGrace = 60 * time.Second
)

// PeerDiscovery lists the internal base URLs of all live replicas.
type PeerDiscovery interface {
	Peers(ctx context.Context) ([]string, error)
}

type peerDemand struct {
	prefixes []string
	seenAt   time.Time
}

// PeerDemandClient is a SubscribeClient that reports the union of realtime
// subscription demand across all replicas, collected by polling each replica's
// internal endpoint. Registered on the leader's Subscriber, it makes the
// single upstream connection cover websocket clients attached to any replica.
type PeerDemandClient struct {
	discovery PeerDiscovery
	client    *http.Client

	mu        sync.Mutex
	lastKnown map[string]peerDemand
}

func NewPeerDemandClient(discovery PeerDiscovery) *PeerDemandClient {
	return &PeerDemandClient{
		discovery: discovery,
		client:    &http.Client{Timeout: peerPollTimeout},
		lastKnown: make(map[string]peerDemand),
	}
}

func (p *PeerDemandClient) CurrentSubscriptions() []string {
	ctx, cancel := context.WithTimeout(context.Background(), peerPollTimeout*2)
	defer cancel()

	peers, err := p.discovery.Peers(ctx)
	if err != nil {
		slog.Warn(
			"failed to discover peers, falling back to last known demand",
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "peer-demand"),
		)
		peers = nil
	}

	type pollResult struct {
		peer     string
		prefixes []string
		err      error
	}

	results := make(chan pollResult, len(peers))
	for _, peer := range peers {
		go func(peer string) {
			prefixes, err := p.fetchDemand(ctx, peer)
			results <- pollResult{peer: peer, prefixes: prefixes, err: err}
		}(peer)
	}

	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	for range peers {
		result := <-results
		if result.err != nil {
			slog.Warn(
				"failed to poll peer demand",
				slog.String("peer", result.peer),
				slog.String("error", result.err.Error()),
				slog.String("module", "worker"),
				slog.String("group", "peer-demand"),
			)
			continue
		}
		p.lastKnown[result.peer] = peerDemand{prefixes: result.prefixes, seenAt: now}
	}

	subscriptionSet := make(map[string]bool)
	for peer, demand := range p.lastKnown {
		if now.Sub(demand.seenAt) > peerDemandGrace {
			delete(p.lastKnown, peer)
			continue
		}
		for _, prefix := range demand.prefixes {
			subscriptionSet[prefix] = true
		}
	}

	subscriptions := make([]string, 0, len(subscriptionSet))
	for prefix := range subscriptionSet {
		subscriptions = append(subscriptions, prefix)
	}

	return subscriptions
}

func (p *PeerDemandClient) fetchDemand(ctx context.Context, peer string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer+"/internal/subscriptions", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var prefixes []string
	if err := json.Unmarshal(body, &prefixes); err != nil {
		return nil, err
	}

	return prefixes, nil
}
