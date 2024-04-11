package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/totegamma/concurrent/x/cdid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"strings"
)

// Repository is the interface for collection repository
type Service interface {
	CreateSubscription(ctx context.Context, objectStr string, signature string) (core.Subscription, error)
	GetSubscription(ctx context.Context, id string) (core.Subscription, error)
	UpdateSubscription(ctx context.Context, obj core.Subscription) (core.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error)

	Subscribe(ctx context.Context, document string, signature string) (core.SubscriptionItem, error)
	Unsubscribe(ctx context.Context, document string) (core.SubscriptionItem, error)
}

type service struct {
	repo Repository
}

// NewRepository creates a new collection repository
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// CreateSubscription creates new collection
func (s *service) CreateSubscription(ctx context.Context, objectStr string, signature string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.CreateSubscription")
	defer span.End()

	var object core.SubscriptionDocument[any]
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	hash := util.GetHash([]byte(objectStr))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	signedAt := object.SignedAt
	id := cdid.New(hash10, signedAt).String()

	subscription := core.Subscription{
		ID:          id,
		Author:      object.Signer,
		Indexable:   object.Indexable,
		DomainOwned: object.DomainOwned,
		Schema:      object.Schema,
		Document:    objectStr,
		Signature:   signature,
	}

	created, err := s.repo.CreateSubscription(ctx, subscription)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	return created, nil
}

// GetSubscription returns a Subscription by ID
func (s *service) GetSubscription(ctx context.Context, id string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetSubscription")
	defer span.End()

	return s.repo.GetSubscription(ctx, id)
}

// UpdateSubscription updates a collection
func (s *service) UpdateSubscription(ctx context.Context, obj core.Subscription) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.UpdateSubscription")
	defer span.End()

	return s.repo.UpdateSubscription(ctx, obj)
}

// DeleteSubscription deletes a collection by ID
func (s *service) DeleteSubscription(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Subscription.Service.DeleteSubscription")
	defer span.End()

	return s.repo.DeleteSubscription(ctx, id)
}

// GetOwnSubscriptions returns all subscriptions owned by the owner
func (s *service) GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetOwnSubscriptions")
	defer span.End()

	return s.repo.GetOwnSubscriptions(ctx, owner)
}

// Subscribe creates new collection item
func (s *service) Subscribe(ctx context.Context, document string, signature string) (core.SubscriptionItem, error) {
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
func (s *service) Unsubscribe(ctx context.Context, document string) (core.SubscriptionItem, error) {
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
