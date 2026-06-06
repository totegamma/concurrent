package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/patrickmn/go-cache"
)

func TestQuery(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	since := time.Date(2026, 5, 16, 10, 11, 12, 123456789, time.FixedZone("JST", 9*60*60))
	until := time.Date(2026, 5, 16, 11, 12, 13, 987654321, time.FixedZone("JST", 9*60*60))

	cckv := "cckv://con1example/concrnt.world/profiles/main"
	want := []concrnt.SignedDocument{
		{
			CCKV:     &cckv,
			Document: `{"key":"cckv://con1example/concrnt.world/profiles/main","value":{"username":"tester"},"author":"con1example","schema":"https://schema.concrnt.world/p/main.json","createdAt":"2026-05-16T00:00:00Z"}`,
			Proof:    concrnt.Proof{Type: concrnt.ProofTypeNone},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.query": "/query{?prefix,schema,since,until,limit,order,parent}",
				},
			}
			if err := json.NewEncoder(w).Encode(wkc); err != nil {
				t.Fatalf("encode well-known: %v", err)
			}
		case "/query":
			query := r.URL.Query()
			assertQueryParam(t, query.Get("prefix"), "cckv://")
			assertQueryParam(t, query.Get("schema"), "https://schema.concrnt.world/p/main.json")
			assertQueryParam(t, query.Get("since"), since.UTC().Format(time.RFC3339Nano))
			assertQueryParam(t, query.Get("until"), until.UTC().Format(time.RFC3339Nano))
			assertQueryParam(t, query.Get("limit"), "100")
			assertQueryParam(t, query.Get("order"), "asc")
			assertQueryParam(t, r.Header.Get("User-Agent"), "concrnt-test/dev (Concrnt)")
			assertQueryParam(t, r.Header.Get("Accept"), "application/json")

			if err := json.NewEncoder(w).Encode(want); err != nil {
				t.Fatalf("encode query response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cl := New(domain)
	cl.SetUserAgent("concrnt-test", "dev")
	cl.AddHostRemapping(domain, server.URL)

	got, err := cl.Query(context.Background(), domain, QueryParams{
		Prefix: "cckv://",
		Schema: "https://schema.concrnt.world/p/main.json",
		Since:  &since,
		Until:  &until,
		Limit:  100,
		Order:  "asc",
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("Query returned %d results, want 1", len(got))
	}
	if got[0].CCKV == nil || *got[0].CCKV != cckv {
		t.Fatalf("Query returned CCKV %v, want %q", got[0].CCKV, cckv)
	}
	if got[0].Document != want[0].Document {
		t.Fatalf("Query returned document %q, want %q", got[0].Document, want[0].Document)
	}
}

func TestQueryRejectsPrefixAndParent(t *testing.T) {
	t.Parallel()

	cl := New("example.test")
	_, err := cl.Query(context.Background(), "example.test", QueryParams{
		Prefix: "cckv://",
		Parent: "cckv://con1example",
	})
	if err == nil {
		t.Fatal("Query returned nil error")
	}
}

func TestQueryEndpointMissing(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wkc := concrnt.WellKnownConcrnt{
			Version:   "2.0",
			Domain:    domain,
			CSID:      "ccs1example",
			Layer:     "concrnt",
			Endpoints: map[string]string{},
		}
		if err := json.NewEncoder(w).Encode(wkc); err != nil {
			t.Fatalf("encode well-known: %v", err)
		}
	}))
	defer server.Close()

	cl := New(domain)
	cl.AddHostRemapping(domain, server.URL)

	_, err := cl.Query(context.Background(), domain, QueryParams{Prefix: "cckv://"})
	if !errors.Is(err, ErrEndpointMissing) {
		t.Fatalf("Query returned error %v, want ErrEndpointMissing", err)
	}
}

func TestQuerySkipsOfflineDomain(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	cl := &Client{
		client:     &http.Client{},
		cache:      cache.New(10*time.Minute, 15*time.Minute),
		lastFailed: map[string]time.Time{domain: time.Now()},
		failCount:  make(map[string]int),
	}
	cl.cache.Set("server:"+domain, concrnt.WellKnownConcrnt{
		Version: "2.0",
		Domain:  domain,
		CSID:    "ccs1example",
		Layer:   "concrnt",
		Endpoints: map[string]string{
			"net.concrnt.core.query": "/query{?prefix}",
		},
	}, cache.DefaultExpiration)

	_, err := cl.Query(context.Background(), domain, QueryParams{Prefix: "cckv://"})
	if err == nil || err.Error() != "Domain is offline" {
		t.Fatalf("Query returned error %v, want Domain is offline", err)
	}
}

func TestTimeoutMarksDomainOffline(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	cl := &Client{
		lastFailed: make(map[string]time.Time),
		failCount:  make(map[string]int),
	}

	cl.markOfflineIfTimeout(domain, "testing", timeoutError{})

	if cl.IsOnline(domain) {
		t.Fatal("domain is online after timeout")
	}
}

func assertQueryParam(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ interface {
	error
	Timeout() bool
	Temporary() bool
} = timeoutError{}
