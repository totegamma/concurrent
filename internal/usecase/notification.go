package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/concrnt/concrnt/internal/domain"
)

type NotificationRepository interface {
	Subscribe(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error)
	List(ctx context.Context) ([]domain.NotificationSubscription, error)
	Get(ctx context.Context, vendorID, owner string) (domain.NotificationSubscription, error)
	Delete(ctx context.Context, vendorID, owner string) error
}

// NotificationCounterStore holds the per-(vendor, owner) unread counter: a
// plain integer bumped on every delivered push and zeroed when the client
// opens its notification view. No read-position tracking by design, so a badge
// can never linger after the user has looked.
type NotificationCounterStore interface {
	Incr(ctx context.Context, key string) (int64, error)
	GetInt(ctx context.Context, key string) (int64, error)
	Delete(ctx context.Context, key string) error
}

// NotificationPusher delivers the out-of-band counter-reset push. nil when
// web push is not configured (no VAPID keys).
type NotificationPusher interface {
	SendCounterReset(ctx context.Context, sub domain.NotificationSubscription) error
}

type NotificationUsecase struct {
	repo    NotificationRepository
	counter NotificationCounterStore
	pusher  NotificationPusher
}

func NewNotificationUsecase(repo NotificationRepository, counter NotificationCounterStore, pusher NotificationPusher) *NotificationUsecase {
	return &NotificationUsecase{repo: repo, counter: counter, pusher: pusher}
}

func notificationCounterKey(vendorID, owner string) string {
	return "notification_counter:" + vendorID + ":" + owner
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

	if err := uc.repo.Delete(ctx, vendorID, owner); err != nil {
		return err
	}
	return uc.counter.Delete(ctx, notificationCounterKey(vendorID, owner))
}

func (uc *NotificationUsecase) IncrementCounter(ctx context.Context, vendorID, owner string) (int64, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.IncrementCounter")
	defer span.End()

	return uc.counter.Incr(ctx, notificationCounterKey(vendorID, owner))
}

func (uc *NotificationUsecase) GetCounter(ctx context.Context, vendorID, owner string) (int64, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.GetCounter")
	defer span.End()

	return uc.counter.GetInt(ctx, notificationCounterKey(vendorID, owner))
}

func (uc *NotificationUsecase) ResetCounter(ctx context.Context, vendorID, owner string) error {
	ctx, span := tracer.Start(ctx, "Usecase.Notification.ResetCounter")
	defer span.End()

	if err := uc.counter.Delete(ctx, notificationCounterKey(vendorID, owner)); err != nil {
		return err
	}

	// tell the device that got the pushes to drop its badge now, rather than
	// the next time the app is opened. best-effort and off the request path
	if uc.pusher == nil {
		return nil
	}
	sub, err := uc.repo.Get(ctx, vendorID, owner)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := uc.pusher.SendCounterReset(ctx, sub); err != nil {
			slog.Warn("failed to push counter reset",
				slog.String("vendorID", vendorID), slog.String("owner", owner), slog.String("error", err.Error()))
		}
	}()
	return nil
}
