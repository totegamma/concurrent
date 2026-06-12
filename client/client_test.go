package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

func TestGetResourceBatchUsesBatchEndpoint(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	uris := []string{
		"cckv://example.test/concrnt.world/profiles/main",
		"cckv://example.test/concrnt.world/profiles/sub",
	}
	resources := map[string]map[string]string{
		uris[0]: {"name": "main"},
		uris[1]: {"name": "sub"},
	}

	var batchHits atomic.Int64
	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve?uri={uri}",
					"net.concrnt.core.batch":   "/batch",
				},
			}
			if err := json.NewEncoder(w).Encode(wkc); err != nil {
				t.Fatalf("encode well-known: %v", err)
			}
		case "/resolve":
			uriParam, err := url.QueryUnescape(r.URL.Query().Get("uri"))
			if err != nil {
				t.Fatalf("unescape uri: %v", err)
			}
			resource, ok := resources[uriParam]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if r.Header.Get("Accept") != "application/json" {
				t.Fatalf("Accept = %q, want application/json", r.Header.Get("Accept"))
			}
			if err := json.NewEncoder(w).Encode(resource); err != nil {
				t.Fatalf("encode resource: %v", err)
			}
		case "/batch":
			batchHits.Add(1)
			serveTestBatch(t, handler, w, r)
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	cl := New(domain)
	cl.AddHostRemapping(domain, server.URL)

	got := make([]map[string]string, len(uris))
	results := []any{&got[0], &got[1]}
	err := cl.GetResourceBatch(context.Background(), uris, "application/json", nil, results)
	if err != nil {
		t.Fatalf("GetResourceBatch returned error: %v", err)
	}

	if batchHits.Load() != 1 {
		t.Fatalf("batch endpoint hit %d times, want 1", batchHits.Load())
	}
	for i, uri := range uris {
		if got[i]["name"] != resources[uri]["name"] {
			t.Fatalf("resource %d name = %q, want %q", i, got[i]["name"], resources[uri]["name"])
		}
	}
}

func TestGetResourceBatchDecodesLargeBatchResponse(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	uris := []string{
		"cckv://example.test/concrnt.world/profiles/main",
		"cckv://example.test/concrnt.world/profiles/sub",
	}
	resources := map[string]map[string]string{
		uris[0]: {"name": "main", "body": strings.Repeat("a", 4096)},
		uris[1]: {"name": "sub", "body": strings.Repeat("b", 4096)},
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve?uri={uri}",
					"net.concrnt.core.batch":   "/batch",
				},
			}
			if err := json.NewEncoder(w).Encode(wkc); err != nil {
				t.Fatalf("encode well-known: %v", err)
			}
		case "/resolve":
			uriParam, err := url.QueryUnescape(r.URL.Query().Get("uri"))
			if err != nil {
				t.Fatalf("unescape uri: %v", err)
			}
			resource, ok := resources[uriParam]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if err := json.NewEncoder(w).Encode(resource); err != nil {
				t.Fatalf("encode resource: %v", err)
			}
		case "/batch":
			serveTestBatch(t, handler, w, r)
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	cl := New(domain)
	cl.AddHostRemapping(domain, server.URL)

	got := make([]map[string]string, len(uris))
	results := []any{&got[0], &got[1]}
	err := cl.GetResourceBatch(context.Background(), uris, "application/json", nil, results)
	if err != nil {
		t.Fatalf("GetResourceBatch returned error: %v", err)
	}

	for i, uri := range uris {
		if got[i]["body"] != resources[uri]["body"] {
			t.Fatalf("resource %d body length = %d, want %d", i, len(got[i]["body"]), len(resources[uri]["body"]))
		}
	}
}

func TestGetResourceBatchFallsBackWhenBatchEndpointMissing(t *testing.T) {
	t.Parallel()

	const domain = "example.test"
	uris := []string{
		"cckv://example.test/item/0",
		"cckv://example.test/item/1",
		"cckv://example.test/item/2",
		"cckv://example.test/item/3",
		"cckv://example.test/item/4",
		"cckv://example.test/item/5",
		"cckv://example.test/item/6",
	}

	var current atomic.Int64
	var maxConcurrent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/concrnt":
			wkc := concrnt.WellKnownConcrnt{
				Version: "2.0",
				Domain:  domain,
				CSID:    "ccs1example",
				Layer:   "concrnt",
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve?uri={uri}",
				},
			}
			if err := json.NewEncoder(w).Encode(wkc); err != nil {
				t.Fatalf("encode well-known: %v", err)
			}
		case "/resolve":
			now := current.Add(1)
			for {
				seen := maxConcurrent.Load()
				if now <= seen || maxConcurrent.CompareAndSwap(seen, now) {
					break
				}
			}
			defer current.Add(-1)

			time.Sleep(25 * time.Millisecond)

			uriParam, err := url.QueryUnescape(r.URL.Query().Get("uri"))
			if err != nil {
				t.Fatalf("unescape uri: %v", err)
			}
			if err := json.NewEncoder(w).Encode(map[string]string{"uri": uriParam}); err != nil {
				t.Fatalf("encode resource: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cl := New(domain)
	cl.AddHostRemapping(domain, server.URL)

	got := make([]map[string]string, len(uris))
	results := make([]any, len(uris))
	for i := range got {
		results[i] = &got[i]
	}

	err := cl.GetResourceBatch(context.Background(), uris, "application/json", nil, results)
	if err != nil {
		t.Fatalf("GetResourceBatch returned error: %v", err)
	}

	if maxConcurrent.Load() <= 1 {
		t.Fatalf("fallback max concurrency = %d, want > 1", maxConcurrent.Load())
	}
	if maxConcurrent.Load() > resourceBatchFallbackConcurrency {
		t.Fatalf("fallback max concurrency = %d, want <= %d", maxConcurrent.Load(), resourceBatchFallbackConcurrency)
	}
	for i, uri := range uris {
		if got[i]["uri"] != uri {
			t.Fatalf("resource %d uri = %q, want %q", i, got[i]["uri"], uri)
		}
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

func serveTestBatch(t *testing.T, handler http.Handler, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse batch content type: %v", err)
	}
	if !strings.EqualFold(mediaType, "multipart/mixed") {
		t.Fatalf("batch content type = %q, want multipart/mixed", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("missing batch boundary")
	}

	w.Header().Set("Content-Type", "multipart/mixed; boundary="+boundary)
	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(boundary); err != nil {
		t.Fatalf("set response boundary: %v", err)
	}
	defer mw.Close()

	mr := multipart.NewReader(r.Body, boundary)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read batch part: %v", err)
		}

		contentID := part.Header.Get("Content-ID")
		req, err := http.ReadRequest(bufio.NewReader(part))
		if err != nil {
			t.Fatalf("read batch request: %v", err)
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		resp := recorder.Result()

		pw, err := mw.CreatePart(map[string][]string{
			"Content-Type": {"application/http"},
			"Content-ID":   {contentID},
		})
		if err != nil {
			t.Fatalf("create batch response part: %v", err)
		}
		if err := resp.Write(pw); err != nil {
			t.Fatalf("write batch response: %v", err)
		}
		resp.Body.Close()
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
