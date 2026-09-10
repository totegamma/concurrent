package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/concrnt/concrnt/impl/interop"
)

func TestModuleManagerServicesBuildInfo(t *testing.T) {
	version := "1.0.0"
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"concrnt-activitypub","version":"` + version + `","endpoints":{}}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	m := &ModuleManager{
		baseEndpoints:   map[string]string{},
		services:        []interop.Service{{Name: "activitypub", Host: u.Hostname(), Port: port, Path: "/ap"}},
		endpoints:       map[string]string{},
		buildInfoLabels: map[[3]string]struct{}{},
	}

	m.UpdateEndpointRoutine()
	if got := testutil.ToFloat64(servicesBuildInfo.WithLabelValues("activitypub", "concrnt-activitypub", "1.0.0")); got != 1 {
		t.Fatalf("expected series for 1.0.0, got %v", got)
	}

	// a version bump replaces the old series instead of leaving both
	version = "1.1.0"
	m.UpdateEndpointRoutine()
	if n := testutil.CollectAndCount(servicesBuildInfo); n != 1 {
		t.Fatalf("expected exactly one series after version bump, got %d", n)
	}
	if got := testutil.ToFloat64(servicesBuildInfo.WithLabelValues("activitypub", "concrnt-activitypub", "1.1.0")); got != 1 {
		t.Fatalf("expected series for 1.1.0, got %v", got)
	}

	// an unreachable service drops out entirely
	up = false
	m.UpdateEndpointRoutine()
	if n := testutil.CollectAndCount(servicesBuildInfo); n != 0 {
		t.Fatalf("expected no series while service is down, got %d", n)
	}
}
