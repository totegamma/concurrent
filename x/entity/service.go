package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"strconv"
	"bytes"

	"github.com/rs/xid"
	"net/http"

	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"golang.org/x/exp/slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

)

// Service is the interface for entity service
type Service interface {
	Create(ctx context.Context, ccid string, payload string, signature string, info string) error
	Register(ctx context.Context, ccid, payload, signature, info, invitation string) error
	Get(ctx context.Context, ccid string) (core.Entity, error)
	List(ctx context.Context) ([]core.Entity, error)
	ListModified(ctx context.Context, modified time.Time) ([]core.Entity, error)
	ResolveHost(ctx context.Context, user string) (string, error)
	Update(ctx context.Context, entity *core.Entity) error
	IsUserExists(ctx context.Context, user string) bool
	Delete(ctx context.Context, id string) error
	Ack(ctx context.Context, from, to string) error
	Unack(ctx context.Context, from, to string) error
	Total(ctx context.Context) (int64, error)
	GetAcker(ctx context.Context, key string) ([]core.Ack, error)
	GetAcking(ctx context.Context, key string) ([]core.Ack, error)
    GetAddress(ctx context.Context, ccid string) (core.Address, error)
    UpdateAddress(ctx context.Context, ccid string, domain string) error
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


// Total returns the total number of entities
func (s *service) Total(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceTotal")
	defer span.End()

	return s.repository.Total(ctx)
}

// Create creates new entity
func (s *service) Create(ctx context.Context, ccid, payload, signature, info string) error {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

    // check certificate
    err := util.VerifySignature(payload, ccid, signature)
    if err != nil {
        span.RecordError(err)
        return err
    }

    // TOOD: check if ccid is known, validate registration

    return s.repository.CreateEntity(ctx, &core.Entity{
        ID:   ccid,
        Tag:  "",
        Payload: payload,
        Signature: signature,
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

    // check certificate
    err := util.VerifySignature(payload, ccid, signature)
    if err != nil {
        span.RecordError(err)
    }

    // TOOD: check if ccid is known, validate registration

	if s.config.Concurrent.Registration == "open" {
		return s.repository.CreateEntity(ctx,
            &core.Entity{
                ID:      ccid,
                Tag:     "",
                Signature: signature,
            },
            &core.EntityMeta{
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
                ID:      ccid,
                Tag:     "",
            },
            &core.EntityMeta{
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
func (s *service) ResolveHost(ctx context.Context, ccid string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceResolveHost")
	defer span.End()

    addr, err := s.repository.GetAddress(ctx, ccid)
    if err != nil {
        span.RecordError(err)

        // check for local user
        _, err := s.repository.GetEntity(ctx, ccid)
        if err != nil {
            span.RecordError(err)
            return "", err
        }

        return s.config.Concurrent.FQDN, nil

    }

    return addr.Domain, nil
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

// Ack creates new Ack
func (s *service) Ack(ctx context.Context, objectStr string, signature string) error {
	ctx, span := tracer.Start(ctx, "ServiceAck")
	defer span.End()

	var object AckSignedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if object.Type != "ack" {
		return fmt.Errorf("object is not ack")
	}

	err = util.VerifySignature(objectStr, object.From, signature)
	if err != nil {
		span.RecordError(err)
		return err
	}

    address, err := s.repository.GetAddress(ctx, object.To)
    if err == nil {
		packet := ackRequest{
			SignedObject: objectStr,
			Signature:    signature,
		}
		packetStr, err := json.Marshal(packet)
		if err != nil {
			span.RecordError(err)
			return err
		}

		req, err := http.NewRequest("POST", "https://"+address.Domain+"/api/v1/entities/checkpoint/ack", bytes.NewBuffer([]byte(packetStr)))

		if err != nil {
			span.RecordError(err)
			return err
		}

		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		jwt, err := jwt.Create(jwt.Claims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CONCURRENT_API",
			Audience:       address.Domain,
			ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
			JWTID:          xid.New().String(),
		}, s.config.Concurrent.PrivateKey)

		req.Header.Add("content-type", "application/json")
		req.Header.Add("authorization", "Bearer "+jwt)
		client := new(http.Client)
		resp, err := client.Do(req)
		if err != nil {
			span.RecordError(err)
			return err
		}
		defer resp.Body.Close()
	}

	return s.repository.Ack(ctx, &core.Ack{
		From:      object.From,
		To:        object.To,
		Signature: signature,
		Payload:   objectStr,
	})
}

// Unack creates new Unack
func (s *service) Unack(ctx context.Context, objectStr string, signature string) error {
	ctx, span := tracer.Start(ctx, "ServiceUnack")
	defer span.End()

	var object AckSignedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if object.Type != "unack" {
		return fmt.Errorf("object is not unack")
	}

	err = util.VerifySignature(objectStr, object.From, signature)
	if err != nil {
		span.RecordError(err)
		return err
	}

    address, err := s.repository.GetAddress(ctx, object.To)
    if err == nil {
		packet := ackRequest{
			SignedObject: objectStr,
			Signature:    signature,
		}
		packetStr, err := json.Marshal(packet)
		if err != nil {
			span.RecordError(err)
			return err
		}

		req, err := http.NewRequest("DELETE", "https://"+address.Domain+"/api/v1/entities/checkpoint/ack", bytes.NewBuffer([]byte(packetStr)))

		if err != nil {
			span.RecordError(err)
			return err
		}

		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		jwt, err := jwt.Create(jwt.Claims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CONCURRENT_API",
			Audience:       address.Domain,
			ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
			JWTID:          xid.New().String(),
		}, s.config.Concurrent.PrivateKey)

		req.Header.Add("content-type", "application/json")
		req.Header.Add("authorization", "Bearer "+jwt)
		client := new(http.Client)
		resp, err := client.Do(req)
		if err != nil {
			span.RecordError(err)
			return err
		}
		defer resp.Body.Close()
	}

	return s.repository.Unack(ctx, object.From, object.To)
}

// GetAcker returns acker
func (s *service) GetAcker(ctx context.Context, user string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetAcker")
	defer span.End()

	return s.repository.GetAcker(ctx, user)
}

// GetAcking returns acking
func (s *service) GetAcking(ctx context.Context, user string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetAcking")
	defer span.End()

	return s.repository.GetAcking(ctx, user)
}

// GetAddress returns the address of a entity
func (s *service) GetAddress(ctx context.Context, ccid string) (core.Address, error) {
    ctx, span := tracer.Start(ctx, "ServiceGetAddress")
    defer span.End()

    return s.repository.GetAddress(ctx, ccid)
}

// UpdateAddress updates the address of a entity
func (s *service) UpdateAddress(ctx context.Context, ccid string, domain string) error {
    ctx, span := tracer.Start(ctx, "ServiceUpdateAddress")
    defer span.End()

    return s.repository.UpdateAddress(ctx, ccid, domain)
}


