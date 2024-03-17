package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/xid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/util"
)

// Service is the interface for host service
type Service interface {
	Upsert(ctx context.Context, host core.Domain) (core.Domain, error)
	GetByFQDN(ctx context.Context, key string) (core.Domain, error)
	GetByCCID(ctx context.Context, key string) (core.Domain, error)
	List(ctx context.Context) ([]core.Domain, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, host core.Domain) error
	UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error
	SayHello(ctx context.Context, target string) (core.Domain, error)
	Hello(ctx context.Context, newcomer Profile) (Profile, error)
}

type service struct {
	repository Repository
	config     util.Config
}

// NewService creates a new host service
func NewService(repository Repository, config util.Config) Service {
	return &service{repository, config}
}

// Upsert creates new host
func (s *service) Upsert(ctx context.Context, host core.Domain) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	return s.repository.Upsert(ctx, host)
}

// GetByFQDN returns domain by FQDN
func (s *service) GetByFQDN(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.GetByFQDN(ctx, key)
}

// GetByCCID returns domain by CCID
func (s *service) GetByCCID(ctx context.Context, key string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetByCCID")
	defer span.End()

	return s.repository.GetByCCID(ctx, key)
}

// List returns list of domains
func (s *service) List(ctx context.Context) ([]core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// Delete deletes a domain
func (s *service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}

// Update updates a domain
func (s *service) Update(ctx context.Context, host core.Domain) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	return s.repository.Update(ctx, host)
}

// UpdateScrapeTime updates a domain's scrape time
func (s *service) UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateScrapeTime")
	defer span.End()

	return s.repository.UpdateScrapeTime(ctx, id, scrapeTime)
}

func (s *service) Hello(ctx context.Context, newcomer Profile) (Profile, error) {
	ctx, span := tracer.Start(ctx, "ServiceHello")
	defer span.End()

	slog.DebugContext(
		ctx, fmt.Sprintf("hello from %s", newcomer.ID),
		slog.String("module", "domain"),
	)

	// challenge
	req, err := http.NewRequest("GET", "https://"+newcomer.ID+"/api/v1/domain", nil)
	if err != nil {
		span.RecordError(err)
		return Profile{}, err
	}
	// Inject the current span context into the request
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return Profile{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var fetchedProf ProfileResponse
	err = json.Unmarshal(body, &fetchedProf)
	if err != nil {
		slog.ErrorContext(
			ctx, fmt.Sprintf("failed to unmarshal profile: %s", err.Error()),
			slog.String("module", "domain"),
		)
		return Profile{}, err
	}

	if newcomer.ID != fetchedProf.Content.ID {
		slog.ErrorContext(
			ctx, fmt.Sprintf("target does not match fetched profile: %v", fetchedProf.Content.ID),
			slog.String("module", "domain"),
		)
		return Profile{}, fmt.Errorf("validation failed")
	}

	s.Upsert(ctx, core.Domain{
		ID:     newcomer.ID,
		CCID:   newcomer.CCID,
		Tag:    "",
		Pubkey: newcomer.Pubkey,
	})

	slog.InfoContext(
		ctx, fmt.Sprint("Successfully added ", newcomer.ID),
		slog.String("module", "domain"),
		slog.String("type", "audit"),
	)

	return Profile{
		ID:     s.config.Concurrent.FQDN,
		CCID:   s.config.Concurrent.CCID,
		Pubkey: s.config.Concurrent.PublicKey,
	}, nil
}

func (s *service) SayHello(ctx context.Context, target string) (core.Domain, error) {
	ctx, span := tracer.Start(ctx, "ServiceSayHello")
	defer span.End()

	slog.DebugContext(
		ctx, fmt.Sprintf("saying hello to %s", target),
		slog.String("module", "domain"),
	)

	me := Profile{
		ID:     s.config.Concurrent.FQDN,
		CCID:   s.config.Concurrent.CCID,
		Pubkey: s.config.Concurrent.PublicKey,
	}

	meStr, err := json.Marshal(me)

	// challenge
	jwt, err := jwt.Create(jwt.Claims{
		Issuer:         s.config.Concurrent.CCID,
		Subject:        "CC_API",
		Audience:       target,
		ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		JWTID:          xid.New().String(),
	}, s.config.Concurrent.PrivateKey)

	req, err := http.NewRequest("POST", "https://"+target+"/api/v1/domains/hello", bytes.NewBuffer(meStr))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return core.Domain{}, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Add("content-type", "application/json")
	req.Header.Add("authorization", "Bearer "+jwt)
	client := new(http.Client)
	client.Timeout = 10 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return core.Domain{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(
			ctx, fmt.Sprintf("failed to read response body"),
			slog.String("error", err.Error()),
			slog.String("module", "domain"),
		)
	}

	var fetchedProf ProfileResponse
	json.Unmarshal(body, &fetchedProf)
	if err != nil {
		slog.ErrorContext(
			ctx, fmt.Sprintf("failed to unmarshal profile"),
			slog.String("error", err.Error()),
			slog.String("module", "domain"),
		)
		return core.Domain{}, err
	}

	if target != fetchedProf.Content.ID {
		slog.ErrorContext(
			ctx, fmt.Sprintf("target does not match fetched profile: %v", fetchedProf.Content.ID),
			slog.String("module", "domain"),
		)
		span.SetStatus(codes.Error, fmt.Sprintf("target does not match fetched profile: %v", fetchedProf.Content.ID))
		return core.Domain{}, fmt.Errorf("validation failed")
	}

	created, err := s.Upsert(ctx, core.Domain{
		ID:     fetchedProf.Content.ID,
		CCID:   fetchedProf.Content.CCID,
		Tag:    "",
		Pubkey: fetchedProf.Content.Pubkey,
	})

	if err != nil {
		span.RecordError(err)
		return core.Domain{}, err
	}

	slog.InfoContext(
		ctx, fmt.Sprint("Successfully added ", fetchedProf.Content.ID),
		slog.String("module", "domain"),
		slog.String("type", "audit"),
	)

	return created, nil
}
