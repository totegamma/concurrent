package firestore

import (
	"context"
	"time"

	gcfirestore "cloud.google.com/go/firestore"

	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
)

type NotificationRepository struct {
	*store
}

func NewNotificationRepository(client *gcfirestore.Client, namespace string) usecase.NotificationRepository {
	return &NotificationRepository{store: newStore(client, namespace)}
}

func (r *NotificationRepository) Subscribe(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Notification.Subscribe")
	defer span.End()

	ref := r.doc(kindSubscription, compoundKey(subscription.VendorID, subscription.Owner))
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
	err := r.get(ctx, ref, &existing)
	if err == nil {
		model.CDate = existing.CDate
	} else if err != nil && !isNoSuchEntity(err) {
		span.RecordError(err)
		return domain.NotificationSubscription{}, err
	}

	if _, err := ref.Set(ctx, &model); err != nil {
		span.RecordError(err)
		return domain.NotificationSubscription{}, err
	}
	return subscriptionModelToDomain(model), nil
}

func (r *NotificationRepository) List(ctx context.Context) ([]domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Notification.List")
	defer span.End()

	models, _, err := getAll[subscriptionModel](ctx, r.query(kindSubscription))
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
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Notification.Get")
	defer span.End()

	var model subscriptionModel
	err := r.get(ctx, r.doc(kindSubscription, compoundKey(vendorID, owner)), &model)
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
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Notification.Delete")
	defer span.End()

	_, err := r.doc(kindSubscription, compoundKey(vendorID, owner)).Delete(ctx)
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
