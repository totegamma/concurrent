package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	workerHTTPTimeout    = 2 * time.Second
	workerDemandCacheTTL = 3 * time.Second
)

// LeaderLocator is the subset of cluster.Elector a WorkerSubscriber needs to
// find the current leader's internal base URL.
type LeaderLocator interface {
	LeaderURL() (string, bool)
}

// EnsureRequest is the payload of POST /internal/subscriptions/ensure.
type EnsureRequest struct {
	Prefixes []string `json:"prefixes"`
}

// WorkerSubscriber is the Subscriber that runs on non-leader replicas: it
// never dials an upstream websocket itself, only relays subscription demand
// to whichever replica currently holds cluster leadership.
type WorkerSubscriber struct {
	elector LeaderLocator
	client  *http.Client

	mu       sync.Mutex
	cached   []string
	cachedAt time.Time
	fetching bool // a refresh is in flight; concurrent callers reuse cached
}

func NewWorkerSubscriber(elector LeaderLocator) *WorkerSubscriber {
	return &WorkerSubscriber{
		elector: elector,
		client:  &http.Client{Timeout: workerHTTPTimeout},
	}
}

// CurrentSubscriptions asks the leader for the cluster-wide aggregate
// subscription state, short-TTL cached so that a frequent caller (e.g. the
// chunkline cache-freshness check on every read) doesn't hammer the leader.
// Failures are cached too (as "no subscriptions") so an unreachable leader
// costs at most one 2s attempt per TTL per replica instead of one per read,
// and only one refresh is ever in flight — concurrent callers reuse the
// cached value. Understating demand only costs a cache write, never
// correctness.
func (w *WorkerSubscriber) CurrentSubscriptions() []string {
	w.mu.Lock()
	if time.Since(w.cachedAt) < workerDemandCacheTTL || w.fetching {
		cached := w.cached
		w.mu.Unlock()
		return cached
	}
	w.fetching = true
	w.mu.Unlock()

	var prefixes []string
	defer func() {
		w.mu.Lock()
		w.fetching = false
		w.cached = prefixes
		w.cachedAt = time.Now()
		w.mu.Unlock()
	}()

	leaderURL, ok := w.elector.LeaderURL()
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), workerHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, leaderURL+"/internal/subscriptions", nil)
	if err != nil {
		return nil
	}
	resp, err := w.client.Do(req)
	if err != nil {
		slog.Warn(
			"failed to fetch aggregate subscriptions from leader",
			slog.String("leader", leaderURL),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn(
			"leader rejected aggregate subscriptions request",
			slog.String("leader", leaderURL),
			slog.Int("status", resp.StatusCode),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(&prefixes); err != nil {
		prefixes = nil
		return nil
	}

	return prefixes
}

// EnsureSubscriptions forwards subscription demand to the leader. Best-effort
// — the leader's periodic peer poll (see LeaderSubscriber.pollPeerDemand) is
// the safety net if this forward is lost.
func (w *WorkerSubscriber) EnsureSubscriptions(ctx context.Context, prefixes []string) {
	leaderURL, ok := w.elector.LeaderURL()
	if !ok {
		slog.Warn(
			"no leader known, skipping subscription forward",
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return
	}

	body, err := json.Marshal(EnsureRequest{Prefixes: prefixes})
	if err != nil {
		slog.Error("failed to marshal ensure request", slog.String("error", err.Error()))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, leaderURL+"/internal/subscriptions/ensure", bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create ensure request", slog.String("error", err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		slog.Warn(
			"failed to forward subscription demand to leader",
			slog.String("leader", leaderURL),
			slog.String("error", err.Error()),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn(
			"leader rejected subscription forward",
			slog.String("leader", leaderURL),
			slog.Int("status", resp.StatusCode),
			slog.String("module", "worker"),
			slog.String("group", "realtime"),
		)
	}
}
