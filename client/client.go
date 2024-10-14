//go:generate go run go.uber.org/mock/mockgen -source=client.go -destination=mock/client.go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
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
	defaultTimeout = 3 * time.Second
)

var tracer = otel.Tracer("client")

type Client interface {
	Commit(ctx context.Context, domain, body string, response any, opts *Options) (*http.Response, error)
	GetEntity(ctx context.Context, domain, address string, opts *Options) (core.Entity, error)
	GetMessage(ctx context.Context, domain, id string, opts *Options) (core.Message, error)
	GetAssociation(ctx context.Context, domain, id string, opts *Options) (core.Association, error)
	GetProfile(ctx context.Context, domain, address string, opts *Options) (core.Profile, error)
	GetTimeline(ctx context.Context, domain, id string, opts *Options) (core.Timeline, error)
	GetChunks(ctx context.Context, domain string, timelines []string, queryTime time.Time, opts *Options) (map[string]core.Chunk, error)
	GetKey(ctx context.Context, domain, id string, opts *Options) ([]core.Key, error)
	GetDomain(ctx context.Context, domain string, opts *Options) (core.Domain, error)
	GetChunkItrs(ctx context.Context, domain string, timelines []string, epoch string, opts *Options) (map[string]string, error)
	GetChunkBodies(ctx context.Context, domain string, query map[string]string, opts *Options) (map[string]core.Chunk, error)
	GetRetracted(ctx context.Context, domain string, timelines []string, opts *Options) (map[string][]string, error)
}

type client struct {
	client     http.Client
	lastFailed map[string]time.Time
	failCount  map[string]int
}

func NewClient() Client {
	httpClient := new(http.Client)
	httpClient.Timeout = defaultTimeout
	client := &client{
		client:     *httpClient,
		lastFailed: make(map[string]time.Time),
		failCount:  make(map[string]int),
	}
	go client.UpKeeper()
	return client
}

type Options struct {
	AuthToken string
}

func (c *client) IsOnline(domain string) bool {
	lastfailed, ok := c.lastFailed[domain]
	if !ok {
		return true
	}
	if lastfailed.IsZero() {
		return true
	}
	return false
}

func (c *client) UpKeeper() {
	ctx := context.Background()
	for {
		time.Sleep(1 * time.Second)
		for domain, lastFailed := range c.lastFailed {
			// exponential backoff (max 10 minutes)
			if _, ok := c.failCount[domain]; !ok {
				c.failCount[domain] = 0
			}
			var span int = 600
			if c.failCount[domain] < 10 {
				span = 1 << c.failCount[domain]
			}
			if time.Since(lastFailed) > time.Duration(span)*time.Second {
				log.Printf("Domain %s is offline. Fail count: %d", domain, c.failCount[domain])
				// health check
				_, err := httpRequest[core.Domain](ctx, &c.client, "GET", "https://"+domain+"/api/v1/domain", "", &Options{})
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						c.lastFailed[domain] = time.Now()
					}
					c.failCount[domain]++
					if c.failCount[domain] > 20 {
						log.Printf("Domain %s is still offline after 20 retries. Bye bye :(", domain)
						delete(c.lastFailed, domain)
						delete(c.failCount, domain)
					}
				} else {
					log.Printf("Domain %s is back online :3", domain)
					delete(c.lastFailed, domain)
					delete(c.failCount, domain)
				}
			}
		}
	}
}

