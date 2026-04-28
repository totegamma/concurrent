package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func resolveCCID2Domain(ccid string) (string, error) {
	switch ccid {
	case "ccs15zt7a8kxv2k9pguy4m6wfucwj2makte8fzfl7v":
		return "denken.concrnt.net", nil
	default:
		return "", fmt.Errorf("unknown ccid: %s", ccid)
	}
}

func parseCCURI(escaped string) (string, string, error) {
	uriString, err := url.QueryUnescape(escaped)
	if err != nil {
		return "", "", fmt.Errorf("invalid uri encoding")
	}
	uri, err := url.Parse(uriString)
	if err != nil {
		return "", "", fmt.Errorf("invalid uri")
	}

	if uri.Scheme != "cc" {
		return "", "", fmt.Errorf("unsupported uri scheme")
	}

	owner := uri.Host
	path := uri.Path

	key := strings.TrimPrefix(path, "/")

	return owner, key, nil
}

func getServerInfo(cdid string) (WellKnownConcrnt, error) {

	domain, err := resolveCCID2Domain(cdid)
	if err != nil {
		return WellKnownConcrnt{}, fmt.Errorf("failed to resolve ccid to domain: %v", err)
	}

	resp, err := http.Get("https://" + domain + "/.well-known/concrnt")
	if err != nil {
		return WellKnownConcrnt{}, fmt.Errorf("failed to get well-known concrnt: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return WellKnownConcrnt{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var wkc WellKnownConcrnt
	err = json.NewDecoder(resp.Body).Decode(&wkc)
	if err != nil {
		return WellKnownConcrnt{}, fmt.Errorf("failed to decode well-known concrnt: %v", err)
	}
	return wkc, nil

}

func resolveResource[T any](uri string, accept string) (T, error) {

	result := *new(T)

	owner, key, err := parseCCURI(uri)
	if err != nil {
		return result, fmt.Errorf("failed to parse cc uri: %v", err)
	}

	info, err := getServerInfo(owner)
	if err != nil {
		return result, fmt.Errorf("failed to get server info: %v", err)
	}

	endpoint, ok := info.Endpoints["net.concrnt.core.resource"]
	if !ok {
		return result, fmt.Errorf("resource endpoint not found")
	}

	endpoint = strings.ReplaceAll(endpoint, "{ccid}", owner)
	endpoint = strings.ReplaceAll(endpoint, "{key}", key)
	endpoint = strings.ReplaceAll(endpoint, "{uri}", url.QueryEscape(uri))
	endpoint = "https://" + info.Domain + endpoint

	fmt.Printf("Resolved endpoint: %s\n", endpoint)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return result, fmt.Errorf("failed to create request: %v", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("failed to get resource: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return result, fmt.Errorf("failed to decode resource: %v", err)
	}

	return result, nil
}

type WellKnownConcrnt struct {
	Version   string            `json:"version"`
	Domain    string            `json:"domain"`
	CSID      string            `json:"csid"`
	Layer     string            `json:"layer"`
	Endpoints map[string]string `json:"endpoints"`
}
