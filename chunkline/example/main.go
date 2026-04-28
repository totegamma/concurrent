package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/totegamma/concrnt-playground/chunkline"
)

type resolver struct {
}

func (r *resolver) ResolveTimelines(ctx context.Context, timelines []string) (map[string]chunkline.Manifest, error) {

	result := make(map[string]chunkline.Manifest)
	for _, tl := range timelines {
		manifest, err := resolveResource[chunkline.Manifest](tl, "application/chunkline+json")
		if err != nil {
			return nil, fmt.Errorf("failed to resolve timeline %s: %v", tl, err)
		}
		result[tl] = manifest
	}
	return result, nil

}

func (r *resolver) GetRemovedItems(ctx context.Context, timelines []string) (map[string][]string, error) {
	result := make(map[string][]string)
	for _, tl := range timelines {
		result[tl] = []string{}
	}
	return result, nil
}

func (r *resolver) LookupChunkItrs(ctx context.Context, timelines []string, until time.Time) (map[string]string, error) {
	fmt.Println("Looking up chunk iterators...")

	manifests, err := r.ResolveTimelines(ctx, timelines)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, tl := range timelines {

		manifest := manifests[tl]

		if manifest.Descending.Iterator == "" {
			return nil, fmt.Errorf("timeline %s does not support descending iteration", tl)
		}

		owner, _, err := parseCCURI(tl)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timeline URI %s: %v", tl, err)
		}

		domain, err := resolveCCID2Domain(owner)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve ccid to domain: %v", err)
		}

		chunkId := manifest.Time2Chunk(until)

		endpoint := "https://" + domain + manifest.Descending.Iterator
		endpoint = strings.ReplaceAll(endpoint, "{chunk}", fmt.Sprintf("%d", chunkId))

		resp, err := http.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get iterator from %s: %v", endpoint, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, endpoint)
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read iterator from %s: %v", endpoint, err)
		}
		result[tl] = string(bytes)

	}
	return result, nil
}

func (r *resolver) LoadChunkBodies(ctx context.Context, query map[string]string) (map[string]chunkline.BodyChunk, error) {
	fmt.Println("Loading chunk bodies...")

	uris := []string{}
	for itr := range query {
		uris = append(uris, itr)
	}

	manifests, err := r.ResolveTimelines(ctx, uris)
	if err != nil {
		return nil, err
	}

	result := make(map[string]chunkline.BodyChunk)
	for tl, itr := range query {

		manifest := manifests[tl]

		owner, _, err := parseCCURI(tl)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timeline URI %s: %v", tl, err)
		}

		domain, err := resolveCCID2Domain(owner)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve ccid to domain: %v", err)
		}

		endpoint := "https://" + domain + manifest.Descending.Body
		endpoint = strings.ReplaceAll(endpoint, "{chunk}", itr)

		resp, err := http.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get chunk body from %s: %v", endpoint, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, endpoint)
		}

		var items []chunkline.BodyItem
		err = json.NewDecoder(resp.Body).Decode(&items)
		if err != nil {
			return nil, fmt.Errorf("failed to decode chunk body from %s: %v", endpoint, err)
		}

		chunkID, err := strconv.ParseInt(itr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk ID %s: %v", itr, err)
		}

		result[tl] = chunkline.BodyChunk{
			URI:     tl,
			ChunkID: chunkID,
			Items:   items,
		}

	}
	return result, nil

}

func main() {
	ctx := context.Background()

	resolver := &resolver{}
	client := chunkline.NewClient(resolver)

	timelines := []string{
		"cc://ccs15zt7a8kxv2k9pguy4m6wfucwj2makte8fzfl7v/tjv0dzcwkcm7jmnbp06afthgerc",
		"cc://ccs15zt7a8kxv2k9pguy4m6wfucwj2makte8fzfl7v/t2rdc4zdvpvn8a6ms067ztjt430",
	}

	items, err := client.QueryDescending(ctx, timelines, time.Now(), 100)
	if err != nil {
		fmt.Println("Error querying items:", err)
	}

	for _, item := range items {
		fmt.Printf("Item: %+v\n", item)
	}
}
