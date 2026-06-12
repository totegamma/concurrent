package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type batchChunklineRepo struct {
	mu    sync.Mutex
	calls []batchChunklineLookupCall
}

type batchChunklineLookupCall struct {
	uris    []string
	chunkID int64
}

func (r *batchChunklineRepo) GetChunklineManifest(ctx context.Context, uri string) (*chunkline.Manifest, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *batchChunklineRepo) LookupLocalItrs(ctx context.Context, uris []string, chunkID int64) (map[string]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, batchChunklineLookupCall{
		uris:    append([]string(nil), uris...),
		chunkID: chunkID,
	})

	results := make(map[string]int64, len(uris))
	for i, uri := range uris {
		results[uri] = chunkID - int64(i)
	}
	return results, nil
}

func (r *batchChunklineRepo) LoadLocalBody(ctx context.Context, uri string, chunkID int64) ([]chunkline.BodyItem, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestBatchHandlerAggregatesChunklineItrRequests(t *testing.T) {
	repo := &batchChunklineRepo{}
	handler := NewHandler(
		domain.Config{},
		nil,
		nil,
		usecase.NewChunklineUsecase(repo, nil),
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	requests := map[string]*http.Request{}
	req0, err := http.NewRequest("GET", server.URL+"/chunkline/itr/100?uri=cckv://example.test/timeline/0", nil)
	require.NoError(t, err)
	requests["0"] = req0
	req1, err := http.NewRequest("GET", server.URL+"/chunkline/itr/100?uri=cckv://example.test/timeline/1", nil)
	require.NoError(t, err)
	requests["1"] = req1

	responses, err := client.DoBatchRequestWithClient(context.Background(), server.Client(), server.URL+"/batch", requests)
	require.NoError(t, err)

	require.Len(t, responses, 2)
	require.Equal(t, http.StatusOK, responses["0"].StatusCode)
	require.Equal(t, http.StatusOK, responses["1"].StatusCode)

	require.Len(t, repo.calls, 1)
	require.Equal(t, int64(100), repo.calls[0].chunkID)
	require.ElementsMatch(t, []string{
		"cckv://example.test/timeline/0",
		"cckv://example.test/timeline/1",
	}, repo.calls[0].uris)
}

var _ usecase.ChunklineRepository = (*batchChunklineRepo)(nil)
