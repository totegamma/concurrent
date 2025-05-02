package notification

import (
	"context"

	"github.com/concrnt/concrnt/core"
)

type service struct {
	repo Repo
}

func NewService(repo Repo) core.NotificationService {
	return &service{
		repo,
	}
}

// Subscribe creates or updates a notification subscription.
func (s *service) Subscribe(ctx context.Context, subscription core.NotificationSubscription) (core.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Notification.Service.Subscribe")
	defer span.End()

	subscription, err := s.repo.Subscribe(ctx, subscription)
	if err != nil {
		return core.NotificationSubscription{}, err
	}

	return subscription, nil
}

// GetAllSubscriptions retrieves all notification subscriptions.
func (s *service) GetAllSubscriptions(ctx context.Context) ([]core.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Notification.Service.GetAllSubscriptions")
	defer span.End()

	subscriptions, err := s.repo.GetAllSubscriptions(ctx)
	if err != nil {
		return nil, err
	}

	return subscriptions, nil
}

// Delete removes a notification subscription by vendor ID and owner.
func (s *service) Delete(ctx context.Context, vendorID, owner string) error {
	ctx, span := tracer.Start(ctx, "Notification.Service.Delete")
	defer span.End()

	err := s.repo.Delete(ctx, vendorID, owner)
	if err != nil {
		return err
	}

	return nil
}

// Get retrieves a notification subscription by vendor ID and owner.
func (s *service) Get(ctx context.Context, vendorID, owner string) (core.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Notification.Service.Get")
	defer span.End()

	subscription, err := s.repo.Get(ctx, vendorID, owner)
	if err != nil {
		return core.NotificationSubscription{}, err
	}

	return subscription, nil
}
