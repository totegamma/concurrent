package ack

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
)

type service struct {
	repository Repository
	client     client.Client
	entity     core.EntityService
	key        core.KeyService
	config     core.Config
}

// NewService creates a new entity service
func NewService(repository Repository, client client.Client, entity core.EntityService, key core.KeyService, config core.Config) core.AckService {
	return &service{
		repository,
		client,
		entity,
		key,
		config,
	}
}

// Ack creates new Ack
func (s *service) Ack(ctx context.Context, mode core.CommitMode, document string, signature string) (core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Service.Ack")
	defer span.End()

	var doc core.AckDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Ack{}, err
	}

	switch doc.Type {
	case "ack":
		to, err := s.entity.Get(ctx, doc.To)
		if err != nil {
			span.RecordError(err)
			return core.Ack{}, err
		}

		if to.Domain != s.config.FQDN {
			packet := core.Commit{
				Document:  document,
				Signature: signature,
			}

			packetStr, err := json.Marshal(packet)
			if err != nil {
				span.RecordError(err)
				return core.Ack{}, err
			}

			resp, err := s.client.Commit(ctx, to.Domain, string(packetStr), nil)
			if err != nil {
				span.RecordError(err)
				return core.Ack{}, err
			}

			defer resp.Body.Close()
		}

		return s.repository.Ack(ctx, &core.Ack{
			From:      doc.From,
			To:        doc.To,
			Document:  document,
			Signature: signature,
		})
	case "unack":
		to, err := s.entity.Get(ctx, doc.To)
		if err != nil {
			span.RecordError(err)
			return core.Ack{}, err
		}

		if to.Domain != s.config.FQDN {
			packet := core.Commit{
				Document:  document,
				Signature: signature,
			}

			packetStr, err := json.Marshal(packet)
			if err != nil {
				span.RecordError(err)
				return core.Ack{}, err
			}

			resp, err := s.client.Commit(ctx, to.Domain, string(packetStr), nil)
			if err != nil {
				span.RecordError(err)
				return core.Ack{}, err
			}

			defer resp.Body.Close()
		}

		return s.repository.Unack(ctx, &core.Ack{
			From:      doc.From,
			To:        doc.To,
			Document:  document,
			Signature: signature,
		})
	default:
		return core.Ack{}, fmt.Errorf("invalid object type")
	}
}

// GetAcker returns acker
func (s *service) GetAcker(ctx context.Context, user string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Service.GetAcker")
	defer span.End()

	return s.repository.GetAcker(ctx, user)
}

// GetAcking returns acking
func (s *service) GetAcking(ctx context.Context, user string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Service.GetAcking")
	defer span.End()

	return s.repository.GetAcking(ctx, user)
}
