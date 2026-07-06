package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/concrnt/concrnt/internal/testutil"
)

type fakeLeaderLocator struct {
	url string
	ok  bool
}

func (f fakeLeaderLocator) LeaderURL() (string, bool) { return f.url, f.ok }

func TestWorkerCurrentSubscriptionsFetchesFromLeader(t *testing.T) {
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/subscriptions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["cckv://alice/home"]`))
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})

	got := w.CurrentSubscriptions()
	if !reflect.DeepEqual(got, []string{"cckv://alice/home"}) {
		t.Fatalf("expected leader's aggregate, got %v", got)
	}
}

// Repeated calls within the TTL must be served from cache, not refetched: a
// chunkline cache-freshness check runs on every read, so an unbounded fetch
// rate would hammer the leader.
func TestWorkerCurrentSubscriptionsCachesWithinTTL(t *testing.T) {
	hits := 0
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["cckv://alice/home"]`))
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})

	w.CurrentSubscriptions()
	w.CurrentSubscriptions()
	w.CurrentSubscriptions()

	if hits != 1 {
		t.Fatalf("expected a single fetch within the cache TTL, got %d", hits)
	}
}

func TestWorkerCurrentSubscriptionsRefetchesAfterTTL(t *testing.T) {
	hits := 0
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["cckv://alice/home"]`))
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})
	w.CurrentSubscriptions()

	w.mu.Lock()
	w.cachedAt = time.Now().Add(-2 * workerDemandCacheTTL)
	w.mu.Unlock()

	w.CurrentSubscriptions()

	if hits != 2 {
		t.Fatalf("expected a refetch once the cache expired, got %d hits", hits)
	}
}

func TestWorkerCurrentSubscriptionsNoLeaderKnown(t *testing.T) {
	w := NewWorkerSubscriber(fakeLeaderLocator{ok: false})
	if got := w.CurrentSubscriptions(); got != nil {
		t.Fatalf("expected nil when no leader is known, got %v", got)
	}
}

func TestWorkerEnsureSubscriptionsForwardsToLeader(t *testing.T) {
	received := make(chan []string, 1)
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/internal/subscriptions/ensure" {
			http.NotFound(w, req)
			return
		}
		var body EnsureRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		received <- body.Prefixes
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})
	w.EnsureSubscriptions(context.Background(), []string{"cckv://bob/home"})

	select {
	case prefixes := <-received:
		if !reflect.DeepEqual(prefixes, []string{"cckv://bob/home"}) {
			t.Fatalf("unexpected prefixes forwarded: %v", prefixes)
		}
	default:
		t.Fatal("leader did not receive the forward")
	}
}

func TestWorkerEnsureSubscriptionsNoLeaderKnown(t *testing.T) {
	w := NewWorkerSubscriber(fakeLeaderLocator{ok: false})
	// must not panic or block when no leader is known
	w.EnsureSubscriptions(context.Background(), []string{"cckv://bob/home"})
}

// A failed fetch must be cached like a success: during a leader outage each
// replica probes at most once per TTL instead of once per chunkline read.
func TestWorkerCurrentSubscriptionsNegativeCachesFailures(t *testing.T) {
	hits := 0
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})

	if got := w.CurrentSubscriptions(); got != nil {
		t.Fatalf("expected nil on failure, got %v", got)
	}
	if got := w.CurrentSubscriptions(); got != nil {
		t.Fatalf("expected nil from the negative cache, got %v", got)
	}
	if hits != 1 {
		t.Fatalf("expected a single fetch attempt within the TTL after a failure, got %d", hits)
	}
}

// Concurrent callers racing an expired cache must not each dial the leader:
// one refresh flies, the rest reuse the cached value immediately.
func TestWorkerCurrentSubscriptionsSingleflight(t *testing.T) {
	var hits atomic.Int64
	release := make(chan struct{})
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["cckv://alice/home"]`))
	}))
	defer leader.Close()

	w := NewWorkerSubscriber(fakeLeaderLocator{url: leader.URL, ok: true})

	first := make(chan []string, 1)
	go func() { first <- w.CurrentSubscriptions() }()

	// wait until the first fetch is holding the in-flight slot
	testutil.WaitFor(t, func() bool { return hits.Load() == 1 })

	// with the fetch still blocked, these must return promptly without dialing
	done := make(chan struct{})
	go func() {
		for range 5 {
			w.CurrentSubscriptions()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent callers blocked behind the in-flight fetch")
	}

	close(release)
	if got := <-first; !reflect.DeepEqual(got, []string{"cckv://alice/home"}) {
		t.Fatalf("unexpected fetch result: %v", got)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly one fetch, got %d", got)
	}
}
