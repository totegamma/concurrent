package ack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/xid"
	"net/http"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Service is the interface for entity service
type Service interface {
	Ack(ctx context.Context, from, to string) error
	GetAcker(ctx context.Context, key string) ([]core.Ack, error)
	GetAcking(ctx context.Context, key string) ([]core.Ack, error)
}

type service struct {
	repository Repository
	entity     entity.Service
	key        key.Service
	config     util.Config
}

// NewService creates a new entity service
func NewService(repository Repository, entity entity.Service, key key.Service, config util.Config) Service {
	return &service{
		repository,
		entity,
		key,
		config,
	}
}

// Ack creates new Ack
func (s *service) Ack(ctx context.Context, objectStr string, signature string) error {
	ctx, span := tracer.Start(ctx, "ServiceAck")
	defer span.End()

	var object core.AckDocument
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = s.key.ValidateSignedObject(ctx, objectStr, signature)
	if err != nil {
		span.RecordError(err)
		return err
	}

	switch object.Type {
	case "ack":
		address, err := s.entity.GetAddress(ctx, object.To)
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
				Subject:        "CC_API",
				Audience:       address.Domain,
				ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
				IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
				JWTID:          xid.New().String(),
			}, s.config.Concurrent.PrivateKey)

			req.Header.Add("content-type", "application/json")
			req.Header.Add("authorization", "Bearer "+jwt)
			client := new(http.Client)
			client.Timeout = 10 * time.Second
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
			Document:  objectStr,
		})
	case "unack":
		address, err := s.entity.GetAddress(ctx, object.To)
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
				Subject:        "CC_API",
				Audience:       address.Domain,
				ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
				IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
				JWTID:          xid.New().String(),
			}, s.config.Concurrent.PrivateKey)

			req.Header.Add("content-type", "application/json")
			req.Header.Add("authorization", "Bearer "+jwt)
			client := new(http.Client)
			client.Timeout = 10 * time.Second
			resp, err := client.Do(req)
			if err != nil {
				span.RecordError(err)
				return err
			}
			defer resp.Body.Close()
		}
		return s.repository.Unack(ctx, &core.Ack{
			From:      object.From,
			To:        object.To,
			Signature: signature,
			Document:  objectStr,
		})
	default:
		return fmt.Errorf("invalid object type")
	}
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
