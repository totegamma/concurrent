package store

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/key"
)

type service struct {
	repo           Repository
	key            core.KeyService
	entity         core.EntityService
	message        core.MessageService
	association    core.AssociationService
	profile        core.ProfileService
	timeline       core.TimelineService
	ack            core.AckService
	subscription   core.SubscriptionService
	semanticID     core.SemanticIDService
	config         core.Config
	repositoryPath string
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
	semanticID core.SemanticIDService,
	config core.Config,
	repositoryPath string,
) core.StoreService {
	return &service{
		repo:           repo,
		key:            key,
		entity:         entity,
		message:        message,
		association:    association,
		profile:        profile,
		timeline:       timeline,
		ack:            ack,
		subscription:   subscription,
		semanticID:     semanticID,
		config:         config,
		repositoryPath: repositoryPath,
	}
}

func (s *service) Commit(ctx context.Context, mode core.CommitMode, document string, signature string, option string, keys []core.Key) (any, error) {
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

	err = s.ValidateDocument(ctx, document, signature, keys)
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

		entry := fmt.Sprintf("%s %s", signature, document)

		if base.Type == "ack" || base.Type == "unack" { // ack/unackはfrom/toの両方にログを残す
			var ackDoc core.AckDocument
			err := json.Unmarshal([]byte(document), &ackDoc)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to unmarshal ack document"))
			}

			fromEntity, err := s.entity.Get(ctx, ackDoc.From)
			if err == nil && fromEntity.Domain == s.config.FQDN {
				err = s.repo.Log(ctx, ackDoc.From, entry)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to log ack.from document"))
				}
			} else {
				span.RecordError(errors.Wrap(err, "failed to get ack.from entity"))
			}

			toEntity, err := s.entity.Get(ctx, ackDoc.To)
			if err == nil && toEntity.Domain == s.config.FQDN {
				err = s.repo.Log(ctx, ackDoc.To, entry)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to log ack.to document"))
				}
			} else {
				span.RecordError(errors.Wrap(err, "failed to get ack.to entity"))
			}
		} else {
			// save document to history
			owner := base.Owner
			if owner == "" {
				owner = base.Signer
			}

			err = s.repo.Log(ctx, owner, entry)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to log document"))
			}
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
	path := filepath.Join(s.repositoryPath, "user", filename)

	return path
}

func (s *service) Restore(ctx context.Context, archive io.Reader, from string) ([]core.BatchResult, error) {
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

		var doc core.DocumentBase[any]
		err := json.Unmarshal([]byte(document), &doc)
		if err != nil {
			results = append(results, core.BatchResult{ID: split[0], Error: fmt.Sprintf("%v", errors.Wrap(err, "failed to unmarshal document"))})
			continue
		}

		signer, err := s.entity.GetWithHint(ctx, doc.Owner, from)
		if err != nil {
			results = append(results, core.BatchResult{ID: split[0], Error: fmt.Sprintf("%v", errors.Wrap(err, "failed to resolve signer"))})
			continue
		}

		var keys []core.Key
		if doc.KeyID != "" {
			if signer.Domain == s.config.FQDN { // local
				keys, err = s.key.GetKeyResolution(ctx, doc.KeyID)
			} else { // remote
				keys, err = s.key.GetRemoteKeyResolution(ctx, signer.Domain, doc.KeyID)
			}
		}
		if err != nil {
			results = append(results, core.BatchResult{ID: split[0], Error: fmt.Sprintf("%v", errors.Wrap(err, "failed to resolve key"))})
			continue
		}

		_, err = s.Commit(ctx, core.CommitModeLocalOnlyExec, document, signature, "", keys)
		results = append(results, core.BatchResult{ID: split[0], Error: fmt.Sprintf("%v", err)})
	}

	return results, nil
}

func (s *service) ValidateDocument(ctx context.Context, document, signature string, keys []core.Key) error {
	ctx, span := tracer.Start(ctx, "Key.Service.ValidateDocument")
	defer span.End()

	object := core.DocumentBase[any]{}
	err := json.Unmarshal([]byte(document), &object)
	if err != nil {
		span.RecordError(err)
		return errors.Wrap(err, "failed to unmarshal payload")
	}

	// マスターキーの場合: そのまま検証して終了
	if object.KeyID == "" {
		signatureBytes, err := hex.DecodeString(signature)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[master] failed to decode signature")
		}
		err = core.VerifySignature([]byte(document), signatureBytes, object.Signer)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[master] failed to verify signature")
		}
	} else { // サブキーの場合: 親キーを取得して検証

		signer, err := s.entity.Get(ctx, object.Signer)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to resolve host")
		}

		ccid := ""

		if signer.Domain == s.config.FQDN {
			ccid, err = s.key.ResolveSubkey(ctx, object.KeyID)
			if err != nil {
				span.RecordError(err)
				return errors.Wrap(err, "[sub] failed to resolve subkey")
			}
		} else {
			ccid, err = key.ValidateKeyResolution(keys)
			if err != nil {
				span.RecordError(err)
				return errors.Wrap(err, "[sub] failed to resolve remote subkey")
			}
		}

		if ccid != object.Signer {
			err := fmt.Errorf("Signer is not matched with the resolved signer")
			span.RecordError(err)
			return err
		}

		signatureBytes, err := hex.DecodeString(signature)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to decode signature")
		}
		err = core.VerifySignature([]byte(document), signatureBytes, object.KeyID)
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "[sub] failed to verify signature")
		}
	}

	return nil
}

func (s *service) CleanUserAllData(ctx context.Context, target string) error {
	ctx, span := tracer.Start(ctx, "Store.Service.CleanUserAllData")
	defer span.End()

	var err error
	err = s.entity.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean entity"))
		return err
	}

	err = s.profile.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean profile"))
		return err
	}

	err = s.message.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean message"))
		return err
	}

	err = s.association.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean association"))
		return err
	}

	err = s.timeline.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean timeline"))
		return err
	}

	err = s.subscription.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean subscription"))
		return err
	}

	err = s.semanticID.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean semanticID"))
		return err
	}

	err = s.key.Clean(ctx, target)
	if err != nil {
		span.RecordError(errors.Wrap(err, "failed to clean key"))
		return err
	}

	return nil
}
