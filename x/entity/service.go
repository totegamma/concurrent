//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mock/service.go

package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"net/http"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/util"
	"golang.org/x/exp/slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Service is the interface for entity service
type Service interface {
	Create(ctx context.Context, ccid, payload, signature, info string) error
	Register(ctx context.Context, ccid, payload, signature, info, invitation string) error
	Get(ctx context.Context, ccid string) (core.Entity, error)
	List(ctx context.Context) ([]core.Entity, error)
	ListModified(ctx context.Context, modified time.Time) ([]core.Entity, error)
	ResolveHost(ctx context.Context, user, hint string) (string, error)
	Update(ctx context.Context, entity *core.Entity) error
	IsUserExists(ctx context.Context, user string) bool
	Delete(ctx context.Context, id string) error
	GetAddress(ctx context.Context, ccid string) (core.Address, error)
	UpdateAddress(ctx context.Context, ccid string, domain string, signedAt time.Time) error
	Count(ctx context.Context) (int64, error)
	PullEntityFromRemote(ctx context.Context, id, domain string) (string, error)
}

type service struct {
	repository Repository
	config     util.Config
	jwtService jwt.Service
}

// NewService creates a new entity service
func NewService(repository Repository, config util.Config, jwtService jwt.Service) Service {
	return &service{
		repository,
		config,
		jwtService,
	}
}

type addressResponse struct {
	Status  string `json:"status"`
	Content string `json:"content"`
}

// PullEntityFromRemote pulls entity from remote
func (s *service) PullEntityFromRemote(ctx context.Context, id, hintDomain string) (string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryPullEntityFromRemote")
	defer span.End()

	client := new(http.Client)
	client.Timeout = 3 * time.Second
	req, err := http.NewRequest("GET", "https://"+hintDomain+"/api/v1/address/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var remoteAddress addressResponse
	json.Unmarshal(body, &remoteAddress)

	if remoteAddress.Status != "ok" {
		return "", fmt.Errorf("Remote address is not found")
	}

	targetDomain := remoteAddress.Content

	req, err = http.NewRequest("GET", "https://"+targetDomain+"/api/v1/entity/"+id, nil)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err = client.Do(req)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)

	var remoteEntity entityResponse
	json.Unmarshal(body, &remoteEntity)

	entity := remoteEntity.Content

	err = util.VerifySignature([]byte(entity.AffiliationPayload), []byte(entity.AffiliationSignature), entity.ID)
	if err != nil {
		span.RecordError(err)
		slog.Error(
			"Invalid signature",
			slog.String("error", err.Error()),
			slog.String("module", "agent"),
		)
		return "", fmt.Errorf("Invalid signature")
	}

	var signedObj core.EntityAffiliation
	err = json.Unmarshal([]byte(entity.AffiliationPayload), &signedObj)
	if err != nil {
		span.RecordError(err)
		slog.Error(
			"pullRemoteEntities",
			slog.String("error", err.Error()),
			slog.String("module", "agent"),
		)
		return "", fmt.Errorf("Invalid payload")
	}

	if signedObj.Body.Domain != targetDomain {
		err = fmt.Errorf("Remote entity is not for the target domain")
		span.RecordError(err)
		return "", err
	}

	existanceAddr, err := s.GetAddress(ctx, entity.ID)
	if err == nil {
		// compare signed date
		if signedObj.SignedAt.Unix() <= existanceAddr.SignedAt.Unix() {
			err = fmt.Errorf("Remote entity is older than local entity")
			span.RecordError(err)
			return "", err
		}
	}

	existanceEntity, err := s.Get(ctx, entity.ID)
	if err == nil {
		if signedObj.SignedAt.Unix() <= existanceEntity.CDate.Unix() {
			err = fmt.Errorf("Remote entity is older than local entity")
			span.RecordError(err)
			return "", err
		}
	}

	err = s.UpdateAddress(ctx, entity.ID, targetDomain, signedObj.SignedAt)

	if err != nil {
		span.RecordError(err)
		slog.Error(
			"pullRemoteEntities",
			slog.String("error", err.Error()),
			slog.String("module", "agent"),
		)
		return "", err
	}

	return targetDomain, nil
}

// Total returns the count number of entities
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repository.Count(ctx)
}

// Create creates new entity
func (s *service) Create(ctx context.Context, ccid, payload, signature, info string) error {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

	err := checkRegistration(ccid, payload, signature, s.config.Concurrent.FQDN)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return s.repository.CreateEntity(ctx, &core.Entity{
		ID:                   ccid,
		Tag:                  "",
		AffiliationPayload:   payload,
		AffiliationSignature: signature,
	}, &core.EntityMeta{
		ID:   ccid,
		Info: info,
	})
}

