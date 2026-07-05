package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type fakeElector struct {
	leader    bool
	leaderURL string
}

func (f fakeElector) Run(ctx context.Context, onLead func(ctx context.Context)) {}
func (f fakeElector) IsLeader() bool                                            { return f.leader }
func (f fakeElector) LeaderURL() (string, bool) {
	return f.leaderURL, f.leaderURL != ""
}

type recordingEnsurer struct {
	prefixes []string
}

func (r *recordingEnsurer) EnsureSubscriptions(ctx context.Context, prefixes []string) {
	r.prefixes = prefixes
}

func TestRoutingEnsurerLocalWhenLeader(t *testing.T) {
	local := &recordingEnsurer{}
	r := NewRoutingEnsurer(fakeElector{leader: true}, local)

	r.EnsureSubscriptions(context.Background(), []string{"cckv://alice/home"})

	if !reflect.DeepEqual(local.prefixes, []string{"cckv://alice/home"}) {
		t.Fatalf("expected local ensure, got %v", local.prefixes)
	}
}

func TestRoutingEnsurerForwardsToLeader(t *testing.T) {
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

	local := &recordingEnsurer{}
	r := NewRoutingEnsurer(fakeElector{leader: false, leaderURL: leader.URL}, local)

	r.EnsureSubscriptions(context.Background(), []string{"cckv://bob/home"})

	select {
	case prefixes := <-received:
		if !reflect.DeepEqual(prefixes, []string{"cckv://bob/home"}) {
			t.Fatalf("unexpected prefixes forwarded: %v", prefixes)
		}
	default:
		t.Fatal("leader did not receive the forward")
	}

	if local.prefixes != nil {
		t.Fatalf("local ensurer must not be called on a non-leader, got %v", local.prefixes)
	}
}
