package subscription

import (
	"context"
	"errors"

	"github.com/totegamma/concurrent/core"
	"gorm.io/gorm"
)

// Repository is the interface for collection repository
type Repository interface {
	CreateSubscription(ctx context.Context, subscription core.Subscription) (core.Subscription, error)
	GetSubscription(ctx context.Context, id string) (core.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error)

	CreateItem(ctx context.Context, item core.SubscriptionItem) (core.SubscriptionItem, error)
	GetItem(ctx context.Context, id string, subscription string) (core.SubscriptionItem, error)
	DeleteItem(ctx context.Context, id string, subscription string) error
}

type repository struct {
	db     *gorm.DB
	schema core.SchemaService
}

// NewRepository creates a new collection repository
func NewRepository(db *gorm.DB, schema core.SchemaService) Repository {
	return &repository{db, schema}
}

func (r *repository) normalizeDBID(id string) (string, error) {
	normalized := id

	if len(normalized) == 27 {
		if normalized[0] != 's' {
			return "", errors.New("subscription id must start with 's'")
		}
		normalized = normalized[1:]
	}

	if len(normalized) != 26 {
		return "", errors.New("subscription id must be 26 characters")
	}

	return normalized, nil
}

func (r *repository) preProcess(ctx context.Context, subscription *core.Subscription) error {

	var err error
	subscription.ID, err = r.normalizeDBID(subscription.ID)
	if err != nil {
		return err
	}

	if subscription.SchemaID == 0 {
		schemaID, err := r.schema.UrlToID(ctx, subscription.Schema)
		if err != nil {
			return err
		}
		subscription.SchemaID = schemaID
	}

	if subscription.PolicyID == 0 && subscription.Policy != "" {
		policyID, err := r.schema.UrlToID(ctx, subscription.Policy)
		if err != nil {
			return err
		}
		subscription.PolicyID = policyID
	}

	return nil
}

func (r *repository) postProcess(ctx context.Context, subscription *core.Subscription) error {

	if len(subscription.ID) == 26 {
		subscription.ID = "s" + subscription.ID
	}

	if subscription.SchemaID != 0 && subscription.Schema == "" {
		schemaUrl, err := r.schema.IDToUrl(ctx, subscription.SchemaID)
		if err != nil {
			return err
		}
		subscription.Schema = schemaUrl
	}

	if subscription.PolicyID != 0 && subscription.Policy == "" {
		policyUrl, err := r.schema.IDToUrl(ctx, subscription.PolicyID)
		if err != nil {
			return err
		}
		subscription.Policy = policyUrl
	}

	return nil
}

// CreateSubscription creates new collection
func (r *repository) CreateSubscription(ctx context.Context, subscription core.Subscription) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.CreateSubscription")
	defer span.End()

	err := r.preProcess(ctx, &subscription)
	if err != nil {
		return subscription, err
	}

	err = r.db.WithContext(ctx).Create(&subscription).Error
	if err != nil {
		return subscription, err
	}

	err = r.postProcess(ctx, &subscription)
	if err != nil {
		return subscription, err
	}

	return subscription, nil
}

// GetSubscription returns a Subscription by ID
func (r *repository) GetSubscription(ctx context.Context, id string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.GetSubscription")
	defer span.End()

	id, err := r.normalizeDBID(id)
	if err != nil {
		return core.Subscription{}, err
	}

	var subscription core.Subscription
	err = r.db.WithContext(ctx).Preload("Items").First(&subscription, "id = ?", id).Error
	if err != nil {
		return subscription, err
	}

	err = r.postProcess(ctx, &subscription)
	if err != nil {
		return subscription, err
	}

	return subscription, err
}

// DeleteSubscription deletes a collection by ID
func (r *repository) DeleteSubscription(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.DeleteSubscription")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Subscription{}, "id = ?", id).Error
}

// GetOwnSubscriptions returns a list of collections by owner
func (r *repository) GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.GetOwnSubscriptions")
	defer span.End()

	var subscriptions []core.Subscription
	err := r.db.WithContext(ctx).Preload("Items").Find(&subscriptions, "author = ?", owner).Error

	for i := range subscriptions {
		err := r.postProcess(ctx, &subscriptions[i])
		if err != nil {
			return []core.Subscription{}, err
		}
	}

	return subscriptions, err
}

// CreateItem creates new collection item
func (r *repository) CreateItem(ctx context.Context, item core.SubscriptionItem) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.CreateItem")
	defer span.End()

	var err error
	item.Subscription, err = r.normalizeDBID(item.Subscription)
	if err != nil {
		return item, err
	}

	err = r.db.WithContext(ctx).Create(&item).Error
	return item, err
}

// GetItem returns a collection item by ID
func (r *repository) GetItem(ctx context.Context, id, subscription string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.GetItem")
	defer span.End()

	var err error
	subscription, err = r.normalizeDBID(subscription)
	if err != nil {
		return core.SubscriptionItem{}, err
	}

	var obj core.SubscriptionItem
	return obj, r.db.WithContext(ctx).First(&obj, "id = ? and subscription = ?", id, subscription).Error
}

// DeleteItem deletes a collection item by ID
func (r *repository) DeleteItem(ctx context.Context, id, subscription string) error {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.DeleteItem")
	defer span.End()

	var err error
	subscription, err = r.normalizeDBID(subscription)
	if err != nil {
		return err
	}

	// get deleted
	var deleted core.SubscriptionItem
	err = r.db.WithContext(ctx).First(&deleted, "id = ? and subscription = ?", id, subscription).Error
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Where("id = ? and subscription = ?", id, subscription).Delete(&core.SubscriptionItem{}).Error

	return err
}
