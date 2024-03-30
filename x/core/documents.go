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
	Body     T         `json:"body,omitempty"`
	Meta     any       `json:"meta,omitempty"`
	SignedAt time.Time `json:"signedAt"`
}

// entity
type EntityAffiliation struct { // type: affiliation
	Domain string `json:"domain"`
	DocumentBase[any]
}

type EntityTombstone struct { // type: tombstone
	Reason string `json:"reason"`
	DocumentBase[any]
}

type ExtensionDocument[T any] struct { // type: extension
	DocumentBase[T]
}

// ack
type AckDocument struct { // type: ack
	DocumentBase[any]
	From string `json:"from"`
	To   string `json:"to"`
}

type UnackDocument struct { // type: unack
	DocumentBase[any]
	From string `json:"from"`
	To   string `json:"to"`
}

// message
type CreateMessage[T any] struct { // type: message
	DocumentBase[T]
	Timelines []string `json:"timelines"`
}

type DeleteDocument struct { // type: delete
	DocumentBase[any]
	Target string `json:"target"`
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
type EnactKey struct { // type: c.key
	DocumentBase[any]
	Target string `json:"target"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type RevokeKey struct { // type: d.key
	DocumentBase[any]
	Target string `json:"target"`
}

// timeline
type TimelineDocument[T any] struct { // type: timeline
	DocumentBase[T]
	Indexable   bool `json:"indexable"`
	DomainOwned bool `json:"domainOwned"`
}
