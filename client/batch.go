package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
)

func DoBatchRequest(endpoint string, requests map[string]*http.Request) map[string]*http.Response {
	responses, err := DoBatchRequestWithClient(context.Background(), http.DefaultClient, endpoint, requests)
	if err != nil {
		panic(err)
	}
	return responses
}

// DoBatchRequestWithClient sends application/http requests to a multipart batch endpoint.
func DoBatchRequestWithClient(ctx context.Context, client *http.Client, endpoint string, requests map[string]*http.Request) (map[string]*http.Response, error) {
	buffer := new(bytes.Buffer)
	mw := multipart.NewWriter(buffer)
	err := mw.SetBoundary("batch_boundary")
	if err != nil {
		return nil, err
	}

	for key, req := range requests {
		pw, err := mw.CreatePart(map[string][]string{
			"Content-Type": {"application/http"},
			"Content-ID":   {key},
		})
		if err != nil {
			return nil, err
		}

		req.Header.Set("User-Agent", "")
		if err := req.Write(pw); err != nil {
			return nil, err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}

	batchReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, buffer)
	if err != nil {
		return nil, err
	}
	batchReq.Header.Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())

	if client == nil {
		client = http.DefaultClient
	}
	batchResp, err := client.Do(batchReq)
	if err != nil {
		return nil, err
	}
	defer batchResp.Body.Close()

	if batchResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("batch request failed: status code %d", batchResp.StatusCode)
	}

	mediaType, params, err := mime.ParseMediaType(batchResp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	if mediaType != "multipart/mixed" {
		return nil, fmt.Errorf("expected multipart/mixed response, got %s", mediaType)
	}
	if params["boundary"] == "" {
		return nil, errors.New("missing multipart boundary")
	}

	responses := make(map[string]*http.Response)

	mr := multipart.NewReader(batchResp.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		contentID := part.Header.Get("Content-ID")

		req, ok := requests[contentID]
		if !ok {
			continue
		}

		reader := bufio.NewReader(part)
		resp, err := http.ReadResponse(reader, req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))

		responses[contentID] = resp
	}

	return responses, nil
}
