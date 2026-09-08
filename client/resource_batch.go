package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/concrnt/concrnt"
	"github.com/patrickmn/go-cache"
)

// batchFallbackConcurrency bounds how many per-request fallback fetches
// (used when a server doesn't support a batch endpoint) run concurrently.
const batchFallbackConcurrency = 8

// batchDomainConcurrency bounds how many domains BatchGet fetches from at
// once. Each domain may additionally fan out up to batchFallbackConcurrency
// per-request fetches when it lacks a batch endpoint.
const batchDomainConcurrency = 8

// runBounded runs fn(i) for every i in [0, n) on at most limit goroutines,
// returning once all calls have finished.
func runBounded(limit, n int, fn func(i int)) {
	var wg sync.WaitGroup
	jobs := make(chan int)

	for range min(limit, n) {
		wg.Go(func() {
			for i := range jobs {
				fn(i)
			}
		})
	}

	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

type resourceBatchItem struct {
	index         int
	uri           string
	endpoint      string
	batchEndpoint string
}

// GetResourceBatch retrieves multiple resources into results.
// results must have the same length as uris, and each element must be a decode target.
func (c *Client) GetResourceBatch(ctx context.Context, uris []string, accept string, opts *Options, results []any) error {
	ctx, span := tracer.Start(ctx, "Client.GetResourceBatch")
	defer span.End()

	if opts == nil {
		opts = &Options{}
	}

	if len(uris) != len(results) {
		err := fmt.Errorf("uris and results length mismatch: %d != %d", len(uris), len(results))
		span.RecordError(err)
		return err
	}

	batchGroups := map[string][]resourceBatchItem{}
	fallbackIndexes := make([]int, 0)

	for i, uri := range uris {
		if results[i] == nil {
			err := fmt.Errorf("nil result target for resource %s", uri)
			span.RecordError(err)
			return err
		}

		if !opts.NoCache {
			cacheKey := "resource:" + uri
			x, found := c.cache.Get(cacheKey)
			if found {
				resultBytes := x.([]byte)
				err := json.Unmarshal(resultBytes, &results[i])
				if err != nil {
					err := errors.Join(fmt.Errorf("failed to unmarshal cached resource for uri %s", uri), err)
					span.RecordError(err)
					return err
				}
				continue
			}
		}

		item, useBatch, err := c.resolveResourceBatchItem(ctx, i, uri, opts)
		if err != nil {
			span.RecordError(err)
			return err
		}
		if !useBatch {
			fallbackIndexes = append(fallbackIndexes, i)
			continue
		}
		batchGroups[item.batchEndpoint] = append(batchGroups[item.batchEndpoint], item)
	}

	for batchEndpoint, items := range batchGroups {
		if err := c.getResourceBatchGroup(ctx, batchEndpoint, items, accept, opts, results); err != nil {
			span.RecordError(err)
			return err
		}
	}

	if len(fallbackIndexes) > 0 {
		if err := c.getResourceBatchFallback(ctx, fallbackIndexes, uris, accept, opts, results); err != nil {
			span.RecordError(err)
			return err
		}
	}

	return nil
}

func (c *Client) resolveResourceBatchItem(ctx context.Context, index int, uri string, opts *Options) (resourceBatchItem, bool, error) {
	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		return resourceBatchItem{}, false, errors.Join(fmt.Errorf("invalid cc uri %s", uri), err)
	}

	if parsed.Scheme == "http" {
		return resourceBatchItem{index: index, uri: uri}, false, nil
	}

	var info concrnt.WellKnownConcrnt
	if opts.Resolver != "" {
		info, err = c.GetServer(ctx, opts.Resolver, nil)
		if err != nil {
			return resourceBatchItem{}, false, errors.Join(fmt.Errorf("failed to get server for resolver %s", opts.Resolver), err)
		}
	} else {
		domain, err := c.resolveResolver(ctx, parsed.Owner)
		if err != nil {
			return resourceBatchItem{}, false, errors.Join(fmt.Errorf("failed to resolve default resolver for owner %s", parsed.Owner), err)
		}
		info, err = c.GetServer(ctx, domain, nil)
		if err != nil {
			return resourceBatchItem{}, false, errors.Join(fmt.Errorf("failed to get server for default resolver %s", domain), err)
		}
	}

	resolveDesc, ok := info.Endpoints["net.concrnt.core.resolve"]
	if !ok {
		return resourceBatchItem{}, false, fmt.Errorf("resource endpoint not found in server %s", info.Domain)
	}

	resolvePath, err := concrnt.RenderURITemplate(resolveDesc, map[string]string{
		"owner": parsed.Owner,
		"key":   parsed.Key,
		"uri":   url.QueryEscape(uri),
	})
	if err != nil {
		return resourceBatchItem{}, false, errors.Join(fmt.Errorf("failed to render resource endpoint template for server %s", info.Domain), err)
	}

	batchDesc, ok := info.Endpoints["net.concrnt.core.batch"]
	if !ok {
		return resourceBatchItem{index: index, uri: uri}, false, nil
	}

	batchPath, err := concrnt.RenderURITemplate(batchDesc, map[string]string{})
	if err != nil {
		return resourceBatchItem{}, false, errors.Join(fmt.Errorf("failed to render batch endpoint template for server %s", info.Domain), err)
	}

	return resourceBatchItem{
		index:         index,
		uri:           uri,
		endpoint:      "https://" + info.Domain + resolvePath,
		batchEndpoint: "https://" + info.Domain + batchPath,
	}, true, nil
}

