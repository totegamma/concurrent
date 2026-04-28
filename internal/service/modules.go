package service

import (
	"encoding/json"
	"maps"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/totegamma/concrnt-playground/impl/interop"
)

type ModuleManager struct {
	baseEndpoints map[string]string
	services      []interop.Service
	endpoints     map[string]string
}

func NewModuleManager(
	baseEndpoints map[string]string,
	services []interop.Service,
) *ModuleManager {

	manager := ModuleManager{
		baseEndpoints: baseEndpoints,
		services:      services,
		endpoints:     make(map[string]string),
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

		for key, endpoint := range info.Endpoints {
			endpoint = path.Join(service.Path, endpoint)
			endpoints[key] = endpoint
		}
	}

	m.endpoints = endpoints
}

func (m *ModuleManager) GetEndpoints() map[string]string {
	return m.endpoints
}
