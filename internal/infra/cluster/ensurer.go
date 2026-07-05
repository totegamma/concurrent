package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Ensurer is the local subscription ensurer (satisfied by *worker.Subscriber).
type Ensurer interface {
	EnsureSubscriptions(ctx context.Context, prefixes []string)
}

// EnsureRequest is the payload of POST /internal/subscriptions/ensure.
type EnsureRequest struct {
	Prefixes []string `json:"prefixes"`
}

// RoutingEnsurer applies subscription demand on the leader: locally when this
// replica is the leader, otherwise by forwarding to the leader's internal
// endpoint. Forwarding is best-effort — the leader's periodic peer poll is the
// safety net.
type RoutingEnsurer struct {
	elector Elector
	local   Ensurer
	client  *http.Client
}

func NewRoutingEnsurer(elector Elector, local Ensurer) *RoutingEnsurer {
	return &RoutingEnsurer{
		elector: elector,
		local:   local,
		client:  &http.Client{Timeout: 2 * time.Second},
	}
}

func (r *RoutingEnsurer) EnsureSubscriptions(ctx context.Context, prefixes []string) {
	if r.elector.IsLeader() {
		r.local.EnsureSubscriptions(ctx, prefixes)
		return
	}

	leaderURL, ok := r.elector.LeaderURL()
	if !ok {
		slog.Warn(
			"no leader known, skipping subscription forward",
			slog.String("module", "cluster"),
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

	resp, err := r.client.Do(req)
	if err != nil {
		slog.Warn(
			"failed to forward subscription demand to leader",
			slog.String("leader", leaderURL),
			slog.String("error", err.Error()),
			slog.String("module", "cluster"),
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn(
			"leader rejected subscription forward",
			slog.String("leader", leaderURL),
			slog.Int("status", resp.StatusCode),
			slog.String("module", "cluster"),
		)
	}
}
