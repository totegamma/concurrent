package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

type service struct {
	repo         Repository
	key          core.KeyService
	entity       core.EntityService
	message      core.MessageService
	association  core.AssociationService
	profile      core.ProfileService
	timeline     core.TimelineService
	ack          core.AckService
	subscription core.SubscriptionService
	config       util.Config
}

func NewService(
	repo Repository,
	key core.KeyService,
	entity core.EntityService,
	message core.MessageService,
	association core.AssociationService,
	profile core.ProfileService,
	timeline core.TimelineService,
	ack core.AckService,
	subscription core.SubscriptionService,
	config util.Config,
) core.StoreService {
	return &service{
		repo:         repo,
		key:          key,
		entity:       entity,
		message:      message,
		association:  association,
		profile:      profile,
		timeline:     timeline,
		ack:          ack,
		subscription: subscription,
		config:       config,
	}
}

func (s *service) Commit(ctx context.Context, mode core.CommitMode, document string, signature string, option string) (any, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.Commit")
	defer span.End()

	if mode == core.CommitModeUnknown {
		return nil, fmt.Errorf("unknown commit mode")
	}

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
		result, err = s.message.Create(ctx, mode, document, signature)
	case "association":
		result, err = s.association.Create(ctx, mode, document, signature)
	case "profile":
		result, err = s.profile.Upsert(ctx, mode, document, signature)
	case "affiliation":
		result, err = s.entity.Affiliation(ctx, mode, document, signature, option)
	case "tombstone":
		result, err = s.entity.Tombstone(ctx, mode, document, signature)
	case "timeline":
		result, err = s.timeline.UpsertTimeline(ctx, mode, document, signature)
	case "event":
		result, err = s.timeline.Event(ctx, mode, document, signature)
	case "ack", "unack":
		result, err = nil, s.ack.Ack(ctx, mode, document, signature)
	case "enact":
		result, err = s.key.Enact(ctx, mode, document, signature)
	case "revoke":
		result, err = s.key.Revoke(ctx, mode, document, signature)
	case "subscription":
		result, err = s.subscription.CreateSubscription(ctx, mode, document, signature)
	case "subscribe":
		result, err = s.subscription.Subscribe(ctx, mode, document, signature)
	case "unsubscribe":
		result, err = s.subscription.Unsubscribe(ctx, mode, document)
	case "delete":
		var doc core.DeleteDocument
		err := json.Unmarshal([]byte(document), &doc)
		if err != nil {
			return nil, err
		}
		typ := doc.Target[0]
		switch typ {
		case 'm': // message
			result, err = s.message.Delete(ctx, mode, document, signature)
		case 'a': // association
			result, err = s.association.Delete(ctx, mode, document, signature)
		case 'p': // profile
			result, err = s.profile.Delete(ctx, mode, document)
		case 't': // timeline
			result, err = s.timeline.DeleteTimeline(ctx, mode, document)
		case 's': // subscription
			result, err = s.subscription.DeleteSubscription(ctx, mode, document)
		default:
			result, err = nil, fmt.Errorf("unknown document type: %s", string(typ))
		}
	default:
		return nil, fmt.Errorf("unknown document type: %s", base.Type)
	}

	if err == nil && (mode == core.CommitModeExecute || mode == core.CommitModeLocalOnlyExec) {
		// save document to history
		owner := base.Owner
		if owner == "" {
			owner = base.Signer
		}

		entry := fmt.Sprintf("%s %s", signature, document)
		err = s.repo.Log(ctx, owner, entry)
		if err != nil {
			return nil, err
		}
	}

	return result, err
}

func (s *service) Since(ctx context.Context, since string) ([]core.CommitLog, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.Since")
	defer span.End()

	entries, err := s.repo.Since(ctx, since)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return entries, nil
}

func (s *service) GetPath(ctx context.Context, id string) string {
	ctx, span := tracer.Start(ctx, "Store.Service.GetPath")
	defer span.End()

	filename := fmt.Sprintf("%s.log", id)
	path := filepath.Join(s.config.Server.RepositoryPath, "user", filename)

	return path
}

func (s *service) Restore(ctx context.Context, archive io.Reader) ([]core.BatchResult, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.Restore")
	defer span.End()

	results := make([]core.BatchResult, 0)

	scanner := bufio.NewScanner(archive)

	for scanner.Scan() {
		job := scanner.Text()
		split := strings.Split(job, " ")
		if len(split) < 4 {
			results = append(results, core.BatchResult{ID: split[0], Error: "invalid job"})
			continue

		}
		// id := split[0]
		// owner := split[1]
		signature := split[2]
		document := strings.Join(split[3:], " ")
		_, err := s.Commit(ctx, core.CommitModeLocalOnlyExec, document, signature, "")
		results = append(results, core.BatchResult{ID: split[0], Error: fmt.Sprintf("%v", err)})
	}

	return results, nil
}