func (c *client) Commit(ctx context.Context, domain, body string, response any, opts *Options) (*http.Response, error) {
	ctx, span := tracer.Start(ctx, "Client.Commit")
	defer span.End()

	if !c.IsOnline(domain) {
		return &http.Response{}, fmt.Errorf("Domain is offline")
	}

	req, err := http.NewRequest("POST", "https://"+domain+"/api/v1/commit", bytes.NewBuffer([]byte(body)))
	if err != nil {
		span.RecordError(err)
		return &http.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	if opts != nil {
		if opts.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
		}
	}

	passport, ok := ctx.Value(core.RequesterPassportKey).(string)
	if ok {
		req.Header.Set(core.RequesterPassportHeader, passport)
	}
	span.SetAttributes(attribute.String("passport", passport))

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = defaultTimeout
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

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

func httpRequest[T any](ctx context.Context, client *http.Client, method, url, body string, opts *Options) (*T, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if opts != nil {
		if opts.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
		}
	}

	passport, ok := ctx.Value(core.RequesterPassportKey).(string)
	if ok {
		req.Header.Set(core.RequesterPassportHeader, passport)
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	respbody, _ := io.ReadAll(resp.Body)
	var response core.ResponseBase[T]
	err = json.Unmarshal(respbody, &response)
	if err != nil {
		return nil, err
	}

	if response.Status != "ok" {
		log.Printf("error: %v", string(body))
		return nil, fmt.Errorf("Request failed(%s): %v", resp.Status, string(body))
	}

	return &response.Content, nil
}

func (c *client) GetEntity(ctx context.Context, domain, address string, opts *Options) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Client.GetEntity")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Entity{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/entity/" + address
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Entity](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return core.Entity{}, err
	}

	return *response, nil
}

func (c *client) GetMessage(ctx context.Context, domain, id string, opts *Options) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Client.GetMessage")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Message{}, fmt.Errorf("Domain is offline")

	}

	url := "https://" + domain + "/api/v1/message/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Message](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return core.Message{}, err
	}

	return *response, nil
}

func (c *client) GetAssociation(ctx context.Context, domain, id string, opts *Options) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Client.GetAssociation")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Association{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/association/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Association](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return core.Association{}, err
	}

	return *response, nil
}

func (c *client) GetProfile(ctx context.Context, domain, id string, opts *Options) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Client.GetProfile")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Profile{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/profile/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Profile](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return core.Profile{}, err
	}

	return *response, nil
}

func (c *client) GetTimeline(ctx context.Context, domain, id string, opts *Options) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Client.GetTimeline")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Timeline{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/timeline/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Timeline](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return core.Timeline{}, err
	}

	return *response, nil
}

func (c *client) GetChunks(ctx context.Context, domain string, timelines []string, queryTime time.Time, opts *Options) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunks")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")
	timeStr := fmt.Sprintf("%d", queryTime.Unix())

	url := "https://" + domain + "/api/v1/timelines/chunks?timelines=" + timelinesStr + "&time=" + timeStr
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]core.Chunk](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetChunkItrs(ctx context.Context, domain string, timelines []string, epoch string, opts *Options) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunkItrs")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")

	url := "https://" + domain + "/api/v1/chunks/itr?timelines=" + timelinesStr + "&epoch=" + epoch
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]string](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetChunkBodies(ctx context.Context, domain string, query map[string]string, opts *Options) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunkBodies")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	queries := []string{}
	for key, value := range query {
		queries = append(queries, key+":"+value)
	}

	url := "https://" + domain + "/api/v1/chunks/body?query=" + strings.Join(queries, ",")
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]core.Chunk](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetKey(ctx context.Context, domain, id string, opts *Options) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Client.GetKey")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/key/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[[]core.Key](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetDomain(ctx context.Context, domain string, opts *Options) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Client.GetDomain")
	defer span.End()

	if !c.IsOnline(domain) {
		return core.Domain{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/domain"
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Domain](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		} else if _, ok := err.(*json.SyntaxError); ok {
			c.lastFailed[domain] = time.Now()
		}

		return core.Domain{}, err
	}

	return *response, nil
}

func (c *client) GetRetracted(ctx context.Context, domain string, timelines []string, opts *Options) (map[string][]string, error) {
	ctx, span := tracer.Start(ctx, "Client.GetRetracted")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")
	url := "https://" + domain + "/api/v1/timelines/retracted?timelines=" + timelinesStr
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string][]string](ctx, &c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}
