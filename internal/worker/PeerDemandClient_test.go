package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

type staticDiscovery struct {
	peers []string
}

func (d staticDiscovery) Peers(ctx context.Context) ([]string, error) {
	return d.peers, nil
}

func demandServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/subscriptions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func TestPeerDemandUnion(t *testing.T) {
	peerA := demandServer(t, `["cckv://alice/home"]`)
	defer peerA.Close()
	peerB := demandServer(t, `["cckv://bob/home"]`)
	defer peerB.Close()

	p := NewPeerDemandClient(staticDiscovery{peers: []string{peerA.URL, peerB.URL}})

	subs := p.CurrentSubscriptions()
	if !slices.Contains(subs, "cckv://alice/home") || !slices.Contains(subs, "cckv://bob/home") {
		t.Fatalf("expected union of both peers, got %v", subs)
	}
}

// A transiently unreachable peer must keep contributing its last known demand
// (within the grace period), so that remote subscriptions aren't torn down by
// a single failed poll.
func TestPeerDemandKeepsLastKnownOnFailure(t *testing.T) {
	peerA := demandServer(t, `["cckv://alice/home"]`)

	p := NewPeerDemandClient(staticDiscovery{peers: []string{peerA.URL}})

	subs := p.CurrentSubscriptions()
	if !slices.Contains(subs, "cckv://alice/home") {
		t.Fatalf("expected initial demand, got %v", subs)
	}

	peerA.Close()

	subs = p.CurrentSubscriptions()
	if !slices.Contains(subs, "cckv://alice/home") {
		t.Fatalf("expected last known demand to survive a failed poll, got %v", subs)
	}

	// past the grace period the peer's demand must be dropped
	p.mu.Lock()
	for peer, demand := range p.lastKnown {
		demand.seenAt = time.Now().Add(-2 * peerDemandGrace)
		p.lastKnown[peer] = demand
	}
	p.mu.Unlock()

	subs = p.CurrentSubscriptions()
	if slices.Contains(subs, "cckv://alice/home") {
		t.Fatalf("expected demand to expire after grace period, got %v", subs)
	}
}
