package datastore

import (
	"context"
	"time"

	gcdatastore "cloud.google.com/go/datastore"

	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
)

type NotificationRepository struct {
	*store
}

func NewNotificationRepository(client *gcdatastore.Client, namespace string) usecase.NotificationRepository {
	return &NotificationRepository{store: newStore(client, namespace)}
}

func (r *NotificationRepository) Subscribe(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Notification.Subscribe")
	defer span.End()

	key := r.key(kindSubscription, compoundKey(subscription.VendorID, subscription.Owner))
	now := time.Now().UTC()
	model := subscriptionModel{
		VendorID:     subscription.VendorID,
		Owner:        subscription.Owner,
		Schemas:      append([]string(nil), subscription.Schemas...),
		Prefixes:     append([]string(nil), subscription.Prefixes...),
		Subscription: subscription.Subscription,
		CDate:        now,
		MDate:        now,
	}
	var existing subscriptionModel
	err := r.client.Get(ctx, key, &existing)
	if err == nil {
		model.CDate = existing.CDate
	} else if err != nil && !isNoSuchEntity(err) {
		span.RecordError(err)
		return domain.NotificationSubscription{}, err
	}

	if _, err := r.client.Put(ctx, key, &model); err != nil {
		span.RecordError(err)
		return domain.NotificationSubscription{}, err
	}
	return subscriptionModelToDomain(model), nil
}

func (r *NotificationRepository) List(ctx context.Context) ([]domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Notification.List")
	defer span.End()

	var models []subscriptionModel
	_, err := r.client.GetAll(ctx, r.query(kindSubscription), &models)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make([]domain.NotificationSubscription, 0, len(models))
	for _, model := range models {
		result = append(result, subscriptionModelToDomain(model))
	}
	return result, nil
}

func (r *NotificationRepository) Get(ctx context.Context, vendorID, owner string) (domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Notification.Get")
	defer span.End()

	var model subscriptionModel
	err := r.client.Get(ctx, r.key(kindSubscription, compoundKey(vendorID, owner)), &model)
	if err != nil {
		span.RecordError(err)
		if isNoSuchEntity(err) {
			return domain.NotificationSubscription{}, domain.NotFoundError{Resource: "notification subscription"}
		}
		return domain.NotificationSubscription{}, err
	}
	return subscriptionModelToDomain(model), nil
}

func (r *NotificationRepository) Delete(ctx context.Context, vendorID, owner string) error {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Notification.Delete")
	defer span.End()

	err := r.client.Delete(ctx, r.key(kindSubscription, compoundKey(vendorID, owner)))
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func subscriptionModelToDomain(model subscriptionModel) domain.NotificationSubscription {
	return domain.NotificationSubscription{
		VendorID:     model.VendorID,
		Owner:        model.Owner,
		Schemas:      append([]string(nil), model.Schemas...),
		Prefixes:     append([]string(nil), model.Prefixes...),
		Subscription: model.Subscription,
		CDate:        model.CDate,
		MDate:        model.MDate,
	}
}

var _ usecase.NotificationRepository = (*NotificationRepository)(nil)
