package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"

	"github.com/gorilla/websocket"
	"github.com/patrickmn/go-cache"
	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/schemas"
)

var tracer = otel.Tracer("client")

const (
	defaultTimeout = 3 * time.Second
	maxFailCount   = 23 // max 10 minutes
)

type Client struct {
	client          *http.Client
	cache           *cache.Cache
	userAgent       string
	defaultResolver string
	remappings      map[string]*url.URL
}

func New(defaultResolver string) *Client {
	httpClient := http.Client{
		Timeout: defaultTimeout,
	}

	c := &Client{
		client:          &httpClient,
		cache:           cache.New(10*time.Minute, 15*time.Minute),
		defaultResolver: defaultResolver,
		remappings:      make(map[string]*url.URL),
	}
	httpClient.Transport = c
	return c
}

func (c *Client) SetUserAgent(software, version string) {
	c.userAgent = fmt.Sprintf("%s/%s (Concrnt)", software, version)
}

func (c *Client) AddHostRemapping(host string, target string) {
	parsed, err := url.Parse(target)
	if err != nil {
		slog.Warn("Failed to parse remapping target "+target, "error", err)
		return
	}
	c.remappings[host] = parsed
}

type Options struct {
	Resolver string
	NoCache  bool
}

func (c *Client) GetClient() *http.Client {
	return c.client
}

func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := tracer.Start(req.Context(), "HTTP "+req.Method)
	defer span.End()

	if remap, ok := c.remappings[req.Host]; ok {
		req.Host = remap.Host
		req.URL.Host = remap.Host
		req.URL.Scheme = remap.Scheme
	}

	req.Header.Set("User-Agent", c.userAgent)

	span.SetAttributes(attribute.String("http.method", req.Method))
	span.SetAttributes(attribute.String("http.url", req.URL.String()))

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	return http.DefaultTransport.RoundTrip(req)
}

func (c *Client) resolveResolver(ctx context.Context, resolver string) (string, error) {
	ctx, span := tracer.Start(ctx, "Client.resolveResolver")
	defer span.End()

	span.SetAttributes(attribute.String("resolver", resolver))

	if resolver == "" {
		return c.defaultResolver, nil
	}

	if concrnt.IsCCID(resolver) {
		var entity concrnt.Document[schemas.Entity]
		err := c.GetRecord(
			ctx,
			concrnt.ComposeCCURI("cckv", resolver, ""),
			&Options{Resolver: c.defaultResolver},
			&entity,
		)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to get entity record for ccid %s", resolver), err)
			span.RecordError(err)
			return "", err
		}
		span.SetAttributes(attribute.String("entity_domain", entity.Value.Domain))
		return entity.Value.Domain, nil
	}

	if concrnt.IsCSID(resolver) {
		wkc, err := c.GetServer(ctx, resolver, nil)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to get server for csid %s", resolver), err)
			span.RecordError(err)
			return "", err
		}
		span.SetAttributes(attribute.String("server_domain", wkc.Domain))
		return wkc.Domain, nil
	}

	return resolver, nil
}

func (c *Client) ResolveResourceHost(ctx context.Context, uri string) (string, error) {
	ctx, span := tracer.Start(ctx, "Client.ResolveResourceHost")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		err := errors.Join(fmt.Errorf("invalid cc uri %s", uri), err)
		span.RecordError(err)
		return "", err
	}

	return c.resolveResolver(ctx, parsed.Owner)
}

