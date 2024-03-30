package core

import (
	"time"
)

// commons
type DocumentBase[T any] struct {
	ID       string    `json:"id,omitempty"`
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

type ExtensionDocument[T any] struct { // type: extension
	DocumentBase[T]
}

// ack
type AckBody struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AckDocument struct { // type: ack
	DocumentBase[AckBody]
}

type UnackDocument struct { // type: unack
	DocumentBase[AckBody]
}

// message
type CreateMessage[T any] struct { // type: message
	DocumentBase[T]
	Timelines []string `json:"streams"`
}

type DeleteDocument struct { // type: delete
	DocumentBase[DeleteBody]
}

// association
type CreateAssociation[T any] struct { // type: association
	DocumentBase[T]
	Timelines []string `json:"timelines"`
	Variant   string   `json:"variant"`
	Target    string   `json:"target"`
}

// profile
type UpsertProfile[T any] struct { // type: profile
	DocumentBase[T]
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
type TimelineDocument[T any] struct { // type: timeline
	DocumentBase[T]
	Indexable   bool `json:"indexable"`
	DomainOwned bool `json:"domainOwned"`
}
