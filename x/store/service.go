package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/totegamma/concurrent/x/ack"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/profile"
	"github.com/totegamma/concurrent/x/subscription"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/util"
)

type Service interface {
	Commit(ctx context.Context, document, signature, option string) (any, error)
}

type service struct {
	key          key.Service
	entity       entity.Service
	message      message.Service
	association  association.Service
	profile      profile.Service
	timeline     timeline.Service
	ack          ack.Service
	subscription subscription.Service
	storage      *os.File
	config       util.Config
}

func NewService(
	key key.Service,
	entity entity.Service,
	message message.Service,
	association association.Service,
	profile profile.Service,
	timeline timeline.Service,
	ack ack.Service,
	subscription subscription.Service,
	config util.Config,
) Service {

	err := os.MkdirAll(config.Server.RepositoryPath, 0755)
	if err != nil {
		slog.Error("failed to create repository directory:", err)
		panic(err)
	}

	logpath := filepath.Join(config.Server.RepositoryPath, "concrnt-all.log")
	storage, err := os.OpenFile(logpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open repository log file:", err)
		panic(err)
	}

	return &service{
		key:          key,
		entity:       entity,
		message:      message,
		association:  association,
		profile:      profile,
		timeline:     timeline,
		ack:          ack,
		subscription: subscription,
		storage:      storage,
	}
}

func (s *service) Commit(ctx context.Context, document string, signature string, option string) (any, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.Commit")
	defer span.End()

	var base core.DocumentBase[any]
	err := json.Unmarshal([]byte(document), &base)
	if err != nil {
		return nil, err
	}

	keys, ok := ctx.Value(core.RequesterKeychainKey).([]core.Key)
	if !ok {
		keys = []core.Key{}
	}

	err = s.key.ValidateDocument(ctx, document, signature, keys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var result any

	switch base.Type {
	case "message":
		result, err = s.message.Create(ctx, document, signature)
	case "association":
		result, err = s.association.Create(ctx, document, signature)
	case "profile":
		result, err = s.profile.Upsert(ctx, document, signature)
	case "affiliation":
		result, err = s.entity.Affiliation(ctx, document, signature, option)
	case "tombstone":
		result, err = s.entity.Tombstone(ctx, document, signature)
	case "timeline":
		result, err = s.timeline.UpsertTimeline(ctx, document, signature)
	case "event":
		result, err = s.timeline.Event(ctx, document, signature)
	case "ack", "unack":
		result, err = nil, s.ack.Ack(ctx, document, signature)
	case "enact":
		result, err = s.key.Enact(ctx, document, signature)
	case "revoke":
		result, err = s.key.Revoke(ctx, document, signature)
	case "subscription":
		result, err = s.subscription.CreateSubscription(ctx, document, signature)
	case "subscribe":
		result, err = s.subscription.Subscribe(ctx, document, signature)
	case "unsubscribe":
		result, err = s.subscription.Unsubscribe(ctx, document)
	case "delete":
		var doc core.DeleteDocument
		err := json.Unmarshal([]byte(document), &doc)
		if err != nil {
			return nil, err
		}
		typ := doc.Target[0]
		switch typ {
		case 'm': // message
			result, err = s.message.Delete(ctx, document, signature)
		case 'a': // association
			result, err = s.association.Delete(ctx, document, signature)
		case 'p': // profile
			result, err = s.profile.Delete(ctx, document)
		case 't': // timeline
			result, err = s.timeline.DeleteTimeline(ctx, document)
		default:
			result, err = nil, fmt.Errorf("unknown document type: %s", string(typ))
		}
	default:
		return nil, fmt.Errorf("unknown document type: %s", base.Type)
	}

	if err == nil {
		// save document to history
		owner := base.Owner
		if owner == "" {
			owner = base.Signer
		}

		_, err = s.storage.WriteString(fmt.Sprintf("%s %s %s\n", owner, document, signature))
	}

	return result, err
}
