package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/service"
	"github.com/totegamma/concrnt-playground/internal/usecase"
)

type NotificationReactor struct {
	notification *usecase.NotificationUsecase
	signal       *service.SignalService
	opts         webpush.Options
}

func NewNotificationReactor(
	notification *usecase.NotificationUsecase,
	signal *service.SignalService,
	opts webpush.Options,
) *NotificationReactor {
	return &NotificationReactor{
		notification: notification,
		signal:       signal,
		opts:         opts,
	}
}

type notificationWorker struct {
	mdate  time.Time
	cancel context.CancelFunc
}

func (r *NotificationReactor) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *NotificationReactor) run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	workers := make(map[string]notificationWorker)

	for {
		select {
		case <-ctx.Done():
			for _, worker := range workers {
				worker.cancel()
			}
			return
		case <-ticker.C:
			r.syncWorkers(ctx, workers)
		}
	}
}

func (r *NotificationReactor) syncWorkers(ctx context.Context, workers map[string]notificationWorker) {
	subscriptions, err := r.notification.List(ctx)
	if err != nil {
		slog.Error("failed to list notification subscriptions", slog.String("error", err.Error()))
		return
	}

	valid := make([]string, 0, len(subscriptions))
	for _, sub := range subscriptions {
		id := sub.VendorID + sub.Owner
		valid = append(valid, id)

		if worker, ok := workers[id]; ok {
			if worker.mdate.Equal(sub.MDate) {
				continue
			}
			worker.cancel()
			delete(workers, id)
		}

		workerCtx, cancel := context.WithCancel(ctx)
		workers[id] = notificationWorker{mdate: sub.MDate, cancel: cancel}
		go r.runWorker(workerCtx, sub)
	}

	for id, worker := range workers {
		if slices.Contains(valid, id) {
			continue
		}
		worker.cancel()
		delete(workers, id)
	}
}

func (r *NotificationReactor) runWorker(ctx context.Context, sub domain.NotificationSubscription) {
	slog.Info("notification worker started", slog.String("vendorID", sub.VendorID), slog.String("owner", sub.Owner))

	var subscription webpush.Subscription
	if err := json.Unmarshal([]byte(sub.Subscription), &subscription); err != nil {
		slog.Error("failed to decode webpush subscription", slog.String("error", err.Error()))
		return
	}

	request := make(chan []string)
	realtime := make(chan concrnt.Event)

	go r.signal.Realtime(ctx, request, realtime)
	select {
	case request <- sub.Prefixes:
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-realtime:
			if !eventMatchesSchemas(event, sub.Schemas) {
				continue
			}

			payload, err := json.Marshal(event)
			if err != nil {
				slog.Error("failed to encode notification payload", slog.String("error", err.Error()))
				continue
			}

			resp, err := webpush.SendNotification(payload, &subscription, &r.opts)
			if err != nil {
				slog.Error("failed to send notification", slog.String("error", err.Error()))
				continue
			}

			if resp.StatusCode != httpStatusCreated {
				body, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					slog.Error("failed to read notification response body", slog.String("error", readErr.Error()))
				}
				slog.Error(
					"notification failed",
					slog.String("vendorID", sub.VendorID),
					slog.String("owner", sub.Owner),
					slog.String("status", resp.Status),
					slog.String("body", string(body)),
				)
			}
			resp.Body.Close()
		}
	}
}

const httpStatusCreated = 201

func eventMatchesSchemas(event concrnt.Event, schemas []string) bool {
	if len(schemas) == 0 {
		return true
	}

	for _, sd := range event.References {
		var doc concrnt.Document[any]
		if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
			continue
		}
		if slices.Contains(schemas, doc.Schema) {
			return true
		}
	}

	return false
}