// Register creates new entity
// check if registration is open
func (s *service) Register(ctx context.Context, ccid, payload, signature, info, invitation string) error {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

	err := checkRegistration(ccid, payload, signature, s.config.Concurrent.FQDN)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if s.config.Concurrent.Registration == "open" {
		return s.repository.CreateEntity(ctx,
			&core.Entity{
				ID:                   ccid,
				Tag:                  "",
				AffiliationPayload:   payload,
				AffiliationSignature: signature,
			},
			&core.EntityMeta{
				ID:      ccid,
				Info:    info,
				Inviter: "",
			},
		)
	} else if s.config.Concurrent.Registration == "invite" {
		if invitation == "" {
			return fmt.Errorf("invitation code is required")
		}

		claims, err := jwt.Validate(invitation)
		if err != nil {
			span.RecordError(err)
			return err
		}
		if claims.Subject != "CONCURRENT_INVITE" {
			return fmt.Errorf("invalid invitation code")
		}

		ok, err := s.jwtService.CheckJTI(ctx, claims.JWTID)
		if err != nil {
			span.RecordError(err)
			return err
		}
		if !ok {
			return fmt.Errorf("token is already used")
		}

		inviter, err := s.repository.GetEntity(ctx, claims.Issuer)
		if err != nil {
			span.RecordError(err)
			return err
		}

		inviterTags := strings.Split(inviter.Tag, ",")
		if !slices.Contains(inviterTags, "_invite") {
			return fmt.Errorf("inviter is not allowed to invite")
		}

		err = s.repository.CreateEntity(ctx,
			&core.Entity{
				ID:                   ccid,
				AffiliationPayload:   payload,
				AffiliationSignature: signature,
				Tag:                  "",
			},
			&core.EntityMeta{
				ID:      ccid,
				Info:    info,
				Inviter: claims.Issuer,
			},
		)

		if err != nil {
			span.RecordError(err)
			return err
		}

		expireAt, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
		if err != nil {
			span.RecordError(err)
			return err
		}
		err = s.jwtService.InvalidateJTI(ctx, claims.JWTID, time.Unix(expireAt, 0))

		if err != nil {
			span.RecordError(err)
			return err
		}

		return nil

	} else {
		return fmt.Errorf("registration is not open")
	}
}

// Get returns entity by ccid
func (s *service) Get(ctx context.Context, key string) (core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	entity, err := s.repository.GetEntity(ctx, key)
	if err != nil {
		span.RecordError(err)
		return core.Entity{}, err
	}

	return entity, nil
}

// List returns all entities
func (s *service) List(ctx context.Context) ([]core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceList")
	defer span.End()

	return s.repository.GetList(ctx)
}

// ListModified returns all entities modified after time
func (s *service) ListModified(ctx context.Context, time time.Time) ([]core.Entity, error) {
	ctx, span := tracer.Start(ctx, "ServiceListModified")
	defer span.End()

	return s.repository.ListModified(ctx, time)
}

// ResolveHost returns host for user
func (s *service) ResolveHost(ctx context.Context, ccid string, hint string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceResolveHost")
	defer span.End()

	// check address book
	addr, err := s.repository.GetAddress(ctx, ccid)
	if err == nil {
		return addr.Domain, nil
	}

	// check local user
	_, err = s.repository.GetEntity(ctx, ccid)
	if err == nil {
		return s.config.Concurrent.FQDN, nil
	}

	if hint != "" {
		host, err := s.PullEntityFromRemote(ctx, ccid, hint)
		if err == nil {
			return host, nil
		}
	}

	return "", fmt.Errorf("User not found")
}

// Update updates entity
func (s *service) Update(ctx context.Context, entity *core.Entity) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	return s.repository.UpdateEntity(ctx, entity)
}

// IsUserExists returns true if user exists
func (s *service) IsUserExists(ctx context.Context, user string) bool {
	ctx, span := tracer.Start(ctx, "ServiceIsUserExists")
	defer span.End()

	_, err := s.repository.GetEntity(ctx, user)
	if err != nil {
		return false
	}
	return true
}

// Delete deletes entity
func (s *service) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, id)
}

// GetAddress returns the address of a entity
func (s *service) GetAddress(ctx context.Context, ccid string) (core.Address, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetAddress")
	defer span.End()

	return s.repository.GetAddress(ctx, ccid)
}

// UpdateAddress updates the address of a entity
func (s *service) UpdateAddress(ctx context.Context, ccid string, domain string, signedAt time.Time) error {
	ctx, span := tracer.Start(ctx, "ServiceUpdateAddress")
	defer span.End()

	return s.repository.UpdateAddress(ctx, ccid, domain, signedAt)
}

// ---

func checkRegistration(ccid, payload, signature, mydomain string) error {

	err := util.VerifySignature([]byte(payload), []byte(ccid), ccid)
	if err != nil {
		return err
	}

	var signedObject core.DocumentBase[any] //TODO
	err = json.Unmarshal([]byte(payload), &signedObject)
	if err != nil {
		return err
	}

	if signedObject.Type != "Entity" {
		return fmt.Errorf("object is not entity")
	}

	domain, ok := signedObject.Body.(map[string]interface{})["domain"].(string)
	if !ok {
		return fmt.Errorf("domain is not string")
	}

	if domain != mydomain {
		return fmt.Errorf("domain is not match")
	}

	return nil
}
