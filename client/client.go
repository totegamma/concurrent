//go:generate go run go.uber.org/mock/mockgen -source=client.go -destination=mock/client.go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

const (
	defaultTimeout = 10 * time.Second
)

var tracer = otel.Tracer("client")

type Client interface {
	Commit(ctx context.Context, domain, body string, response any) (*http.Response, error)
	GetEntity(ctx context.Context, domain, address string) (core.Entity, error)
	GetMessage(ctx context.Context, domain, id string) (core.Message, error)
	GetAssociation(ctx context.Context, domain, id string) (core.Association, error)
	GetProfile(ctx context.Context, domain, address string) (core.Profile, error)
	GetTimeline(ctx context.Context, domain, id string) (core.Timeline, error)
	GetChunks(ctx context.Context, domain string, timelines []string, queryTime time.Time) (map[string]core.Chunk, error)
	GetKey(ctx context.Context, domain, id string) ([]core.Key, error)
	GetDomain(ctx context.Context, domain string) (core.Domain, error)
}

type client struct{}

func NewClient() Client {
	return &client{}
}

func (c *client) Commit(ctx context.Context, domain, body string, response any) (*http.Response, error) {
	ctx, span := tracer.Start(ctx, "Client.Commit")
	defer span.End()

	req, err := http.NewRequest("POST", "https://"+domain+"/api/v1/commit", bytes.NewBuffer([]byte(body)))
	if err != nil {
		span.RecordError(err)
		return &http.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	passport, ok := ctx.Value(core.RequesterPassportKey).(string)
	if ok {
		req.Header.Set(core.RequesterPassportHeader, passport)
	}
	span.SetAttributes(attribute.String("passport", passport))

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

	if response != nil && !reflect.ValueOf(response).IsNil() {
		respbody, _ := io.ReadAll(resp.Body)
		err = json.Unmarshal(respbody, &response)
		if err != nil {
			span.RecordError(err)
			return &http.Response{}, err
		}
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
		log.Printf("error: %v", string(body))
		return core.Entity{}, fmt.Errorf("Remote entity is not found")
	}

	return remoteEntity.Content, nil
}

func (c *client) GetMessage(ctx context.Context, domain, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Client.GetMessage")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/message/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteMessage core.ResponseBase[core.Message]
	err = json.Unmarshal(body, &remoteMessage)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if remoteMessage.Status != "ok" {
		log.Printf("error: %v", string(body))
		return core.Message{}, fmt.Errorf("Remote message is not found")
	}

	return remoteMessage.Content, nil
}

func (c *client) GetAssociation(ctx context.Context, domain, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Client.GetAssociation")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/association/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteAssociation core.ResponseBase[core.Association]
	err = json.Unmarshal(body, &remoteAssociation)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if remoteAssociation.Status != "ok" {
		log.Printf("error: %v", string(body))
		return core.Association{}, fmt.Errorf("Remote association is not found")
	}

	return remoteAssociation.Content, nil
}

func (c *client) GetProfile(ctx context.Context, domain, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Client.GetProfile")
	defer span.End()

	req, err := http.NewRequest("GET", "https://"+domain+"/api/v1/profile/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteProfile core.ResponseBase[core.Profile]
	err = json.Unmarshal(body, &remoteProfile)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if remoteProfile.Status != "ok" {
		log.Printf("error: %v", string(body))
		return core.Profile{}, fmt.Errorf("Remote profile is not found")
	}

	return remoteProfile.Content, nil
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
		log.Printf("error: %v", string(body))
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
		log.Printf("error: %v", string(body))
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