func (c *Client) GetServer(ctx context.Context, domainOrCSID string, hint *string) (concrnt.WellKnownConcrnt, error) {
	ctx, span := tracer.Start(ctx, "Client.GetServer")
	defer span.End()

	cacheKey := "server:" + domainOrCSID

	x, found := c.cache.Get(cacheKey)
	if found {
		return x.(concrnt.WellKnownConcrnt), nil
	}

	if concrnt.IsCSID(domainOrCSID) {
		var wkc concrnt.WellKnownConcrnt
		resolver := c.defaultResolver
		if hint != nil {
			resolver = *hint
		}
		err := c.GetResource(
			ctx,
			"cckv://"+domainOrCSID,
			"application/json",
			&Options{Resolver: resolver},
			&wkc,
		)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to get well-known concrnt for csid %s. resolver: %s", domainOrCSID, resolver), err)
			span.RecordError(err)
			return concrnt.WellKnownConcrnt{}, err
		}
		c.cache.Set(cacheKey, wkc, cache.DefaultExpiration)
		return wkc, nil
	} else {

		domain := domainOrCSID

		url := "https://" + domain + "/.well-known/concrnt"
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to create request for well-known concrnt at %s", url), err)
			span.RecordError(err)
			return concrnt.WellKnownConcrnt{}, err
		}
		resp, err := c.client.Do(req)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to perform request for well-known concrnt at %s", url), err)
			span.RecordError(err)
			return concrnt.WellKnownConcrnt{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			err := errors.Join(fmt.Errorf("failed to get well-known concrnt from %s", url), err)
			span.RecordError(err)
			return concrnt.WellKnownConcrnt{}, err
		}
		var wkc concrnt.WellKnownConcrnt
		err = json.NewDecoder(resp.Body).Decode(&wkc)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to decode well-known concrnt from %s", url), err)
			span.RecordError(err)
			return concrnt.WellKnownConcrnt{}, err
		}
		c.cache.Set(cacheKey, wkc, cache.DefaultExpiration)
		return wkc, nil
	}
}

func (c *Client) GetResource(ctx context.Context, uri string, accept string, opts *Options, result any) error {
	ctx, span := tracer.Start(ctx, "Client.GetResource")
	defer span.End()

	if opts == nil {
		opts = &Options{}
	}

	// ==== cache check =============
	cacheKey := "resource:" + uri
	if !opts.NoCache {
		x, found := c.cache.Get(cacheKey)
		if found {
			resultBytes := x.([]byte)
			err := json.Unmarshal(resultBytes, &result)
			if err != nil {
				err := errors.Join(fmt.Errorf("failed to unmarshal cached resource for uri %s", uri), err)
				span.RecordError(err)
				return err
			}
			return nil
		}
	}
	// ==============================

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		err := errors.Join(fmt.Errorf("invalid cc uri %s", uri), err)
		span.RecordError(err)
		return err
	}

	endpoint := uri

	if parsed.Scheme != "http" {
		var info concrnt.WellKnownConcrnt
		if opts.Resolver != "" {
			info, err = c.GetServer(ctx, opts.Resolver, nil)
			if err != nil {
				err := errors.Join(fmt.Errorf("failed to get server for resolver %s", opts.Resolver), err)
				span.RecordError(err)
				return err
			}
		} else {
			domain, err := c.resolveResolver(ctx, parsed.Owner)
			if err != nil {
				err := errors.Join(fmt.Errorf("failed to resolve default resolver for owner %s", parsed.Owner), err)
				span.RecordError(err)
				return err
			}
			info, err = c.GetServer(ctx, domain, nil)
			if err != nil {
				err := errors.Join(fmt.Errorf("failed to get server for default resolver %s", domain), err)
				span.RecordError(err)
				return err
			}
		}

		desc, ok := info.Endpoints["net.concrnt.core.resolve"]
		if !ok {
			err := fmt.Errorf("resource endpoint not found in server %s", info.Domain)
			span.RecordError(err)
			return err
		}

		path, err := concrnt.RenderURITemplate(desc, map[string]string{
			"owner": parsed.Owner,
			"key":   parsed.Key,
			"uri":   url.QueryEscape(uri),
		})
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to render resource endpoint template for server %s", info.Domain), err)
			span.RecordError(err)
			return err
		}

		endpoint = "https://" + info.Domain + path
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to create request for resource %s", uri), err)
		span.RecordError(err)
		return err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to perform request for resource %s", uri), err)
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("failed to get resource %s: status code %d", uri, resp.StatusCode)
		span.RecordError(err)
		return err
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to decode resource %s", uri), err)
		span.RecordError(err)
		return err
	}

	if !opts.NoCache {
		bytes, err := json.Marshal(result)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to marshal resource for caching for uri %s", uri), err)
			span.RecordError(err)
			return err
		}
		c.cache.Set(cacheKey, bytes, cache.DefaultExpiration)
	}

	return nil
}

