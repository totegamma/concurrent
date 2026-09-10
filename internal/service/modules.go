package service

import (
	"encoding/json"
	"maps"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/concrnt/concrnt/impl/interop"
)

// the usual *_build_info shape for each proxied service: a constant 1 whose
// labels carry what the service reported via /cc-info. `name` is the
// configured service name, the same value the HTTP metrics carry in their
// `service` label, so the two can be joined. a service whose /cc-info could
// not be fetched has no series at all.
var servicesBuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "concrnt",
	Name:      "services_build_info",
	Help:      "Build information of proxied services as reported by their /cc-info; the value is always 1.",
}, []string{"name", "software", "version"})

type ModuleManager struct {
	baseEndpoints map[string]string
	services      []interop.Service
	endpoints     map[string]string

	// label sets currently exported on servicesBuildInfo, so a service that
	// disappears or changes version has its stale series removed
	buildInfoLabels map[[3]string]struct{}
}

func NewModuleManager(
	baseEndpoints map[string]string,
	services []interop.Service,
) *ModuleManager {

	manager := ModuleManager{
		baseEndpoints: baseEndpoints,
		services:      services,
		endpoints:     make(map[string]string),

		buildInfoLabels: make(map[[3]string]struct{}),
	}

	go func() {
		manager.UpdateEndpointRoutine()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			manager.UpdateEndpointRoutine()
		}
	}()

	return &manager
}

func (m *ModuleManager) UpdateEndpointRoutine() {

	endpoints := make(map[string]string)

	maps.Copy(endpoints, m.baseEndpoints)

	client := &http.Client{}

	seen := make(map[[3]string]struct{})

	for _, service := range m.services {
		var resp *http.Response
		var err error

		url := "http://" + service.Host + ":" + strconv.Itoa(service.Port) + "/cc-info"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}

		resp, err = client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var info interop.CCInfo
		err = json.NewDecoder(resp.Body).Decode(&info)
		if err != nil {
			continue
		}

		labels := [3]string{service.Name, info.Name, info.Version}
		servicesBuildInfo.WithLabelValues(labels[0], labels[1], labels[2]).Set(1)
		seen[labels] = struct{}{}

		for key, endpoint := range info.Endpoints {
			if !service.PreservePath {
				endpoint = path.Join(service.Path, endpoint)
			}
			endpoints[key] = endpoint
		}
	}

	for labels := range m.buildInfoLabels {
		if _, ok := seen[labels]; !ok {
			servicesBuildInfo.DeleteLabelValues(labels[0], labels[1], labels[2])
		}
	}
	m.buildInfoLabels = seen

	m.endpoints = endpoints
}

func (m *ModuleManager) GetEndpoints() map[string]string {
	return m.endpoints
}
