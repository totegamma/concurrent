package rest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sync"
	"testing"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type batchChunklineRepo struct {
	mu      sync.Mutex
	calls   []batchChunklineLookupCall
	missing map[string]bool
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
		if r.missing[uri] {
			continue
		}
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
		usecase.NewChunklineUsecase(repo, nil, nil),
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
	req0, err := http.NewRequest("GET", server.URL+apiPrefix+"/chunkline/itr/100?uri=cckv://example.test/timeline/0", nil)
	require.NoError(t, err)
	requests["0"] = req0
	req1, err := http.NewRequest("GET", server.URL+apiPrefix+"/chunkline/itr/100?uri=cckv://example.test/timeline/1", nil)
	require.NoError(t, err)
	requests["1"] = req1

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	responses, err := client.DoBatchRequestWithClient(context.Background(), server.Client(), server.URL+batchPath, requests)
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

// A timeline with no iterator (absent from the lookup result) must yield a
// 404, not a 200 "0", both on the plain endpoint and inside a batch part.
func TestChunklineItrMissingIsNotFound(t *testing.T) {
	present := "cckv://example.test/timeline/0"
	absent := "cckv://example.test/timeline/empty"

	repo := &batchChunklineRepo{missing: map[string]bool{absent: true}}
	handler := NewHandler(
		domain.Config{},
		nil,
		nil,
		usecase.NewChunklineUsecase(repo, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + apiPrefix + "/chunkline/itr/100?uri=" + absent)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	requests := map[string]*http.Request{}
	req0, err := http.NewRequest("GET", server.URL+apiPrefix+"/chunkline/itr/100?uri="+present, nil)
	require.NoError(t, err)
	requests["0"] = req0
	req1, err := http.NewRequest("GET", server.URL+apiPrefix+"/chunkline/itr/100?uri="+absent, nil)
	require.NoError(t, err)
	requests["1"] = req1

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	responses, err := client.DoBatchRequestWithClient(context.Background(), server.Client(), server.URL+batchPath, requests)
	require.NoError(t, err)

	require.Len(t, responses, 2)
	require.Equal(t, http.StatusOK, responses["0"].StatusCode)
	require.Equal(t, http.StatusNotFound, responses["1"].StatusCode)
}

var _ usecase.ChunklineRepository = (*batchChunklineRepo)(nil)

// CIP-14 §5: a duplicate Content-ID rejects the whole batch with a 400 before
// any part is executed.
func TestBatchHandlerRejectsDuplicateContentID(t *testing.T) {
	repo := &batchChunklineRepo{}
	handler := NewHandler(
		domain.Config{},
		nil,
		nil,
		usecase.NewChunklineUsecase(repo, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for range 2 {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": {"application/http"},
			"Content-ID":   {"dup"},
		})
		require.NoError(t, err)
		_, err = fmt.Fprintf(pw, "GET %s/chunkline/itr/100?uri=cckv://example.test/timeline/0 HTTP/1.1\r\nHost: example.test\r\n\r\n", apiPrefix)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	resp, err := server.Client().Post(server.URL+batchPath, "multipart/mixed; boundary="+mw.Boundary(), &buf)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, repo.calls, "no part should be dispatched")
}

// CIP-14 §8: authentication is evaluated once on the outer request —
// credential headers inside parts are stripped before dispatch, so a part
// cannot smuggle its own identity.
func TestBatchHandlerStripsPartCredentials(t *testing.T) {
	handler := NewHandler(
		domain.Config{},
		nil,
		nil,
		usecase.NewChunklineUsecase(&batchChunklineRepo{}, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))
	app.GET("/echo-auth", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Request().Header.Get("Authorization"))
	})

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	req0, err := http.NewRequest("GET", server.URL+"/echo-auth", nil)
	require.NoError(t, err)
	req0.Header.Set("Authorization", "Bearer sneaky-part-token")

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	responses, err := client.DoBatchRequestWithClient(context.Background(), server.Client(), server.URL+batchPath, map[string]*http.Request{"0": req0})
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Equal(t, http.StatusOK, responses["0"].StatusCode)

	body, err := io.ReadAll(responses["0"].Body)
	require.NoError(t, err)
	require.Empty(t, string(body), "part Authorization header must be stripped before dispatch")
}

// CIP-14 §5: an absolute-form part target is only accepted when its host is
// this server's own FQDN (converted to origin-form); other hosts fail with a
// per-part error — the batch endpoint is not a proxy.
func TestBatchHandlerAbsoluteFormTargets(t *testing.T) {
	repo := &batchChunklineRepo{}
	handler := NewHandler(
		domain.Config{FQDN: "example.test"},
		nil,
		nil,
		usecase.NewChunklineUsecase(repo, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	writePart := func(contentID, target string) {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": {"application/http"},
			"Content-ID":   {contentID},
		})
		require.NoError(t, err)
		_, err = fmt.Fprintf(pw, "GET %s HTTP/1.1\r\nHost: example.test\r\n\r\n", target)
		require.NoError(t, err)
	}
	itrPath := apiPrefix + "/chunkline/itr/100?uri=cckv://example.test/timeline/0"
	writePart("own", "http://example.test"+itrPath)
	writePart("foreign", "http://other.example.net"+itrPath)
	require.NoError(t, mw.Close())

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	resp, err := server.Client().Post(server.URL+batchPath, "multipart/mixed; boundary="+mw.Boundary(), &buf)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	require.NoError(t, err)
	mr := multipart.NewReader(resp.Body, params["boundary"])

	statuses := map[string]int{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		pr, err := http.ReadResponse(bufio.NewReader(part), nil)
		require.NoError(t, err)
		statuses[part.Header.Get("Content-ID")] = pr.StatusCode
		pr.Body.Close()
	}

	require.Equal(t, http.StatusOK, statuses["own"], "own-host absolute-form target must be served")
	require.Equal(t, http.StatusMisdirectedRequest, statuses["foreign"], "foreign-host absolute-form target must be rejected")
}

// A batch carrying more than maxBatchParts application/http parts is rejected
// with a 400 before any part is dispatched.
func TestBatchHandlerRejectsTooManyParts(t *testing.T) {
	repo := &batchChunklineRepo{}
	handler := NewHandler(
		domain.Config{},
		nil,
		nil,
		usecase.NewChunklineUsecase(repo, nil, nil),
		nil,
		nil,
		nil,
		nil,
	)

	app := echo.New()
	handler.RegisterRoutes(app, app.Group(""))

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for i := 0; i <= maxBatchParts; i++ {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": {"application/http"},
			"Content-ID":   {fmt.Sprintf("%d", i)},
		})
		require.NoError(t, err)
		_, err = fmt.Fprintf(pw, "GET %s/chunkline/itr/100?uri=cckv://example.test/timeline/0 HTTP/1.1\r\nHost: example.test\r\n\r\n", apiPrefix)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	batchPath, err := concrnt.RenderURITemplate(Endpoints["net.concrnt.core.batch"], map[string]string{})
	require.NoError(t, err)

	resp, err := server.Client().Post(server.URL+batchPath, "multipart/mixed; boundary="+mw.Boundary(), &buf)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, repo.calls, "no part should be dispatched")
}
