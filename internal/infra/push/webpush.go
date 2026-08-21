package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push endpoint returned %s: %s", resp.Status, string(body))
	}
	return nil
}
