package worker

import (
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

// Two different associations to the same target (e.g. two users liking the
// same post) must produce distinct dedup keys, or the second push within the
// dedup TTL is silently dropped.
func TestNotificationDedupKeyDistinguishesAssociations(t *testing.T) {
	sub := domain.NotificationSubscription{VendorID: "vendor", Owner: "owner"}
	ts := time.Unix(1751760000, 0)

	assocA := "ccfs://alice/like1"
	assocB := "ccfs://bob/like2"
	base := concrnt.Event{
		Type:      "associated",
		Source:    "cckv://carol/home",
		URI:       "ccfs://carol/post1",
		Timestamp: ts,
	}

	eventA := base
	eventA.Association = &assocA
	eventB := base
	eventB.Association = &assocB

	if notificationDedupKey(eventA, sub) == notificationDedupKey(eventB, sub) {
		t.Fatal("different associations to the same target must not share a dedup key")
	}
	if notificationDedupKey(eventA, sub) != notificationDedupKey(eventA, sub) {
		t.Fatal("the same event must produce a stable dedup key")
	}
}

// Events that differ only in timestamp (two operations that are otherwise
// byte-identical, e.g. unassociate/re-associate cycles) must not collide.
func TestNotificationDedupKeyDistinguishesTimestamps(t *testing.T) {
	sub := domain.NotificationSubscription{VendorID: "vendor", Owner: "owner"}

	base := concrnt.Event{
		Type:      "unassociated",
		Source:    "cckv://carol/home",
		URI:       "ccfs://carol/post1",
		Timestamp: time.Unix(1751760000, 0),
	}
	later := base
	later.Timestamp = base.Timestamp.Add(time.Second)

	if notificationDedupKey(base, sub) == notificationDedupKey(later, sub) {
		t.Fatal("events at different times must not share a dedup key")
	}
}
