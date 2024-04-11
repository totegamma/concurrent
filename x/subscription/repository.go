package subscription

import (
	"context"
	"errors"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/schema"
	"gorm.io/gorm"
)

// Repository is the interface for collection repository
type Repository interface {
	CreateSubscription(ctx context.Context, subscription core.Subscription) (core.Subscription, error)
	GetSubscription(ctx context.Context, id string) (core.Subscription, error)
	UpdateSubscription(ctx context.Context, subscription core.Subscription) (core.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	GetOwnSubscriptions(ctx context.Context, owner string) ([]core.Subscription, error)

	CreateItem(ctx context.Context, item core.SubscriptionItem) (core.SubscriptionItem, error)
	GetItem(ctx context.Context, id string, subscription string) (core.SubscriptionItem, error)
	DeleteItem(ctx context.Context, id string, subscription string) error
}

type repository struct {
	db     *gorm.DB
	schema schema.Service
}

// NewRepository creates a new collection repository
func NewRepository(db *gorm.DB, schema schema.Service) Repository {
	return &repository{db, schema}
}

// CreateSubscription creates new collection
func (r *repository) CreateSubscription(ctx context.Context, subscription core.Subscription) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.CreateSubscription")
	defer span.End()

	if subscription.ID == "" {
		return subscription, errors.New("ID is required")
	}

	if len(subscription.ID) == 27 {
		if subscription.ID[0] != 's' {
			return subscription, errors.New("subscription id must start with 's'")
		}
		subscription.ID = subscription.ID[1:]
	}

	if len(subscription.ID) != 26 {
		return subscription, errors.New("subscription id must be 26 characters")
	}

	schemaID, err := r.schema.UrlToID(ctx, subscription.Schema)
	if err != nil {
		return subscription, err
	}
	subscription.SchemaID = schemaID

	err = r.db.WithContext(ctx).Create(&subscription).Error
	if err != nil {
		return subscription, err
	}

	subscription.ID = "s" + subscription.ID

	return subscription, nil
}

// GetSubscription returns a Subscription by ID
func (r *repository) GetSubscription(ctx context.Context, id string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.GetSubscription")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 's' {
			return core.Subscription{}, errors.New("subscription typed-id must start with 's'")
		}
		id = id[1:]
	}

	if len(id) != 26 {
		return core.Subscription{}, errors.New("subscription typed-id must be 26 characters long")
	}

	var subscription core.Subscription
	err := r.db.WithContext(ctx).Preload("Items").First(&subscription, "id = ?", id).Error

	schemaUrl, err := r.schema.IDToUrl(ctx, subscription.SchemaID)
	if err != nil {
		return subscription, err
	}
	subscription.Schema = schemaUrl

	subscription.ID = "s" + subscription.ID

	return subscription, err
}

// UpdateSubscription updates a collection
func (r *repository) UpdateSubscription(ctx context.Context, obj core.Subscription) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.UpdateSubscription")
	defer span.End()

	return obj, r.db.WithContext(ctx).Save(&obj).Error
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
		schemaUrl, err := r.schema.IDToUrl(ctx, subscriptions[i].SchemaID)
		if err != nil {
			return subscriptions, err
		}
		subscriptions[i].Schema = schemaUrl

		subscriptions[i].ID = "s" + subscriptions[i].ID
	}

	return subscriptions, err
}

// CreateItem creates new collection item
func (r *repository) CreateItem(ctx context.Context, item core.SubscriptionItem) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.CreateItem")
	defer span.End()

    if len(item.Subscription) == 27 {
        if item.Subscription[0] != 's' {
            return item, errors.New("subscription typed-id must start with 's'")
        }
        item.Subscription = item.Subscription[1:]
    }

	err := r.db.WithContext(ctx).Create(&item)
	return item, err.Error
}

// GetItem returns a collection item by ID
func (r *repository) GetItem(ctx context.Context, id, subscription string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.GetItem")
	defer span.End()

    if len(subscription) == 27 {
        if subscription[0] != 's' {
            return core.SubscriptionItem{}, errors.New("subscription typed-id must start with 's'")
        }
        subscription = subscription[1:]
    }

	var obj core.SubscriptionItem
	return obj, r.db.WithContext(ctx).First(&obj, "id = ? and subscription = ?", id, subscription).Error
}

// DeleteItem deletes a collection item by ID
func (r *repository) DeleteItem(ctx context.Context, id, subscription string) error {
	ctx, span := tracer.Start(ctx, "Subscription.Repository.DeleteItem")
	defer span.End()

    if len(subscription) == 27 {
        if subscription[0] != 's' {
            return errors.New("subscription typed-id must start with 's'")
        }
        subscription = subscription[1:]
    }

	// get deleted
	var deleted core.SubscriptionItem
	err := r.db.WithContext(ctx).First(&deleted, "id = ? and subscription = ?", id, subscription).Error
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Where("id = ? and subscription = ?", id, subscription).Delete(&core.SubscriptionItem{}).Error

	return err
}
