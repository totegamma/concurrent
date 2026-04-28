package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/concrnt/concrnt/legacy/core"
	"github.com/gorilla/websocket"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

const (
	defaultTimeout = 3 * time.Second
	maxFailCount   = 23 // max 10 minutes
)

var tracer = otel.Tracer("client")

type Client interface {
	SetUserAgent(software, version string)
	RegisterHostRemap(host string, remap string, useHttps bool)

	Commit(ctx context.Context, domain, body string, response any, opts *Options) (*http.Response, error)
	ConnectWebsocket(ctx context.Context, domain string, path string) (*websocket.Conn, error)

	GetEntity(ctx context.Context, address string, opts *Options) (core.Entity, error)
	GetMessage(ctx context.Context, id string, opts *Options) (core.Message, error)
	GetAssociation(ctx context.Context, id string, opts *Options) (core.Association, error)
	GetProfile(ctx context.Context, address string, opts *Options) (core.Profile, error)
	GetTimeline(ctx context.Context, id string, opts *Options) (core.Timeline, error)
	GetChunks(ctx context.Context, timelines []string, queryTime time.Time, opts *Options) (map[string]core.Chunk, error)
	GetKey(ctx context.Context, id string, opts *Options) ([]core.Key, error)
	GetDomain(ctx context.Context, domain string, opts *Options) (core.Domain, error)
	GetChunkItrs(ctx context.Context, timelines []string, epoch string, opts *Options) (map[string]string, error)
	GetChunkBodies(ctx context.Context, query map[string]string, opts *Options) (map[string]core.Chunk, error)
	GetRetracted(ctx context.Context, timelines []string, opts *Options) (map[string][]string, error)
	GetAck(ctx context.Context, from, to string, opts *Options) (core.Ack, error)
}

type remapRecord struct {
	Remap    string
	UseHttps bool
}

type client struct {
	client          *http.Client
	cache           *cache.Cache
	lastFailed      map[string]time.Time
	failCount       map[string]int
	userAgent       string
	hostRemap       map[string]remapRecord
	defaultResolver string
}

func NewClient(defaultResolver string) Client {
	httpClient := http.Client{
		Timeout: defaultTimeout,
	}
	client := &client{
		client:          &httpClient,
		cache:           cache.New(1*time.Hour, 3*time.Hour),
		lastFailed:      make(map[string]time.Time),
		failCount:       make(map[string]int),
		defaultResolver: defaultResolver,
	}
	httpClient.Transport = client
	client.hostRemap = make(map[string]remapRecord)
	go client.UpKeeper()
	return client
}

type Options struct {
	Resolver  string
	AuthToken string
	Passport  string
	Cache     string
}

func (c *client) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)

	// remap host
	if remap, ok := c.hostRemap[req.Host]; ok {
		req.Host = remap.Remap
		req.URL.Host = remap.Remap
		if remap.UseHttps {
			req.URL.Scheme = "https"
		} else {
			req.URL.Scheme = "http"
		}
	}

	return http.DefaultTransport.RoundTrip(req)
}

func (c *client) SetUserAgent(software, version string) {
	c.userAgent = fmt.Sprintf("%s/%s (Concrnt)", software, version)
}