func (c *Client) getResourceBatchGroup(ctx context.Context, batchEndpoint string, items []resourceBatchItem, accept string, opts *Options, results []any) error {
	endpointURL, err := url.Parse(batchEndpoint)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to parse batch endpoint %s", batchEndpoint), err)
	}
	domain := endpointURL.Hostname()
	if domain != "" && !c.IsOnline(domain) {
		return fmt.Errorf("Domain is offline")
	}

	requests := make(map[string]*http.Request, len(items))
	itemByID := make(map[string]resourceBatchItem, len(items))
	for _, item := range items {
		contentID := strconv.Itoa(item.index)
		req, err := http.NewRequestWithContext(ctx, "GET", item.endpoint, nil)
		if err != nil {
			return errors.Join(fmt.Errorf("failed to create request for resource %s", item.uri), err)
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		requests[contentID] = req
		itemByID[contentID] = item
	}

	responses, err := DoBatchRequestWithClient(ctx, c.client, batchEndpoint, requests)
	if err != nil {
		c.markOfflineIfTimeout(domain, "getting resource batch", err)
		return errors.Join(fmt.Errorf("failed to perform batch request to %s", batchEndpoint), err)
	}

	for contentID, item := range itemByID {
		resp, ok := responses[contentID]
		if !ok {
			return fmt.Errorf("missing batch response for resource %s", item.uri)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get resource %s: status code %d", item.uri, resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(&results[item.index])
		if err != nil {
			return errors.Join(fmt.Errorf("failed to decode resource %s", item.uri), err)
		}

		if !opts.NoCache {
			bytes, err := json.Marshal(results[item.index])
			if err != nil {
				return errors.Join(fmt.Errorf("failed to marshal resource for caching for uri %s", item.uri), err)
			}
			c.cache.Set("resource:"+item.uri, bytes, cache.DefaultExpiration)
		}
	}

	return nil
}

func (c *Client) getResourceBatchFallback(ctx context.Context, indexes []int, uris []string, accept string, opts *Options, results []any) error {
	var mu sync.Mutex
	var err error

	runBounded(batchFallbackConcurrency, len(indexes), func(i int) {
		index := indexes[i]
		if e := c.GetResource(ctx, uris[index], accept, opts, results[index]); e != nil {
			mu.Lock()
			err = errors.Join(err, e)
			mu.Unlock()
		}
	})

	return err
}
