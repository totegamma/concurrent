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

	CreateItem(ctx context.Context, objectStr string, signature string) (core.SubscriptionItem, error)
	DeleteItem(ctx context.Context, id string, itemId string) (core.SubscriptionItem, error)
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
	ctx, span := tracer.Start(ctx, "ServiceCreateSubscription")
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
	ctx, span := tracer.Start(ctx, "ServiceGetSubscription")
	defer span.End()

	return s.repo.GetSubscription(ctx, id)
}

// UpdateSubscription updates a collection
func (s *service) UpdateSubscription(ctx context.Context, obj core.Subscription) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdateSubscription")
	defer span.End()

	return s.repo.UpdateSubscription(ctx, obj)
}

// DeleteSubscription deletes a collection by ID
func (s *service) DeleteSubscription(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "ServiceDeleteSubscription")
	defer span.End()

	return s.repo.DeleteSubscription(ctx, id)
}

// CreateItem creates new collection item
func (s *service) CreateItem(ctx context.Context, objectStr string, signature string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreateItem")
	defer span.End()

	var object core.SubscriptionItemDocument[any]
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	fullID := object.Target
	split := strings.Split(fullID, "@")
	if len(split) != 2 {
		return core.SubscriptionItem{}, errors.New("target must be in the format of id@resolver")
	}

	item := core.SubscriptionItem{
		Target:       split[0],
		Subscription: object.Subscription,
	}

	resolver := split[1]
	if resolver[:3] == "con" { // ccid resolvation
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

// GetItem returns a SubscriptionItem by ID
func (s *service) GetItem(ctx context.Context, id string, itemId string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetItem")
	defer span.End()

	return s.repo.GetItem(ctx, id, itemId)
}

// DeleteItem deletes a collection item by ID
func (s *service) DeleteItem(ctx context.Context, id string, itemId string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceDeleteItem")
	defer span.End()

	return s.repo.DeleteItem(ctx, id, itemId)
}
