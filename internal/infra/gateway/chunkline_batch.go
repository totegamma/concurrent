package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
)

const chunklineBatchFallbackConcurrency = 5

type chunklineBatchTask struct {
	key      string
	endpoint *url.URL
	fallback func(context.Context) ([]byte, error)
}

func (r *resolver) fetchChunklineBatch(ctx context.Context, tasks []chunklineBatchTask) (map[string][]byte, map[string]error) {
	results := make(map[string][]byte, len(tasks))
	errs := make(map[string]error)
	fallbackTasks := make([]chunklineBatchTask, 0)
	groups := make(map[string][]chunklineBatchTask)

	for _, task := range tasks {
		batchEndpoint, ok := r.batchEndpointForURL(ctx, task.endpoint)
		if !ok {
			fallbackTasks = append(fallbackTasks, task)
			continue
		}
		groups[batchEndpoint] = append(groups[batchEndpoint], task)
	}

	for batchEndpoint, group := range groups {
		groupResults, groupErrs, failed := r.fetchChunklineBatchGroup(ctx, batchEndpoint, group)
		for key, value := range groupResults {
			results[key] = value
		}
		for key, err := range groupErrs {
			errs[key] = err
		}
		fallbackTasks = append(fallbackTasks, failed...)
	}

	fallbackResults, fallbackErrs := r.fetchChunklineBatchFallback(ctx, fallbackTasks)
	for key, value := range fallbackResults {
		results[key] = value
	}
	for key, err := range fallbackErrs {
		errs[key] = err
	}

	return results, errs
}

func (r *resolver) batchEndpointForURL(ctx context.Context, endpoint *url.URL) (string, bool) {
	if endpoint == nil || endpoint.Host == "" || endpoint.Scheme != "https" {
		return "", false
	}

	server, err := r.client.GetServer(ctx, endpoint.Host, nil)
	if err != nil {
		return "", false
	}

	desc, ok := server.Endpoints["net.concrnt.core.batch"]
	if !ok {
		return "", false
	}

	path, err := concrnt.RenderURITemplate(desc, map[string]string{})
	if err != nil {
		return "", false
	}

	return "https://" + server.Domain + path, true
}

func (r *resolver) fetchChunklineBatchGroup(ctx context.Context, batchEndpoint string, tasks []chunklineBatchTask) (map[string][]byte, map[string]error, []chunklineBatchTask) {
	requests := make(map[string]*http.Request, len(tasks))
	taskByKey := make(map[string]chunklineBatchTask, len(tasks))
	errs := make(map[string]error)
	for _, task := range tasks {
		req, err := http.NewRequestWithContext(ctx, "GET", task.endpoint.String(), nil)
		if err != nil {
			errs[task.key] = err
			continue
		}
		requests[task.key] = req
		taskByKey[task.key] = task
	}
	if len(requests) == 0 {
		return nil, errs, nil
	}

	responses, err := client.DoBatchRequestWithClient(ctx, r.client.GetClient(), batchEndpoint, requests)
	if err != nil {
		return nil, nil, tasks
	}

	results := make(map[string][]byte, len(tasks))
	for key, task := range taskByKey {
		resp, ok := responses[key]
		if !ok {
			errs[key] = fmt.Errorf("missing batch response for %s", task.endpoint.String())
			continue
		}

		body, err := readChunklineBatchResponse(resp)
		if err != nil {
			errs[key] = err
			continue
		}
		results[key] = body
	}

	return results, errs, nil
}

func (r *resolver) fetchChunklineBatchFallback(ctx context.Context, tasks []chunklineBatchTask) (map[string][]byte, map[string]error) {
	results := make(map[string][]byte, len(tasks))
	errs := make(map[string]error)
	if len(tasks) == 0 {
		return results, errs
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	jobs := make(chan chunklineBatchTask)

	workerCount := min(chunklineBatchFallbackConcurrency, len(tasks))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				body, err := task.fallback(ctx)
				mu.Lock()
				if err != nil {
					errs[task.key] = err
				} else {
					results[task.key] = body
				}
				mu.Unlock()
			}
		}()
	}

	for _, task := range tasks {
		jobs <- task
	}
	close(jobs)
	wg.Wait()

	return results, errs
}

func readChunklineBatchResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 response: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
