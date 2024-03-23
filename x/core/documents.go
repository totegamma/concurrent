package core

import (
	"time"
)

// commons
type DocumentBase[T any] struct {
	ID       string    `json:"id"`
	Signer   string    `json:"signer"`
	Type     string    `json:"type"`
	Schema   string    `json:"schema,omitempty"`
	KeyID    string    `json:"keyID,omitempty"`
	Body     T         `json:"body"`
	Meta     any       `json:"meta,omitempty"`
	SignedAt time.Time `json:"signedAt"`
}

type DeleteBody struct {
	TargetID string `json:"targetID"`
}

// --------

// entity
type AffiliationBody struct {
	Domain string `json:"domain"`
}

type TombstoneBody struct {
}

type EntityAffiliation struct { // type: affiliation
	DocumentBase[AffiliationBody]
}

type EntityTombstone struct { // type: tombstone
	DocumentBase[TombstoneBody]
}

// ack
type AckPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AckDocument struct { // type: ack
	DocumentBase[AckPayload]
}

type UnackDocument struct { // type: unack
	DocumentBase[AckPayload]
}

// message
type CreateMessage[T any] struct { // type: c.message
	DocumentBase[T]
	Streams []string `json:"streams"`
}

type DeleteMessage struct { // type: d.message
	DocumentBase[DeleteBody]
}

// association
type CreateAssociation[T any] struct { // type: c.association
	DocumentBase[T]
	Streams []string `json:"streams"`
	Variant string   `json:"variant"`
	Target  string   `json:"target"`
}

type DeleteAssociation struct { // type: d.association
	DocumentBase[DeleteBody]
}

// profile
type CreateProfile[T any] struct { // type: c.profile
	DocumentBase[T]
}

type UpdateProfile[T any] struct { // type: u.profile
	DocumentBase[T]
}

type DeleteProfile struct { // type: d.profile
	DocumentBase[DeleteBody]
}

// key
type EnactBody struct {
	CKID   string `json:"ckid"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type RevokeBody struct {
	CKID string `json:"ckid"`
}

type EnactKey struct { // type: c.key
	DocumentBase[EnactBody]
}

type RevokeKey struct { // type: d.key
	DocumentBase[RevokeBody]
}

// timeline
type CreateTimeline[T any] struct { // type: c.timeline
	DocumentBase[T]
}

type UpdateTimeline[T any] struct { // type: u.timeline
	DocumentBase[T]
}

type DeleteTimeline struct { // type: d.stream
	DocumentBase[DeleteBody]
}

type DeleteTimelineItemBody struct {
	TimelineID string `json:"timelineID"`
	ItemID     string `json:"itemID"`
}

type DeleteTimelineItem struct { // type: d.stream.item
	DocumentBase[DeleteTimelineItemBody]
}
