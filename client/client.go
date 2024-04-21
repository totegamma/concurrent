//go:generate go run go.uber.org/mock/mockgen -source=client.go -destination=mock/client.go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	defaultTimeout = 10 * time.Second
)

var tracer = otel.Tracer("client")

type Client interface {
	Commit(ctx context.Context, domain, body string) (*http.Response, error)
	GetEntity(ctx context.Context, domain, address string) (core.Entity, error)
	GetTimeline(ctx context.Context, domain, id string) (core.Timeline, error)
	GetChunks(ctx context.Context, domain string, timelines []string, queryTime time.Time) (map[string]core.Chunk, error)
	GetKey(ctx context.Context, domain, id string) ([]core.Key, error)
	GetDomain(ctx context.Context, domain string) (core.Domain, error)
}

type client struct{}

func NewClient() Client {
	return &client{}
}

func (c *client) Commit(ctx context.Context, domain, body string) (*http.Response, error) {
	ctx, span := tracer.Start(ctx, "Client.Commit")
	defer span.End()

	req, err := http.NewRequest("POST", "https://"+domain+"/api/v1/commit", bytes.NewBuffer([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if err != nil {
		span.RecordError(err)
		return &http.Response{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = defaultTimeout
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return &http.Response{}, err
	}

	return resp, nil
}

func (c *client) GetEntity(ctx context.Context, domain, address string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Client.GetEntity")
	defer span.End()

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/entity/"+address, nil)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteEntity core.ResponseBase[core.Entity]
	err = json.Unmarshal(body, &remoteEntity)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	if remoteEntity.Status != "ok" {
		return core.Entity{}, fmt.Errorf("Remote entity is not found")
	}

	return remoteEntity.Content, nil
}

func (c *client) GetTimeline(ctx context.Context, domain, id string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Client.GetTimeline")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/timeline/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	var timelineResp core.ResponseBase[core.Timeline]
	err = json.Unmarshal(body, &timelineResp)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	if timelineResp.Status != "ok" {
		return core.Timeline{}, fmt.Errorf("Remote timeline is not found")
	}

	return timelineResp.Content, nil
}

func (c *client) GetChunks(ctx context.Context, domain string, timelines []string, queryTime time.Time) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunks")
	defer span.End()

	timelinesStr := strings.Join(timelines, ",")
	timeStr := fmt.Sprintf("%d", queryTime.Unix())
	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/timelines/chunks?timelines="+timelinesStr+"&time="+timeStr, nil)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var chunkResp core.ResponseBase[map[string]core.Chunk]
	err = json.Unmarshal(body, &chunkResp)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if chunkResp.Status != "ok" {
		return nil, fmt.Errorf("Remote chunks are not found")
	}

	return chunkResp.Content, nil
}

func (c *client) GetKey(ctx context.Context, domain, id string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Client.GetKey")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/key/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteKey core.ResponseBase[[]core.Key]
	err = json.Unmarshal(body, &remoteKey)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return remoteKey.Content, nil
}

func (c *client) GetDomain(ctx context.Context, domain string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Client.GetDomain")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/domain", nil)
	if err != nil {
		span.RecordError(err)
		return core.Domain{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Domain{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteDomain core.ResponseBase[core.Domain]
	err = json.Unmarshal(body, &remoteDomain)
	if err != nil {
		span.RecordError(err)
		return core.Domain{}, err
	}

	return remoteDomain.Content, nil
}
