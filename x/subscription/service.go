package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
	"strings"
)

type service struct {
	repo Repository
}

// NewRepository creates a new collection repository
func NewService(repo Repository) core.SubscriptionService {
	return &service{repo: repo}
}

// CreateSubscription creates new collection
func (s *service) CreateSubscription(ctx context.Context, mode core.CommitMode, document, signature string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.CreateSubscription")
	defer span.End()

	var doc core.SubscriptionDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	hash := util.GetHash([]byte(document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := doc.SignedAt
	id := cdid.New(hash10, signedAt).String()

	var policyparams *string = nil
	if doc.PolicyParams != "" {
		policyparams = &doc.PolicyParams
	}

	subscription := core.Subscription{
		ID:           id,
		Author:       doc.Signer,
		Indexable:    doc.Indexable,
		DomainOwned:  doc.DomainOwned,
		Schema:       doc.Schema,
		Policy:       doc.Policy,
		PolicyParams: policyparams,
		Document:     document,
		Signature:    signature,
	}

	if doc.ID != "" { // update
		existance, err := s.repo.GetSubscription(ctx, doc.ID)
		if err != nil {
			span.RecordError(err)
			return core.Subscription{}, err
		}

		subscription.ID = doc.ID
		subscription.DomainOwned = existance.DomainOwned // make sure the domain owned is immutable

		updated, err := s.repo.UpdateSubscription(ctx, subscription)
		if err != nil {
			span.RecordError(err)
			return updated, err
		}

		return updated, nil
	} else { // create
		created, err := s.repo.CreateSubscription(ctx, subscription)
		if err != nil {
			span.RecordError(err)
			return created, err
		}

		return created, nil
	}
}

// GetSubscription returns a Subscription by ID
func (s *service) GetSubscription(ctx context.Context, id string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetSubscription")
	defer span.End()

	return s.repo.GetSubscription(ctx, id)
}

// DeleteSubscription deletes a collection by ID
func (s *service) DeleteSubscription(ctx context.Context, mode core.CommitMode, document string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.DeleteSubscription")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	deleteTarget, err := s.repo.GetSubscription(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	if deleteTarget.Author != doc.Signer {
		return core.Subscription{}, errors.New("you are not authorized to perform this action")
	}

	err = s.repo.DeleteSubscription(ctx, doc.Target)

	return deleteTarget, err
}

// GetOwnSubscriptions returns all subscriptions owned by the owner
func (s *service) GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetOwnSubscriptions")
	defer span.End()

	return s.repo.GetOwnSubscriptions(ctx, owner)
}

// Subscribe creates new collection item
func (s *service) Subscribe(ctx context.Context, mode core.CommitMode, document, signature string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.Subscribe")
	defer span.End()

	var doc core.SubscribeDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	subscription, err := s.repo.GetSubscription(ctx, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	if doc.Signer != subscription.Author {
		return core.SubscriptionItem{}, errors.New("you are not authorized to perform this action")
	}

	fullID := doc.Target
	split := strings.Split(fullID, "@")
	if len(split) != 2 {
		return core.SubscriptionItem{}, errors.New("target must be in the format of id@resolver")
	}

	item := core.SubscriptionItem{
		ID:           doc.Target,
		Subscription: doc.Subscription,
	}

	resolver := split[1]
	if core.IsCCID(resolver) {
		item.Entity = &resolver
		item.ResolverType = core.ResolverTypeEntity
	} else { // web resolvation
		item.Domain = &resolver
		item.ResolverType = core.ResolverTypeDomain
	}

	created, err := s.repo.CreateItem(ctx, item)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	return created, nil
}

// DeleteItem deletes a collection item by ID
func (s *service) Unsubscribe(ctx context.Context, mode core.CommitMode, document string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.Unsubscribe")
	defer span.End()

	var doc core.UnsubscribeDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	item, err := s.repo.GetItem(ctx, doc.Target, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	subscription, err := s.repo.GetSubscription(ctx, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	if doc.Signer != subscription.Author {
		return core.SubscriptionItem{}, errors.New("you are not authorized to perform this action")
	}

	err = s.repo.DeleteItem(ctx, doc.Target, doc.Subscription)

	return item, err
}