func (c *Client) GetRecord(ctx context.Context, uri string, opts *Options, result any) error {
	ctx, span := tracer.Start(ctx, "Client.GetRecord")
	defer span.End()

	var sd concrnt.SignedDocument
	err := c.GetResource(ctx, uri, "application/json", opts, &sd)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to get signed document for resource %s", uri), err)
		span.RecordError(err)
		return err
	}

	err = json.Unmarshal([]byte(sd.Document), &result)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to decode document in signed document for resource %s", uri), err)
		span.RecordError(err)
		return err
	}

	return nil
}

func (c *Client) Commit(ctx context.Context, resolver string, sd concrnt.SignedDocument) error {
	ctx, span := tracer.Start(ctx, "Client.Commit")
	defer span.End()

	if resolver == "" || resolver == c.defaultResolver {
		resolver = c.defaultResolver
	} else {
		domain, err := c.resolveResolver(ctx, resolver)
		if err != nil {
			err := errors.Join(fmt.Errorf("failed to resolve resolver %s", resolver), err)
			span.RecordError(err)
			return err
		}
		resolver = domain
	}

	if resolver == "" {
		err := fmt.Errorf("resolver cannot be empty")
		span.RecordError(err)
		return err
	}

	server, err := c.GetServer(ctx, resolver, nil)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to get server for resolver %s", resolver), err)
		span.RecordError(err)
		return err
	}

	desc, ok := server.Endpoints["net.concrnt.core.commit"]
	if !ok {
		err := fmt.Errorf("commit endpoint not found in server %s", server.Domain)
		span.RecordError(err)
		return err
	}

	path, err := concrnt.RenderURITemplate(desc, map[string]string{})
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to render commit endpoint template for server %s", server.Domain), err)
		span.RecordError(err)
		return err
	}
	url := "https://" + server.Domain + path

	body, err := json.Marshal(sd)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to marshal signed document for commit to %s", url), err)
		span.RecordError(err)
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to create request for commit to %s", url), err)
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to perform request for commit to %s", url), err)
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("failed to commit to %s: status code %d", url, resp.StatusCode)
		span.RecordError(err)
		return err
	}

	return nil
}

func (c *Client) Realtime(ctx context.Context, fqdn string) (*websocket.Conn, error) {
	_, span := tracer.Start(ctx, "Client.Realtime")
	defer span.End()

	server, err := c.GetServer(ctx, fqdn, nil)
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to get server for realtime connection to %s", fqdn), err)
		span.RecordError(err)
		return nil, err
	}

	desc, ok := server.Endpoints["net.concrnt.core.realtime"]
	if !ok {
		err := fmt.Errorf("realtime endpoint not found in server %s", server.Domain)
		span.RecordError(err)
		return nil, err
	}

	path, err := concrnt.RenderURITemplate(desc, map[string]string{})
	if err != nil {
		err := errors.Join(fmt.Errorf("failed to render realtime endpoint template for server %s", server.Domain), err)
		span.RecordError(err)
		return nil, err
	}
	domain := server.Domain

	/*
		if !c.IsOnline(domain) {
			return nil, fmt.Errorf("Domain is offline")
		}
	*/

	u := url.URL{Scheme: "wss", Host: domain, Path: path}
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	header := http.Header{}
	header.Set("User-Agent", c.userAgent)

	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		slog.Warn("Failed to connect to websocket. Mark domain "+domain+" as offline", "error", err)
		//c.lastFailed[domain] = time.Now()
		span.RecordError(err)
		return nil, err
	}

	return conn, nil
}