func (c *client) RegisterHostRemap(host string, remap string, useHttps bool) {
	c.hostRemap[host] = remapRecord{
		Remap:    remap,
		UseHttps: useHttps,
	}
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
	for range time.Tick(100 * time.Millisecond) {
		for domain, lastFailed := range c.lastFailed {
			// exponential backoff (max 10 minutes)
			if _, ok := c.failCount[domain]; !ok {
				c.failCount[domain] = 0
			}

			var span = 0.5 * math.Pow(1.5, float64(min(c.failCount[domain], maxFailCount)))
			if time.Since(lastFailed) > time.Duration(span)*time.Second {
				// health check
				_, err := httpRequest[core.Domain](ctx, c.client, "GET", "https://"+domain+"/api/v1/domain", "", &Options{})
				if err != nil {
					slog.Info(fmt.Sprintf("Domain %s is offline. Fail count: %d", domain, c.failCount[domain]))
					c.lastFailed[domain] = time.Now()
					c.failCount[domain]++
				} else {
					slog.Info(fmt.Sprintf("Domain %s is back online :3", domain))
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
		if opts.Passport != "" {
			req.Header.Set(core.RequesterPassportHeader, opts.Passport)
		}
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.client.Do(req)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while committing", "error", err)
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
	ctx, span := tracer.Start(ctx, "Client.httpRequest")
	defer span.End()

	span.SetAttributes(attribute.String("url", url))

	req, err := http.NewRequest(method, url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if opts != nil {
		if opts.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
		}
		if opts.Passport != "" {
			req.Header.Set(core.RequesterPassportHeader, opts.Passport)
		}
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
		if resp.StatusCode == http.StatusNotFound {
			err = core.NewErrorNotFound()
		} else if resp.StatusCode == http.StatusForbidden {
			err = core.NewErrorPermissionDenied()
		} else {
			err = fmt.Errorf("Request failed(%s): %v", resp.Status, string(body))
			slog.InfoContext(ctx, err.Error())
		}
		return nil, err
	}

	return &response.Content, nil
}

func (c *client) resolveResolver(ctx context.Context, resolver string) (string, error) {
	ctx, span := tracer.Start(ctx, "Client.resolveResolver")
	defer span.End()

	if resolver == "" {
		return c.defaultResolver, nil
	}

	if core.IsCCID(resolver) {
		entity, err := c.GetEntity(ctx, resolver, &Options{Resolver: c.defaultResolver, Cache: "try-cache"})
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		return entity.Domain, nil
	}

	if core.IsCSID(resolver) {
		domain, err := c.LookupCSID(ctx, resolver, &Options{Resolver: c.defaultResolver, Cache: "try-cache"})
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		return domain.ID, nil
	}

	return resolver, nil
}

func (c *client) GetEntity(ctx context.Context, address string, opts *Options) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "Client.GetEntity")
	defer span.End()

	resolver := address
	if opts != nil {
		if opts.Resolver != "" {
			resolver = opts.Resolver
		}
		if opts.Cache == "try-cache" {
			if val, found := c.cache.Get(address); found {
				return val.(core.Entity), nil
			}
		}
	}

	domain, err := c.resolveResolver(ctx, resolver)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	if !c.IsOnline(domain) {
		return core.Entity{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/entity/" + address
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Entity](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting entity", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Entity{}, err
	}

	go func() {
		c.cache.Set(address, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetMessage(ctx context.Context, id string, opts *Options) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Client.GetMessage")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(id); found {
			return val.(core.Message), nil
		}
	}

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return core.Message{}, err
	}

	if !c.IsOnline(domain) {
		return core.Message{}, fmt.Errorf("Domain is offline")

	}

	url := "https://" + domain + "/api/v1/message/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Message](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting message", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Message{}, err
	}

	go func() {
		c.cache.Set(id, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetAssociation(ctx context.Context, id string, opts *Options) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Client.GetAssociation")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(id); found {
			return val.(core.Association), nil
		}
	}

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return core.Association{}, err
	}

	if !c.IsOnline(domain) {
		return core.Association{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/association/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Association](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting association", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Association{}, err
	}

	go func() {
		c.cache.Set(id, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetProfile(ctx context.Context, id string, opts *Options) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Client.GetProfile")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(id); found {
			return val.(core.Profile), nil
		}
	}

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return core.Profile{}, err
	}

	if !c.IsOnline(domain) {
		return core.Profile{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/profile/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Profile](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting profile", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Profile{}, err
	}

	go func() {
		c.cache.Set(id, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetTimeline(ctx context.Context, id string, opts *Options) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Client.GetTimeline")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(id); found {
			return val.(core.Timeline), nil
		}
	}

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	if !c.IsOnline(domain) {
		return core.Timeline{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/timeline/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Timeline](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting timeline", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Timeline{}, err
	}

	go func() {
		c.cache.Set(id, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetChunks(ctx context.Context, timelines []string, queryTime time.Time, opts *Options) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunks")
	defer span.End()

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")
	timeStr := fmt.Sprintf("%d", queryTime.Unix())

	url := "https://" + domain + "/api/v1/timelines/chunks?timelines=" + timelinesStr + "&time=" + timeStr
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]core.Chunk](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting chunks", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetChunkItrs(ctx context.Context, timelines []string, epoch string, opts *Options) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunkItrs")
	defer span.End()

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")

	url := "https://" + domain + "/api/v1/chunks/itr?timelines=" + timelinesStr + "&epoch=" + epoch
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]string](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting chunk itrs", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetChunkBodies(ctx context.Context, query map[string]string, opts *Options) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Client.GetChunkBodies")
	defer span.End()

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	queries := []string{}
	for key, value := range query {
		queries = append(queries, key+":"+value)
	}

	url := "https://" + domain + "/api/v1/chunks/body?query=" + strings.Join(queries, ",")
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string]core.Chunk](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting chunk bodies", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetKey(ctx context.Context, id string, opts *Options) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Client.GetKey")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(id); found {
			return val.([]core.Key), nil
		}
	}

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/key/" + id
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[[]core.Key](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting key", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	go func() {
		c.cache.Set(id, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) LookupCSID(ctx context.Context, csid string, opts *Options) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Client.LookupCSID")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(csid); found {
			return val.(core.Domain), nil
		}
	}

	url := "https://" + c.defaultResolver + "/api/v1/domain/" + csid
	span.SetAttributes(attribute.String("url", url))
	response, err := httpRequest[core.Domain](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)
		return core.Domain{}, err
	}

	go func() {
		c.cache.Set(csid, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetDomain(ctx context.Context, domain string, opts *Options) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "Client.GetDomain")
	defer span.End()

	if opts != nil && opts.Cache == "try-cache" {
		if val, found := c.cache.Get(domain); found {
			return val.(core.Domain), nil
		}
	}

	if core.IsCSID(domain) {
		d, err := c.LookupCSID(ctx, domain, &Options{Cache: "try-cache"})
		if err != nil {
			span.RecordError(err)
			return core.Domain{}, err
		}
		domain = d.ID
	}

	if !c.IsOnline(domain) {
		return core.Domain{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/domain"
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Domain](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting domain", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return core.Domain{}, err
	}

	go func() {
		c.cache.Set(domain, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) GetRetracted(ctx context.Context, timelines []string, opts *Options) (map[string][]string, error) {
	ctx, span := tracer.Start(ctx, "Client.GetRetracted")
	defer span.End()

	domain, err := c.resolveResolver(ctx, opts.Resolver)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	timelinesStr := strings.Join(timelines, ",")
	url := "https://" + domain + "/api/v1/timelines/retracted?timelines=" + timelinesStr
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[map[string][]string](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting retracted", "error", err)
			c.lastFailed[domain] = time.Now()
		}

		return nil, err
	}

	return *response, nil
}

func (c *client) GetAck(ctx context.Context, from, to string, opts *Options) (core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Client.GetAck")
	defer span.End()

	cacheKey := "ack:" + from + ":" + to
	resolver := from
	if opts != nil {
		if opts.Resolver != "" {
			resolver = opts.Resolver
		}
		if opts.Cache == "try-cache" {
			if val, found := c.cache.Get(cacheKey); found {
				return val.(core.Ack), nil
			}
		}
	}

	domain, err := c.resolveResolver(ctx, resolver)
	if err != nil {
		span.RecordError(err)
		return core.Ack{}, err
	}

	if !c.IsOnline(from) {
		return core.Ack{}, fmt.Errorf("Domain is offline")
	}

	url := "https://" + domain + "/api/v1/ack/" + from + "/" + to
	span.SetAttributes(attribute.String("url", url))

	response, err := httpRequest[core.Ack](ctx, c.client, "GET", url, "", opts)
	if err != nil {
		span.RecordError(err)

		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			slog.Warn("Mark domain "+domain+" as offline while getting ack", "error", err)
			c.lastFailed[from] = time.Now()
		}

		return core.Ack{}, err
	}

	go func() {
		c.cache.Set(cacheKey, *response, cache.DefaultExpiration)
	}()

	return *response, nil
}

func (c *client) ConnectWebsocket(ctx context.Context, domain string, path string) (*websocket.Conn, error) {
	_, span := tracer.Start(ctx, "Client.ConnectWebsocket")
	defer span.End()

	if !c.IsOnline(domain) {
		return nil, fmt.Errorf("Domain is offline")
	}

	u := url.URL{Scheme: "wss", Host: domain, Path: path}
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	header := http.Header{}
	header.Set("User-Agent", c.userAgent)

	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		slog.Warn("Failed to connect to websocket. Mark domain "+domain+" as offline", "error", err)
		c.lastFailed[domain] = time.Now()
		span.RecordError(err)
		return nil, err
	}

	return conn, nil
}
