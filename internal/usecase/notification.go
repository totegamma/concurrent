package usecase

import (
	"context"

	"github.com/totegamma/concrnt-playground/internal/domain"
)

type NotificationRepository interface {
	Subscribe(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	List(ctx context.Context) ([]domain.NotificationSubscription, error)
	Get(ctx context.Context, vendorID, owner string) (domain.NotificationSubscription, error)
	Delete(ctx context.Context, vendorID, owner string) error
}

type NotificationUsecase struct {
	repo NotificationRepository
}

func NewNotificationUsecase(repo NotificationRepository) *NotificationUsecase {
	return &NotificationUsecase{repo: repo}
}

func (uc *NotificationUsecase) Subscribe(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.Subscribe")
	defer span.End()

	return uc.repo.Subscribe(ctx, subscription)
}

func (uc *NotificationUsecase) List(ctx context.Context) ([]domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.List")
	defer span.End()

	return uc.repo.List(ctx)
}

func (uc *NotificationUsecase) Get(ctx context.Context, vendorID, owner string) (domain.NotificationSubscription, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.Get")
	defer span.End()

	return uc.repo.Get(ctx, vendorID, owner)
}

func (uc *NotificationUsecase) Delete(ctx context.Context, vendorID, owner string) error {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.Delete")
	defer span.End()

	return uc.repo.Delete(ctx, vendorID, owner)
}
