package store

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"

	"github.com/totegamma/concurrent/cdid"
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

type CommitOption struct {
	IsEphemeral bool `json:"isEphemeral,omitempty"`
}

func (s *service) Commit(
	ctx context.Context,
	mode core.CommitMode,
	document string,
	signature string,
	option string,
	keys []core.Key,
	IP string,
) (any, error) {
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
	owners := []string{}

	switch base.Type {
	case "message":
		result, owners, err = s.message.Create(ctx, mode, document, signature)

	case "association":
		result, owners, err = s.association.Create(ctx, mode, document, signature)

	case "profile":
		var p core.Profile
		p, err = s.profile.Upsert(ctx, mode, document, signature)
		result = p
		owners = []string{p.Author}

	case "affiliation":
		var e core.Entity
		e, err = s.entity.Affiliation(ctx, mode, document, signature, option)
		result = e
		owners = []string{e.ID}

	case "tombstone":
		var e core.Entity
		e, err = s.entity.Tombstone(ctx, mode, document, signature)
		result = e
		owners = []string{e.ID}

	case "timeline":
		var t core.Timeline
		t, err = s.timeline.UpsertTimeline(ctx, mode, document, signature)
		result = t
		owners = []string{t.Owner}

	case "retract":
		result, owners, err = s.timeline.Retract(ctx, mode, document, signature)

	case "event":
		result, err = s.timeline.Event(ctx, mode, document, signature)

	case "ack", "unack":
		var a core.Ack
		a, err = s.ack.Ack(ctx, mode, document, signature)
		result = a
		owners = []string{a.From, a.To}

	case "enact":
		var k core.Key
		k, err = s.key.Enact(ctx, mode, document, signature)
		result = k
		owners = []string{k.Root}

	case "revoke":
		var k core.Key
		k, err = s.key.Revoke(ctx, mode, document, signature)
		result = k
		owners = []string{k.Root}

	case "subscription":
		var sub core.Subscription
		sub, err = s.subscription.UpsertSubscription(ctx, mode, document, signature)
		result = sub
		owners = []string{sub.Owner}

	case "subscribe":
		var si core.SubscriptionItem
		si, err = s.subscription.Subscribe(ctx, mode, document, signature)
		result = si
		owners = []string{base.Signer}

	case "unsubscribe":
		var si core.SubscriptionItem
		si, err = s.subscription.Unsubscribe(ctx, mode, document)
		result = si
		owners = []string{base.Signer}

	case "delete":
		var doc core.DeleteDocument
		err = json.Unmarshal([]byte(document), &doc)
		if err != nil {
			return nil, err
		}
		typ := doc.Target[0]
		switch typ {
		case 'm': // message
			result, owners, err = s.message.Delete(ctx, mode, document, signature)
		case 'a': // association
			result, owners, err = s.association.Delete(ctx, mode, document, signature)
		case 'p': // profile
			var dp core.Profile
			dp, err = s.profile.Delete(ctx, mode, document)
			result = dp
			owners = []string{dp.Author}
		case 't': // timeline
			var dt core.Timeline
			dt, err = s.timeline.DeleteTimeline(ctx, mode, document)
			result = dt
			owners = []string{dt.Owner}
		case 's': // subscription
			var ds core.Subscription
			ds, err = s.subscription.DeleteSubscription(ctx, mode, document)
			result = ds
			owners = []string{ds.Owner}
		default:
			result, err = nil, fmt.Errorf("unknown document type: %s", string(typ))
		}
	default:
		return nil, fmt.Errorf("unknown document type: %s", base.Type)
	}

	if err == nil && base.Type != "event" && (mode == core.CommitModeExecute || mode == core.CommitModeLocalOnlyExec) {
		var localOwners []string
		for _, owner := range owners {
			if owner == s.config.CSID {
				localOwners = append(localOwners, owner)
			}
			if core.IsCCID(owner) {
				ownerEntity, err := s.entity.Get(ctx, owner)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to get owner entity"))
					continue
				}

				if ownerEntity.Domain == s.config.FQDN {
					localOwners = append(localOwners, owner)
				}
			}
		}

		isEphemeral := false
		var commitOption CommitOption
		err = json.Unmarshal([]byte(option), &commitOption)
		if err == nil {
			isEphemeral = commitOption.IsEphemeral
		}

		hash := core.GetHash([]byte(document))
		hash10 := [10]byte{}
		copy(hash10[:], hash[:10])
		signedAt := base.SignedAt
		documentID := cdid.New(hash10, signedAt).String()

		commitLog := core.CommitLog{
			IP:          IP,
			DocumentID:  documentID,
			IsEphemeral: isEphemeral,
			Type:        base.Type,
			Document:    document,
			Signature:   signature,
			SignedAt:    base.SignedAt,
			Owners:      localOwners,
		}

		_, err = s.repo.Log(ctx, commitLog)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	return result, err
}

func (s *service) Restore(ctx context.Context, archive io.Reader, from string, IP string) ([]core.BatchResult, error) {
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

		signer, err := s.entity.GetWithHint(ctx, doc.Signer, from)
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

		_, err = s.Commit(ctx, core.CommitModeLocalOnlyExec, document, signature, "", keys, IP)
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

func (s *service) SyncCommitFile(ctx context.Context, owner string) (core.SyncStatus, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.SyncCommitFile")
	defer span.End()

	go s.repo.SyncCommitFile(context.Background(), owner)
	status, err := s.repo.SyncStatus(ctx, owner)
	if err != nil {
		span.RecordError(err)
		return core.SyncStatus{}, err
	}
	status.Status = "syncing"
	return status, nil
}

func (s *service) SyncStatus(ctx context.Context, owner string) (core.SyncStatus, error) {
	ctx, span := tracer.Start(ctx, "Store.Service.SyncStatus")
	defer span.End()

	return s.repo.SyncStatus(ctx, owner)
}
