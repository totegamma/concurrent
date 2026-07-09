package worker

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/schemas"
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

// A reference record's own schema is always reference.json. A subscription
// that filters on the underlying content schema must still match, whether the
// target document is bundled in the reference's References or only its schema
// hint is carried in the reference value.
func TestEventMatchesSchemasResolvesReference(t *testing.T) {
	const replySchema = "https://schema.concrnt.world/m/reply.json"
	href := "ccfs://alice/post1"

	// reference wrapper carrying the target document under References
	bundled := concrnt.Event{
		Type: "created",
		URI:  "ccfs://bob/ref1",
		References: map[string]concrnt.SignedDocument{
			"ccfs://bob/ref1": {
				Document: `{"schema":"` + schemas.ReferenceURL + `","value":{"href":"` + href + `"}}`,
				References: map[string]concrnt.SignedDocument{
					href: {Document: `{"schema":"` + replySchema + `"}`},
				},
			},
		},
	}
	if !eventMatchesSchemas(bundled, []string{replySchema}) {
		t.Fatal("reference with bundled target document must match the content schema")
	}

	// reference wrapper carrying only the schema hint (no bundled target)
	hintOnly := concrnt.Event{
		Type: "created",
		URI:  "ccfs://bob/ref2",
		References: map[string]concrnt.SignedDocument{
			"ccfs://bob/ref2": {
				Document: `{"schema":"` + schemas.ReferenceURL + `","value":{"href":"` + href + `","schema":"` + replySchema + `"}}`,
			},
		},
	}
	if !eventMatchesSchemas(hintOnly, []string{replySchema}) {
		t.Fatal("reference with a schema hint must match the content schema")
	}

	// a filter that matches neither the wrapper nor the content schema
	if eventMatchesSchemas(bundled, []string{"https://schema.concrnt.world/m/like.json"}) {
		t.Fatal("reference must not match an unrelated content schema")
	}

	// reference.json itself remains matchable for clients subscribing to it
	if !eventMatchesSchemas(bundled, []string{schemas.ReferenceURL}) {
		t.Fatal("reference.json must still match when explicitly subscribed")
	}

	// an empty filter matches everything
	if !eventMatchesSchemas(bundled, nil) {
		t.Fatal("an empty schema filter must match every event")
	}
}

// The push payload must be the minimal notification struct (uri/schema/author/
// createdAt), not the whole Event, so it stays well under the 4096-byte
// WebPush/FCM limit. uri must be the association document, and schema/author
// come from that document.
func TestBuildNotificationPayloadIsMinimal(t *testing.T) {
	const likeSchema = "https://schema.concrnt.world/a/like.json"
	assoc := "ccfs://alice/like1"
	createdAt := time.Unix(1751760000, 0).UTC()

	event := concrnt.Event{
		Type:        "associated",
		Source:      "cckv://carol/home",
		URI:         "ccfs://carol/post1", // the target post
		Association: &assoc,
		Timestamp:   time.Unix(1751760005, 0).UTC(),
		References: map[string]concrnt.SignedDocument{
			assoc: {
				Document: `{"kind":"association","schema":"` + likeSchema + `","author":"con1alice","createdAt":"` + createdAt.Format(time.RFC3339) + `","associate":"ccfs://carol/post1"}`,
			},
		},
	}

	payload := buildNotificationPayload(event)

	if payload.URI != assoc {
		t.Fatalf("uri must be the association document, got %q", payload.URI)
	}
	if payload.Schema != likeSchema {
		t.Fatalf("schema must be the association's schema, got %q", payload.Schema)
	}
	if payload.Author != "con1alice" {
		t.Fatalf("author must come from the association document, got %q", payload.Author)
	}
	if !payload.CreatedAt.Equal(createdAt) {
		t.Fatalf("createdAt must be the document's, got %v", payload.CreatedAt)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("payload must marshal: %v", err)
	}
	if len(encoded) > 512 {
		t.Fatalf("minimal payload should be tiny, got %d bytes", len(encoded))
	}
}

// A malformed or non-association event must degrade gracefully to just the
// URI/timestamp rather than being dropped.
func TestBuildNotificationPayloadFallsBack(t *testing.T) {
	ts := time.Unix(1751760000, 0).UTC()
	event := concrnt.Event{Type: "created", URI: "ccfs://bob/post9", Timestamp: ts}

	payload := buildNotificationPayload(event)

	if payload.URI != "ccfs://bob/post9" {
		t.Fatalf("uri must fall back to event.URI, got %q", payload.URI)
	}
	if payload.Schema != "" || payload.Author != "" {
		t.Fatalf("schema/author must be empty without an association doc, got %q/%q", payload.Schema, payload.Author)
	}
	if !payload.CreatedAt.Equal(ts) {
		t.Fatalf("createdAt must fall back to event.Timestamp, got %v", payload.CreatedAt)
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
