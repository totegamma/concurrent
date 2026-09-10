package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

const (
	// KindNotification is a regular notification push (worker.NotificationReactor).
	KindNotification = "notification"
	// KindCounterReset is a badge counter reset push (WebPush.SendCounterReset).
	KindCounterReset = "counter_reset"
)

// sends is the outcome of every push handed to a push service, by kind.
// `result` is "ok", "error" (no response: network, encryption, ...), or the
// HTTP status code the push service answered with — a rising "410" means
// subscriptions are dying and should be pruned.
var sends = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "concrnt",
	Subsystem: "webpush",
	Name:      "sends_total",
	Help:      "Web pushes handed to push services by kind and result (ok, error, or the HTTP status code).",
}, []string{"kind", "result"})

// RecordSend accounts for one push attempt. statusCode is ignored when err
// is non-nil.
func RecordSend(kind string, statusCode int, err error) {
	result := "ok"
	switch {
	case err != nil:
		result = "error"
	case statusCode != http.StatusCreated:
		result = strconv.Itoa(statusCode)
	}
	sends.WithLabelValues(kind, result).Inc()
}

// WebPush sends out-of-band pushes (currently only counter resets) to a single
// subscription from the request path. Regular notifications are delivered by
// worker.NotificationReactor on the leader; this runs on whichever replica
// handled the request, so no dedup is involved.
type WebPush struct {
	opts webpush.Options
}

func NewWebPush(opts webpush.Options) *WebPush {
	return &WebPush{opts: opts}
}

// SendCounterReset pushes a counter-reset to the device so it drops its app
// icon badge without waiting for the user to open the app. The Topic header
// doubles as a plaintext hint for webpush-relay (badge-only APNs) and as the
// RFC 8030 collapse key, so back-to-back resets coalesce.
func (w *WebPush) SendCounterReset(ctx context.Context, sub domain.NotificationSubscription) error {
	var subscription webpush.Subscription
	if err := json.Unmarshal([]byte(sub.Subscription), &subscription); err != nil {
		return fmt.Errorf("decode webpush subscription: %w", err)
	}

	payload, err := json.Marshal(concrnt.NotificationPayload{Type: concrnt.NotificationTypeCounterReset, Badge: 0})
	if err != nil {
		return err
	}

	opts := w.opts
	opts.Topic = concrnt.NotificationTopicCounterReset
	opts.Urgency = webpush.UrgencyLow

	resp, err := webpush.SendNotificationWithContext(ctx, payload, &subscription, &opts)
	if err != nil {
		RecordSend(KindCounterReset, 0, err)
		return err
	}
	defer resp.Body.Close()
	RecordSend(KindCounterReset, resp.StatusCode, nil)
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push endpoint returned %s: %s", resp.Status, string(body))
	}
	return nil
}
