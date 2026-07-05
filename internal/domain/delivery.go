package domain

import (
	"time"

	"github.com/concrnt/concrnt"
)

// DeliveryLocalKind describes what to do when a delivery job's destination
// resolves to this server itself.
type DeliveryLocalKind string

// DeliveryRemoteKind describes what to do when a delivery job's destination
// resolves to another domain.
type DeliveryRemoteKind string

const (
	DeliveryLocalNone    DeliveryLocalKind = "none"    // nothing to do locally
	DeliveryLocalPublish DeliveryLocalKind = "publish" // publish Event on Dest/ResolveURI
	DeliveryLocalCommit  DeliveryLocalKind = "commit"  // re-enter Commit(ip, Payload, CommitModeExecute)

	DeliveryRemoteNone   DeliveryRemoteKind = "none"   // nothing to do remotely
	DeliveryRemoteCommit DeliveryRemoteKind = "commit" // POST Payload to the resolved host
)

// DeliveryJob describes a single unit of federation delivery work: resolve a
// destination (unless already known), then act locally and/or remotely.
//
// Exactly one of ResolveURI / Host should be set: ResolveURI is a CCURI that
// the worker must resolve to a host via Client.ResolveResourceHost; Host is
// an already-known destination domain that requires no resolution (e.g. an
// entity's registered Domain).
type DeliveryJob struct {
	ID string `json:"id"`

	ResolveURI string `json:"resolveUri,omitempty"`
	Host       string `json:"host,omitempty"`

	Payload concrnt.SignedDocument `json:"payload,omitempty"`

	Local  DeliveryLocalKind  `json:"local"`
	Remote DeliveryRemoteKind `json:"remote"`

	// Event is used when Local == DeliveryLocalPublish; the publish channel
	// is ResolveURI.
	Event *concrnt.Event `json:"event,omitempty"`

	// IP is used when Local == DeliveryLocalCommit, to re-enter Commit.
	IP string `json:"ip,omitempty"`

	Attempt   int       `json:"attempt"`
	CreatedAt time.Time `json:"createdAt"`
	LastError string    `json:"lastError,omitempty"`
}
